#!/usr/bin/env bash
# Verifies that the Go version in the Dockerfile is not older than the version
# go.mod asks for.
#
# A newer toolchain builds an older `go` directive, so the builder image running
# ahead of go.mod is fine. The reverse is not: if go.mod asks for a newer Go than
# the image ships, `go build` downloads a toolchain over the network mid-build,
# and fails outright when offline or under GOTOOLCHAIN=local.
#
# Scope: this reads one file, ./Dockerfile, whose only golang references are
# three plain `FROM golang:X.Y.Z` lines. Registry prefixes, `@sha256:` digests
# and `-alpine` variants are not handled; such a tag fails the version check
# below with an actionable message. $GOTOOLCHAIN is ignored so that the verdict
# depends only on the committed files and matches CI on every machine.

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DOCKERFILE="${REPO_ROOT}/Dockerfile"
GO_MOD="${REPO_ROOT}/go.mod"

# Pad to three components so `1.26` and `1.26.0` compare as equal under sort -V.
normalise() {
	local v="$1"
	while [[ "${v//[^.]/}" != ".." ]]; do v="${v}.0"; done
	printf '%s' "${v}"
}

# Every `FROM golang:<version>` stage must agree, otherwise "the" image version
# is ambiguous and the stages would build against different Gos.
mapfile -t image_versions < <(sed -nE 's/^[[:space:]]*FROM[[:space:]].*golang:([^[:space:]]+).*/\1/p' "${DOCKERFILE}" | sort -u)

if [[ "${#image_versions[@]}" -eq 0 ]]; then
	echo "ERROR: found no 'FROM golang:<version>' lines in ${DOCKERFILE}." >&2
	exit 1
fi

for version in "${image_versions[@]}"; do
	if [[ ! "${version}" =~ ^[0-9]+(\.[0-9]+)*$ ]]; then
		echo "ERROR: Dockerfile uses an unpinned golang tag: golang:${version}" >&2
		echo "       Pin every 'FROM golang:' stage to an explicit version." >&2
		exit 1
	fi
done

if [[ "${#image_versions[@]}" -gt 1 ]]; then
	echo "ERROR: Dockerfile pins more than one golang version: ${image_versions[*]}" >&2
	echo "       All 'FROM golang:<version>' stages must use the same version." >&2
	exit 1
fi

image_version="${image_versions[0]}"

# `go 1.26.0` -> 1.26.0. Required.
go_directive="$(sed -nE 's/^go[[:space:]]+([0-9]+(\.[0-9]+)*).*/\1/p' "${GO_MOD}" | head -n1)"
if [[ -z "${go_directive}" ]]; then
	echo "ERROR: could not find a 'go' directive in ${GO_MOD}." >&2
	exit 1
fi

# `toolchain go1.26.5` -> 1.26.5. Optional, and when present it raises the bar.
toolchain="$(sed -nE 's/^toolchain[[:space:]]+go([0-9]+(\.[0-9]+)*).*/\1/p' "${GO_MOD}" | head -n1)"

required="${go_directive}"
required_from="go directive"
if [[ -n "${toolchain}" ]]; then
	highest="$(printf '%s\n%s\n' "$(normalise "${go_directive}")" "$(normalise "${toolchain}")" | sort -V | tail -n1)"
	if [[ "${highest}" != "$(normalise "${go_directive}")" ]]; then
		required="${toolchain}"
		required_from="toolchain directive"
	fi
fi

# Ordering holds when the lower of {required, image} is `required`.
lowest="$(printf '%s\n%s\n' "$(normalise "${required}")" "$(normalise "${image_version}")" | sort -V | head -n1)"

if [[ "${lowest}" != "$(normalise "${required}")" ]]; then
	cat >&2 <<-EOF
		ERROR: the Dockerfile's Go version is older than go.mod requires.

		  Dockerfile  FROM golang:${image_version}
		  go.mod      ${required} (${required_from})

		Building this image would download a Go toolchain over the network, and
		would fail offline or under GOTOOLCHAIN=local.

		Fix by bumping the 'FROM golang:' stages in Dockerfile to at least
		${required}, or by lowering the go.mod ${required_from}.
	EOF
	exit 1
fi

echo "Dockerfile Go version (${image_version}) satisfies go.mod (${required} from ${required_from})."
