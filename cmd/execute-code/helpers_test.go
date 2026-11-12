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

package main

import (
	"testing"
	"time"
)

func TestParseTimeoutSeconds(t *testing.T) {
	const def = 42 * time.Second
	cases := []struct {
		name string
		in   string
		want time.Duration
	}{
		{"empty falls back to default", "", def},
		{"non-numeric falls back to default", "abc", def},
		{"zero falls back to default", "0", def},
		{"negative falls back to default", "-5", def},
		{"positive is seconds", "90", 90 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseTimeoutSeconds(tc.in, def); got != tc.want {
				t.Errorf("parseTimeoutSeconds(%q) = %v; want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestTimeoutFor(t *testing.T) {
	d := testDispatcher(&fakeProvider{}) // defaultTimeout = 60s
	cases := []struct {
		name    string
		seconds int
		want    time.Duration
	}{
		{"zero uses dispatcher default", 0, 60 * time.Second},
		{"negative uses dispatcher default", -1, 60 * time.Second},
		{"positive caller value honoured", 30, 30 * time.Second},
		{"above cap is clamped to maxTimeout", int(maxTimeout.Seconds()) + 60, maxTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := d.timeoutFor(tc.seconds); got != tc.want {
				t.Errorf("timeoutFor(%d) = %v; want %v", tc.seconds, got, tc.want)
			}
		})
	}
}

// TestBuildDispatcherUnknownProvider confirms an unregistered provider name is
// rejected before any kube config is loaded, so the failure is a clear
// configuration error rather than a connection error.
func TestBuildDispatcherUnknownProvider(t *testing.T) {
	t.Setenv(envProvider, "no-such-provider")

	_, _, err := buildDispatcher()
	if err == nil {
		t.Fatal("buildDispatcher accepted an unknown provider; want error")
	}
}
