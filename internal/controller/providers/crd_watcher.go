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
	"sync"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
)

// crdWatcher watches CustomResourceDefinition objects and starts provider
// controllers when their required CRDs become available.
type crdWatcher struct {
	mgr     ctrl.Manager
	mu      sync.Mutex
	pending []namedRegistration
	started map[string]bool
}

// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch

func (w *crdWatcher) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithName("crd-watcher")

	w.mu.Lock()
	defer w.mu.Unlock()

	remaining := make([]namedRegistration, 0, len(w.pending))
	for _, nf := range w.pending {
		if w.started[nf.name] {
			continue
		}
		missing, err := missingCRDs(w.mgr, nf.reg.RequiredCRDs)
		if err != nil {
			log.V(1).Info("CRD discovery error, will retry", "provider", nf.name, "error", err)
			remaining = append(remaining, nf)
			continue
		}
		if len(missing) > 0 {
			remaining = append(remaining, nf)
			continue
		}
		log.Info("Required CRDs available, starting provider", "provider", nf.name)
		if err := nf.reg.Factory(w.mgr); err != nil {
			log.Error(err, "Failed to start provider, will retry", "provider", nf.name)
			remaining = append(remaining, nf)
			continue
		}
		w.started[nf.name] = true
	}
	w.pending = remaining
	if len(w.pending) > 0 {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (w *crdWatcher) setupWithManager(mgr ctrl.Manager) error {
	// Watch CRDs metadata-only. The watch is used purely as an event trigger;
	// CRD availability is checked via the RESTMapper (see missingCRDs) and the
	// CRD object content - in particular the large per-version OpenAPI v3 schema -
	// is never read. A metadata-only watch keeps the full CRD spec out of the
	// manager cache, mirroring the ConfigMap metadata-only watch already used in
	// this repo.
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiextensionsv1.CustomResourceDefinition{}, builder.OnlyMetadata).
		Named("provider-crd-watcher").
		Complete(w)
}

// missingCRDs returns the subset of gvks that are not yet served by the API
// server. It returns a non-nil error when the RESTMapper encounters a
// non-NoMatch error (e.g. transient discovery failure).
func missingCRDs(mgr ctrl.Manager, gvks []schema.GroupVersionKind) ([]schema.GroupVersionKind, error) {
	mapper := mgr.GetRESTMapper()
	var missing []schema.GroupVersionKind
	for _, gvk := range gvks {
		_, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err == nil {
			continue
		}
		if meta.IsNoMatchError(err) {
			missing = append(missing, gvk)
			continue
		}
		return nil, err
	}
	return missing, nil
}
