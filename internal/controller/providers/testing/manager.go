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

package testing

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
)

// CRDMissingManager wraps a ctrl.Manager and overrides its RESTMapper to
// simulate missing or erroring CRDs during SetupWithManager calls.
type CRDMissingManager struct {
	ctrl.Manager
	MissingGVKs map[schema.GroupVersionKind]bool
	ErrorGVKs   map[schema.GroupVersionKind]error
}

func (m *CRDMissingManager) GetRESTMapper() meta.RESTMapper {
	return &crdMissingMapper{
		RESTMapper:  m.Manager.GetRESTMapper(),
		missingGVKs: m.MissingGVKs,
		errorGVKs:   m.ErrorGVKs,
	}
}

type crdMissingMapper struct {
	meta.RESTMapper
	missingGVKs map[schema.GroupVersionKind]bool
	errorGVKs   map[schema.GroupVersionKind]error
}

func (m *crdMissingMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	gvk := schema.GroupVersionKind{Group: gk.Group, Kind: gk.Kind}
	if len(versions) > 0 {
		gvk.Version = versions[0]
	}
	if m.missingGVKs[gvk] {
		return nil, &meta.NoKindMatchError{
			GroupKind:        gk,
			SearchedVersions: versions,
		}
	}
	if err, ok := m.errorGVKs[gvk]; ok {
		return nil, err
	}
	return m.RESTMapper.RESTMapping(gk, versions...)
}
