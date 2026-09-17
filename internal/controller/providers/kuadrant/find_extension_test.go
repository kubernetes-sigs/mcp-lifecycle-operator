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

package kuadrant

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kuadrantapi "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/kuadrant/api"
)

// These specs exercise the Gateway-identity matching in findMCPGatewayExtension
// with a controller-runtime fake client. Their focus is the targetRef filter
// branches that the envtest suite in controller_test.go does not vary: a
// non-empty non-Gateway group, an empty and a non-Gateway kind, and the
// namespace-defaulting fallback (refNS == "" -> ext.Namespace). The testdata CRD
// requires only name and sectionName on targetRef, so group, kind, and namespace
// are optional and cheapest to vary here. These branches decide whether an
// extension counts as targeting the Gateway at all.
//
// The 0/1/>1 outcomes are also pinned directly: they are cheap to assert without
// envtest, and the "sole match survives coexisting non-matching extensions" case
// is what proves the filter selects rather than merely counts.
var _ = Describe("findMCPGatewayExtension", func() {
	// ext builds an MCPGatewayExtension whose targetRef points at my-gateway in
	// gateway-ns by default. group, kind, and refNS override the corresponding
	// targetRef fields so individual specs can probe the filter branches.
	ext := func(name, group, kind, refNS, publicHost string) *kuadrantapi.MCPGatewayExtension {
		return &kuadrantapi.MCPGatewayExtension{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "gateway-ns"},
			Spec: kuadrantapi.MCPGatewayExtensionSpec{
				PublicHost: publicHost,
				TargetRef: kuadrantapi.TargetReference{
					Group:       group,
					Kind:        kind,
					Name:        "my-gateway",
					Namespace:   refNS,
					SectionName: "mcps",
				},
			},
		}
	}

	find := func(exts ...*kuadrantapi.MCPGatewayExtension) (*kuadrantapi.MCPGatewayExtension, error) {
		scheme := runtime.NewScheme()
		Expect(kuadrantapi.AddToScheme(scheme)).To(Succeed())
		builder := fake.NewClientBuilder().WithScheme(scheme)
		for _, e := range exts {
			builder = builder.WithObjects(e)
		}
		r := &Reconciler{Client: builder.Build(), Scheme: scheme}
		return r.findMCPGatewayExtension(context.Background(), "my-gateway", "gateway-ns")
	}

	DescribeTable("selects the single extension that targets the gateway",
		func(exts []*kuadrantapi.MCPGatewayExtension, wantName, wantHost string) {
			got, err := find(exts...)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
			Expect(got.Name).To(Equal(wantName))
			Expect(got.Spec.PublicHost).To(Equal(wantHost))
		},
		Entry("empty group defaults to the Gateway API group",
			[]*kuadrantapi.MCPGatewayExtension{ext("ext-a", "", "Gateway", "gateway-ns", "a.example.com")},
			"ext-a", "a.example.com"),
		Entry("explicit Gateway API group matches",
			[]*kuadrantapi.MCPGatewayExtension{ext("ext-a", "gateway.networking.k8s.io", "Gateway", "gateway-ns", "a.example.com")},
			"ext-a", "a.example.com"),
		Entry("omitted targetRef namespace defaults to the extension namespace",
			[]*kuadrantapi.MCPGatewayExtension{ext("ext-a", "", "Gateway", "", "a.example.com")},
			"ext-a", "a.example.com"),
		Entry("sole match is selected amid coexisting non-matching extensions",
			[]*kuadrantapi.MCPGatewayExtension{
				ext("ext-wrong-group", "example.com", "Gateway", "gateway-ns", "wrong-group.example.com"),
				ext("ext-wrong-kind", "", "HTTPRoute", "gateway-ns", "wrong-kind.example.com"),
				ext("ext-other-ns", "", "Gateway", "other-ns", "other-ns.example.com"),
				ext("ext-match", "", "Gateway", "gateway-ns", "match.example.com"),
			},
			"ext-match", "match.example.com"),
	)

	DescribeTable("returns no match",
		func(exts []*kuadrantapi.MCPGatewayExtension) {
			got, err := find(exts...)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeNil())
		},
		Entry("extension targets a Gateway in a different group",
			[]*kuadrantapi.MCPGatewayExtension{ext("ext-wrong-group", "example.com", "Gateway", "gateway-ns", "wrong.example.com")}),
		Entry("extension has an empty kind (empty kind is not defaulted)",
			[]*kuadrantapi.MCPGatewayExtension{ext("ext-empty-kind", "", "", "gateway-ns", "empty-kind.example.com")}),
		Entry("extension targets a non-Gateway kind",
			[]*kuadrantapi.MCPGatewayExtension{ext("ext-wrong-kind", "", "HTTPRoute", "gateway-ns", "wrong.example.com")}),
		Entry("extension targets a gateway in a different namespace",
			[]*kuadrantapi.MCPGatewayExtension{ext("ext-other-ns", "", "Gateway", "other-ns", "other.example.com")}),
		Entry("no extension targets the gateway", []*kuadrantapi.MCPGatewayExtension(nil)),
	)

	It("errors when multiple extensions target the same gateway", func() {
		_, err := find(
			ext("ext-a", "", "Gateway", "gateway-ns", "a.example.com"),
			ext("ext-b", "", "Gateway", "gateway-ns", "b.example.com"),
		)
		Expect(err).To(MatchError(ContainSubstring("multiple MCPGatewayExtensions target gateway")))
	})
})
