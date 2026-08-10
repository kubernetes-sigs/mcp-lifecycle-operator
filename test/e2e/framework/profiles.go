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
	"flag"
	"fmt"
	"os"
	"sort"
)

// Profile is a named label selector for filtering e2e tests.
// Higher Priority wins when multiple registrations use the same Name.
// Upstream defaults use priority 0; downstream overrides should use > 0.
type Profile struct {
	Name     string
	Priority int
	Labels   map[string][]string
}

var (
	profiles    = map[string]Profile{}
	profileFlag string
)

// RegisterProfile adds a profile to the global registry.
// If a profile with the same name already exists, the higher priority wins.
// Equal priority overwrites (last-write-wins).
func RegisterProfile(p Profile) {
	existing, ok := profiles[p.Name]
	if !ok || p.Priority >= existing.Priority {
		profiles[p.Name] = p
	}
}

// LookupProfile returns a profile by name.
func LookupProfile(name string) (Profile, bool) {
	p, ok := profiles[name]
	return p, ok
}

// ProfileNames returns sorted profile names.
func ProfileNames() []string {
	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// RegisterProfileFlag registers the -profile flag. Call before envconf.NewFromFlags().
func RegisterProfileFlag() {
	flag.StringVar(&profileFlag, "profile", "", "Test profile (smoke, extended, or distribution-specific)")
}

// ResolveProfile returns the labels for the selected profile, or nil if
// no profile was selected. Exits with an error if the profile is unknown.
func ResolveProfile() map[string][]string {
	if profileFlag == "" {
		return nil
	}
	p, ok := LookupProfile(profileFlag)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown profile %q, available: %v\n", profileFlag, ProfileNames())
		os.Exit(1)
	}
	return p.Labels
}
