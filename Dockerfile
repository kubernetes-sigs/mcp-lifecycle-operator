# Generated from kubebuilder template:
# https://github.com/kubernetes-sigs/kubebuilder/blob/v4.11.1/pkg/plugins/golang/v4/scaffolds/internal/templates/dockerfile.go

# Build the manager binary
FROM --platform=${BUILDPLATFORM} golang:1.27.4 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the Go source (relies on .dockerignore to filter)
COPY . .

# Build
# the GOARCH has no default value to allow the binary to be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager ./cmd

# Build the manager binary with debug symbols
FROM golang:1.27.4 AS debug-builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -gcflags="all=-N -l" -o manager ./cmd

# Debug image with Delve
FROM golang:1.27.4 AS debug
RUN go install github.com/go-delve/delve/cmd/dlv@v1.26.3
WORKDIR /
COPY --from=debug-builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["dlv", "exec", "/manager", "--headless", "--listen=:40000", "--api-version=2", "--accept-multiclient", "--"]

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot AS production
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]

# Build a coverage-instrumented manager binary for E2E code coverage (see #177).
# `go build -cover` instruments the main module's packages; at runtime the binary
# writes coverage data to $GOCOVERDIR. This image is for E2E runs only and is
# never shipped.
FROM --platform=${BUILDPLATFORM} golang:1.27.4 AS coverage-builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY . .

# The e2ecoverage build tag compiles cmd/coverage.go (the real coverage-flushing
# logic) in place of the production no-op stub, so the instrumented binary
# actually flushes counters to $GOCOVERDIR. See #177.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -cover -covermode=atomic -tags e2ecoverage -o manager ./cmd

# Coverage runtime image. Unlike the distroless production image, this uses a
# busybox base so that `kubectl cp` (which shells out to `tar` inside the
# container) can extract the collected coverage data from the running pod.
FROM busybox:1.38.0 AS coverage
WORKDIR /
COPY --from=coverage-builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]

# Code-mode data-plane images. These are built from the same module as the
# manager but ship as separate binaries/images so the dispatcher and runner
# stay off the operator's reconcile hot path (design invariant, FR-009).

# Build the code-mode dispatcher (execute_code MCP server).
FROM --platform=${BUILDPLATFORM} golang:1.27.1 AS dispatcher-builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o execute-code ./cmd/execute-code

FROM gcr.io/distroless/static:nonroot AS dispatcher
WORKDIR /
COPY --from=dispatcher-builder /workspace/execute-code .
USER 65532:65532

ENTRYPOINT ["/execute-code"]

# Build the portable code-mode runner. The same image runs unchanged under any
# SandboxProvider (portability contract).
FROM --platform=${BUILDPLATFORM} golang:1.27.1 AS runner-builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY . .

# Output to /out/runner, not /workspace/runner: the latter is the source package
# directory, and `go build -o` into an existing directory fails (or nests the
# binary). /out keeps the binary path unambiguous for the COPY stages below.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o /out/runner ./runner

FROM gcr.io/distroless/static:nonroot AS runner
WORKDIR /
COPY --from=runner-builder /out/runner .
USER 65532:65532

ENTRYPOINT ["/runner"]

# Build the agent-sandbox sandboxd runtime daemon. The agentsandbox provider
# drives executions through sandboxd (not kubectl exec), so the sandbox image must
# run it. sandboxd lives in the sigs.k8s.io/agent-sandbox module we already depend
# on; `go install <pkg>@<version>` resolves that module's own build graph in
# isolation (it does NOT touch this repo's go.mod/go.sum, which omit sandboxd's
# server-side deps). GOTOOLCHAIN=local reuses this 1.27.1 toolchain instead of
# fetching agent-sandbox's pinned 1.26.x.
FROM --platform=${BUILDPLATFORM} golang:1.27.1 AS sandboxd-builder
ARG TARGETOS
ARG TARGETARCH
ARG AGENT_SANDBOX_VERSION=v1.0.4

ENV GOTOOLCHAIN=local
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} GOBIN=/out \
    go install sigs.k8s.io/agent-sandbox/packages/sandboxd/cmd/sandboxd@${AGENT_SANDBOX_VERSION}

# The mcplo-agentsandbox sandbox image: sandboxd (the runtime server the SDK
# talks to) + our portable runner binary + a shell. It is NOT distroless because
# sandboxd execs user commands (the runner) via /bin/sh and needs coreutils;
# procps supplies ps/kill for sandboxd's process management. This image is the
# value the operator passes to --runner-image (baked into the SandboxTemplate).
FROM debian:bookworm-slim AS agentsandbox

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates procps \
    && rm -rf /var/lib/apt/lists/*

COPY --from=sandboxd-builder /out/sandboxd /usr/local/bin/sandboxd
COPY --from=runner-builder /out/runner /runner

# /codemode is sandboxd's --root-dir (set in the SandboxTemplate) and the
# dispatcher's writable input dir. It is a mounted emptyDir at runtime; create it
# here owned by the non-root runtime user so writes succeed before the mount and
# the directory exists for local runs.
RUN mkdir -p /codemode && chown 1000:1000 /codemode
WORKDIR /codemode
USER 1000

# sandboxd REST (:8080) and gRPC (:9090) default ports the SDK connects to.
EXPOSE 8080 9090

ENTRYPOINT ["/usr/local/bin/sandboxd"]
