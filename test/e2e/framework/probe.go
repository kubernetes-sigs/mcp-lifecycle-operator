/*
Copyright 2026 The Kubernetes Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package framework

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	uexec "k8s.io/client-go/util/exec"
	"k8s.io/streaming/pkg/httpstream"

	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

// ExecProbeInPod runs a command inside a target pod's network namespace using an
// ephemeral debug container, and reports the command's stdout, stderr, and exit
// code.
//
// The operator-managed mcp-server image is a minimal binary with no shell, so a
// probe cannot be run via "kubectl exec" of the main container. Instead this
// helper injects an ephemeral container (BusyboxImage) into the pod - which
// shares the pod's network namespace - waits for it to start, and execs the
// caller-supplied command in it. This lets a test assert egress reachability
// from the mcp-server pod itself. A plain TCP reachability check is the cleanest
// probe against a real listening Service, for example:
//
//	_, _, exitCode, err := ExecProbeInPod(ctx, t, cfg, ns, pod,
//		[]string{"nc", "-z", "-w3", host, "8080"})
//	if err != nil {
//		t.Fatalf("probe harness/transport failure: %v", err)
//	}
//	// exitCode == 0 => connected (allowed); non-zero => blocked (timeout).
//
// The returned error is reserved for harness/transport failures only (for
// example a stream that never upgrades): when it is non-nil the exit code is
// meaningless and the caller must treat it as a test-infrastructure failure, not
// a policy result. Setup problems (fetching the pod, injecting the ephemeral
// container, waiting for it to start, building the executor) hard-fail the test
// via t.Fatalf, following the framework convention.
//
// When the error is nil the exit code carries the command result: 0 means the
// command succeeded (for "nc -z" the destination accepted the connection), and
// non-zero means it failed (for "nc -z -w3" a timeout, which is how an egress
// NetworkPolicy drop surfaces). This separation lets a caller distinguish a
// policy drop (err == nil, exitCode != 0) from a broken probe (err != nil)
// instead of collapsing both into a single error.
//
// Note that a non-zero exit code alone does not prove a NetworkPolicy dropped
// the traffic: a connection refused (no listener) or DNS failure also exits
// non-zero. An egress-enforcement test should therefore probe a real listening
// Service with a short connect timeout, so that "blocked" surfaces as a timeout
// rather than a refusal, and rely on the controller's automatic DNS egress rule
// (added whenever egressTo is set) so name resolution is never the cause of
// failure.
func ExecProbeInPod(ctx context.Context, t *testing.T, cfg *envconf.Config,
	namespace, podName string, command []string) (string, string, int, error) {
	t.Helper()

	cs := Clientset(t, cfg)
	restCfg := cfg.Client().Resources().GetConfig()

	ecName := envconf.RandomName("probe", 12)

	pod, err := cs.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get pod %s/%s: %v", namespace, podName, err)
	}

	pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, corev1.EphemeralContainer{
		EphemeralContainerCommon: corev1.EphemeralContainerCommon{
			Name:    ecName,
			Image:   BusyboxImage,
			Command: []string{"sleep", "3600"},
		},
	})
	if _, err := cs.CoreV1().Pods(namespace).UpdateEphemeralContainers(
		ctx, podName, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to add ephemeral container to pod %s/%s: %v", namespace, podName, err)
	}

	// Wait for the ephemeral container to start before exec-ing into it.
	podObj := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: namespace}}
	if err := wait.For(
		conditions.New(cfg.Client().Resources(namespace)).ResourceMatch(podObj, func(obj k8s.Object) bool {
			p := obj.(*corev1.Pod)
			for _, s := range p.Status.EphemeralContainerStatuses {
				if s.Name == ecName && s.State.Running != nil {
					return true
				}
			}
			return false
		}),
		wait.WithTimeout(2*time.Minute),
		wait.WithInterval(2*time.Second),
		wait.WithContext(ctx),
	); err != nil {
		t.Fatalf("ephemeral container %s in pod %s/%s never started: %v", ecName, namespace, podName, err)
	}

	req := cs.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: ecName,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	// Prefer WebSocket and fall back to SPDY so the probe does not false-report
	// "blocked" on a cluster that only speaks one of the two exec transports.
	spdyExec, err := remotecommand.NewSPDYExecutor(restCfg, "POST", req.URL())
	if err != nil {
		t.Fatalf("failed to create SPDY executor for pod %s/%s: %v", namespace, podName, err)
	}
	wsExec, err := remotecommand.NewWebSocketExecutor(restCfg, "GET", req.URL().String())
	if err != nil {
		t.Fatalf("failed to create WebSocket executor for pod %s/%s: %v", namespace, podName, err)
	}
	executor, err := remotecommand.NewFallbackExecutor(wsExec, spdyExec, httpstream.IsUpgradeFailure)
	if err != nil {
		t.Fatalf("failed to create fallback executor for pod %s/%s: %v", namespace, podName, err)
	}

	var outBuf, errBuf bytes.Buffer
	streamErr := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &outBuf,
		Stderr: &errBuf,
	})
	if streamErr == nil {
		// Command ran and exited 0.
		return outBuf.String(), errBuf.String(), 0, nil
	}

	// A non-zero command exit surfaces as a CodeExitError. That is a legitimate
	// probe result (for "nc -z -w3" a non-zero code means the connection was not
	// established, i.e. the traffic was dropped), so return it via exitCode with
	// a nil error rather than conflating it with a harness failure.
	if codeErr, ok := errors.AsType[uexec.CodeExitError](streamErr); ok {
		return outBuf.String(), errBuf.String(), codeErr.ExitStatus(), nil
	}

	// Anything else is a harness/transport failure: the exit code is undefined,
	// so signal it through the error and leave interpretation to the caller.
	return outBuf.String(), errBuf.String(), -1, streamErr
}
