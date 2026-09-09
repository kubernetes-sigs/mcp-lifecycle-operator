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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// applyTLSProfileWithFloor applies the operator-wide TLS profile to the
// MCP-server client config (min version, cipher suites and TLS 1.3
// group/curve preferences) while never letting the negotiated minimum version
// drop below the TLS 1.2 floor the transport is built with.
var _ = Describe("applyTLSProfileWithFloor", func() {
	It("propagates group preferences to the client config", func() {
		cfg := &tls.Config{MinVersion: tls.VersionTLS12}
		groups := []tls.CurveID{tls.X25519MLKEM768, tls.X25519}
		profile := func(c *tls.Config) { c.CurvePreferences = groups }

		applyTLSProfileWithFloor(cfg, profile)

		Expect(cfg.CurvePreferences).To(Equal(groups))
		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
	})

	It("does not let the profile lower the min version below the TLS 1.2 floor", func() {
		cfg := &tls.Config{MinVersion: tls.VersionTLS12}
		profile := func(c *tls.Config) { c.MinVersion = tls.VersionTLS10 }

		applyTLSProfileWithFloor(cfg, profile)

		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
	})

	It("lets the profile raise the min version to TLS 1.3", func() {
		cfg := &tls.Config{MinVersion: tls.VersionTLS12}
		profile := func(c *tls.Config) { c.MinVersion = tls.VersionTLS13 }

		applyTLSProfileWithFloor(cfg, profile)

		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
	})

	It("keeps the floor and sets curves for a groups-only profile", func() {
		cfg := &tls.Config{MinVersion: tls.VersionTLS12}
		profile := func(c *tls.Config) { c.CurvePreferences = []tls.CurveID{tls.CurveP256} }

		applyTLSProfileWithFloor(cfg, profile)

		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		Expect(cfg.CurvePreferences).To(Equal([]tls.CurveID{tls.CurveP256}))
	})
})
