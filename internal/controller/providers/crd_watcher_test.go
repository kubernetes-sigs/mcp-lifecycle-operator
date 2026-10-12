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

package providers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

type fakeManager struct {
	ctrl.Manager
	mapper meta.RESTMapper
}

func (f *fakeManager) GetRESTMapper() meta.RESTMapper {
	return f.mapper
}

func (f *fakeManager) GetLogger() logr.Logger {
	return logr.Discard()
}

type errorMapper struct {
	meta.RESTMapper
	err error
}

func (e *errorMapper) RESTMapping(schema.GroupKind, ...string) (*meta.RESTMapping, error) {
	return nil, e.err
}

func TestAllCRDsPresent(t *testing.T) {
	gv := schema.GroupVersion{Group: "gateway.networking.k8s.io", Version: "v1"}
	httpRouteGVK := gv.WithKind("HTTPRoute")
	gatewayGVK := gv.WithKind("Gateway")

	t.Run("all GVKs present", func(t *testing.T) {
		mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{gv})
		mapper.Add(httpRouteGVK, meta.RESTScopeNamespace)
		mapper.Add(gatewayGVK, meta.RESTScopeNamespace)

		mgr := &fakeManager{mapper: mapper}
		missing, err := missingCRDs(mgr, []schema.GroupVersionKind{httpRouteGVK, gatewayGVK})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(missing) != 0 {
			t.Fatalf("expected no missing CRDs, got %v", missing)
		}
	})

	t.Run("missing GVK", func(t *testing.T) {
		mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{gv})
		mapper.Add(httpRouteGVK, meta.RESTScopeNamespace)

		mgr := &fakeManager{mapper: mapper}
		missing, err := missingCRDs(mgr, []schema.GroupVersionKind{httpRouteGVK, gatewayGVK})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(missing) != 1 || missing[0] != gatewayGVK {
			t.Fatalf("expected [%v] missing, got %v", gatewayGVK, missing)
		}
	})

	t.Run("non-NoMatch error returns error", func(t *testing.T) {
		mgr := &fakeManager{mapper: &errorMapper{err: fmt.Errorf("transient discovery failure")}}
		_, err := missingCRDs(mgr, []schema.GroupVersionKind{httpRouteGVK})
		if err == nil {
			t.Fatal("expected error from missingCRDs")
		}
	})

	t.Run("empty GVK list", func(t *testing.T) {
		mapper := meta.NewDefaultRESTMapper(nil)
		mgr := &fakeManager{mapper: mapper}
		missing, err := missingCRDs(mgr, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(missing) != 0 {
			t.Fatalf("expected no missing CRDs, got %v", missing)
		}
	})
}

func TestSetupWithManager(t *testing.T) {
	// Build a real controller-runtime manager from a dummy, unreachable
	// rest.Config. setupWithManager only constructs the controller and a lazy
	// metadata informer; neither dials the API server until the manager is
	// Started, which this test never does. Metrics and health probe binds are
	// disabled so nothing listens on a port.
	scheme := runtime.NewScheme()
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add apiextensionsv1 to scheme: %v", err)
	}

	mgr, err := ctrl.NewManager(&rest.Config{Host: "127.0.0.1:0"}, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	w := &crdWatcher{mgr: mgr, started: map[string]bool{}}
	if err := w.setupWithManager(mgr); err != nil {
		t.Fatalf("setupWithManager() unexpected error: %v", err)
	}
}

func TestCRDWatcherReconcile(t *testing.T) {
	gv := schema.GroupVersion{Group: "gateway.networking.k8s.io", Version: "v1"}
	httpRouteGVK := gv.WithKind("HTTPRoute")

	t.Run("starts provider when CRDs available", func(t *testing.T) {
		mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{gv})
		mapper.Add(httpRouteGVK, meta.RESTScopeNamespace)

		var started bool
		w := &crdWatcher{
			mgr:     &fakeManager{mapper: mapper},
			started: make(map[string]bool),
			pending: []namedRegistration{
				{
					name: "httproute",
					reg: Registration{
						RequiredCRDs: []schema.GroupVersionKind{httpRouteGVK},
						Factory: func(mgr ctrl.Manager) error {
							started = true
							return nil
						},
					},
				},
			},
		}

		result, err := w.Reconcile(context.Background(), ctrl.Request{})
		if err != nil {
			t.Fatalf("Reconcile() unexpected error: %v", err)
		}
		if !started {
			t.Fatal("expected provider factory to be called")
		}
		if !w.started["httproute"] {
			t.Fatal("expected httproute to be marked as started")
		}
		if len(w.pending) != 0 {
			t.Fatalf("expected no pending providers, got %d", len(w.pending))
		}
		if result.RequeueAfter != 0 {
			t.Fatal("expected no requeue when all providers started")
		}
	})

	t.Run("stays pending when CRDs missing and requeues", func(t *testing.T) {
		mapper := meta.NewDefaultRESTMapper(nil)

		var called bool
		w := &crdWatcher{
			mgr:     &fakeManager{mapper: mapper},
			started: make(map[string]bool),
			pending: []namedRegistration{
				{
					name: "httproute",
					reg: Registration{
						RequiredCRDs: []schema.GroupVersionKind{httpRouteGVK},
						Factory: func(mgr ctrl.Manager) error {
							called = true
							return nil
						},
					},
				},
			},
		}

		result, err := w.Reconcile(context.Background(), ctrl.Request{})
		if err != nil {
			t.Fatalf("Reconcile() unexpected error: %v", err)
		}
		if called {
			t.Fatal("expected provider factory NOT to be called")
		}
		if len(w.pending) != 1 {
			t.Fatalf("expected 1 pending provider, got %d", len(w.pending))
		}
		if result.RequeueAfter != 5*time.Second {
			t.Fatalf("expected RequeueAfter=5s, got %v", result.RequeueAfter)
		}
	})

	t.Run("does not start provider twice", func(t *testing.T) {
		mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{gv})
		mapper.Add(httpRouteGVK, meta.RESTScopeNamespace)

		var callCount int
		w := &crdWatcher{
			mgr:     &fakeManager{mapper: mapper},
			started: map[string]bool{"httproute": true},
			pending: []namedRegistration{
				{
					name: "httproute",
					reg: Registration{
						RequiredCRDs: []schema.GroupVersionKind{httpRouteGVK},
						Factory: func(mgr ctrl.Manager) error {
							callCount++
							return nil
						},
					},
				},
			},
		}

		if _, err := w.Reconcile(context.Background(), ctrl.Request{}); err != nil {
			t.Fatalf("Reconcile() unexpected error: %v", err)
		}
		if callCount != 0 {
			t.Fatalf("expected factory not to be called again, but was called %d times", callCount)
		}
		if len(w.pending) != 0 {
			t.Fatalf("expected pending to be cleared, got %d", len(w.pending))
		}
	})

	t.Run("failed factory keeps provider pending", func(t *testing.T) {
		mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{gv})
		mapper.Add(httpRouteGVK, meta.RESTScopeNamespace)

		w := &crdWatcher{
			mgr:     &fakeManager{mapper: mapper},
			started: make(map[string]bool),
			pending: []namedRegistration{
				{
					name: "httproute",
					reg: Registration{
						RequiredCRDs: []schema.GroupVersionKind{httpRouteGVK},
						Factory: func(mgr ctrl.Manager) error {
							return fmt.Errorf("controller setup failed")
						},
					},
				},
			},
		}

		if _, err := w.Reconcile(context.Background(), ctrl.Request{}); err != nil {
			t.Fatalf("Reconcile() unexpected error: %v", err)
		}
		if w.started["httproute"] {
			t.Fatal("expected httproute NOT to be marked as started after factory error")
		}
		if len(w.pending) != 1 {
			t.Fatalf("expected 1 pending provider for retry, got %d", len(w.pending))
		}
	})
}
