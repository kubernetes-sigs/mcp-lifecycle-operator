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
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
)

// Factory creates and registers a provider controller with the manager.
type Factory func(mgr ctrl.Manager) error

var registry []namedFactory

type namedFactory struct {
	name    string
	factory Factory
}

// Register adds a provider factory to the global registry.
// Providers call this from their init() function.
func Register(name string, f Factory) {
	registry = append(registry, namedFactory{name: name, factory: f})
}

// SetupAll calls each registered factory to set up its controller with the manager.
func SetupAll(mgr ctrl.Manager) error {
	for _, nf := range registry {
		if err := nf.factory(mgr); err != nil {
			return fmt.Errorf("provider %s: %w", nf.name, err)
		}
	}
	return nil
}
