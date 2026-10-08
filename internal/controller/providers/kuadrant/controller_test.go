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
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	mcpcontroller "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller"
	kuadrantapi "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/kuadrant/api"
)

const testNamespace = "default"

func TestParseAllowedExtensionNamespaces(t *testing.T) {
	cases := []struct {
		name     string
		flag     string
		flagSet  bool
		env      string
		expected []string
	}{
		{"flag unset, no env, is disabled", "", false, "", nil},
		{"flag unset falls back to env", "", false, "x, y", []string{"x", "y"}},
		{"flag set non-empty wins over env", "flagns", true, "envns", []string{"flagns"}},
		{"flag explicitly set empty disables and overrides env", "", true, "envns", nil},
		{"flag parsed, trimmed, blanks dropped", " a , b ,, c ", true, "", []string{"a", "b", "c"}},
		{"flag unset, env parsed and trimmed", "", false, " a , b ,, c ", []string{"a", "b", "c"}},
		{"only blanks yields nil", " , , ", true, "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envAllowedExtensionNamespaces, tc.env)
			got := ParseAllowedExtensionNamespaces(tc.flag, tc.flagSet)
			if !slices.Equal(got, tc.expected) {
				t.Errorf("ParseAllowedExtensionNamespaces(%q, %v) with env %q = %v, want %v",
					tc.flag, tc.flagSet, tc.env, got, tc.expected)
			}
		})
	}
}

func TestPartitionExtensionNamespaces(t *testing.T) {
	cases := []struct {
		name        string
		entries     []string
		wantValid   []string
		wantInvalid []string
	}{
		{"nil yields nothing", nil, nil, nil},
		{"all valid", []string{"gateway-ns", "team-a"}, []string{"gateway-ns", "team-a"}, nil},
		{"all invalid (fail-closed trigger)", []string{"Invalid_NS", "UPPER"}, nil, []string{"Invalid_NS", "UPPER"}},
		{"mixed valid and invalid", []string{"good", "Bad_NS", "also-good"}, []string{"good", "also-good"}, []string{"Bad_NS"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			valid, invalid := PartitionExtensionNamespaces(tc.entries)
			if !slices.Equal(valid, tc.wantValid) {
				t.Errorf("PartitionExtensionNamespaces(%v) valid = %v, want %v", tc.entries, valid, tc.wantValid)
			}
			if !slices.Equal(invalid, tc.wantInvalid) {
				t.Errorf("PartitionExtensionNamespaces(%v) invalid = %v, want %v", tc.entries, invalid, tc.wantInvalid)
			}
		})
	}
}

func TestResolveAllowedExtensionNamespaces(t *testing.T) {
	cases := []struct {
		name        string
		flag        string
		flagSet     bool
		env         string
		wantAllowed []string
		wantInvalid []string
		wantErr     bool
	}{
		{"empty is disabled, no error", "", false, "", nil, nil, false},
		{"all valid entries pass through", "a,b", true, "", []string{"a", "b"}, nil, false},
		{"mixed keeps valid and reports invalid", "good,Bad_NS", true, "", []string{"good"}, []string{"Bad_NS"}, false},
		{"non-empty but all invalid fails closed", "Bad_NS,ALSO_BAD", true, "", nil, []string{"Bad_NS", "ALSO_BAD"}, true},
		{"env fallback all invalid fails closed", "", false, "Bad_NS", nil, []string{"Bad_NS"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envAllowedExtensionNamespaces, tc.env)
			allowed, invalid, err := ResolveAllowedExtensionNamespaces(tc.flag, tc.flagSet)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ResolveAllowedExtensionNamespaces(%q, %v) err = %v, wantErr %v",
					tc.flag, tc.flagSet, err, tc.wantErr)
			}
			if !slices.Equal(allowed, tc.wantAllowed) {
				t.Errorf("allowed = %v, want %v", allowed, tc.wantAllowed)
			}
			if !slices.Equal(invalid, tc.wantInvalid) {
				t.Errorf("invalid = %v, want %v", invalid, tc.wantInvalid)
			}
		})
	}
}

func hostnamePtr(h string) *gatewayv1.Hostname {
	hostname := gatewayv1.Hostname(h)
	return &hostname
}

func ensureGatewayNamespace(ctx context.Context) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "gateway-ns"}}
	_ = k8sClient.Create(ctx, ns)
}

func newTestMCPServer(name string) *mcpv1beta1.MCPServer {
	return &mcpv1beta1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: mcpv1beta1.MCPServerSpec{
			Source: mcpv1beta1.Source{
				Type: mcpv1beta1.SourceTypeContainerImage,
				ContainerImage: &mcpv1beta1.ContainerImageSource{
					Ref: "docker.io/library/test-image:latest",
				},
			},
			Config: mcpv1beta1.ServerConfig{
				Port: 8080,
			},
		},
	}
}

func setRegistrationReady(ctx context.Context, name, namespace string) { //nolint:unparam
	reg := &kuadrantapi.MCPServerRegistration{}
	Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, reg)).To(Succeed())
	reg.Status.Conditions = []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			LastTransitionTime: metav1.Now(),
		},
	}
	Expect(k8sClient.Status().Update(ctx, reg)).To(Succeed())
}

func setHTTPRouteAccepted(ctx context.Context, route *gatewayv1.HTTPRoute) {
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(route), route)).To(Succeed())
	route.Status = gatewayv1.HTTPRouteStatus{
		RouteStatus: gatewayv1.RouteStatus{
			Parents: []gatewayv1.RouteParentStatus{
				{
					ParentRef:      route.Spec.ParentRefs[0],
					ControllerName: "gateway.example.com/controller",
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.RouteConditionAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             "Accepted",
							LastTransitionTime: metav1.Now(),
							ObservedGeneration: route.Generation,
						},
						{
							Type:               string(gatewayv1.RouteConditionResolvedRefs),
							Status:             metav1.ConditionTrue,
							Reason:             "ResolvedRefs",
							LastTransitionTime: metav1.Now(),
							ObservedGeneration: route.Generation,
						},
					},
				},
			},
		},
	}
	Expect(k8sClient.Status().Update(ctx, route)).To(Succeed())
}

func setExtensionReady(ctx context.Context, name string) {
	ext := &kuadrantapi.MCPGatewayExtension{}
	Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: "gateway-ns"}, ext)).To(Succeed())
	ext.Status.Conditions = []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			LastTransitionTime: metav1.Now(),
		},
	}
	Expect(k8sClient.Status().Update(ctx, ext)).To(Succeed())
}

var _ = Describe("Kuadrant Provider Controller", func() {
	ctx := context.Background()

	const (
		mcpServerName = "test-kuadrant-mcp"
		bindingName   = "test-kuadrant-binding"
		configMapName = "test-kuadrant-config"
	)

	newReconciler := func() *Reconciler {
		return &Reconciler{
			Client:    k8sClient,
			APIReader: k8sClient,
			Scheme:    k8sClient.Scheme(),
		}
	}

	const gatewayExtensionName = "test-gateway-extension"

	createMCPServer := func() {
		server := newTestMCPServer(mcpServerName)
		server.Spec.Config.Path = "/mcp"
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
	}

	createGatewayExtension := func(publicHost string, ready bool) {
		ensureGatewayNamespace(ctx)
		ext := &kuadrantapi.MCPGatewayExtension{
			ObjectMeta: metav1.ObjectMeta{
				Name:      gatewayExtensionName,
				Namespace: "gateway-ns",
			},
			Spec: kuadrantapi.MCPGatewayExtensionSpec{
				PublicHost: publicHost,
				TargetRef: kuadrantapi.TargetReference{
					Group:       "gateway.networking.k8s.io",
					Kind:        "Gateway",
					Name:        "my-gateway",
					Namespace:   "gateway-ns",
					SectionName: "mcps",
				},
			},
		}
		ext.SetGroupVersionKind(kuadrantapi.SchemeGroupVersion.WithKind("MCPGatewayExtension"))
		Expect(k8sClient.Create(ctx, ext)).To(Succeed())
		if ready {
			setExtensionReady(ctx, gatewayExtensionName)
		}
	}

	createConfigMap := func(data map[string]string) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: testNamespace,
			},
			Data: data,
		}
		Expect(k8sClient.Create(ctx, cm)).To(Succeed())
	}

	validConfigData := func() map[string]string {
		return map[string]string{
			configKeyExtensionName:      gatewayExtensionName,
			configKeyExtensionNamespace: "gateway-ns",
			configKeyRouteHostname:      "myserver.mcp.local",
			configKeyPrefix:             "myserver_",
		}
	}

	createBinding := func() {
		binding := &mcpv1alpha1.MCPGatewayBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: testNamespace,
			},
			Spec: mcpv1alpha1.MCPGatewayBindingSpec{
				MCPServerRef: mcpServerName,
				Provider:     ProviderName,
				ConfigRef:    configMapName,
			},
		}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())
	}

	doReconcile := func() (reconcile.Result, error) {
		r := newReconciler()
		return r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
	}

	AfterEach(func() {
		for _, obj := range []client.Object{
			&kuadrantapi.MCPServerRegistration{ObjectMeta: metav1.ObjectMeta{Name: bindingName, Namespace: testNamespace}},
			&kuadrantapi.MCPServerRegistration{ObjectMeta: metav1.ObjectMeta{Name: "conflicting-reg", Namespace: testNamespace}},
			&gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: bindingName, Namespace: testNamespace}},
			&mcpv1alpha1.MCPGatewayBinding{ObjectMeta: metav1.ObjectMeta{Name: bindingName, Namespace: testNamespace}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: testNamespace}},
			&mcpv1beta1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: mcpServerName, Namespace: testNamespace}},
		} {
			_ = k8sClient.Delete(ctx, obj)
		}
		for _, extName := range []string{gatewayExtensionName, "second-extension", "other-extension"} {
			ext := &kuadrantapi.MCPGatewayExtension{ObjectMeta: metav1.ObjectMeta{Name: extName, Namespace: "gateway-ns"}}
			_ = k8sClient.Delete(ctx, ext)
		}
	})

	It("should create HTTPRoute and MCPServerRegistration from valid binding", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", true)
		createConfigMap(validConfigData())
		createBinding()

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		By("verifying HTTPRoute was created with sectionName from extension")
		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())

		Expect(route.Spec.ParentRefs).To(HaveLen(1))
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal("my-gateway"))
		Expect(string(*route.Spec.ParentRefs[0].Namespace)).To(Equal("gateway-ns"))
		Expect(route.Spec.ParentRefs[0].SectionName).NotTo(BeNil())
		Expect(string(*route.Spec.ParentRefs[0].SectionName)).To(Equal("mcps"))

		Expect(route.Spec.Hostnames).To(HaveLen(1))
		Expect(string(route.Spec.Hostnames[0])).To(Equal("myserver.mcp.local"))

		Expect(route.Spec.Rules).To(HaveLen(1))
		Expect(route.Spec.Rules[0].BackendRefs).To(HaveLen(1))
		Expect(string(route.Spec.Rules[0].BackendRefs[0].Name)).To(Equal(mcpServerName))
		Expect(*route.Spec.Rules[0].BackendRefs[0].Port).To(Equal(gatewayv1.PortNumber(8080)))

		ownerRef := metav1.GetControllerOf(route)
		Expect(ownerRef).NotTo(BeNil())
		Expect(ownerRef.Name).To(Equal(bindingName))

		By("verifying MCPServerRegistration was created")
		reg := &kuadrantapi.MCPServerRegistration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())

		Expect(reg.Spec.TargetRef.Group).To(Equal("gateway.networking.k8s.io"))
		Expect(reg.Spec.TargetRef.Kind).To(Equal("HTTPRoute"))
		Expect(reg.Spec.TargetRef.Name).To(Equal(bindingName))
		Expect(reg.Spec.Path).To(Equal("/mcp"))
		Expect(reg.Spec.State).To(Equal("Enabled"))
		Expect(reg.Spec.Prefix).To(Equal("myserver_"))

		regOwner := metav1.GetControllerOf(reg)
		Expect(regOwner).NotTo(BeNil())
		Expect(regOwner.Name).To(Equal(bindingName))

		By("initially setting Registered=False until gateway accepts the route")
		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonRouteNotAccepted))
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		By("setting Registered=False (RegistrationNotReady) once the route is accepted but registration is not ready")
		setHTTPRouteAccepted(ctx, route)

		result, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered = meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonRegistrationNotReady))
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		By("setting Registered=True once the MCPServerRegistration is ready")
		setRegistrationReady(ctx, bindingName, testNamespace)

		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered = meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionTrue))
		Expect(binding.Status.URL).To(Equal("http://myserver.mcp.local/mcp"))
	})

	It("should not set Registered=True when MCPServerRegistration is not ready", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", true)
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		setHTTPRouteAccepted(ctx, route)

		By("setting a NotReady condition on the MCPServerRegistration")
		reg := &kuadrantapi.MCPServerRegistration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())
		reg.Status.Conditions = []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "NotReady",
				Message:            "no valid mcpgatewayextensions configured",
				LastTransitionTime: metav1.Now(),
			},
		}
		Expect(k8sClient.Status().Update(ctx, reg)).To(Succeed())

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonRegistrationNotReady))
		Expect(registered.Message).To(Equal("no valid mcpgatewayextensions configured"))
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		By("transitioning to Registered=True once the MCPServerRegistration becomes ready")
		setRegistrationReady(ctx, bindingName, testNamespace)

		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered = meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionTrue))
		Expect(binding.Status.URL).To(Equal("http://myserver.mcp.local/mcp"))
	})

	It("should auto-generate prefix as hash when prefix omitted", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", true)
		data := validConfigData()
		delete(data, configKeyPrefix)
		createConfigMap(data)
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		h := sha256.Sum256([]byte(mcpServerName + "/" + testNamespace))
		expectedPrefix := "mcp_" + hex.EncodeToString(h[:4]) + "_"

		reg := &kuadrantapi.MCPServerRegistration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())
		Expect(reg.Spec.Prefix).To(Equal(expectedPrefix))
	})

	It("should auto-generate prefix when prefix is empty string in ConfigMap", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", true)
		data := validConfigData()
		data[configKeyPrefix] = ""
		createConfigMap(data)
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		h := sha256.Sum256([]byte(mcpServerName + "/" + testNamespace))
		expectedPrefix := "mcp_" + hex.EncodeToString(h[:4]) + "_"

		reg := &kuadrantapi.MCPServerRegistration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())
		Expect(reg.Spec.Prefix).To(Equal(expectedPrefix))
	})

	It("should produce different prefixes for different name/namespace pairs", func() {
		h1 := sha256.Sum256([]byte("a-b/c"))
		h2 := sha256.Sum256([]byte("a/b-c"))
		prefix1 := "mcp_" + hex.EncodeToString(h1[:4]) + "_"
		prefix2 := "mcp_" + hex.EncodeToString(h2[:4]) + "_"
		Expect(prefix1).NotTo(Equal(prefix2))
	})

	It("should set Registered=False with RequeueAfter when auto-generated prefix conflicts", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", true)
		data := validConfigData()
		delete(data, configKeyPrefix)
		createConfigMap(data)

		h := sha256.Sum256([]byte(mcpServerName + "/" + testNamespace))
		autoPrefix := "mcp_" + hex.EncodeToString(h[:4]) + "_"

		By("creating a pre-existing MCPServerRegistration with the same prefix")
		conflictingReg := &kuadrantapi.MCPServerRegistration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "conflicting-reg",
				Namespace: testNamespace,
			},
			Spec: kuadrantapi.MCPServerRegistrationSpec{
				TargetRef: kuadrantapi.TargetReference{
					Group: "gateway.networking.k8s.io",
					Kind:  "HTTPRoute",
					Name:  "other-route",
				},
				Path:   "/mcp",
				Prefix: autoPrefix,
				State:  "Enabled",
			},
		}
		conflictingReg.SetGroupVersionKind(kuadrantapi.SchemeGroupVersion.WithKind("MCPServerRegistration"))
		Expect(k8sClient.Create(ctx, conflictingReg)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, conflictingReg) }()

		createBinding()

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(30 * time.Second))

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring(autoPrefix))
		Expect(registered.Message).To(ContainSubstring("already in use"))
		Expect(registered.Message).To(ContainSubstring(configKeyPrefix))
	})

	It("should detect conflict for explicit prefix and set Registered=False", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", true)
		data := validConfigData()
		data[configKeyPrefix] = "shared_prefix_"
		createConfigMap(data)

		By("creating a pre-existing MCPServerRegistration with the same explicit prefix")
		conflictingReg := &kuadrantapi.MCPServerRegistration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "conflicting-reg",
				Namespace: testNamespace,
			},
			Spec: kuadrantapi.MCPServerRegistrationSpec{
				TargetRef: kuadrantapi.TargetReference{
					Group: "gateway.networking.k8s.io",
					Kind:  "HTTPRoute",
					Name:  "other-route",
				},
				Path:   "/mcp",
				Prefix: "shared_prefix_",
				State:  "Enabled",
			},
		}
		conflictingReg.SetGroupVersionKind(kuadrantapi.SchemeGroupVersion.WithKind("MCPServerRegistration"))
		Expect(k8sClient.Create(ctx, conflictingReg)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, conflictingReg) }()

		createBinding()

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(30 * time.Second))

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring("shared_prefix_"))
		Expect(registered.Message).To(ContainSubstring("already in use"))
	})

	It("should not flag conflict for own registration", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", true)
		data := validConfigData()
		delete(data, configKeyPrefix)
		createConfigMap(data)
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		By("reconciling again — own registration should not be treated as conflict")
		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		reg := &kuadrantapi.MCPServerRegistration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())
		h := sha256.Sum256([]byte(mcpServerName + "/" + testNamespace))
		expectedPrefix := "mcp_" + hex.EncodeToString(h[:4]) + "_"
		Expect(reg.Spec.Prefix).To(Equal(expectedPrefix))
	})

	It("should default sectionName from extension", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", true)
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(route.Spec.ParentRefs[0].SectionName).NotTo(BeNil())
		Expect(string(*route.Spec.ParentRefs[0].SectionName)).To(Equal("mcps"))
	})

	It("should use custom sectionName when provided", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", true)
		data := validConfigData()
		data[configKeySectionName] = "custom-listener"
		createConfigMap(data)
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(string(*route.Spec.ParentRefs[0].SectionName)).To(Equal("custom-listener"))
	})

	It("should use default /mcp path when MCPServer path not set", func() {
		server := newTestMCPServer(mcpServerName)
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		createGatewayExtension("myserver.mcp.local", true)
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(route.Spec.Rules[0].Matches[0].Path.Value).NotTo(BeNil())
		Expect(*route.Spec.Rules[0].Matches[0].Path.Value).To(Equal(mcpcontroller.DefaultMCPPath))

		reg := &kuadrantapi.MCPServerRegistration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())
		Expect(reg.Spec.Path).To(Equal(mcpcontroller.DefaultMCPPath))
	})

	It("should set Registered=False when extension-name is empty string", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyExtensionName:      "",
			configKeyExtensionNamespace: "gateway-ns",
		})
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring(configKeyExtensionName))
	})

	It("should return early when MCPServer not found", func() {
		createConfigMap(validConfigData())

		binding := &mcpv1alpha1.MCPGatewayBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: testNamespace,
			},
			Spec: mcpv1alpha1.MCPGatewayBindingSpec{
				MCPServerRef: "nonexistent",
				Provider:     ProviderName,
				ConfigRef:    configMapName,
			},
		}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).To(BeNil())
	})

	It("should set Registered=False when ConfigMap missing required keys", func() {
		createMCPServer()
		createConfigMap(map[string]string{"some-key": "some-value"})
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring(configKeyExtensionName))
	})

	It("should auto-construct hostname from Gateway wildcard listener when hostname is omitted", func() {
		createMCPServer()

		By("creating a Gateway with a wildcard listener")
		gw := &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-gateway",
				Namespace: "gateway-ns",
			},
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: "test",
				Listeners: []gatewayv1.Listener{
					{
						Name:     "mcps",
						Port:     80,
						Protocol: gatewayv1.HTTPProtocolType,
						Hostname: hostnamePtr("*.mcp.local"),
					},
				},
			},
		}
		ensureGatewayNamespace(ctx)
		Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, gw) }()

		createGatewayExtension("", true)

		createConfigMap(map[string]string{
			configKeyExtensionName:      gatewayExtensionName,
			configKeyExtensionNamespace: "gateway-ns",
			configKeyPrefix:             "myserver_",
		})
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(route.Spec.Hostnames).To(HaveLen(1))
		Expect(string(route.Spec.Hostnames[0])).To(Equal(mcpServerName + "." + testNamespace + ".mcp.local"))
	})

	It("should set Registered=False when hostname omitted and listener has no wildcard", func() {
		createMCPServer()

		gw := &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-gateway",
				Namespace: "gateway-ns",
			},
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: "test",
				Listeners: []gatewayv1.Listener{
					{
						Name:     "mcps",
						Port:     80,
						Protocol: gatewayv1.HTTPProtocolType,
						Hostname: hostnamePtr("mcp.example.com"),
					},
				},
			},
		}
		ensureGatewayNamespace(ctx)
		Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, gw) }()

		createGatewayExtension("", true)

		createConfigMap(map[string]string{
			configKeyExtensionName:      gatewayExtensionName,
			configKeyExtensionNamespace: "gateway-ns",
			configKeyPrefix:             "myserver_",
		})
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring("not a wildcard"))
	})

	It("should set Registered=False when hostname omitted and listener section not found", func() {
		createMCPServer()

		gw := &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-gateway",
				Namespace: "gateway-ns",
			},
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: "test",
				Listeners: []gatewayv1.Listener{
					{
						Name:     "other-listener",
						Port:     80,
						Protocol: gatewayv1.HTTPProtocolType,
						Hostname: hostnamePtr("*.mcp.local"),
					},
				},
			},
		}
		ensureGatewayNamespace(ctx)
		Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, gw) }()

		createGatewayExtension("", true)

		createConfigMap(map[string]string{
			configKeyExtensionName:      gatewayExtensionName,
			configKeyExtensionNamespace: "gateway-ns",
			configKeyPrefix:             "myserver_",
		})
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring("no listener named"))
	})

	It("should resolve public hostname from MCPGatewayExtension", func() {
		createMCPServer()

		By("creating a Gateway with a wildcard listener")
		gw := &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-gateway",
				Namespace: "gateway-ns",
			},
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: "test",
				Listeners: []gatewayv1.Listener{
					{
						Name:     "mcps",
						Port:     80,
						Protocol: gatewayv1.HTTPProtocolType,
						Hostname: hostnamePtr("*.mcp.local"),
					},
				},
			},
		}
		ensureGatewayNamespace(ctx)
		Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, gw) }()

		createGatewayExtension("mcp.example.com", true)

		createConfigMap(map[string]string{
			configKeyExtensionName:      gatewayExtensionName,
			configKeyExtensionNamespace: "gateway-ns",
			configKeyPrefix:             "myserver_",
		})
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		setHTTPRouteAccepted(ctx, route)
		setRegistrationReady(ctx, bindingName, testNamespace)

		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionTrue))
		Expect(binding.Status.URL).To(Equal("http://mcp.example.com/mcp"))
	})

	It("should fall back to public listener hostname when MCPGatewayExtension has no publicHost", func() {
		createMCPServer()

		gw := &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-gateway",
				Namespace: "gateway-ns",
			},
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: "test",
				Listeners: []gatewayv1.Listener{
					{
						Name:     "mcps",
						Port:     443,
						Protocol: gatewayv1.HTTPSProtocolType,
						Hostname: hostnamePtr("public.mcp.example.com"),
					},
				},
			},
		}
		ensureGatewayNamespace(ctx)
		Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, gw) }()

		createGatewayExtension("", true)

		By("providing route-hostname explicitly since the listener has no wildcard")

		createConfigMap(map[string]string{
			configKeyExtensionName:      gatewayExtensionName,
			configKeyExtensionNamespace: "gateway-ns",
			configKeyRouteHostname:      "route.mcp.local",
			configKeyPrefix:             "myserver_",
		})
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		setHTTPRouteAccepted(ctx, route)
		setRegistrationReady(ctx, bindingName, testNamespace)

		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionTrue))
		Expect(binding.Status.URL).To(Equal("https://public.mcp.example.com/mcp"))
	})

	It("should set PublicAddressPending when no publicHost and no public listener hostname", func() {
		createMCPServer()

		gw := &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-gateway",
				Namespace: "gateway-ns",
			},
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: "test",
				Listeners: []gatewayv1.Listener{
					{
						Name:     "mcps",
						Port:     80,
						Protocol: gatewayv1.HTTPProtocolType,
						Hostname: hostnamePtr("*.mcp.local"),
					},
				},
			},
		}
		ensureGatewayNamespace(ctx)
		Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, gw) }()

		createGatewayExtension("", true)

		createConfigMap(map[string]string{
			configKeyExtensionName:      gatewayExtensionName,
			configKeyExtensionNamespace: "gateway-ns",
			configKeyPrefix:             "myserver_",
		})
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		setHTTPRouteAccepted(ctx, route)
		setRegistrationReady(ctx, bindingName, testNamespace)

		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(mcpcontroller.ReasonPublicAddressPending))
	})

	It("should derive scheme from public listener protocol (HTTPS)", func() {
		createMCPServer()

		gw := &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-gateway",
				Namespace: "gateway-ns",
			},
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: "test",
				Listeners: []gatewayv1.Listener{
					{
						Name:     "mcps",
						Port:     443,
						Protocol: gatewayv1.HTTPSProtocolType,
						Hostname: hostnamePtr("*.mcp.local"),
					},
				},
			},
		}
		ensureGatewayNamespace(ctx)
		Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, gw) }()

		createGatewayExtension("secure.example.com", true)

		createConfigMap(map[string]string{
			configKeyExtensionName:      gatewayExtensionName,
			configKeyExtensionNamespace: "gateway-ns",
			configKeyPrefix:             "myserver_",
		})
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		setHTTPRouteAccepted(ctx, route)
		setRegistrationReady(ctx, bindingName, testNamespace)

		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionTrue))
		Expect(binding.Status.URL).To(Equal("https://secure.example.com/mcp"))
	})

	It("should use route-hostname for HTTPRoute independently from public-hostname for status URL", func() {
		createMCPServer()

		gw := &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-gateway",
				Namespace: "gateway-ns",
			},
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: "test",
				Listeners: []gatewayv1.Listener{
					{
						Name:     "mcps",
						Port:     80,
						Protocol: gatewayv1.HTTPProtocolType,
						Hostname: hostnamePtr("*.mcp.local"),
					},
				},
			},
		}
		ensureGatewayNamespace(ctx)
		Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, gw) }()

		createGatewayExtension("public.example.com", true)

		createConfigMap(map[string]string{
			configKeyExtensionName:      gatewayExtensionName,
			configKeyExtensionNamespace: "gateway-ns",
			configKeyRouteHostname:      "internal.mcp.local",
			configKeyPrefix:             "myserver_",
		})
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		By("verifying HTTPRoute uses route-hostname")
		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(route.Spec.Hostnames).To(HaveLen(1))
		Expect(string(route.Spec.Hostnames[0])).To(Equal("internal.mcp.local"))

		setHTTPRouteAccepted(ctx, route)
		setRegistrationReady(ctx, bindingName, testNamespace)

		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		By("verifying status URL uses public-hostname from extension")
		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		Expect(binding.Status.URL).To(Equal("http://public.example.com/mcp"))
	})

	It("should prefer explicit route-hostname from ConfigMap over auto-construction", func() {
		createMCPServer()

		gw := &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-gateway",
				Namespace: "gateway-ns",
			},
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: "test",
				Listeners: []gatewayv1.Listener{
					{
						Name:     "mcps",
						Port:     80,
						Protocol: gatewayv1.HTTPProtocolType,
						Hostname: hostnamePtr("*.mcp.local"),
					},
				},
			},
		}
		ensureGatewayNamespace(ctx)
		Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, gw) }()

		createGatewayExtension("myserver.mcp.local", true)
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(route.Spec.Hostnames).To(HaveLen(1))
		Expect(string(route.Spec.Hostnames[0])).To(Equal("myserver.mcp.local"))
	})

	DescribeTable("should reject an invalid route-hostname without creating an HTTPRoute",
		func(badHostname string) {
			createMCPServer()
			createGatewayExtension("public.example.com", true)
			data := validConfigData()
			data[configKeyRouteHostname] = badHostname
			createConfigMap(data)
			createBinding()

			_, err := doReconcile()
			Expect(err).NotTo(HaveOccurred())

			By("verifying no HTTPRoute was created for the invalid hostname")
			route := &gatewayv1.HTTPRoute{}
			getErr := k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)
			Expect(apierrors.IsNotFound(getErr)).To(BeTrue())

			binding := &mcpv1alpha1.MCPGatewayBinding{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
			registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
			Expect(registered).NotTo(BeNil())
			Expect(registered.Status).To(Equal(metav1.ConditionFalse))
			Expect(registered.Message).To(ContainSubstring(configKeyRouteHostname))
		},
		Entry("with port", "mcp.example.com:8080"),
		Entry("with scheme", "https://mcp.example.com"),
		Entry("with path", "mcp.example.com/mcp"),
		Entry("uppercase", "MCP.example.com"),
		Entry("IP address", "10.0.0.1"),
	)

	It("should set Registered=False when configRef is empty", func() {
		createMCPServer()

		binding := &mcpv1alpha1.MCPGatewayBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: testNamespace,
			},
			Spec: mcpv1alpha1.MCPGatewayBindingSpec{
				MCPServerRef: mcpServerName,
				Provider:     ProviderName,
			},
		}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring("configRef is required"))
	})

	It("should update HTTPRoute when ConfigMap extension-name changes", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", true)
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal("my-gateway"))

		By("creating a new extension pointing to a different gateway")
		ensureGatewayNamespace(ctx)
		ext2 := &kuadrantapi.MCPGatewayExtension{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-extension",
				Namespace: "gateway-ns",
			},
			Spec: kuadrantapi.MCPGatewayExtensionSpec{
				PublicHost: "myserver.mcp.local",
				TargetRef: kuadrantapi.TargetReference{
					Group:       "gateway.networking.k8s.io",
					Kind:        "Gateway",
					Name:        "updated-gateway",
					Namespace:   "gateway-ns",
					SectionName: "mcps",
				},
			},
		}
		ext2.SetGroupVersionKind(kuadrantapi.SchemeGroupVersion.WithKind("MCPGatewayExtension"))
		Expect(k8sClient.Create(ctx, ext2)).To(Succeed())
		setExtensionReady(ctx, "other-extension")

		By("updating ConfigMap extension-name")
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: testNamespace}, cm)).To(Succeed())
		cm.Data[configKeyExtensionName] = "other-extension"
		Expect(k8sClient.Update(ctx, cm)).To(Succeed())

		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal("updated-gateway"))
	})

	It("should ignore HTTPRoute status from a different parent gateway", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", true)
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())

		By("setting accepted status from a different gateway")
		differentGW := gatewayv1.Namespace("other-ns")
		route.Status = gatewayv1.HTTPRouteStatus{
			RouteStatus: gatewayv1.RouteStatus{
				Parents: []gatewayv1.RouteParentStatus{
					{
						ParentRef: gatewayv1.ParentReference{
							Name:      "other-gateway",
							Namespace: &differentGW,
						},
						ControllerName: "gateway.example.com/controller",
						Conditions: []metav1.Condition{
							{
								Type:               string(gatewayv1.RouteConditionAccepted),
								Status:             metav1.ConditionTrue,
								Reason:             "Accepted",
								LastTransitionTime: metav1.Now(),
							},
							{
								Type:               string(gatewayv1.RouteConditionResolvedRefs),
								Status:             metav1.ConditionTrue,
								Reason:             "ResolvedRefs",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Status().Update(ctx, route)).To(Succeed())

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonRouteNotAccepted))
	})

	It("should ignore stale HTTPRoute conditions from a previous generation", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", true)
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		oldGeneration := route.Generation

		By("updating the route spec to bump its generation")
		newPath := "/mcp/v2"
		route.Spec.Rules[0].Matches[0].Path.Value = &newPath
		Expect(k8sClient.Update(ctx, route)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(route.Generation).To(BeNumerically(">", oldGeneration))

		By("setting accepted conditions with the old generation")
		route.Status = gatewayv1.HTTPRouteStatus{
			RouteStatus: gatewayv1.RouteStatus{
				Parents: []gatewayv1.RouteParentStatus{
					{
						ParentRef:      route.Spec.ParentRefs[0],
						ControllerName: "gateway.example.com/controller",
						Conditions: []metav1.Condition{
							{
								Type:               string(gatewayv1.RouteConditionAccepted),
								Status:             metav1.ConditionTrue,
								Reason:             "Accepted",
								LastTransitionTime: metav1.Now(),
								ObservedGeneration: oldGeneration,
							},
							{
								Type:               string(gatewayv1.RouteConditionResolvedRefs),
								Status:             metav1.ConditionTrue,
								Reason:             "ResolvedRefs",
								LastTransitionTime: metav1.Now(),
								ObservedGeneration: oldGeneration,
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Status().Update(ctx, route)).To(Succeed())

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonRouteNotAccepted))
	})

	Describe("findBindingsForConfigMap", func() {
		It("should return requests for bindings referencing the ConfigMap", func() {
			createBinding()

			r := newReconciler()
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: testNamespace,
				},
			}
			requests := r.findBindingsForConfigMap(ctx, cm)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(bindingName))
		})

		It("should map from PartialObjectMetadata (what the metadata-only watch delivers)", func() {
			createBinding()

			r := newReconciler()
			// WatchesMetadata delivers a *metav1.PartialObjectMetadata, not a full
			// ConfigMap. The handler must map using only name/namespace.
			partial := &metav1.PartialObjectMetadata{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: testNamespace,
				},
			}
			requests := r.findBindingsForConfigMap(ctx, partial)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(bindingName))
		})

		It("should not return bindings for unrelated ConfigMaps", func() {
			createBinding()

			r := newReconciler()
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unrelated-cm",
					Namespace: testNamespace,
				},
			}
			requests := r.findBindingsForConfigMap(ctx, cm)
			Expect(requests).To(BeEmpty())
		})

		It("should not return bindings with non-kuadrant provider", func() {
			binding := &mcpv1alpha1.MCPGatewayBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bindingName,
					Namespace: testNamespace,
				},
				Spec: mcpv1alpha1.MCPGatewayBindingSpec{
					MCPServerRef: mcpServerName,
					Provider:     "httproute",
					ConfigRef:    configMapName,
				},
			}
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())

			r := newReconciler()
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: testNamespace,
				},
			}
			requests := r.findBindingsForConfigMap(ctx, cm)
			Expect(requests).To(BeEmpty())
		})
	})

	It("should return early when binding is not found", func() {
		r := newReconciler()
		result, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))
	})

	It("should set Registered=False when ConfigMap does not exist", func() {
		createMCPServer()
		binding := &mcpv1alpha1.MCPGatewayBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: testNamespace,
			},
			Spec: mcpv1alpha1.MCPGatewayBindingSpec{
				MCPServerRef: mcpServerName,
				Provider:     ProviderName,
				ConfigRef:    "missing-configmap",
			},
		}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring("not found"))
	})

	It("should set Registered=False when extension-namespace is missing", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyExtensionName: gatewayExtensionName,
		})
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring(configKeyExtensionNamespace))
	})

	It("should not accept route when gateway name matches but namespace differs", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", true)
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())

		By("setting accepted status with matching name but different namespace")
		wrongNS := gatewayv1.Namespace("wrong-namespace")
		route.Status = gatewayv1.HTTPRouteStatus{
			RouteStatus: gatewayv1.RouteStatus{
				Parents: []gatewayv1.RouteParentStatus{
					{
						ParentRef: gatewayv1.ParentReference{
							Name:      gatewayv1.ObjectName("my-gateway"),
							Namespace: &wrongNS,
						},
						ControllerName: "gateway.example.com/controller",
						Conditions: []metav1.Condition{
							{
								Type:               string(gatewayv1.RouteConditionAccepted),
								Status:             metav1.ConditionTrue,
								Reason:             "Accepted",
								LastTransitionTime: metav1.Now(),
							},
							{
								Type:               string(gatewayv1.RouteConditionResolvedRefs),
								Status:             metav1.ConditionTrue,
								Reason:             "ResolvedRefs",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Status().Update(ctx, route)).To(Succeed())

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonRouteNotAccepted))
	})

	It("should skip status update when nothing changed", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", true)
		createConfigMap(validConfigData())
		createBinding()

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		result2, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(result2.RequeueAfter).To(BeNumerically(">", 0))

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonRouteNotAccepted))
	})

	It("should skip status update when reconciled twice after accepted", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", true)
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		setHTTPRouteAccepted(ctx, route)
		setRegistrationReady(ctx, bindingName, testNamespace)

		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionTrue))
	})

	It("should delete stale resources when config becomes invalid", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", true)
		createConfigMap(validConfigData())
		createBinding()

		By("first reconcile creates HTTPRoute and MCPServerRegistration")
		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		reg := &kuadrantapi.MCPServerRegistration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())

		By("deleting the ConfigMap to invalidate the config")
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: testNamespace}, cm)).To(Succeed())
		Expect(k8sClient.Delete(ctx, cm)).To(Succeed())

		By("second reconcile should delete stale resources")
		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg))).To(BeTrue())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring("not found"))
	})

	It("should not delete stale resources owned by a different controller", func() {
		createMCPServer()
		createBinding()

		foreignOwner := metav1.OwnerReference{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Name:       "foreign-owner",
			UID:        "foreign-uid",
			Controller: ptr.To(true), //nolint:modernize // new(bool) yields false, not true
		}

		foreignRoute := &gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:            bindingName,
				Namespace:       testNamespace,
				OwnerReferences: []metav1.OwnerReference{foreignOwner},
			},
			Spec: gatewayv1.HTTPRouteSpec{},
		}
		Expect(k8sClient.Create(ctx, foreignRoute)).To(Succeed())

		foreignReg := &kuadrantapi.MCPServerRegistration{
			ObjectMeta: metav1.ObjectMeta{
				Name:            bindingName,
				Namespace:       testNamespace,
				OwnerReferences: []metav1.OwnerReference{foreignOwner},
			},
			Spec: kuadrantapi.MCPServerRegistrationSpec{
				TargetRef: kuadrantapi.TargetReference{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: bindingName},
				Path:      "/mcp",
				Prefix:    "pfx_",
				State:     "Enabled",
			},
		}
		foreignReg.SetGroupVersionKind(kuadrantapi.SchemeGroupVersion.WithKind("MCPServerRegistration"))
		Expect(k8sClient.Create(ctx, foreignReg)).To(Succeed())

		By("reconciling without a ConfigMap to trigger setNotRegistered")
		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		By("verifying the foreign-owned resources were NOT deleted")
		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(metav1.GetControllerOf(route).Name).To(Equal("foreign-owner"))

		reg := &kuadrantapi.MCPServerRegistration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())
		Expect(metav1.GetControllerOf(reg).Name).To(Equal("foreign-owner"))
	})

	It("should restore ownerReference when stripped but spec unchanged", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", true)
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		By("stripping owner reference from HTTPRoute")
		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		route.OwnerReferences = nil
		Expect(k8sClient.Update(ctx, route)).To(Succeed())

		By("stripping owner reference from MCPServerRegistration")
		reg := &kuadrantapi.MCPServerRegistration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())
		reg.OwnerReferences = nil
		Expect(k8sClient.Update(ctx, reg)).To(Succeed())

		By("reconciling should restore owner references")
		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(metav1.GetControllerOf(route)).NotTo(BeNil())
		Expect(metav1.GetControllerOf(route).Name).To(Equal(bindingName))

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())
		Expect(metav1.GetControllerOf(reg)).NotTo(BeNil())
		Expect(metav1.GetControllerOf(reg).Name).To(Equal(bindingName))
	})

	Describe("findBindingsForGateway", func() {
		It("should return requests for bindings whose extension references the gateway", func() {
			createGatewayExtension("myserver.mcp.local", true)
			createConfigMap(validConfigData())
			createBinding()

			r := newReconciler()
			gw := &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-gateway",
					Namespace: "gateway-ns",
				},
			}
			requests := r.findBindingsForGateway(ctx, gw)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(bindingName))
		})

		It("should not return requests for unrelated gateways", func() {
			createGatewayExtension("myserver.mcp.local", true)
			createConfigMap(validConfigData())
			createBinding()

			r := newReconciler()
			gw := &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-gateway",
					Namespace: "gateway-ns",
				},
			}
			requests := r.findBindingsForGateway(ctx, gw)
			Expect(requests).To(BeEmpty())
		})

		It("should enqueue binding when ConfigMap read fails (avoid dropped reconcile)", func() {
			// The ConfigMap is intentionally not created, so the uncached read
			// returns NotFound. The binding must still be enqueued so the
			// idempotent Reconcile runs rather than the Gateway event being
			// silently dropped for this binding.
			createBinding()

			r := newReconciler()
			gw := &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-gateway",
					Namespace: "gateway-ns",
				},
			}
			requests := r.findBindingsForGateway(ctx, gw)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(bindingName))
		})

		It("should skip bindings with empty configRef", func() {
			binding := &mcpv1alpha1.MCPGatewayBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bindingName,
					Namespace: testNamespace,
				},
				Spec: mcpv1alpha1.MCPGatewayBindingSpec{
					MCPServerRef: mcpServerName,
					Provider:     ProviderName,
				},
			}
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())

			r := newReconciler()
			gw := &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-gateway",
					Namespace: "gateway-ns",
				},
			}
			requests := r.findBindingsForGateway(ctx, gw)
			Expect(requests).To(BeEmpty())
		})

		It("should skip bindings with non-kuadrant provider", func() {
			createConfigMap(validConfigData())
			binding := &mcpv1alpha1.MCPGatewayBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bindingName,
					Namespace: testNamespace,
				},
				Spec: mcpv1alpha1.MCPGatewayBindingSpec{
					MCPServerRef: mcpServerName,
					Provider:     "httproute",
					ConfigRef:    configMapName,
				},
			}
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())

			r := newReconciler()
			gw := &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-gateway",
					Namespace: "gateway-ns",
				},
			}
			requests := r.findBindingsForGateway(ctx, gw)
			Expect(requests).To(BeEmpty())
		})
	})

	Describe("findBindingsForGatewayExtension", func() {
		It("should enqueue bindings when extension matches ConfigMap reference", func() {
			createConfigMap(validConfigData())
			createBinding()

			ensureGatewayNamespace(ctx)
			ext := &kuadrantapi.MCPGatewayExtension{
				ObjectMeta: metav1.ObjectMeta{Name: gatewayExtensionName, Namespace: "gateway-ns"},
				Spec: kuadrantapi.MCPGatewayExtensionSpec{
					PublicHost: "public.example.com",
					TargetRef: kuadrantapi.TargetReference{
						Kind:        "Gateway",
						Name:        "my-gateway",
						SectionName: "mcp",
					},
				},
			}

			r := newReconciler()
			requests := r.findBindingsForGatewayExtension(ctx, ext)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(bindingName))
		})

		It("should not enqueue bindings for an extension with a different name", func() {
			createConfigMap(validConfigData())
			createBinding()

			ext := &kuadrantapi.MCPGatewayExtension{
				ObjectMeta: metav1.ObjectMeta{Name: "different-extension", Namespace: "gateway-ns"},
				Spec: kuadrantapi.MCPGatewayExtensionSpec{
					PublicHost: "public.example.com",
					TargetRef: kuadrantapi.TargetReference{
						Kind:        "Gateway",
						Name:        "my-gateway",
						SectionName: "mcp",
					},
				},
			}

			r := newReconciler()
			requests := r.findBindingsForGatewayExtension(ctx, ext)
			Expect(requests).To(BeEmpty())
		})

		It("should enqueue binding when ConfigMap read fails (avoid dropped reconcile)", func() {
			// The ConfigMap is intentionally not created, so the uncached read
			// returns NotFound. The binding must still be enqueued so the
			// idempotent Reconcile runs rather than the Extension event being
			// silently dropped for this binding.
			createBinding()

			ext := &kuadrantapi.MCPGatewayExtension{
				ObjectMeta: metav1.ObjectMeta{Name: gatewayExtensionName, Namespace: "gateway-ns"},
				Spec: kuadrantapi.MCPGatewayExtensionSpec{
					PublicHost: "public.example.com",
					TargetRef: kuadrantapi.TargetReference{
						Kind:        "Gateway",
						Name:        "my-gateway",
						SectionName: "mcp",
					},
				},
			}

			r := newReconciler()
			requests := r.findBindingsForGatewayExtension(ctx, ext)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(bindingName))
		})
	})

	Describe("SetupWithManager", func() {
		It("should register the controller without error", func() {
			mgr, err := ctrl.NewManager(cfg, ctrl.Options{
				Scheme: k8sClient.Scheme(),
			})
			Expect(err).NotTo(HaveOccurred())

			r := &Reconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}
			Expect(r.SetupWithManager(mgr)).To(Succeed())
		})

	})

	Describe("findBindingsForMCPServer", func() {
		It("should return requests for bindings referencing the MCPServer", func() {
			createBinding()

			r := newReconciler()
			server := &mcpv1beta1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcpServerName,
					Namespace: testNamespace,
				},
			}
			requests := r.findBindingsForMCPServer(ctx, server)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(bindingName))
		})

		It("should not return bindings for unrelated MCPServers", func() {
			createBinding()

			r := newReconciler()
			server := &mcpv1beta1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-server",
					Namespace: testNamespace,
				},
			}
			requests := r.findBindingsForMCPServer(ctx, server)
			Expect(requests).To(BeEmpty())
		})
	})

	It("should set Registered=False when extension is not found", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyExtensionName:      "nonexistent-extension",
			configKeyExtensionNamespace: "gateway-ns",
			configKeyPrefix:             "myserver_",
		})
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring("not found"))
		Expect(registered.Message).To(ContainSubstring("nonexistent-extension"))
	})

	It("should set Registered=False when extension is not ready", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", false)
		createConfigMap(validConfigData())
		createBinding()

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonExtensionNotReady))
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))
	})

	It("should proceed when extension becomes ready", func() {
		createMCPServer()
		createGatewayExtension("myserver.mcp.local", false)
		createConfigMap(validConfigData())
		createBinding()

		By("first reconcile: extension not ready")
		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonExtensionNotReady))

		By("making extension ready")
		setExtensionReady(ctx, gatewayExtensionName)

		By("second reconcile: extension ready, should proceed to create HTTPRoute")
		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(route.Spec.ParentRefs).To(HaveLen(1))
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal("my-gateway"))
	})

	It("should default sectionName from extension targetRef.SectionName", func() {
		createMCPServer()

		ensureGatewayNamespace(ctx)
		ext := &kuadrantapi.MCPGatewayExtension{
			ObjectMeta: metav1.ObjectMeta{
				Name:      gatewayExtensionName,
				Namespace: "gateway-ns",
			},
			Spec: kuadrantapi.MCPGatewayExtensionSpec{
				PublicHost: "myserver.mcp.local",
				TargetRef: kuadrantapi.TargetReference{
					Group:       "gateway.networking.k8s.io",
					Kind:        "Gateway",
					Name:        "my-gateway",
					Namespace:   "gateway-ns",
					SectionName: "custom-section",
				},
			},
		}
		ext.SetGroupVersionKind(kuadrantapi.SchemeGroupVersion.WithKind("MCPGatewayExtension"))
		Expect(k8sClient.Create(ctx, ext)).To(Succeed())
		setExtensionReady(ctx, gatewayExtensionName)

		createConfigMap(map[string]string{
			configKeyExtensionName:      gatewayExtensionName,
			configKeyExtensionNamespace: "gateway-ns",
			configKeyRouteHostname:      "myserver.mcp.local",
			configKeyPrefix:             "myserver_",
		})
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(route.Spec.ParentRefs[0].SectionName).NotTo(BeNil())
		Expect(string(*route.Spec.ParentRefs[0].SectionName)).To(Equal("custom-section"))
	})

	Describe("extension-namespace trust gate", func() {
		reconcileWithAllowlist := func(allowlist ...string) (reconcile.Result, error) {
			r := &Reconciler{
				Client:                     k8sClient,
				APIReader:                  k8sClient,
				Scheme:                     k8sClient.Scheme(),
				AllowedExtensionNamespaces: allowlist,
			}
			return r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
			})
		}

		expectRejected := func(substrings ...string) {
			By("not creating an HTTPRoute for the rejected binding")
			route := &gatewayv1.HTTPRoute{}
			getErr := k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)
			Expect(apierrors.IsNotFound(getErr)).To(BeTrue())

			binding := &mcpv1alpha1.MCPGatewayBinding{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
			registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
			Expect(registered).NotTo(BeNil())
			Expect(registered.Status).To(Equal(metav1.ConditionFalse))
			Expect(registered.Reason).To(Equal(mcpcontroller.ReasonGatewayNotRegistered))
			for _, s := range substrings {
				Expect(registered.Message).To(ContainSubstring(s))
			}
		}

		It("allows any valid extension-namespace when the allowlist is unset", func() {
			createMCPServer()
			createGatewayExtension("myserver.mcp.local", true)
			createConfigMap(validConfigData())
			createBinding()

			_, err := reconcileWithAllowlist()
			Expect(err).NotTo(HaveOccurred())

			route := &gatewayv1.HTTPRoute{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		})

		It("allows an in-allowlist extension-namespace", func() {
			createMCPServer()
			createGatewayExtension("myserver.mcp.local", true)
			createConfigMap(validConfigData())
			createBinding()

			_, err := reconcileWithAllowlist("other-trusted", "gateway-ns")
			Expect(err).NotTo(HaveOccurred())

			route := &gatewayv1.HTTPRoute{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		})

		It("rejects an out-of-allowlist extension-namespace without reading the extension", func() {
			createMCPServer()
			createGatewayExtension("myserver.mcp.local", true)
			createConfigMap(validConfigData())
			createBinding()

			_, err := reconcileWithAllowlist("trusted-ns")
			Expect(err).NotTo(HaveOccurred())

			expectRejected("gateway-ns", "allowlist")
		})

		It("rejects a syntactically invalid extension-namespace even when the allowlist is unset", func() {
			createMCPServer()
			data := validConfigData()
			data[configKeyExtensionNamespace] = "Invalid_NS"
			createConfigMap(data)
			createBinding()

			_, err := reconcileWithAllowlist()
			Expect(err).NotTo(HaveOccurred())

			expectRejected(configKeyExtensionNamespace, "Invalid_NS")
		})

		It("rejects a syntactically invalid extension-name even when the allowlist is unset", func() {
			createMCPServer()
			data := validConfigData()
			data[configKeyExtensionName] = "Invalid_Name"
			createConfigMap(data)
			createBinding()

			_, err := reconcileWithAllowlist()
			Expect(err).NotTo(HaveOccurred())

			expectRejected(configKeyExtensionName, "Invalid_Name")
		})
	})
})
