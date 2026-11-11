//go:build e2e

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

package e2e

import (
	"context"
	"net"
	"sort"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

// TestNetworkPolicyEgressAPIServerEnforcement drives a managed
// kubernetes-mcp-server with a real MCP client and a tool call that requires the
// Kubernetes API server (namespaces_list), to prove - functionally, not just
// structurally - that the operator's egress NetworkPolicy admits the server's
// access to the API server.
//
// # What is proven (the ALLOWED path)
//
// The server is configured with an egressTo allow-list built at runtime from the
// real API server addresses (the kubernetes Service ClusterIP plus its backing
// Endpoints), and its pod ServiceAccount is granted a test-scoped ClusterRole to
// list namespaces. The namespaces_list tool call then SUCCEEDS and returns real
// cluster data (it contains kube-system). This proves the functional path end to
// end: an egress policy that admits the API server does not break legitimate API
// access, and the MCP client->tool->API-server round trip works.
//
// # Why the client path requires the open posture
//
// The MCP client reaches the server through the API server proxy, a node-sourced
// transport that NetworkPolicy cannot match, so it is denied under the restricted
// default posture (identical to TestMCPHandshake). The functional test therefore
// only runs under the open posture; under restricted it is skipped and the
// structural egress coverage (TestNetworkPolicyEgressRestrictionApplied) stands.
//
// # Why the BLOCKED direction is not asserted here
//
// This test deliberately asserts only the allow path. Proving that a narrowed
// egress policy BLOCKS the API server is not reliably possible on the e2e CNI
// (kindnet) for two independent reasons:
//
//  1. Host-network destination. kubernetes.default.svc is a ClusterIP that
//     kube-proxy DNATs to the control-plane node's own host-network IP (the same
//     node in single-node kind). Pod->local-host traffic does not traverse the
//     pod-to-pod forwarding path that kindnet's NetworkPolicy implementation
//     hooks, so an egress ipBlock may simply not be evaluated for it.
//  2. Established-connection reuse. A keep-alive connection opened before a
//     policy change survives it: conntrack lets an already-established flow
//     continue even after egress is narrowed.
//
// Blocking is therefore CNI-dependent (enforced on Calico/Cilium/OVN, not
// guaranteed on kindnet) rather than universally provable, so only the allow path
// is asserted in this per-PR suite.
func TestNetworkPolicyEgressAPIServerEnforcement(t *testing.T) {
	t.Parallel()

	const mcpServerPort = 8080

	// Closure state shared across Setup/Assess/Teardown.
	var posture string

	feature := features.New("Egress NetworkPolicy admits a managed MCP server's access to the Kubernetes API server").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Slow).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			posture = networkPolicyPosture(ctx, t, cfg)
			if posture == posturePolicyRestricted {
				// The client drives the server through the node-sourced
				// apiserver-proxy, which the restricted ingress denies. Create
				// nothing; the assessment skips and teardown is a no-op.
				t.Log("restricted posture: skipping functional API server egress test (apiserver-proxy client transport is not NetworkPolicy-matchable)")
				return ctx
			}

			// Build the ALLOWED egress destination from the live API server
			// addresses. EgressPorts is left empty so all ports to those IPs are
			// permitted (covers both the ClusterIP:443 the client dials and the
			// endpoint:6443 it DNATs to, without hard-coding either).
			allowed := apiServerEgressPeers(ctx, t, cfg)

			ctx = f.SetupMCPServer(ctx, t, cfg, "egress-apiserver", true,
				f.WithNetwork(&mcpv1beta1.NetworkConfig{
					EgressTo: allowed,
				}),
			)
			server := f.ServerFromContext(ctx)

			// Grant the managed pod's ServiceAccount (the namespace default SA,
			// since NewMCPServer sets no serviceAccountName) permission to list
			// namespaces, so a successful namespaces_list call proves network
			// reachability rather than racing an RBAC 403. Cluster-scoped because
			// namespaces are cluster-scoped.
			//
			// Each created object registers its own t.Cleanup as the SINGLE
			// authority for its deletion. t.Cleanup is registered on this
			// feature-level *testing.T and always runs - after Teardown on the
			// normal and assessment-failure paths, on a Setup-step t.Fatalf/Goexit
			// (which skips Teardown), and under -fail-fast - so every outcome
			// deletes each object exactly once with no gating flag. Deletion lives
			// only here; Teardown does not touch RBAC.
			rbacName := envconf.RandomName("e2e-apiserver-egress", 40)
			grantNamespacesList(ctx, t, cfg, rbacName, server.Namespace, func(obj k8s.Object) {
				t.Cleanup(func() {
					cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if err := cfg.Client().Resources().Delete(cleanupCtx, obj); err != nil && !apierrors.IsNotFound(err) {
						t.Logf("cleanup: failed to delete %T %s: %v", obj, obj.GetName(), err)
					}
				})
			})

			// A ready listener, so the tool call exercises the server rather than
			// racing pod startup (the apiserver-proxy also 503s until endpoints
			// propagate).
			f.WaitForEndpointsReady(ctx, t, cfg, server.Namespace, server.Name)

			return ctx
		}).
		Assess("allowed egress: namespaces_list returns real cluster data", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			if posture == posturePolicyRestricted {
				t.Skip("restricted posture: apiserver-proxy client transport is not NetworkPolicy-matchable, so the functional tool call cannot be driven; structural egress coverage applies instead")
			}
			server := f.ServerFromContext(ctx)

			// The ClusterRoleBinding granting namespaces_list may not yet be
			// observed by the API server authorizer when this one-shot tool call
			// runs, surfacing as a transient permission denial even though egress
			// works. Retry specifically on an authorization denial for a bounded
			// window; any non-authz error (a real egress/protocol failure) or a
			// timeout fails, and a successful result returns immediately.
			//
			// The outer wait.For governs ONLY the retry window - how long to keep
			// re-attempting on an authz denial. It does NOT bound a single attempt:
			// each call to callNamespacesList builds its own fresh connect (30s) and
			// tool-call (60s) deadlines from context.Background(), so every attempt
			// gets its full documented budget even on a loaded cluster. The window
			// is sized so several full-budget attempts fit before it times out.
			var (
				res     *mcp.CallToolResult
				elapsed time.Duration
			)
			waitErr := wait.For(func(context.Context) (bool, error) {
				var callErr error
				res, elapsed, callErr = callNamespacesList(t, cfg, server.Namespace, server.Name, mcpServerPort)
				if callErr != nil {
					if isAuthzDenied(callErr.Error()) {
						return false, nil // RBAC not yet propagated; retry
					}
					return false, callErr // genuine protocol/network failure; stop
				}
				if res != nil && res.IsError && isAuthzDenied(toolResultText(res)) {
					return false, nil // RBAC not yet propagated; retry
				}
				return true, nil
			},
				wait.WithTimeout(90*time.Second),
				wait.WithInterval(2*time.Second),
				wait.WithImmediate(),
				wait.WithContext(ctx),
			)
			if waitErr != nil {
				t.Fatalf("allowed egress: namespaces_list did not succeed within the retry window (last call took %s): %v", elapsed, waitErr)
			}
			if res == nil {
				t.Fatalf("allowed egress: namespaces_list returned no result and no error after %s", elapsed)
			}
			if res.IsError {
				t.Fatalf("allowed egress: namespaces_list reported a tool error after %s: %q", elapsed, toolResultText(res))
			}
			text := toolResultText(res)
			if !strings.Contains(text, "kube-system") {
				t.Fatalf("allowed egress: namespaces_list succeeded but returned no recognizable cluster data (want kube-system) in %q", text)
			}
			t.Logf("allowed egress confirmed: namespaces_list returned real cluster data in %s (contains kube-system)", elapsed)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// The cluster-scoped RBAC objects are deleted by their per-object
			// t.Cleanup (registered in Setup), which runs after this Teardown on
			// every path, so Teardown does not delete them here.
			//
			// nil-safe: under the restricted-posture early return no server was
			// stored in context, and TeardownMCPServer handles that.
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

// apiServerEgressPeers builds an egress allow-list of ipBlock peers covering the
// Kubernetes API server: the kubernetes Service ClusterIP (the destination the
// in-cluster client dials via KUBERNETES_SERVICE_HOST) plus every backing
// Endpoints address (the host-network control-plane IP the ClusterIP DNATs to).
// Both are included so the allow-list is correct whether the CNI evaluates egress
// before or after kube-proxy DNAT.
func apiServerEgressPeers(ctx context.Context, t *testing.T, cfg *envconf.Config) []networkingv1.NetworkPolicyPeer {
	t.Helper()
	r := cfg.Client().Resources()

	ips := map[string]struct{}{}

	svc := &corev1.Service{}
	if err := r.Get(ctx, "kubernetes", "default", svc); err != nil {
		t.Fatalf("failed to read the kubernetes Service in the default namespace: %v", err)
	}
	if ip := svc.Spec.ClusterIP; ip != "" && ip != corev1.ClusterIPNone {
		ips[ip] = struct{}{}
	}

	var slices discoveryv1.EndpointSliceList
	if err := cfg.Client().Resources("default").List(ctx, &slices,
		resources.WithLabelSelector("kubernetes.io/service-name=kubernetes"),
	); err != nil {
		t.Fatalf("failed to list the kubernetes EndpointSlices in the default namespace: %v", err)
	}
	for _, slice := range slices.Items {
		for _, ep := range slice.Endpoints {
			for _, addr := range ep.Addresses {
				if addr != "" {
					ips[addr] = struct{}{}
				}
			}
		}
	}
	if len(ips) == 0 {
		t.Fatal("discovered no API server IPs from the kubernetes Service/Endpoints")
	}

	cidrs := make([]string, 0, len(ips))
	for ip := range ips {
		cidrs = append(cidrs, hostCIDR(ip))
	}
	sort.Strings(cidrs)

	peers := make([]networkingv1.NetworkPolicyPeer, 0, len(cidrs))
	for _, c := range cidrs {
		peers = append(peers, networkingv1.NetworkPolicyPeer{
			IPBlock: &networkingv1.IPBlock{CIDR: c},
		})
	}
	t.Logf("API server egress allow-list: %v", cidrs)
	return peers
}

// hostCIDR returns a single-host CIDR for the given IP (/32 for IPv4, /128 for
// IPv6). The inputs are API-guaranteed IP addresses (Service.ClusterIP and
// EndpointSlice addresses), so the /32 fallback for an unparseable value is not
// expected to be reached.
func hostCIDR(ip string) string {
	if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() == nil {
		return ip + "/128"
	}
	return ip + "/32"
}

// grantNamespacesList creates a cluster-scoped Role and binding (sharing name)
// that lets the given namespace's default ServiceAccount list namespaces. Each
// object is passed to track the instant its Create succeeds; the caller's track
// callback is responsible for arranging deletion (a per-object t.Cleanup that is
// the single deleter), so cleanup is leak-proof even if a later step fails -
// including the binding Create below, after which the already-tracked ClusterRole
// is still torn down by its registered cleanup.
func grantNamespacesList(ctx context.Context, t *testing.T, cfg *envconf.Config, name, saNamespace string, track func(obj k8s.Object)) {
	t.Helper()
	r := cfg.Client().Resources()

	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"namespaces"},
			Verbs:     []string{"get", "list"},
		}},
	}
	if err := r.Create(ctx, cr); err != nil {
		t.Fatalf("failed to create ClusterRole %s: %v", name, err)
	}
	track(cr)

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     name,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      "default",
			Namespace: saNamespace,
		}},
	}
	if err := r.Create(ctx, crb); err != nil {
		// The ClusterRole was already tracked, so its registered cleanup tears it
		// down on the Setup Goexit this t.Fatalf triggers; nothing leaks here.
		t.Fatalf("failed to create ClusterRoleBinding %s: %v", name, err)
	}
	track(crb)
	t.Logf("granted list namespaces to %s/default via ClusterRole/Binding %s", saNamespace, name)
}

// callNamespacesList connects an MCP client to the server through the API server
// proxy (the same transport TestMCPHandshake uses) and invokes namespaces_list.
// It returns the tool result, the wall-clock time the call took, and any
// protocol-level error. The elapsed duration is returned for diagnostics only
// (logging); callers classify success vs. failure from the result/error, not from
// timing. Connect and CallTool each get their own fresh deadline derived from
// context.Background() - connect 30s, the tool call 60s - so the two budgets are
// independent of each other AND of any outer retry window the caller runs this
// under; a slow handshake does not eat into the tool-call budget and a loaded
// cluster's retries each get the full budget. Connecting (an ingress path)
// hard-fails the test, since egress changes never affect it.
func callNamespacesList(t *testing.T, cfg *envconf.Config,
	namespace, name string, port int) (*mcp.CallToolResult, time.Duration, error) {
	t.Helper()

	httpClient, proxyURL := f.ServiceProxyHTTPClient(t, cfg, namespace, name, port, "/mcp")

	mcpClient := mcp.NewClient(
		&mcp.Implementation{Name: "e2e-apiserver-egress-client", Version: "v0.0.1"},
		nil,
	)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   proxyURL,
		HTTPClient: httpClient,
	}

	connectCtx, connectCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer connectCancel()

	session, err := mcpClient.Connect(connectCtx, transport, nil)
	if err != nil {
		t.Fatalf("failed to connect MCP client to %s/%s: %v", namespace, name, err)
	}
	defer func() { _ = session.Close() }()

	callCtx, callCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer callCancel()

	start := time.Now()
	res, err := session.CallTool(callCtx, &mcp.CallToolParams{
		Name:      "namespaces_list",
		Arguments: map[string]any{},
	})
	return res, time.Since(start), err
}

// isAuthzDenied reports whether msg looks like a Kubernetes API server
// authorization denial (RBAC) rather than a network/egress failure. The
// ClusterRoleBinding granting namespaces_list may not be observed by the
// authorizer the instant the one-shot tool call runs, making such a denial
// transient and worth a bounded retry; a real egress failure surfaces
// differently and must not be retried or swallowed.
func isAuthzDenied(msg string) bool {
	m := strings.ToLower(msg)
	for _, phrase := range []string{"forbidden", "not allowed", "cannot list", "unauthorized"} {
		if strings.Contains(m, phrase) {
			return true
		}
	}
	return false
}

// toolResultText concatenates the textual content of a tool result.
func toolResultText(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
