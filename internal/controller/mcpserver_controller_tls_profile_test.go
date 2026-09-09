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
	"crypto/tls"
	"testing"
)

// TestApplyTLSProfileWithFloor verifies that the operator-wide TLS profile is
// applied to the MCP-server client config (min version, cipher suites and TLS
// 1.3 group/curve preferences) while never letting the negotiated minimum
// version drop below the TLS 1.2 floor the transport is built with.
func TestApplyTLSProfileWithFloor(t *testing.T) {
	t.Run("group preferences propagate to the client config", func(t *testing.T) {
		cfg := &tls.Config{MinVersion: tls.VersionTLS12}
		groups := []tls.CurveID{tls.X25519MLKEM768, tls.X25519}
		profile := func(c *tls.Config) { c.CurvePreferences = groups }

		applyTLSProfileWithFloor(cfg, profile)

		if len(cfg.CurvePreferences) != 2 ||
			cfg.CurvePreferences[0] != tls.X25519MLKEM768 ||
			cfg.CurvePreferences[1] != tls.X25519 {
			t.Errorf("CurvePreferences = %v, want [X25519MLKEM768 X25519]", cfg.CurvePreferences)
		}
		if cfg.MinVersion != tls.VersionTLS12 {
			t.Errorf("MinVersion = %d, want TLS 1.2 floor %d", cfg.MinVersion, tls.VersionTLS12)
		}
	})

	t.Run("profile cannot lower min version below the TLS 1.2 floor", func(t *testing.T) {
		cfg := &tls.Config{MinVersion: tls.VersionTLS12}
		profile := func(c *tls.Config) { c.MinVersion = tls.VersionTLS10 }

		applyTLSProfileWithFloor(cfg, profile)

		if cfg.MinVersion != tls.VersionTLS12 {
			t.Errorf("MinVersion = %d, want floor preserved at TLS 1.2 %d", cfg.MinVersion, tls.VersionTLS12)
		}
	})

	t.Run("profile can raise the min version to TLS 1.3", func(t *testing.T) {
		cfg := &tls.Config{MinVersion: tls.VersionTLS12}
		profile := func(c *tls.Config) { c.MinVersion = tls.VersionTLS13 }

		applyTLSProfileWithFloor(cfg, profile)

		if cfg.MinVersion != tls.VersionTLS13 {
			t.Errorf("MinVersion = %d, want TLS 1.3 %d", cfg.MinVersion, tls.VersionTLS13)
		}
	})

	t.Run("groups-only profile keeps the floor and sets curves", func(t *testing.T) {
		cfg := &tls.Config{MinVersion: tls.VersionTLS12}
		profile := func(c *tls.Config) { c.CurvePreferences = []tls.CurveID{tls.CurveP256} }

		applyTLSProfileWithFloor(cfg, profile)

		if cfg.MinVersion != tls.VersionTLS12 {
			t.Errorf("MinVersion = %d, want TLS 1.2 floor %d", cfg.MinVersion, tls.VersionTLS12)
		}
		if len(cfg.CurvePreferences) != 1 || cfg.CurvePreferences[0] != tls.CurveP256 {
			t.Errorf("CurvePreferences = %v, want [CurveP256]", cfg.CurvePreferences)
		}
	})
}
