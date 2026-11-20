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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcpv1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

// Resolved-server failure reasons, surfaced on the MCPToolset ServersResolved
// condition message and used to distinguish transient from terminal states.
const (
	// ResolveReasonNotFound: the referenced MCPServer does not exist in the
	// toolset namespace.
	ResolveReasonNotFound = "NotFound"
	// ResolveReasonNotReady: the MCPServer exists but has no verified internal endpoint.
	ResolveReasonNotReady = "NotReady"
	// ResolveReasonVerificationStale: verification describes an earlier spec.
	ResolveReasonVerificationStale = "VerificationStale"
)

// UnresolvedServer records why a referenced server could not be resolved to a
// reachable endpoint.
type UnresolvedServer struct {
	// Server is the toolset-local server name (an MCPServer resource name).
	Server string
	// Reason is a stable machine token.
	Reason string
	// Message is a human-readable explanation.
	Message string
}

// ResolveServers resolves each server name in a toolset to its in-cluster MCP
// endpoint URL by reading the corresponding v1.MCPServer in the same
// namespace.
//
// A server resolves only when its current generation has a verified internal
// endpoint. The returned endpoints preserve the input
// order for the servers that resolved; every server that did not resolve is
// reported in unresolved (also in input order). A non-nil error is returned
// only for unexpected API failures (not for a missing or not-ready server),
// so callers can distinguish "retry later" from "surface to the user".
func ResolveServers(ctx context.Context, c client.Client, namespace string, servers []string) (endpoints []sandbox.Endpoint, unresolved []UnresolvedServer, err error) {
	for _, name := range servers {
		var srv mcpv1.MCPServer
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

		verified := meta.FindStatusCondition(srv.Status.Conditions, ConditionTypeVerified)
		if verified != nil && verified.ObservedGeneration != srv.Generation {
			unresolved = append(unresolved, UnresolvedServer{
				Server:  name,
				Reason:  ResolveReasonVerificationStale,
				Message: fmt.Sprintf("MCPServer %q has not been verified for its current generation", name),
			})
			continue
		}
		if verified == nil || verified.Status != metav1.ConditionTrue || srv.Status.VerifiedEndpointURL == "" {
			unresolved = append(unresolved, UnresolvedServer{
				Server:  name,
				Reason:  ResolveReasonNotReady,
				Message: fmt.Sprintf("MCPServer %q has no verified endpoint yet", name),
			})
			continue
		}

		endpoints = append(endpoints, sandbox.Endpoint{Server: name, URL: srv.Status.VerifiedEndpointURL})
	}

	return endpoints, unresolved, nil
}
