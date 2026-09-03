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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

var _ = Describe("buildServerCard", func() {
	It("should build a complete server card from server info and spec", func() {
		info := &mcpv1alpha1.MCPServerInfo{
			Name:            "test-server",
			Version:         "1.0.0",
			ProtocolVersion: "2025-03-26",
			Capabilities: &mcpv1alpha1.MCPServerCapabilities{
				Tools:     true,
				Resources: true,
			},
		}
		spec := &mcpv1alpha1.MCPServerSpec{
			ExtraLabels:      map[string]string{"team": "platform"},
			ExtraAnnotations: map[string]string{"description": "Test server"},
		}

		card := buildServerCard(info, "http://test.default.svc.cluster.local:8080/mcp", spec)

		Expect(card).NotTo(BeNil())
		Expect(card.Name).To(Equal("test-server"))
		Expect(card.Version).To(Equal("1.0.0"))
		Expect(card.ProtocolVersion).To(Equal("2025-03-26"))
		Expect(card.Address).To(Equal("http://test.default.svc.cluster.local:8080/mcp"))
		Expect(card.Capabilities).NotTo(BeNil())
		Expect(card.Capabilities.Tools).To(BeTrue())
		Expect(card.Capabilities.Resources).To(BeTrue())
		Expect(card.Labels).To(HaveKeyWithValue("team", "platform"))
		Expect(card.Annotations).To(HaveKeyWithValue("description", "Test server"))
	})

	It("should return nil when server info is nil", func() {
		spec := &mcpv1alpha1.MCPServerSpec{}
		card := buildServerCard(nil, "http://test:8080/mcp", spec)
		Expect(card).To(BeNil())
	})

	It("should omit labels when none are specified", func() {
		info := &mcpv1alpha1.MCPServerInfo{Name: "test"}
		spec := &mcpv1alpha1.MCPServerSpec{}
		card := buildServerCard(info, "http://test:8080/mcp", spec)
		Expect(card).NotTo(BeNil())
		Expect(card.Labels).To(BeEmpty())
		Expect(card.Annotations).To(BeEmpty())
	})

	It("should omit capabilities when server info has none", func() {
		info := &mcpv1alpha1.MCPServerInfo{
			Name:    "minimal-server",
			Version: "0.1.0",
		}
		spec := &mcpv1alpha1.MCPServerSpec{}
		card := buildServerCard(info, "http://test:8080/mcp", spec)
		Expect(card).NotTo(BeNil())
		Expect(card.Name).To(Equal("minimal-server"))
		Expect(card.Capabilities).To(BeNil())
	})

	It("should include labels but not annotations when only labels are set", func() {
		info := &mcpv1alpha1.MCPServerInfo{Name: "test"}
		spec := &mcpv1alpha1.MCPServerSpec{
			ExtraLabels: map[string]string{"env": "prod"},
		}
		card := buildServerCard(info, "http://test:8080/mcp", spec)
		Expect(card).NotTo(BeNil())
		Expect(card.Labels).To(HaveKeyWithValue("env", "prod"))
		Expect(card.Annotations).To(BeEmpty())
	})
})

var _ = Describe("serverCardToAC", func() {
	It("should convert a full server card to apply configuration", func() {
		card := &mcpv1alpha1.MCPServerCard{
			Name:            "test-server",
			Version:         "1.0.0",
			ProtocolVersion: "2025-03-26",
			Address:         "http://test:8080/mcp",
			Capabilities: &mcpv1alpha1.MCPServerCapabilities{
				Tools:     true,
				Resources: true,
				Prompts:   false,
			},
			Labels:      map[string]string{"team": "platform"},
			Annotations: map[string]string{"desc": "test"},
		}

		ac := serverCardToAC(card)
		Expect(ac).NotTo(BeNil())
		Expect(*ac.Name).To(Equal("test-server"))
		Expect(*ac.Version).To(Equal("1.0.0"))
		Expect(*ac.ProtocolVersion).To(Equal("2025-03-26"))
		Expect(*ac.Address).To(Equal("http://test:8080/mcp"))
		Expect(ac.Capabilities).NotTo(BeNil())
		Expect(*ac.Capabilities.Tools).To(BeTrue())
		Expect(*ac.Capabilities.Resources).To(BeTrue())
		Expect(*ac.Capabilities.Prompts).To(BeFalse())
		Expect(ac.Labels).To(HaveKeyWithValue("team", "platform"))
		Expect(ac.Annotations).To(HaveKeyWithValue("desc", "test"))
	})

	It("should handle a minimal server card with no capabilities", func() {
		card := &mcpv1alpha1.MCPServerCard{
			Name:    "minimal",
			Address: "http://test:8080/mcp",
		}

		ac := serverCardToAC(card)
		Expect(ac).NotTo(BeNil())
		Expect(*ac.Name).To(Equal("minimal"))
		Expect(*ac.Address).To(Equal("http://test:8080/mcp"))
		Expect(ac.Capabilities).To(BeNil())
		Expect(ac.Labels).To(BeEmpty())
		Expect(ac.Annotations).To(BeEmpty())
	})
})
