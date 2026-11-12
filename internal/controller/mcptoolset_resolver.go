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

package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

// Resolved-server failure reasons, surfaced on the MCPToolset ServersResolved
// condition message and used to distinguish transient from terminal states.
const (
	// ResolveReasonNotFound: the referenced MCPServer does not exist in the
	// toolset namespace.
	ResolveReasonNotFound = "NotFound"
	// ResolveReasonNotReady: the MCPServer exists but has not published a
	// verified, reachable endpoint yet (no status.address.url).
	ResolveReasonNotReady = "NotReady"
)

// UnresolvedServer records why a referenced server could not be resolved to a
// reachable endpoint.
type UnresolvedServer struct {
	// Server is the toolset-local server name (an MCPServer resource name).
	Server string
	// Reason is a stable machine token (ResolveReasonNotFound/NotReady).
	Reason string
	// Message is a human-readable explanation.
	Message string
}

// ResolveServers resolves each server name in a toolset to its in-cluster MCP
// endpoint URL by reading the corresponding v1beta1.MCPServer in the same
// namespace.
//
// A server resolves only when its MCPServer exists and has published
// status.address.url, which the MCPServer controller sets only after the
// endpoint is verified and ready. The returned endpoints preserve the input
// order for the servers that resolved; every server that did not resolve is
// reported in unresolved (also in input order). A non-nil error is returned
// only for unexpected API failures (not for a missing or not-ready server),
// so callers can distinguish "retry later" from "surface to the user".
func ResolveServers(ctx context.Context, c client.Client, namespace string, servers []string) (endpoints []sandbox.Endpoint, unresolved []UnresolvedServer, err error) {
	for _, name := range servers {
		var srv mcpv1beta1.MCPServer
		getErr := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &srv)
		if getErr != nil {
			if apierrors.IsNotFound(getErr) {
				unresolved = append(unresolved, UnresolvedServer{
					Server:  name,
					Reason:  ResolveReasonNotFound,
					Message: fmt.Sprintf("MCPServer %q not found in namespace %q", name, namespace),
				})
				continue
			}
			return nil, nil, fmt.Errorf("getting MCPServer %q: %w", name, getErr)
		}

		url := serverEndpointURL(&srv)
		if url == "" {
			unresolved = append(unresolved, UnresolvedServer{
				Server:  name,
				Reason:  ResolveReasonNotReady,
				Message: fmt.Sprintf("MCPServer %q has no verified endpoint yet", name),
			})
			continue
		}

		endpoints = append(endpoints, sandbox.Endpoint{Server: name, URL: url})
	}

	return endpoints, unresolved, nil
}

// serverEndpointURL returns the reachable in-cluster URL for an MCPServer, or
// "" when the server is not ready to receive traffic. It requires both a
// published status.address.url and a Verified=True condition so a stale address
// left over from a previous ready state is not treated as reachable.
func serverEndpointURL(srv *mcpv1beta1.MCPServer) string {
	if srv.Status.Address == nil || srv.Status.Address.URL == "" {
		return ""
	}
	if !meta.IsStatusConditionTrue(srv.Status.Conditions, ConditionTypeVerified) {
		return ""
	}
	return srv.Status.Address.URL
}
