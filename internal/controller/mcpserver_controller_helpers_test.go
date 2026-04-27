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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

var _ = Describe("isSameGroupKind", func() {
	It("should return true for same group and kind with v1alpha1", func() {
		ownerRef := &metav1.OwnerReference{
			APIVersion: mcpv1alpha1.GroupVersion.String(),
			Kind:       mcpv1alpha1.MCPServerKind,
			Name:       "test-server",
			UID:        types.UID("test-uid"),
		}
		Expect(isSameGroupKind(ownerRef, mcpv1alpha1.GroupVersion.Group, mcpv1alpha1.MCPServerKind)).To(BeTrue())
	})

	It("should return true for same group and kind with different version (v1alpha2)", func() {
		ownerRef := &metav1.OwnerReference{
			APIVersion: mcpv1alpha1.GroupVersion.Group + "/v1alpha2",
			Kind:       mcpv1alpha1.MCPServerKind,
			Name:       "test-server",
			UID:        types.UID("test-uid"),
		}
		Expect(isSameGroupKind(ownerRef, mcpv1alpha1.GroupVersion.Group, mcpv1alpha1.MCPServerKind)).To(BeTrue())
	})

	It("should return true for same group and kind with stable version (v1)", func() {
		ownerRef := &metav1.OwnerReference{
			APIVersion: mcpv1alpha1.GroupVersion.Group + "/v1",
			Kind:       mcpv1alpha1.MCPServerKind,
			Name:       "test-server",
			UID:        types.UID("test-uid"),
		}
		Expect(isSameGroupKind(ownerRef, mcpv1alpha1.GroupVersion.Group, mcpv1alpha1.MCPServerKind)).To(BeTrue())
	})

	It("should return false for different group with same kind", func() {
		ownerRef := &metav1.OwnerReference{
			APIVersion: "evil.io/v1",
			Kind:       mcpv1alpha1.MCPServerKind,
			Name:       "test-server",
			UID:        types.UID("test-uid"),
		}
		Expect(isSameGroupKind(ownerRef, mcpv1alpha1.GroupVersion.Group, mcpv1alpha1.MCPServerKind)).To(BeFalse())
	})

	It("should return false for different kind with same group", func() {
		ownerRef := &metav1.OwnerReference{
			APIVersion: mcpv1alpha1.GroupVersion.String(),
			Kind:       "OtherResource",
			Name:       "test-server",
			UID:        types.UID("test-uid"),
		}
		Expect(isSameGroupKind(ownerRef, mcpv1alpha1.GroupVersion.Group, mcpv1alpha1.MCPServerKind)).To(BeFalse())
	})

	It("should return false for both different group and kind", func() {
		ownerRef := &metav1.OwnerReference{
			APIVersion: "other.io/v1",
			Kind:       "OtherResource",
			Name:       "test-server",
			UID:        types.UID("test-uid"),
		}
		Expect(isSameGroupKind(ownerRef, mcpv1alpha1.GroupVersion.Group, mcpv1alpha1.MCPServerKind)).To(BeFalse())
	})

	It("should return false for invalid APIVersion format", func() {
		ownerRef := &metav1.OwnerReference{
			APIVersion: "invalid/format/extra",
			Kind:       mcpv1alpha1.MCPServerKind,
			Name:       "test-server",
			UID:        types.UID("test-uid"),
		}
		Expect(isSameGroupKind(ownerRef, mcpv1alpha1.GroupVersion.Group, mcpv1alpha1.MCPServerKind)).To(BeFalse())
	})

	It("should return true for core API resource with empty group", func() {
		ownerRef := &metav1.OwnerReference{
			APIVersion: "v1",
			Kind:       "Pod",
			Name:       "test-pod",
			UID:        types.UID("test-uid"),
		}
		Expect(isSameGroupKind(ownerRef, "", "Pod")).To(BeTrue())
	})

	Describe("ConfigMap/Secret index extractors", func() {
		Context("extractConfigMapNames", func() {
			It("should extract ConfigMap names from storage mounts", func() {
				mcpServer := &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						Config: mcpv1alpha1.ServerConfig{
							Storage: []mcpv1alpha1.StorageMount{
								{
									Source: mcpv1alpha1.StorageSource{
										Type: mcpv1alpha1.StorageTypeConfigMap,
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "my-config",
											},
										},
									},
								},
							},
						},
					},
				}

				names := extractConfigMapNames(mcpServer)
				Expect(names).To(ConsistOf("my-config"))
			})

			It("should extract ConfigMap names from envFrom", func() {
				mcpServer := &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						Config: mcpv1alpha1.ServerConfig{
							EnvFrom: []corev1.EnvFromSource{
								{
									ConfigMapRef: &corev1.ConfigMapEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "env-config",
										},
									},
								},
							},
						},
					},
				}

				names := extractConfigMapNames(mcpServer)
				Expect(names).To(ConsistOf("env-config"))
			})

			It("should extract ConfigMap names from env valueFrom", func() {
				mcpServer := &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						Config: mcpv1alpha1.ServerConfig{
							Env: []corev1.EnvVar{
								{
									Name: "MY_VAR",
									ValueFrom: &corev1.EnvVarSource{
										ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "var-config",
											},
											Key: "some-key",
										},
									},
								},
							},
						},
					},
				}

				names := extractConfigMapNames(mcpServer)
				Expect(names).To(ConsistOf("var-config"))
			})

			It("should extract and deduplicate ConfigMap names from multiple locations", func() {
				mcpServer := &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						Config: mcpv1alpha1.ServerConfig{
							Storage: []mcpv1alpha1.StorageMount{
								{
									Source: mcpv1alpha1.StorageSource{
										Type: mcpv1alpha1.StorageTypeConfigMap,
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "config-a",
											},
										},
									},
								},
							},
							EnvFrom: []corev1.EnvFromSource{
								{
									ConfigMapRef: &corev1.ConfigMapEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "config-b",
										},
									},
								},
								{
									ConfigMapRef: &corev1.ConfigMapEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "config-a", // Duplicate
										},
									},
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "VAR",
									ValueFrom: &corev1.EnvVarSource{
										ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "config-c",
											},
											Key: "key",
										},
									},
								},
							},
						},
					},
				}

				names := extractConfigMapNames(mcpServer)
				Expect(names).To(ConsistOf("config-a", "config-b", "config-c"))
			})

			It("should return empty slice when no ConfigMaps are referenced", func() {
				mcpServer := &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						Config: mcpv1alpha1.ServerConfig{
							Port: 8080,
						},
					},
				}

				names := extractConfigMapNames(mcpServer)
				Expect(names).To(BeEmpty())
			})
		})

		Context("extractSecretNames", func() {
			It("should extract Secret names from storage mounts", func() {
				mcpServer := &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						Config: mcpv1alpha1.ServerConfig{
							Storage: []mcpv1alpha1.StorageMount{
								{
									Source: mcpv1alpha1.StorageSource{
										Type: mcpv1alpha1.StorageTypeSecret,
										Secret: &corev1.SecretVolumeSource{
											SecretName: "my-secret",
										},
									},
								},
							},
						},
					},
				}

				names := extractSecretNames(mcpServer)
				Expect(names).To(ConsistOf("my-secret"))
			})

			It("should extract Secret names from envFrom", func() {
				mcpServer := &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						Config: mcpv1alpha1.ServerConfig{
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "env-secret",
										},
									},
								},
							},
						},
					},
				}

				names := extractSecretNames(mcpServer)
				Expect(names).To(ConsistOf("env-secret"))
			})

			It("should extract Secret names from env valueFrom", func() {
				mcpServer := &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						Config: mcpv1alpha1.ServerConfig{
							Env: []corev1.EnvVar{
								{
									Name: "MY_VAR",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "var-secret",
											},
											Key: "some-key",
										},
									},
								},
							},
						},
					},
				}

				names := extractSecretNames(mcpServer)
				Expect(names).To(ConsistOf("var-secret"))
			})

			It("should extract and deduplicate Secret names from multiple locations", func() {
				mcpServer := &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						Config: mcpv1alpha1.ServerConfig{
							Storage: []mcpv1alpha1.StorageMount{
								{
									Source: mcpv1alpha1.StorageSource{
										Type: mcpv1alpha1.StorageTypeSecret,
										Secret: &corev1.SecretVolumeSource{
											SecretName: "secret-a",
										},
									},
								},
							},
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "secret-b",
										},
									},
								},
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "secret-a", // Duplicate
										},
									},
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "VAR",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "secret-c",
											},
											Key: "key",
										},
									},
								},
							},
						},
					},
				}

				names := extractSecretNames(mcpServer)
				Expect(names).To(ConsistOf("secret-a", "secret-b", "secret-c"))
			})

			It("should return empty slice when no Secrets are referenced", func() {
				mcpServer := &mcpv1alpha1.MCPServer{
					Spec: mcpv1alpha1.MCPServerSpec{
						Config: mcpv1alpha1.ServerConfig{
							Port: 8080,
						},
					},
				}

				names := extractSecretNames(mcpServer)
				Expect(names).To(BeEmpty())
			})
		})
	})
})
