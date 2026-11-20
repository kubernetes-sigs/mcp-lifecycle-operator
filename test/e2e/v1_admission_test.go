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
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
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

func TestV1AdmissionWebhook(t *testing.T) {
	t.Parallel()
	feature := features.New("v1 admission webhook validates every served MCPServer version").
		WithLabel(category.Label, category.Configuration).
		WithLabel(speed.Label, speed.Moderate).
		Assess("privileged CREATE and UPDATE are denied at all served versions", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			kube, err := kubernetes.NewForConfig(cfg.Client().RESTConfig())
			if err != nil {
				t.Fatal(err)
			}
			configuration, err := kube.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx,
				"mcp-lifecycle-operator-validating-webhook-configuration", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get deployed validating webhook configuration: %v", err)
			}
			if !slices.ContainsFunc(configuration.Webhooks, func(webhook admissionv1.ValidatingWebhook) bool {
				return webhook.Name == "vmcpserver.mcp.x-k8s.io" &&
					webhook.ClientConfig.Service != nil && webhook.ClientConfig.Service.Path != nil &&
					*webhook.ClientConfig.Service.Path == "/validate-mcp-x-k8s-io-v1-mcpserver" &&
					slices.ContainsFunc(webhook.Rules, func(rule admissionv1.RuleWithOperations) bool {
						return slices.Equal(rule.APIVersions, []string{"v1"}) &&
							slices.Contains(rule.APIGroups, "mcp.x-k8s.io") && slices.Contains(rule.Resources, "mcpservers")
					})
			}) {
				t.Fatalf("deployed validating webhook does not target the v1 MCPServer endpoint: %+v", configuration.Webhooks)
			}
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()
			probe := v1AdmissionFixture("v1", ns, "admission-ready", true)
			var lastErr error
			if err := wait.PollUntilContextTimeout(ctx, time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
				lastErr = r.Create(ctx, probe.DeepCopy(), v1AdmissionDryRunCreate)
				return v1AdmissionDenied(lastErr), nil
			}); err != nil {
				t.Fatalf("wait for v1 admission webhook: %v (last response: %v)", err, lastErr)
			}
			for _, version := range []string{"v1", "v1beta1"} {
				bad := v1AdmissionFixture(version, ns, "admission-"+version+"-bad", true)
				if err := r.Create(ctx, bad, v1AdmissionDryRunCreate); !v1AdmissionDenied(err) {
					t.Errorf("%s CREATE: want v1 webhook denial, got %v", version, err)
				}

				good := v1AdmissionFixture(version, ns, "admission-"+version+"-update", false)
				if err := r.Create(ctx, good); err != nil {
					t.Fatalf("%s CREATE of valid MCPServer: %v", version, err)
				}
				err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					current := &unstructured.Unstructured{}
					current.SetAPIVersion("mcp.x-k8s.io/" + version)
					current.SetKind("MCPServer")
					if err := r.Get(ctx, good.GetName(), ns, current); err != nil {
						return err
					}
					if err := unstructured.SetNestedField(current.Object, true, "spec", "runtime", "security", "securityContext", "privileged"); err != nil {
						return err
					}
					return r.Update(ctx, current, v1AdmissionDryRunUpdate)
				})
				if !v1AdmissionDenied(err) {
					t.Errorf("%s UPDATE: want v1 webhook denial, got %v", version, err)
				}
			}
			return ctx
		}).
		Feature()
	testenv.Test(t, feature)
}

func v1AdmissionDenied(err error) bool {
	return err != nil &&
		strings.Contains(err.Error(), `admission webhook "vmcpserver.mcp.x-k8s.io" denied the request`) &&
		strings.Contains(err.Error(), "privileged containers are not allowed")
}

func v1AdmissionFixture(version, namespace, name string, privileged bool) *unstructured.Unstructured {
	server := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "mcp.x-k8s.io/" + version,
		"kind":       "MCPServer",
		"metadata": map[string]any{
			"name": name, "namespace": namespace,
		},
		"spec": map[string]any{
			"source": map[string]any{
				"type": "ContainerImage", "containerImage": map[string]any{"ref": f.DefaultMCPServerImage},
			},
			"config": map[string]any{"port": int64(8080)},
		},
	}}
	if privileged {
		server.Object["spec"].(map[string]any)["runtime"] = map[string]any{
			"security": map[string]any{"securityContext": map[string]any{"privileged": true}},
		}
	}
	return server
}

func v1AdmissionDryRunCreate(options *metav1.CreateOptions) {
	options.DryRun = []string{metav1.DryRunAll}
}

func v1AdmissionDryRunUpdate(options *metav1.UpdateOptions) {
	options.DryRun = []string{metav1.DryRunAll}
}
