//go:build e2e

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

package e2e

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

const (
	webhookNamespace = "mcp-lifecycle-operator-system"
	webhookService   = "mcp-lifecycle-operator-webhook-service"
	webhookCert      = webhookNamespace + "/mcp-lifecycle-operator-serving-cert"
)

func isPrivilegedWebhookDenial(err error) bool {
	var statusErr *apierrors.StatusError
	return errors.As(err, &statusErr) &&
		strings.Contains(statusErr.ErrStatus.Message, `admission webhook "vmcpserver.mcp.x-k8s.io" denied the request`) &&
		strings.Contains(statusErr.ErrStatus.Message, "privileged containers are not allowed")
}

// TestV1WebhookWiring verifies the installed conversion and validating webhook
// configurations, then checks admission through both served API versions.
func TestV1WebhookWiring(t *testing.T) {
	t.Parallel()
	var crd *apiextensionsv1.CustomResourceDefinition
	var configuration *admissionv1.ValidatingWebhookConfiguration
	var validator *admissionv1.ValidatingWebhook
	var kube *kubernetes.Clientset
	feature := features.New("v1 webhook service, CA injection, and admission are wired").
		WithLabel(category.Label, category.Configuration).
		WithLabel(speed.Label, speed.Moderate).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			extensions, err := apiextensionsclient.NewForConfig(cfg.Client().RESTConfig())
			if err != nil {
				t.Fatal(err)
			}
			kube, err = kubernetes.NewForConfig(cfg.Client().RESTConfig())
			if err != nil {
				t.Fatal(err)
			}

			var lastAdmissionErr error
			ns, ok := ctx.Value(f.NsKey).(string)
			if !ok || ns == "" {
				t.Fatal("namespace not found in context; ensure BeforeEachTest has run")
			}
			probe := webhookTestMCPServer("v1", ns, "webhook-admission-ready", true)
			err = wait.PollUntilContextTimeout(ctx, time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
				currentCRD, getErr := extensions.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, "mcpservers.mcp.x-k8s.io", metav1.GetOptions{})
				if getErr != nil {
					return false, getErr
				}
				crd = currentCRD
				currentConfiguration, getErr := kube.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx,
					"mcp-lifecycle-operator-validating-webhook-configuration", metav1.GetOptions{})
				if getErr != nil {
					return false, getErr
				}
				configuration = currentConfiguration
				validator = nil
				for i := range configuration.Webhooks {
					if configuration.Webhooks[i].Name == "vmcpserver.mcp.x-k8s.io" {
						validator = &configuration.Webhooks[i]
						break
					}
				}
				caReady := crd.Spec.Conversion != nil && crd.Spec.Conversion.Webhook != nil &&
					len(crd.Spec.Conversion.Webhook.ClientConfig.CABundle) > 0 &&
					validator != nil && len(validator.ClientConfig.CABundle) > 0 &&
					bytes.Equal(crd.Spec.Conversion.Webhook.ClientConfig.CABundle, validator.ClientConfig.CABundle)
				if !caReady {
					return false, nil
				}
				lastAdmissionErr = cfg.Client().Resources().Create(ctx, probe.DeepCopy(), dryRunCreate)
				return isPrivilegedWebhookDenial(lastAdmissionErr), nil
			})
			if err != nil {
				t.Fatalf("wait for CA injection and admission readiness: %v (last admission result: %v)", err, lastAdmissionErr)
			}
			return ctx
		}).
		Assess("conversion and validation use the deployed webhook service and injected CA", func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
			conversion := crd.Spec.Conversion
			if !slices.ContainsFunc(crd.Spec.Versions, func(version apiextensionsv1.CustomResourceDefinitionVersion) bool {
				return version.Name == "v1" && version.Served && version.Storage
			}) {
				t.Errorf("deployed MCPServer CRD does not serve and store v1: %+v", crd.Spec.Versions)
			}
			if conversion.Strategy != apiextensionsv1.WebhookConverter ||
				conversion.Webhook.ClientConfig.Service == nil ||
				conversion.Webhook.ClientConfig.Service.Name != webhookService ||
				conversion.Webhook.ClientConfig.Service.Namespace != webhookNamespace ||
				conversion.Webhook.ClientConfig.Service.Path == nil ||
				*conversion.Webhook.ClientConfig.Service.Path != "/convert" ||
				!slices.Contains(conversion.Webhook.ConversionReviewVersions, "v1") {
				t.Fatalf("unexpected MCPServer conversion webhook: %+v", conversion)
			}
			if crd.Annotations["cert-manager.io/inject-ca-from"] != webhookCert {
				t.Errorf("CRD CA injection annotation = %q, want %q", crd.Annotations["cert-manager.io/inject-ca-from"], webhookCert)
			}
			if configuration.Annotations["cert-manager.io/inject-ca-from"] != webhookCert {
				t.Errorf("validating webhook CA injection annotation = %q, want %q",
					configuration.Annotations["cert-manager.io/inject-ca-from"], webhookCert)
			}
			if validator.ClientConfig.Service == nil || validator.ClientConfig.Service.Name != webhookService ||
				validator.ClientConfig.Service.Namespace != webhookNamespace ||
				validator.ClientConfig.Service.Path == nil ||
				*validator.ClientConfig.Service.Path != "/validate-mcp-x-k8s-io-v1alpha1-mcpserver" {
				t.Fatalf("unexpected MCPServer validating webhook service: %+v", validator.ClientConfig.Service)
			}
			if validator.MatchPolicy == nil || *validator.MatchPolicy != admissionv1.Equivalent ||
				validator.FailurePolicy == nil || *validator.FailurePolicy != admissionv1.Fail {
				t.Errorf("validating webhook must fail closed and match equivalent versions: %+v", validator)
			}
			if !slices.ContainsFunc(validator.Rules, func(rule admissionv1.RuleWithOperations) bool {
				return slices.Contains(rule.APIGroups, "mcp.x-k8s.io") &&
					slices.Contains(rule.APIVersions, "v1alpha1") &&
					slices.Contains(rule.Resources, "mcpservers") &&
					slices.Contains(rule.Operations, admissionv1.Create) &&
					slices.Contains(rule.Operations, admissionv1.Update)
			}) {
				t.Errorf("validating webhook lacks the MCPServer CREATE/UPDATE rule: %+v", validator.Rules)
			}
			if !bytes.Equal(conversion.Webhook.ClientConfig.CABundle, validator.ClientConfig.CABundle) {
				t.Error("conversion and validating webhooks have different injected CA bundles")
			}
			service, err := kube.CoreV1().Services(webhookNamespace).Get(ctx, webhookService, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get deployed webhook Service: %v", err)
			}
			if !slices.ContainsFunc(service.Spec.Ports, func(port corev1.ServicePort) bool {
				return port.Port == 443
			}) {
				t.Error("deployed webhook Service does not expose port 443")
			}
			return ctx
		}).
		Assess("CREATE and UPDATE admission rejects privileged settings at v1 and v1beta1", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns, ok := ctx.Value(f.NsKey).(string)
			if !ok || ns == "" {
				t.Fatal("namespace not found in context; ensure BeforeEachTest has run")
			}
			r := cfg.Client().Resources()
			for _, version := range []string{"v1", "v1beta1"} {
				badCreate := webhookTestMCPServer(version, ns, "webhook-"+version+"-create", true)
				if err := r.Create(ctx, badCreate, dryRunCreate); !isPrivilegedWebhookDenial(err) {
					t.Errorf("%s CREATE: want validating webhook rejection for privileged container, got %v", version, err)
				}
				valid := webhookTestMCPServer(version, ns, "webhook-"+version+"-update", false)
				if err := r.Create(ctx, valid); err != nil {
					t.Fatalf("%s CREATE of valid MCPServer: %v", version, err)
				}
				otherVersion := "v1"
				if version == "v1" {
					otherVersion = "v1beta1"
				}
				converted := &unstructured.Unstructured{}
				converted.SetAPIVersion("mcp.x-k8s.io/" + otherVersion)
				converted.SetKind("MCPServer")
				if err := r.Get(ctx, valid.GetName(), ns, converted); err != nil {
					t.Fatalf("read %s MCPServer at %s through conversion webhook: %v", version, otherVersion, err)
				}
				image, _, err := unstructured.NestedString(converted.Object, "spec", "source", "containerImage", "ref")
				if err != nil || image != f.DefaultMCPServerImage {
					t.Errorf("%s to %s conversion lost image: got %q, err %v", version, otherVersion, image, err)
				}
				err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
					current := &unstructured.Unstructured{}
					current.SetAPIVersion("mcp.x-k8s.io/" + version)
					current.SetKind("MCPServer")
					if err := r.Get(ctx, valid.GetName(), ns, current); err != nil {
						return err
					}
					if err := unstructured.SetNestedField(current.Object, true, "spec", "runtime", "security", "securityContext", "privileged"); err != nil {
						return err
					}
					return r.Update(ctx, current, dryRunUpdate)
				})
				if !isPrivilegedWebhookDenial(err) {
					t.Errorf("%s UPDATE: want validating webhook rejection for privileged container, got %v", version, err)
				}
			}
			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}

func webhookTestMCPServer(version, namespace, name string, privileged bool) *unstructured.Unstructured {
	server := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "mcp.x-k8s.io/" + version,
		"kind":       "MCPServer",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]any{
			"source": map[string]any{
				"type":           "ContainerImage",
				"containerImage": map[string]any{"ref": f.DefaultMCPServerImage},
			},
			"config": map[string]any{"port": int64(8080)},
		},
	}}
	if privileged {
		server.Object["spec"].(map[string]any)["runtime"] = map[string]any{
			"security": map[string]any{
				"securityContext": map[string]any{"privileged": true},
			},
		}
	}
	return server
}

func dryRunCreate(options *metav1.CreateOptions) {
	options.DryRun = []string{metav1.DryRunAll}
}

func dryRunUpdate(options *metav1.UpdateOptions) {
	options.DryRun = []string{metav1.DryRunAll}
}
