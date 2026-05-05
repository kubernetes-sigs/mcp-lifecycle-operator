package controller

import (
	"maps"
	"testing"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type want struct {
	wantErr bool
	m       map[string]string
}

type extraMetaArgs struct {
	mcp        *mcpv1alpha1.MCPServer
	deployment *appsv1.Deployment
	service    *corev1.Service
}

func Test_mergeMaps(t *testing.T) {
	tt := []struct {
		name string
		args struct {
			dst map[string]string
			src map[string]string
		}
		want want
	}{
		{
			name: "merge two maps with unique keys",
			args: struct {
				dst map[string]string
				src map[string]string
			}{
				dst: map[string]string{
					"app": "web",
					"env": "production",
				},
				src: map[string]string{
					"compontent": "proxy",
				},
			},
			want: want{
				m: map[string]string{
					"app":        "web",
					"env":        "production",
					"compontent": "proxy",
				},
				wantErr: false,
			},
		},
		{
			name: "overwriting known app labels should fail",
			args: struct {
				dst map[string]string
				src map[string]string
			}{
				dst: map[string]string{
					"app": "web",
					"env": "production",
				},
				src: map[string]string{
					"app": "proxy",
				},
			},
			want: want{
				m: map[string]string{
					"app": "web",
					"env": "production",
				},
				wantErr: false,
			},
		},
		{
			name: "overwriting custom labels should pass",
			args: struct {
				dst map[string]string
				src map[string]string
			}{
				dst: map[string]string{
					"app": "web",
					"env": "production",
				},
				src: map[string]string{
					"env": "dev",
				},
			},
			want: want{
				m: map[string]string{
					"app": "web",
					"env": "dev",
				},
				wantErr: false,
			},
		},
		{
			name: "merge defined map with empty src map",
			args: struct {
				dst map[string]string
				src map[string]string
			}{
				dst: map[string]string{
					"app": "web",
					"env": "production",
				},
				src: map[string]string{},
			},
			want: want{
				m: map[string]string{
					"app": "web",
					"env": "production",
				},
				wantErr: false,
			},
		},
		{
			name: "merge empty dst map with source map",
			args: struct {
				dst map[string]string
				src map[string]string
			}{
				dst: map[string]string{},
				src: map[string]string{
					"app": "web",
					"env": "production",
				},
			},
			want: want{
				m: map[string]string{
					"env": "production",
				},
				wantErr: false,
			},
		},
		{
			name: "merging uninitialized dst map with source map should fail",
			args: struct {
				dst map[string]string
				src map[string]string
			}{
				src: map[string]string{
					"app": "web",
					"env": "production",
				},
			},
			want: want{
				m:       map[string]string{},
				wantErr: true,
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			err := mergeMaps(tc.args.dst, tc.args.src)
			if !maps.Equal(tc.args.dst, tc.want.m) {
				t.Errorf("wanted destination map to be %v but got %v\n", tc.want, tc.args.dst)
			}
			if (err != nil) != tc.want.wantErr {
				t.Errorf("Wanted error to be %t but got %t\n", tc.want.wantErr, (err != nil))
			}
		})
	}
}

func Test_applyCustomDeploymentMetadata(t *testing.T) {
	deployment := func() *appsv1.Deployment {
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "web",
				Labels: map[string]string{
					"app": "web",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "web",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "web",
						},
					},
				},
			},
		}
	}

	tt := []struct {
		name string
		args extraMetaArgs
		want *appsv1.Deployment
	}{
		{
			name: "deployment without extra metadata",
			args: extraMetaArgs{
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{},
				},
				deployment: deployment(),
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "web",
					Labels: map[string]string{
						"app": "web",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "web",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "web",
							},
						},
					},
				},
			},
		},
		{
			name: "deployment with extra annotations",
			args: extraMetaArgs{
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						ExtraAnnotations: map[string]string{
							"department": "finance",
						},
					},
				},
				deployment: deployment(),
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "web",
					Labels: map[string]string{
						"app": "web",
					},
					Annotations: map[string]string{
						"department":                             "finance",
						"mcp.x-k8s.io/managed-extra-annotations": "{\"department\":\"finance\"}",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "web",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "web",
							},
							Annotations: map[string]string{
								"department": "finance",
							},
						},
					},
				},
			},
		},
		{
			name: "deployment with extra labels",
			args: extraMetaArgs{
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						ExtraLabels: map[string]string{
							"kubernetes.io/managed-by": "mcp-lifecyle-operator",
						},
					},
				},
				deployment: deployment(),
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "web",
					Labels: map[string]string{
						"app":                      "web",
						"kubernetes.io/managed-by": "mcp-lifecyle-operator",
					},
					Annotations: map[string]string{
						"mcp.x-k8s.io/managed-extra-labels": "{\"kubernetes.io/managed-by\":\"mcp-lifecyle-operator\"}",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "web",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app":                      "web",
								"kubernetes.io/managed-by": "mcp-lifecyle-operator",
							},
						},
					},
				},
			},
		},
		{
			name: "remove all custom labels on deployment",
			args: extraMetaArgs{
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{},
				},
				deployment: &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name: "web",
						Labels: map[string]string{
							"app":                      "web",
							"kubernetes.io/managed-by": "mcp-lifecyle-operator",
						},
						Annotations: map[string]string{
							"mcp.x-k8s.io/managed-extra-labels": "{\"kubernetes.io/managed-by\":\"mcp-lifecyle-operator\"}",
						},
					},
					Spec: appsv1.DeploymentSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": "web",
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app":                      "web",
									"kubernetes.io/managed-by": "mcp-lifecyle-operator",
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "web",
					Labels: map[string]string{
						"app": "web",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "web",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "web",
							},
						},
					},
				},
			},
		},
		{
			name: "remove some custom labels on deployment",
			args: extraMetaArgs{
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						ExtraLabels: map[string]string{
							"department": "procurement",
						},
					},
				},
				deployment: &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name: "web",
						Labels: map[string]string{
							"app":                      "web",
							"department":               "procurement",
							"kubernetes.io/managed-by": "mcp-lifecyle-operator",
						},
						Annotations: map[string]string{
							"mcp.x-k8s.io/managed-extra-labels": "{\"kubernetes.io/managed-by\":\"mcp-lifecyle-operator\", \"department\": \"procurement\"}",
						},
					},
					Spec: appsv1.DeploymentSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": "web",
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app":                      "web",
									"department":               "procurement",
									"kubernetes.io/managed-by": "mcp-lifecyle-operator",
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "web",
					Labels: map[string]string{
						"app":        "web",
						"department": "procurement",
					},
					Annotations: map[string]string{
						"mcp.x-k8s.io/managed-extra-labels": "{\"department\":\"procurement\"}",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "web",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app":        "web",
								"department": "procurement",
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			if err := applyCustomDeploymentMetadata(tc.args.mcp, tc.args.deployment); err != nil {
				t.Fatalf("applyCustomDeploymentMetadata returned unexpected error: %v", err)
			}
			if !maps.Equal(tc.args.deployment.Labels, tc.want.Labels) {
				t.Errorf("wanted deployment labels to be %v but got, %v",
					tc.want.Labels,
					tc.args.deployment.Labels,
				)
			}
			if !maps.Equal(tc.args.deployment.Annotations, tc.want.Annotations) {
				t.Errorf("wanted deployment annotation to be %v but got, %v",
					tc.want.Annotations,
					tc.args.deployment.Annotations,
				)
			}
			if !maps.Equal(tc.want.Spec.Template.Labels,
				tc.args.deployment.Spec.Template.Labels) {
				t.Errorf("wanted pod template labels to be %v but got, %v",
					tc.want.Spec.Template.Labels,
					tc.args.deployment.Spec.Template.Labels,
				)
			}
			if !maps.Equal(tc.want.Spec.Template.Annotations,
				tc.args.deployment.Spec.Template.Annotations) {
				t.Errorf("wanted pod template annotations to be %v but got, %v",
					tc.want.Spec.Template.Annotations,
					tc.args.deployment.Spec.Template.Annotations,
				)
			}
		})
	}
}

func Test_applyCustomServiceMetadata(t *testing.T) {
	tt := []struct {
		name string
		args extraMetaArgs
		want *corev1.Service
	}{
		{
			name: "render service without extra metadata",
			args: extraMetaArgs{
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{},
				},
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "webserver",
						},
					},
				},
			},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "webserver",
					},
				},
			},
		},
		{
			name: "render service with extra labels",
			args: extraMetaArgs{
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						ExtraLabels: map[string]string{
							"kubernetes.io/managed-by": "mcp-lifecycle-operator",
						},
					},
				},
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "webserver",
						},
					},
				},
			},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":                      "webserver",
						"kubernetes.io/managed-by": "mcp-lifecycle-operator",
					},
					Annotations: map[string]string{
						"mcp.x-k8s.io/managed-extra-labels": "{\"kubernetes.io/managed-by\":\"mcp-lifecycle-operator\"}",
					},
				},
			},
		},
		{
			name: "remove all custom labels on service",
			args: extraMetaArgs{
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{},
				},
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":                      "webserver",
							"kubernetes.io/managed-by": "mcp-lifecycle-operator",
						},
						Annotations: map[string]string{
							"mcp.x-k8s.io/managed-extra-labels": "{\"kubernetes.io/managed-by\":\"mcp-lifecycle-operator\"}",
						},
					},
				},
			},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "webserver",
					},
					Annotations: map[string]string{},
				},
			},
		},
		{
			name: "remove some custom labels on service",
			args: extraMetaArgs{
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						ExtraLabels: map[string]string{
							"department": "procurement",
						},
					},
				},
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":                      "webserver",
							"department":               "procurement",
							"kubernetes.io/managed-by": "mcp-lifecycle-operator",
						},
						Annotations: map[string]string{
							"mcp.x-k8s.io/managed-extra-labels": "{\"kubernetes.io/managed-by\":\"mcp-lifecycle-operator\",\"department\":\"procurement\"}",
						},
					},
				},
			},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":        "webserver",
						"department": "procurement",
					},
					Annotations: map[string]string{
						"mcp.x-k8s.io/managed-extra-labels": "{\"department\":\"procurement\"}",
					},
				},
			},
		},
		{
			name: "reserved keys in spec are not tracked or removed",
			args: extraMetaArgs{
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						ExtraLabels: map[string]string{
							"app":        "should-be-ignored",
							"mcp-server": "should-be-ignored",
							"env":        "production",
						},
					},
				},
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "webserver",
						},
					},
				},
			},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "webserver",
						"env": "production",
					},
					Annotations: map[string]string{
						"mcp.x-k8s.io/managed-extra-labels": "{\"env\":\"production\"}",
					},
				},
			},
		},
		{
			name: "render service with extra annotations",
			args: extraMetaArgs{
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						ExtraAnnotations: map[string]string{
							"department": "finance",
						},
					},
				},
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "webserver",
						},
					},
				},
			},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "webserver",
					},
					Annotations: map[string]string{
						"department":                             "finance",
						"mcp.x-k8s.io/managed-extra-annotations": "{\"department\":\"finance\"}",
					},
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			if err := applyCustomServiceMetadata(tc.args.mcp, tc.args.service); err != nil {
				t.Fatalf("applyCustomServiceMetadata returned unexpected error: %v", err)
			}
			if !maps.Equal(tc.want.Labels, tc.args.service.Labels) {
				t.Errorf("wanted service labels to be %v but got, %v",
					tc.want.Labels,
					tc.args.service.Labels,
				)
			}
			if !maps.Equal(tc.want.Annotations, tc.args.service.Annotations) {
				t.Errorf("wanted service annotations to be %v but got, %v",
					tc.want.Annotations,
					tc.args.service.Annotations,
				)
			}
		})
	}
}

func Test_deploymentLabelsChanged(t *testing.T) {
	tt := []struct {
		name string
		args *extraMetaArgs
		want bool
	}{
		{
			name: "add custom metadata to deployment",
			args: &extraMetaArgs{
				deployment: &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "webserver",
						},
						Annotations: map[string]string{},
					},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app": "webserver",
								},
							},
							Spec: corev1.PodSpec{},
						},
					},
				},
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						ExtraLabels: map[string]string{
							"department": "procurement",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "remove all custom metadata on deployment",
			args: &extraMetaArgs{
				deployment: &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":        "webserver",
							"department": "procurement",
						},
						Annotations: map[string]string{
							managedExtraLabels: "{\"department\":\"procurement\"}",
						},
					},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app":        "webserver",
									"department": "procurement",
								},
							},
							Spec: corev1.PodSpec{},
						},
					},
				},
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{},
				},
			},
			want: true,
		},
		{
			name: "no changes to custom metadata on deployment",
			args: &extraMetaArgs{
				deployment: &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":        "webserver",
							"department": "procurement",
						},
						Annotations: map[string]string{
							managedExtraLabels: "{\"department\":\"procurement\"}",
						},
					},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app":        "webserver",
									"department": "procurement",
								},
							},
							Spec: corev1.PodSpec{},
						},
					},
				},
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						ExtraLabels: map[string]string{
							"department": "procurement",
						},
					},
				},
			},
			want: false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			got := deploymentLabelsChanged(tc.args.mcp, tc.args.deployment)
			if got != tc.want {
				t.Errorf("wanted metadata changed to be %t but, got %t\n", tc.want, got)
			}
		})
	}
}

func Test_deploymentAnnotationsChanged(t *testing.T) {
	tt := []struct {
		name string
		args *extraMetaArgs
		want bool
	}{
		{
			name: "add custom metadata to deployment",
			args: &extraMetaArgs{
				deployment: &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "webserver",
						},
						Annotations: map[string]string{},
					},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app": "webserver",
								},
							},
							Spec: corev1.PodSpec{},
						},
					},
				},
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						ExtraAnnotations: map[string]string{
							"department": "procurement",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "remove all custom metadata on deployment",
			args: &extraMetaArgs{
				deployment: &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "webserver",
						},
						Annotations: map[string]string{
							managedExtraAnnotations: "{\"department\":\"procurement\"}",
							"department":            "procurement",
						},
					},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app": "webserver",
								},
								Annotations: map[string]string{
									"department": "procurement",
								},
							},
							Spec: corev1.PodSpec{},
						},
					},
				},
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{},
				},
			},
			want: true,
		},
		{
			name: "no changes to custom metadata on deployment",
			args: &extraMetaArgs{
				deployment: &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "webserver",
						},
						Annotations: map[string]string{
							managedExtraAnnotations: "{\"department\":\"procurement\"}",
							"department":            "procurement",
						},
					},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app": "webserver",
								},
								Annotations: map[string]string{
									"department": "procurement",
								},
							},
							Spec: corev1.PodSpec{},
						},
					},
				},
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						ExtraAnnotations: map[string]string{
							"department": "procurement",
						},
					},
				},
			},
			want: false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			got := deploymentAnnotationsChanged(tc.args.mcp, tc.args.deployment)
			if got != tc.want {
				t.Errorf("wanted metadata changed to be %t but, got %t\n", tc.want, got)
			}
		})
	}
}

func Test_serviceLabelsChanged(t *testing.T) {
	tt := []struct {
		name string
		args *extraMetaArgs
		want bool
	}{
		{
			name: "no custom metadata provided",
			args: &extraMetaArgs{
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						ExtraLabels: map[string]string{},
					},
				},
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "mariadb-mcp",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "custom metadata matches .spec.extraLabels",
			args: &extraMetaArgs{
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						ExtraLabels: map[string]string{
							"department": "procurement",
						},
					},
				},
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":        "mariadb-mcp",
							"department": "procurement",
						},
						Annotations: map[string]string{
							managedExtraLabels: "{\"department\":\"procurement\"}",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "service missing custom labels defined in .spec.extraLabels",
			args: &extraMetaArgs{
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						ExtraLabels: map[string]string{
							"department": "procurement",
							"env":        "production",
						},
					},
				},
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":        "mariadb-mcp",
							"department": "procurement",
						},
						Annotations: map[string]string{
							managedExtraLabels: "{\"department\":\"procurement\",\"env\": \"production\"}",
						},
					},
				},
			},
			want: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			got := serviceLabelsChanged(tc.args.mcp, tc.args.service)
			if got != tc.want {
				t.Errorf("wanted metadata changed to be %t but, got %t\n", got, tc.want)
			}
		})
	}
}

func Test_serviceAnnotationsChanged(t *testing.T) {
	tt := []struct {
		name string
		args *extraMetaArgs
		want bool
	}{
		{
			name: "no custom metadata provided",
			args: &extraMetaArgs{
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						// ExtraAnnotations: map[string]string{},
					},
				},
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "mariadb-mcp",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "custom metadata matches .spec.extraAnnotations",
			args: &extraMetaArgs{
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						ExtraAnnotations: map[string]string{
							"department": "procurement",
						},
					},
				},
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "mariadb-mcp",
						},
						Annotations: map[string]string{
							managedExtraAnnotations: "{\"department\":\"procurement\"}",
							"department":            "procurement",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "service missing custom labels defined in .spec.extraLabels",
			args: &extraMetaArgs{
				mcp: &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						ExtraAnnotations: map[string]string{
							"department": "procurement",
							"env":        "production",
						},
					},
				},
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "mariadb-mcp",
						},
						Annotations: map[string]string{
							managedExtraAnnotations: "{\"department\":\"procurement\",\"env\": \"production\"}",
							"department":            "procurement",
						},
					},
				},
			},
			want: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			got := serviceAnnotationsChanged(tc.args.mcp, tc.args.service)
			if got != tc.want {
				t.Errorf("wanted metadata changed to be %t but, got %t\n", got, tc.want)
			}
		})
	}
}
