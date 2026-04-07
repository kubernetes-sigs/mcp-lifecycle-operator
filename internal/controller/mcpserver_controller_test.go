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

// Generated from kubebuilder template:
// https://github.com/kubernetes-sigs/kubebuilder/blob/v4.11.1/pkg/plugins/golang/v4/scaffolds/internal/templates/controllers/controller_test_template.go

package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

var _ = Describe("MCPServer Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		mcpserver := &mcpv1alpha1.MCPServer{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind MCPServer")
			err := k8sClient.Get(ctx, typeNamespacedName, mcpserver)
			if err != nil && errors.IsNotFound(err) {
				resource := &mcpv1alpha1.MCPServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: mcpv1alpha1.MCPServerSpec{
						Source: mcpv1alpha1.Source{
							Type: mcpv1alpha1.SourceTypeContainerImage,
							ContainerImage: &mcpv1alpha1.ContainerImageSource{
								Ref: "docker.io/library/test-image:latest",
							},
						},
						Config: mcpv1alpha1.ServerConfig{
							Port: 8080,
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance MCPServer")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})

	Context("When reconciling a resource with env vars", func() {
		const resourceName = "test-resource-env"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						Env: []corev1.EnvVar{
							{Name: "TOKEN", Value: "test-token"},
							{Name: "LOG_LEVEL", Value: "debug"},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should propagate env vars to the deployment", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			containers := deployment.Spec.Template.Spec.Containers
			Expect(containers).To(HaveLen(1))
			envVars := containers[0].Env
			Expect(envVars).To(HaveLen(2))
			Expect(envVars).To(ContainElement(corev1.EnvVar{Name: "TOKEN", Value: "test-token"}))
			Expect(envVars).To(ContainElement(corev1.EnvVar{Name: "LOG_LEVEL", Value: "debug"}))
		})

		It("should update deployment env vars when CR is changed", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling to create the initial deployment")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Updating the MCPServer env vars")
			mcpServer := &mcpv1alpha1.MCPServer{}
			err = k8sClient.Get(ctx, typeNamespacedName, mcpServer)
			Expect(err).NotTo(HaveOccurred())
			mcpServer.Spec.Config.Env = []corev1.EnvVar{
				{Name: "TOKEN", Value: "new-token"},
				{Name: "NEW_VAR", Value: "new-value"},
			}
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			By("Reconciling again to pick up the change")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			envVars := deployment.Spec.Template.Spec.Containers[0].Env
			Expect(envVars).To(HaveLen(2))
			Expect(envVars).To(ContainElement(corev1.EnvVar{Name: "TOKEN", Value: "new-token"}))
			Expect(envVars).To(ContainElement(corev1.EnvVar{Name: "NEW_VAR", Value: "new-value"}))
		})
	})

	Context("When reconciling a resource with args", func() {
		const resourceName = "test-resource-args"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should update deployment when args are removed", func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port:      8080,
						Arguments: []string{"--verbose", "--port=8080"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling to create the initial deployment with args")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(Equal([]string{"--verbose", "--port=8080"}))

			By("Removing args from the MCPServer")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			mcpServer.Spec.Config.Arguments = nil
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			By("Reconciling again to pick up the removal")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(BeEmpty())
		})
	})

	Context("When reconciling a resource with serviceAccountName", func() {
		const resourceName = "test-resource-sa"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should update deployment when serviceAccountName is removed", func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Security: mcpv1alpha1.SecurityConfig{
							ServiceAccountName: "my-sa",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling to create the initial deployment with serviceAccountName")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal("my-sa"))

			By("Removing serviceAccountName from the MCPServer")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			// Remove the entire Runtime config to avoid MinProperties validation error
			// since RuntimeConfig only had Security set, which only had ServiceAccountName
			mcpServer.Spec.Runtime = mcpv1alpha1.RuntimeConfig{}
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			By("Reconciling again to pick up the removal")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			// When serviceAccountName is removed, we don't set it - let Kubernetes default it
			Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(BeEmpty())
		})
	})

	Context("When reconciling a resource with security context", func() {
		const resourceName = "test-resource-secctx"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should propagate container security context to the deployment", func() {
			runAsUser := int64(1001)
			runAsGroup := int64(0)
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Security: mcpv1alpha1.SecurityConfig{
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:  &runAsUser,
								RunAsGroup: &runAsGroup,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			sc := deployment.Spec.Template.Spec.Containers[0].SecurityContext
			Expect(sc).NotTo(BeNil())
			Expect(*sc.RunAsUser).To(Equal(int64(1001)))
			Expect(*sc.RunAsGroup).To(Equal(int64(0)))
		})

		It("should propagate pod security context to the deployment", func() {
			runAsUser := int64(1001)
			fsGroup := int64(1001)
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Security: mcpv1alpha1.SecurityConfig{
							PodSecurityContext: &corev1.PodSecurityContext{
								RunAsUser: &runAsUser,
								FSGroup:   &fsGroup,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			podSC := deployment.Spec.Template.Spec.SecurityContext
			Expect(podSC).NotTo(BeNil())
			Expect(*podSC.RunAsUser).To(Equal(int64(1001)))
			Expect(*podSC.FSGroup).To(Equal(int64(1001)))
		})

		It("should apply both pod and container security contexts together", func() {
			runAsUser := int64(1001)
			fsGroup := int64(1001)
			readOnly := true
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Security: mcpv1alpha1.SecurityConfig{
							PodSecurityContext: &corev1.PodSecurityContext{
								RunAsUser: &runAsUser,
								FSGroup:   &fsGroup,
							},
							SecurityContext: &corev1.SecurityContext{
								ReadOnlyRootFilesystem: &readOnly,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			podSC := deployment.Spec.Template.Spec.SecurityContext
			Expect(podSC).NotTo(BeNil())
			Expect(*podSC.RunAsUser).To(Equal(int64(1001)))
			Expect(*podSC.FSGroup).To(Equal(int64(1001)))

			containerSC := deployment.Spec.Template.Spec.Containers[0].SecurityContext
			Expect(containerSC).NotTo(BeNil())
			Expect(*containerSC.ReadOnlyRootFilesystem).To(BeTrue())
		})

		It("should apply default restricted security contexts when not specified", func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName + "-none",
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: resourceName + "-none", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName + "-none",
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying no pod security context is set by default")
			podSC := deployment.Spec.Template.Spec.SecurityContext
			if podSC != nil {
				Expect(*podSC).To(Equal(corev1.PodSecurityContext{}))
			}

			By("Verifying default container security context")
			containerSC := deployment.Spec.Template.Spec.Containers[0].SecurityContext
			Expect(containerSC).NotTo(BeNil())
			Expect(*containerSC.AllowPrivilegeEscalation).To(BeFalse())
			Expect(*containerSC.ReadOnlyRootFilesystem).To(BeTrue())
			Expect(*containerSC.RunAsNonRoot).To(BeTrue())
			Expect(containerSC.Capabilities).NotTo(BeNil())
			Expect(containerSC.Capabilities.Drop).To(ContainElement(corev1.Capability("ALL")))
			Expect(containerSC.SeccompProfile).NotTo(BeNil())
			Expect(containerSC.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName + "-none", Namespace: "default"}, mcpServer)).To(Succeed())
			Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
		})
	})

	Context("When reconciling a resource with replicas", func() {
		const resourceName = "test-resource-replicas"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should set replicas on deployment when specified", func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Replicas: ptr.To(int32(3)),
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(*deployment.Spec.Replicas).To(Equal(int32(3)))
		})

		It("should default to 1 replica when not specified", func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					// No Runtime section - replicas should default to 1
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
		})

		It("should allow 0 replicas for scale-to-zero", func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Replicas: ptr.To(int32(0)),
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(*deployment.Spec.Replicas).To(Equal(int32(0)))
		})

		It("should update deployment when replicas changes", func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Replicas: ptr.To(int32(2)),
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling to create the initial deployment with 2 replicas")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(*deployment.Spec.Replicas).To(Equal(int32(2)))

			By("Updating replicas to 5")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			mcpServer.Spec.Runtime.Replicas = ptr.To(int32(5))
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			By("Reconciling again to pick up the change")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(*deployment.Spec.Replicas).To(Equal(int32(5)))
		})

		It("should update deployment when replicas is removed", func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Replicas: ptr.To(int32(3)),
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling to create the initial deployment with 3 replicas")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(*deployment.Spec.Replicas).To(Equal(int32(3)))

			By("Removing replicas from the MCPServer")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			mcpServer.Spec.Runtime.Replicas = nil
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			By("Reconciling again to pick up the removal")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
		})

		It("should correctly handle MCPServer status after spec update", func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Replicas: ptr.To(int32(1)),
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Initial reconciliation creates deployment with 1 replica")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))

			By("Simulating deployment becoming available")
			deployment.Status.Replicas = 1
			deployment.Status.ReadyReplicas = 1
			deployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   appsv1.DeploymentProgressing,
					Status: corev1.ConditionTrue,
				},
			}
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("Reconciling to update MCPServer status to Ready=True")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCondition.Reason).To(Equal(ReasonAvailable))

			By("Updating replicas to 3")
			mcpServer.Spec.Runtime.Replicas = ptr.To(int32(3))
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			By("Reconciling after spec update")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying deployment spec was updated")
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(*deployment.Spec.Replicas).To(Equal(int32(3)))

			By("Verifying MCPServer status reflects current deployment state")
			// This is the critical test that would have caught the bug:
			// Without the fix, reconcileDeployment would return deployment with stale status,
			// causing determineReadyCondition to incorrectly report DeploymentUnavailable
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			readyCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			// The deployment is still available (we haven't changed its status),
			// so Ready should remain True, not incorrectly flip to False
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCondition.Reason).To(Equal(ReasonAvailable))
		})
	})

	Context("When Deployment is unavailable", func() {
		const resourceName = "test-resource-requeue"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should requeue reconciliation when Deployment is unavailable", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Initial reconciliation creates deployment")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Simulating deployment being unavailable (progressing but not ready)")
			deployment.Status.Replicas = 1
			deployment.Status.ReadyReplicas = 0
			deployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentProgressing,
					Status: corev1.ConditionTrue,
					Reason: "NewReplicaSetCreated",
				},
			}
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("Reconciling should set Ready=False and requeue")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(result.RequeueAfter).To(Equal(15 * time.Second))

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal(ReasonDeploymentUnavailable))
		})

		It("should NOT requeue when Deployment becomes available", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Initial reconciliation creates deployment")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Simulating deployment becoming available")
			deployment.Status.Replicas = 1
			deployment.Status.ReadyReplicas = 1
			deployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   appsv1.DeploymentProgressing,
					Status: corev1.ConditionTrue,
				},
			}
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("Reconciling should set Ready=True and NOT requeue")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Should NOT requeue when available
			Expect(result.RequeueAfter).To(BeZero())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCondition.Reason).To(Equal(ReasonAvailable))
		})

		It("should eventually reach Ready=True after Deployment becomes available", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Initial reconciliation creates deployment")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Deployment starts unavailable")
			deployment.Status.Replicas = 1
			deployment.Status.ReadyReplicas = 0
			deployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentProgressing,
					Status: corev1.ConditionTrue,
					Reason: "NewReplicaSetCreated",
				},
			}
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("First reconciliation: unavailable, requeue")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(15 * time.Second))

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))

			By("Deployment becomes available")
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			deployment.Status.ReadyReplicas = 1
			deployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   appsv1.DeploymentProgressing,
					Status: corev1.ConditionTrue,
				},
			}
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("Second reconciliation: available, no requeue, Ready=True")
			result, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			readyCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCondition.Reason).To(Equal(ReasonAvailable))
		})
	})

	Context("When reconciling a resource with envFrom", func() {
		const resourceName = "test-resource-envfrom"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						EnvFrom: []corev1.EnvFromSource{
							{
								SecretRef: &corev1.SecretEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "my-secret"},
								},
							},
							{
								ConfigMapRef: &corev1.ConfigMapEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "my-configmap"},
								},
							},
						},
					},
				},
			}
			// Create the referenced Secret and ConfigMap so envFrom validation passes
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "default"},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "my-configmap", Namespace: "default"},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			// Clean up the referenced Secret and ConfigMap
			Expect(k8sClient.Delete(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "default"},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "my-configmap", Namespace: "default"},
			})).To(Succeed())
		})

		It("should propagate envFrom to the deployment", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			containers := deployment.Spec.Template.Spec.Containers
			Expect(containers).To(HaveLen(1))

			envFrom := containers[0].EnvFrom
			Expect(envFrom).To(HaveLen(2))
			Expect(envFrom[0].SecretRef).NotTo(BeNil())
			Expect(envFrom[0].SecretRef.Name).To(Equal("my-secret"))
			Expect(envFrom[1].ConfigMapRef).NotTo(BeNil())
			Expect(envFrom[1].ConfigMapRef.Name).To(Equal("my-configmap"))
		})

		It("should support both env and envFrom together", func() {
			By("Updating the CR to also include env vars")
			mcpServer := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, mcpServer)
			Expect(err).NotTo(HaveOccurred())
			mcpServer.Spec.Config.Env = []corev1.EnvVar{
				{Name: "EXTRA_VAR", Value: "extra-value"},
			}
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(HaveLen(1))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: "EXTRA_VAR", Value: "extra-value"}))
			Expect(container.EnvFrom).To(HaveLen(2))
			Expect(container.EnvFrom[0].SecretRef.Name).To(Equal("my-secret"))
			Expect(container.EnvFrom[1].ConfigMapRef.Name).To(Equal("my-configmap"))
		})
	})

	Context("When reconciling a resource with a missing envFrom reference", func() {
		const resourceName = "test-resource-envfrom-missing"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						EnvFrom: []corev1.EnvFromSource{
							{
								ConfigMapRef: &corev1.ConfigMapEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "nonexistent-configmap"},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should set Accepted=False when envFrom references a missing ConfigMap", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			// No error should be returned - configuration issues are reported via status conditions
			Expect(err).NotTo(HaveOccurred())

			// Verify no Deployment was created
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			// Verify MCPServer status has correct conditions
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
			Expect(acceptedCondition.Message).To(ContainSubstring("nonexistent-configmap"))

			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal(ReasonConfigurationInvalid))
		})

		It("should skip validation when envFrom reference is optional", func() {
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			optional := true
			mcpServer.Spec.Config.EnvFrom[0].ConfigMapRef.Optional = &optional
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)).To(Succeed())
		})
	})

	Context("When reconciling a resource with a missing envFrom Secret reference", func() {
		const resourceName = "test-resource-envfrom-missing-secret"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						EnvFrom: []corev1.EnvFromSource{
							{
								SecretRef: &corev1.SecretEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "nonexistent-secret"},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should set Accepted=False when envFrom references a missing Secret", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			// No error should be returned - configuration issues are reported via status conditions
			Expect(err).NotTo(HaveOccurred())

			// Verify no Deployment was created
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			// Verify MCPServer status has correct conditions
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
			Expect(acceptedCondition.Message).To(ContainSubstring("nonexistent-secret"))

			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal(ReasonConfigurationInvalid))
		})

		It("should skip validation when envFrom Secret reference is optional", func() {
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			optional := true
			mcpServer.Spec.Config.EnvFrom[0].SecretRef.Optional = &optional
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)).To(Succeed())
		})

		It("should preserve Accepted condition LastTransitionTime across reconciliations", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconciliation - should set Accepted condition
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Get the initial Accepted condition timestamp
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			initialTimestamp := acceptedCondition.LastTransitionTime

			// Wait a bit to ensure time difference would be detectable
			time.Sleep(100 * time.Millisecond)

			// Second reconciliation - Accepted status hasn't changed, timestamp should be preserved
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify the LastTransitionTime was preserved
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.LastTransitionTime).To(Equal(initialTimestamp),
				"Accepted condition LastTransitionTime should be preserved when status doesn't change")
		})
	})
})

var _ = Describe("MCPServer Controller - Address URL", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-address"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should set the address URL with default path after reconciliation", func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Replicas: ptr.To(int32(1)),
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			Expect(mcpServer.Status.Address).NotTo(BeNil())
			Expect(mcpServer.Status.Address.URL).To(Equal("http://test-address.default.svc.cluster.local:8080/mcp"))
		})

		It("should use the correct port in the address URL", func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 3001,
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			Expect(mcpServer.Status.Address).NotTo(BeNil())
			Expect(mcpServer.Status.Address.URL).To(Equal("http://test-address.default.svc.cluster.local:3001/mcp"))
		})

		It("should use custom path in the address URL when specified", func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						Path: "/sse",
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			Expect(mcpServer.Status.Address).NotTo(BeNil())
			Expect(mcpServer.Status.Address.URL).To(Equal("http://test-address.default.svc.cluster.local:8080/sse"))
		})

		It("should persist the address URL across reconciliations", func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Replicas: ptr.To(int32(1)),
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			Expect(mcpServer.Status.Address).NotTo(BeNil())
			Expect(mcpServer.Status.Address.URL).To(Equal("http://test-address.default.svc.cluster.local:8080/mcp"))
		})
	})
})

var _ = Describe("MCPServer Controller - Service Update", func() {
	Context("When port changes", func() {
		const resourceName = "test-service-update"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should update the Service port when config.port changes", func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the initial Service port")
			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, svc)).To(Succeed())
			Expect(svc.Spec.Ports).To(HaveLen(1))
			Expect(svc.Spec.Ports[0].Port).To(Equal(int32(8080)))

			By("Updating the port in the MCPServer spec")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			mcpServer.Spec.Config.Port = 9090
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			By("Reconciling again to pick up the port change")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the Service port was updated")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, svc)).To(Succeed())
			Expect(svc.Spec.Ports).To(HaveLen(1))
			Expect(svc.Spec.Ports[0].Port).To(Equal(int32(9090)))

			By("Verifying the Deployment container port was also updated")
			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, dep)).To(Succeed())
			Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(dep.Spec.Template.Spec.Containers[0].Ports).To(HaveLen(1))
			Expect(dep.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort).To(Equal(int32(9090)))
		})
	})
})

var _ = Describe("MCPServer Controller - reconcileDeployment", func() {
	const resourceName = "test-reconcile-deployment"

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	BeforeEach(func() {
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
			},
			Spec: mcpv1alpha1.MCPServerSpec{
				Source: mcpv1alpha1.Source{
					Type: mcpv1alpha1.SourceTypeContainerImage,
					ContainerImage: &mcpv1alpha1.ContainerImageSource{
						Ref: "docker.io/library/test-image:latest",
					},
				},
				Config: mcpv1alpha1.ServerConfig{
					Port: 8080,
				},
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
	})

	AfterEach(func() {
		resource := &mcpv1alpha1.MCPServer{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("should create a deployment when none exists", func() {
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		deployment, err := reconciler.reconcileDeployment(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment).NotTo(BeNil())
		Expect(deployment.Name).To(Equal(resourceName))
	})

	It("should return existing deployment without error on second call", func() {
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		_, err := reconciler.reconcileDeployment(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())

		deployment, err := reconciler.reconcileDeployment(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment).NotTo(BeNil())
	})

	It("should recover when existing deployment has empty containers list", func() {
		By("Setting up a fake client with a deployment that has no containers")
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-empty-containers",
				Namespace: "default",
				UID:       "fake-uid",
			},
			Spec: mcpv1alpha1.MCPServerSpec{
				Source: mcpv1alpha1.Source{
					Type: mcpv1alpha1.SourceTypeContainerImage,
					ContainerImage: &mcpv1alpha1.ContainerImageSource{
						Ref: "docker.io/library/test-image:latest",
					},
				},
				Config: mcpv1alpha1.ServerConfig{
					Port: 8080,
				},
			},
		}

		brokenDeployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-empty-containers",
				Namespace: "default",
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"mcp-server": "test-empty-containers"},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":        "mcp-server",
							"mcp-server": "test-empty-containers",
						},
					},
					Spec: corev1.PodSpec{
						Containers: nil,
					},
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(k8sClient.Scheme()).
			WithObjects(mcpServer, brokenDeployment).
			Build()

		reconciler := &MCPServerReconciler{
			Client: fakeClient,
			Scheme: k8sClient.Scheme(),
		}

		By("Reconciling should not panic and should restore the containers")
		deployment, err := reconciler.reconcileDeployment(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("docker.io/library/test-image:latest"))
	})
})

var _ = Describe("MCPServer Controller - Deployment Reconciliation Failures", func() {
	const resourceName = "test-deployment-failure"

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	BeforeEach(func() {
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
			},
			Spec: mcpv1alpha1.MCPServerSpec{
				Source: mcpv1alpha1.Source{
					Type: mcpv1alpha1.SourceTypeContainerImage,
					ContainerImage: &mcpv1alpha1.ContainerImageSource{
						Ref: "docker.io/library/test-image:latest",
					},
				},
				Config: mcpv1alpha1.ServerConfig{
					Port: 8080,
				},
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
	})

	AfterEach(func() {
		resource := &mcpv1alpha1.MCPServer{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		if err == nil {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		}
	})

	It("should update status with DeploymentUnavailable when deployment creation fails", func() {
		By("Creating interceptor that returns error on Deployment Create")
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*appsv1.Deployment); ok {
					return fmt.Errorf("simulated deployment creation failure")
				}
				return c.Create(ctx, obj, opts...)
			},
		})

		deploymentFailureReconciler := &MCPServerReconciler{
			Client: interceptedClient,
			Scheme: k8sClient.Scheme(),
		}

		By("Reconciling with deployment creation failure")
		_, err = deploymentFailureReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("simulated deployment creation failure"))

		By("Verifying status is updated with DeploymentUnavailable")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
		Expect(acceptedCondition).NotTo(BeNil())
		Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(acceptedCondition.Reason).To(Equal("Valid"))

		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(readyCondition.Reason).To(Equal(ReasonDeploymentUnavailable))
		Expect(readyCondition.Message).To(ContainSubstring("Failed to reconcile Deployment"))
		Expect(readyCondition.Message).To(ContainSubstring("simulated deployment creation failure"))
	})

	It("should update status with DeploymentUnavailable when deployment update fails", func() {
		By("Initial reconcile to create resources")
		initialReconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		_, err := initialReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying deployment was created")
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, deployment)).To(Succeed())

		By("Creating interceptor that returns error on Deployment Update")
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*appsv1.Deployment); ok {
					return fmt.Errorf("simulated deployment update failure")
				}
				return c.Update(ctx, obj, opts...)
			},
		})

		deploymentFailureReconciler := &MCPServerReconciler{
			Client: interceptedClient,
			Scheme: k8sClient.Scheme(),
		}

		By("Updating MCPServer spec to trigger deployment reconciliation")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Config.Env = []corev1.EnvVar{{Name: "TEST_VAR", Value: "test_value"}}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Reconciling with deployment update failure")
		_, err = deploymentFailureReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("simulated deployment update failure"))

		By("Verifying status is updated with DeploymentUnavailable")
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
		Expect(acceptedCondition).NotTo(BeNil())
		Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(acceptedCondition.Reason).To(Equal("Valid"))

		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(readyCondition.Reason).To(Equal(ReasonDeploymentUnavailable))
		Expect(readyCondition.Message).To(ContainSubstring("Failed to reconcile Deployment"))
		Expect(readyCondition.Message).To(ContainSubstring("simulated deployment update failure"))
	})
})

var _ = Describe("MCPServer Controller - reconcileService", func() {
	const resourceName = "test-reconcile-service"

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	BeforeEach(func() {
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
			},
			Spec: mcpv1alpha1.MCPServerSpec{
				Source: mcpv1alpha1.Source{
					Type: mcpv1alpha1.SourceTypeContainerImage,
					ContainerImage: &mcpv1alpha1.ContainerImageSource{
						Ref: "docker.io/library/test-image:latest",
					},
				},
				Config: mcpv1alpha1.ServerConfig{
					Port: 8080,
				},
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
	})

	AfterEach(func() {
		resource := &mcpv1alpha1.MCPServer{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("should create a service when none exists", func() {
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		err := reconciler.reconcileService(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())

		svc := &corev1.Service{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, svc)
		Expect(err).NotTo(HaveOccurred())
		Expect(svc.Name).To(Equal(resourceName))
	})

	It("should not error when service already exists", func() {
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		Expect(reconciler.reconcileService(ctx, mcpServer)).To(Succeed())
		Expect(reconciler.reconcileService(ctx, mcpServer)).To(Succeed())
	})
})

var _ = Describe("MCPServer Controller - Service Reconciliation Failures", func() {
	const resourceName = "test-service-failure"

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	BeforeEach(func() {
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
			},
			Spec: mcpv1alpha1.MCPServerSpec{
				Source: mcpv1alpha1.Source{
					Type: mcpv1alpha1.SourceTypeContainerImage,
					ContainerImage: &mcpv1alpha1.ContainerImageSource{
						Ref: "docker.io/library/test-image:latest",
					},
				},
				Config: mcpv1alpha1.ServerConfig{
					Port: 8080,
				},
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
	})

	AfterEach(func() {
		resource := &mcpv1alpha1.MCPServer{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		if err == nil {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		}
	})

	It("should update status with ServiceUnavailable when service creation fails", func() {
		By("Creating interceptor that returns error on Service Create")
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*corev1.Service); ok {
					return fmt.Errorf("simulated service creation failure")
				}
				return c.Create(ctx, obj, opts...)
			},
		})

		serviceFailureReconciler := &MCPServerReconciler{
			Client: interceptedClient,
			Scheme: k8sClient.Scheme(),
		}

		By("Reconciling with service creation failure")
		_, err = serviceFailureReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("simulated service creation failure"))

		By("Verifying status is updated with ServiceUnavailable")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
		Expect(acceptedCondition).NotTo(BeNil())
		Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(acceptedCondition.Reason).To(Equal("Valid"))

		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(readyCondition.Reason).To(Equal(ReasonServiceUnavailable))
		Expect(readyCondition.Message).To(ContainSubstring("Failed to reconcile Service"))
		Expect(readyCondition.Message).To(ContainSubstring("simulated service creation failure"))

		Expect(mcpServer.Status.DeploymentName).To(Equal(resourceName))
	})

	It("should update status with ServiceUnavailable when service update fails", func() {
		By("Initial reconcile to create resources")
		initialReconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		_, err := initialReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying service was created")
		svc := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, svc)).To(Succeed())

		By("Creating interceptor that returns error on Service Update")
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*corev1.Service); ok {
					return fmt.Errorf("simulated service update failure")
				}
				return c.Update(ctx, obj, opts...)
			},
		})

		serviceFailureReconciler := &MCPServerReconciler{
			Client: interceptedClient,
			Scheme: k8sClient.Scheme(),
		}

		By("Updating MCPServer spec to trigger service reconciliation")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Config.Port = 9090
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Reconciling with service update failure")
		_, err = serviceFailureReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("simulated service update failure"))

		By("Verifying status is updated with ServiceUnavailable")
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
		Expect(acceptedCondition).NotTo(BeNil())
		Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(acceptedCondition.Reason).To(Equal("Valid"))

		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(readyCondition.Reason).To(Equal(ReasonServiceUnavailable))
		Expect(readyCondition.Message).To(ContainSubstring("Failed to reconcile Service"))
		Expect(readyCondition.Message).To(ContainSubstring("simulated service update failure"))

		Expect(mcpServer.Status.DeploymentName).To(Equal(resourceName))
	})
})

var _ = Describe("determineReadyCondition", func() {
	var generation int64 = 1
	var acceptedCondition metav1.Condition

	BeforeEach(func() {
		// Default to valid configuration
		acceptedCondition = metav1.Condition{
			Type:   ConditionTypeAccepted,
			Status: metav1.ConditionTrue,
			Reason: ReasonValid,
		}
	})

	It("should return Initializing when deployment has no conditions and no ready replicas", func() {
		deployment := &appsv1.Deployment{
			Status: appsv1.DeploymentStatus{},
		}
		condition := determineReadyCondition(deployment, acceptedCondition, generation, make([]metav1.Condition, 0))
		Expect(condition.Reason).To(Equal(ReasonInitializing))
		Expect(condition.Status).To(Equal(metav1.ConditionUnknown))
	})

	It("should return Available when deployment is available with ready replicas", func() {
		deployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To[int32](1),
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas: 1,
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		condition := determineReadyCondition(deployment, acceptedCondition, generation, make([]metav1.Condition, 0))
		Expect(condition.Reason).To(Equal(ReasonAvailable))
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
	})

	It("should return DeploymentUnavailable when deployment has replica failure", func() {
		deployment := &appsv1.Deployment{
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:    appsv1.DeploymentReplicaFailure,
						Status:  corev1.ConditionTrue,
						Message: "replica failed",
					},
				},
			},
		}
		condition := determineReadyCondition(deployment, acceptedCondition, generation, make([]metav1.Condition, 0))
		Expect(condition.Reason).To(Equal(ReasonDeploymentUnavailable))
		Expect(condition.Message).To(ContainSubstring("replica failed"))
	})

	It("should return DeploymentUnavailable when deployment is progressing", func() {
		deployment := &appsv1.Deployment{
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentProgressing,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		condition := determineReadyCondition(deployment, acceptedCondition, generation, make([]metav1.Condition, 0))
		Expect(condition.Reason).To(Equal(ReasonDeploymentUnavailable))
	})

	It("should return ConfigurationInvalid when configuration is not accepted", func() {
		invalidAcceptedCondition := metav1.Condition{
			Type:   ConditionTypeAccepted,
			Status: metav1.ConditionFalse,
			Reason: ReasonInvalid,
		}
		deployment := &appsv1.Deployment{}
		condition := determineReadyCondition(deployment, invalidAcceptedCondition, generation, make([]metav1.Condition, 0))
		Expect(condition.Reason).To(Equal(ReasonConfigurationInvalid))
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	})

	It("should return Ready=True with ScaledToZero reason when deployment is scaled to 0 replicas", func() {
		deployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To[int32](0),
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas: 0,
			},
		}
		condition := determineReadyCondition(deployment, acceptedCondition, generation, make([]metav1.Condition, 0))
		Expect(condition.Reason).To(Equal(ReasonScaledToZero))
		// Ready=True following Kubernetes Deployment semantics: replicas=0 is a valid desired state
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Message).To(ContainSubstring("scaled to 0 replicas"))
	})

	It("should preserve LastTransitionTime when condition status hasn't changed", func() {
		// Create an existing condition with a specific timestamp
		pastTime := metav1.NewTime(metav1.Now().Add(-5 * time.Minute))
		existingConditions := []metav1.Condition{
			{
				Type:               ConditionTypeReady,
				Status:             metav1.ConditionFalse,
				Reason:             ReasonDeploymentUnavailable,
				LastTransitionTime: pastTime,
			},
		}

		// Create a deployment that would result in the same condition
		deployment := &appsv1.Deployment{
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentProgressing,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		condition := determineReadyCondition(deployment, acceptedCondition, generation, existingConditions)

		// The LastTransitionTime should be preserved from the existing condition
		Expect(condition.LastTransitionTime).To(Equal(pastTime))
	})

	It("should update LastTransitionTime when condition status changes", func() {
		// Create an existing condition with Status=False
		pastTime := metav1.NewTime(metav1.Now().Add(-5 * time.Minute))
		existingConditions := []metav1.Condition{
			{
				Type:               ConditionTypeReady,
				Status:             metav1.ConditionFalse,
				Reason:             ReasonInitializing,
				LastTransitionTime: pastTime,
			},
		}

		// Create a deployment that would result in Status=True (different status)
		deployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To[int32](1),
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas: 1,
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		condition := determineReadyCondition(deployment, acceptedCondition, generation, existingConditions)

		// The LastTransitionTime should be NEW (not the past time)
		Expect(condition.LastTransitionTime).NotTo(Equal(pastTime))
	})

	It("should handle nil replicas gracefully when deployment is available", func() {
		// Create a deployment with nil replicas (tests the ptr.Deref fix)
		deployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Replicas: nil, // nil replicas should default to 1 in the message
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas: 1,
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		condition := determineReadyCondition(deployment, acceptedCondition, generation, make([]metav1.Condition, 0))

		// Should succeed without panicking
		Expect(condition.Reason).To(Equal(ReasonAvailable))
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		// Message should use default value of 1 for nil replicas
		Expect(condition.Message).To(ContainSubstring("1 of 1 instances healthy"))
	})
})

var _ = Describe("analyzeDeploymentFailure", func() {
	It("should identify ImagePullBackOff errors", func() {
		message := "Back-off pulling image \"nonexistent:latest\": ImagePullBackOff"
		result := analyzeDeploymentFailure(message)
		Expect(result).To(ContainSubstring("ImagePullBackOff"))
	})

	It("should identify ErrImagePull errors", func() {
		message := "Failed to pull image: ErrImagePull"
		result := analyzeDeploymentFailure(message)
		Expect(result).To(ContainSubstring("ImagePullBackOff"))
	})

	It("should identify OOMKilled errors", func() {
		message := "Container was OOMKilled"
		result := analyzeDeploymentFailure(message)
		Expect(result).To(ContainSubstring("OOMKilled"))
	})

	It("should identify CrashLoopBackOff errors", func() {
		message := "Back-off restarting failed container: CrashLoopBackOff"
		result := analyzeDeploymentFailure(message)
		Expect(result).To(ContainSubstring("CrashLoopBackOff"))
	})

	It("should identify CreateContainerConfigError errors", func() {
		message := "Error: container has runAsNonRoot and image will run as root: CreateContainerConfigError"
		result := analyzeDeploymentFailure(message)
		Expect(result).To(ContainSubstring("CreateContainerConfigError"))
	})

	It("should identify probe failures", func() {
		message := "Liveness probe failed: HTTP probe failed"
		result := analyzeDeploymentFailure(message)
		Expect(result).To(ContainSubstring("Probe failed"))
	})

	It("should handle empty message", func() {
		message := ""
		result := analyzeDeploymentFailure(message)
		Expect(result).To(Equal("No healthy instances available"))
	})

	It("should handle generic failures", func() {
		message := "Some unknown error occurred"
		result := analyzeDeploymentFailure(message)
		Expect(result).To(ContainSubstring("No healthy instances"))
		Expect(result).To(ContainSubstring("Some unknown error occurred"))
	})
})

var _ = Describe("MCPServer Controller - Storage Mounts", func() {
	ctx := context.Background()

	Context("When reconciling a resource with ConfigMap storage", func() {
		const resourceName = "test-resource-configmap-storage"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			// Create ConfigMap first
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "default",
				},
				Data: map[string]string{
					"config.yaml": "test: value",
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path: "/etc/config",
								Source: mcpv1alpha1.StorageSource{
									Type: mcpv1alpha1.StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-configmap",
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-configmap", Namespace: "default"}, configMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			}
		})

		It("should create deployment with ConfigMap volume and mount", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify volume is created with auto-generated name
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
			volume := deployment.Spec.Template.Spec.Volumes[0]
			Expect(volume.Name).To(Equal("vol-0"))
			Expect(volume.VolumeSource.ConfigMap).NotTo(BeNil())
			Expect(volume.VolumeSource.ConfigMap.Name).To(Equal("test-configmap"))

			// Verify volume mount is created
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.VolumeMounts).To(HaveLen(1))
			volumeMount := container.VolumeMounts[0]
			Expect(volumeMount.Name).To(Equal("vol-0"))
			Expect(volumeMount.MountPath).To(Equal("/etc/config"))
			Expect(volumeMount.ReadOnly).To(BeTrue()) // Default is true
		})
	})

	Context("When reconciling a resource with Secret storage", func() {
		const resourceName = "test-resource-secret-storage"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			// Create Secret first
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				StringData: map[string]string{
					"token": "secret-value",
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path: "/etc/secret",
								Source: mcpv1alpha1.StorageSource{
									Type: mcpv1alpha1.StorageTypeSecret,
									Secret: &corev1.SecretVolumeSource{
										SecretName: "test-secret",
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-secret", Namespace: "default"}, secret)
			if err == nil {
				Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			}
		})

		It("should create deployment with Secret volume and mount", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify volume is created with auto-generated name
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
			volume := deployment.Spec.Template.Spec.Volumes[0]
			Expect(volume.Name).To(Equal("vol-0"))
			Expect(volume.VolumeSource.Secret).NotTo(BeNil())
			Expect(volume.VolumeSource.Secret.SecretName).To(Equal("test-secret"))

			// Verify volume mount is created
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.VolumeMounts).To(HaveLen(1))
			volumeMount := container.VolumeMounts[0]
			Expect(volumeMount.Name).To(Equal("vol-0"))
			Expect(volumeMount.MountPath).To(Equal("/etc/secret"))
			Expect(volumeMount.ReadOnly).To(BeTrue()) // Default is true
		})
	})

	Context("When reconciling a resource with multiple storage mounts", func() {
		const resourceName = "test-resource-multi-storage"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			// Create ConfigMap
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multi-configmap",
					Namespace: "default",
				},
				Data: map[string]string{
					"config.yaml": "test: value",
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			// Create Secret
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multi-secret",
					Namespace: "default",
				},
				StringData: map[string]string{
					"token": "secret-value",
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path: "/etc/config",
								Source: mcpv1alpha1.StorageSource{
									Type: mcpv1alpha1.StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-multi-configmap",
										},
									},
								},
							},
							{
								Path: "/etc/secret",
								Source: mcpv1alpha1.StorageSource{
									Type: mcpv1alpha1.StorageTypeSecret,
									Secret: &corev1.SecretVolumeSource{
										SecretName: "test-multi-secret",
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-multi-configmap", Namespace: "default"}, configMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			}
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-multi-secret", Namespace: "default"}, secret)
			if err == nil {
				Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			}
		})

		It("should create deployment with multiple volumes and mounts with correct names", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify both volumes are created with auto-generated names
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(2))

			volume0 := deployment.Spec.Template.Spec.Volumes[0]
			Expect(volume0.Name).To(Equal("vol-0"))
			Expect(volume0.VolumeSource.ConfigMap).NotTo(BeNil())
			Expect(volume0.VolumeSource.ConfigMap.Name).To(Equal("test-multi-configmap"))

			volume1 := deployment.Spec.Template.Spec.Volumes[1]
			Expect(volume1.Name).To(Equal("vol-1"))
			Expect(volume1.VolumeSource.Secret).NotTo(BeNil())
			Expect(volume1.VolumeSource.Secret.SecretName).To(Equal("test-multi-secret"))

			// Verify both volume mounts are created
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.VolumeMounts).To(HaveLen(2))

			volumeMount0 := container.VolumeMounts[0]
			Expect(volumeMount0.Name).To(Equal("vol-0"))
			Expect(volumeMount0.MountPath).To(Equal("/etc/config"))
			Expect(volumeMount0.ReadOnly).To(BeTrue())

			volumeMount1 := container.VolumeMounts[1]
			Expect(volumeMount1.Name).To(Equal("vol-1"))
			Expect(volumeMount1.MountPath).To(Equal("/etc/secret"))
			Expect(volumeMount1.ReadOnly).To(BeTrue())
		})
	})

	Context("When reconciling a resource with readOnly set to false", func() {
		const resourceName = "test-resource-readonly-false"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			// Create ConfigMap
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap-rw",
					Namespace: "default",
				},
				Data: map[string]string{
					"config.yaml": "test: value",
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path:        "/etc/config",
								Permissions: mcpv1alpha1.MountPermissionsReadWrite, // Explicitly set to read-write
								Source: mcpv1alpha1.StorageSource{
									Type: mcpv1alpha1.StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-configmap-rw",
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-configmap-rw", Namespace: "default"}, configMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			}
		})

		It("should create deployment with readOnly set to false", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify volume mount has ReadOnly set to false
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.VolumeMounts).To(HaveLen(1))
			volumeMount := container.VolumeMounts[0]
			Expect(volumeMount.Name).To(Equal("vol-0"))
			Expect(volumeMount.MountPath).To(Equal("/etc/config"))
			Expect(volumeMount.ReadOnly).To(BeFalse()) // Explicitly false, not default
		})
	})

	Context("When reconciling a resource with EmptyDir storage", func() {
		const resourceName = "test-resource-emptydir-storage"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path:        "/app/logs",
								Permissions: mcpv1alpha1.MountPermissionsReadWrite,
								Source: mcpv1alpha1.StorageSource{
									Type:     mcpv1alpha1.StorageTypeEmptyDir,
									EmptyDir: &corev1.EmptyDirVolumeSource{},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should create deployment with EmptyDir volume and mount", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify volume is created with auto-generated name
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
			volume := deployment.Spec.Template.Spec.Volumes[0]
			Expect(volume.Name).To(Equal("vol-0"))
			Expect(volume.VolumeSource.EmptyDir).NotTo(BeNil())

			// Verify volume mount is created with ReadWrite permissions
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.VolumeMounts).To(HaveLen(1))
			volumeMount := container.VolumeMounts[0]
			Expect(volumeMount.Name).To(Equal("vol-0"))
			Expect(volumeMount.MountPath).To(Equal("/app/logs"))
			Expect(volumeMount.ReadOnly).To(BeFalse()) // ReadWrite
		})
	})

	Context("When reconciling a resource with EmptyDir storage with sizeLimit", func() {
		const resourceName = "test-resource-emptydir-sizelimit"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			sizeLimit := resource.MustParse("100Mi")
			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path:        "/tmp/cache",
								Permissions: mcpv1alpha1.MountPermissionsReadWrite,
								Source: mcpv1alpha1.StorageSource{
									Type: mcpv1alpha1.StorageTypeEmptyDir,
									EmptyDir: &corev1.EmptyDirVolumeSource{
										SizeLimit: &sizeLimit,
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should create deployment with EmptyDir volume with sizeLimit", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify EmptyDir has sizeLimit set
			volume := deployment.Spec.Template.Spec.Volumes[0]
			Expect(volume.VolumeSource.EmptyDir).NotTo(BeNil())
			Expect(volume.VolumeSource.EmptyDir.SizeLimit).NotTo(BeNil())
			Expect(volume.VolumeSource.EmptyDir.SizeLimit.String()).To(Equal("100Mi"))
		})
	})

	Context("When reconciling a resource with mixed storage types including EmptyDir", func() {
		const resourceName = "test-resource-mixed-storage"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			// Create ConfigMap
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mixed-configmap",
					Namespace: "default",
				},
				Data: map[string]string{
					"config.yaml": "test: value",
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path: "/etc/config",
								Source: mcpv1alpha1.StorageSource{
									Type: mcpv1alpha1.StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-mixed-configmap",
										},
									},
								},
							},
							{
								Path:        "/app/logs",
								Permissions: mcpv1alpha1.MountPermissionsReadWrite,
								Source: mcpv1alpha1.StorageSource{
									Type:     mcpv1alpha1.StorageTypeEmptyDir,
									EmptyDir: &corev1.EmptyDirVolumeSource{},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-mixed-configmap", Namespace: "default"}, configMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			}
		})

		It("should create deployment with both ConfigMap and EmptyDir volumes", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify both volumes are created
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(2))

			volume0 := deployment.Spec.Template.Spec.Volumes[0]
			Expect(volume0.Name).To(Equal("vol-0"))
			Expect(volume0.VolumeSource.ConfigMap).NotTo(BeNil())
			Expect(volume0.VolumeSource.ConfigMap.Name).To(Equal("test-mixed-configmap"))

			volume1 := deployment.Spec.Template.Spec.Volumes[1]
			Expect(volume1.Name).To(Equal("vol-1"))
			Expect(volume1.VolumeSource.EmptyDir).NotTo(BeNil())

			// Verify both volume mounts
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.VolumeMounts).To(HaveLen(2))

			volumeMount0 := container.VolumeMounts[0]
			Expect(volumeMount0.Name).To(Equal("vol-0"))
			Expect(volumeMount0.MountPath).To(Equal("/etc/config"))
			Expect(volumeMount0.ReadOnly).To(BeTrue()) // ConfigMap default

			volumeMount1 := container.VolumeMounts[1]
			Expect(volumeMount1.Name).To(Equal("vol-1"))
			Expect(volumeMount1.MountPath).To(Equal("/app/logs"))
			Expect(volumeMount1.ReadOnly).To(BeFalse()) // ReadWrite
		})
	})

	Context("When ConfigMap reference doesn't exist", func() {
		const resourceName = "test-resource-missing-configmap"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path: "/etc/config",
								Source: mcpv1alpha1.StorageSource{
									Type: mcpv1alpha1.StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "nonexistent-configmap",
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should set Accepted=False with 'ConfigMap not found' message", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			// No error should be returned - configuration issues are reported via status conditions
			Expect(err).NotTo(HaveOccurred())

			// Verify MCPServer status has Accepted=False
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
			Expect(acceptedCondition.Message).To(ContainSubstring("nonexistent-configmap"))
			Expect(acceptedCondition.Message).To(ContainSubstring("not found"))
		})
	})

	Context("When Secret reference doesn't exist", func() {
		const resourceName = "test-resource-missing-secret"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path: "/etc/secret",
								Source: mcpv1alpha1.StorageSource{
									Type: mcpv1alpha1.StorageTypeSecret,
									Secret: &corev1.SecretVolumeSource{
										SecretName: "nonexistent-secret",
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should set Accepted=False with 'Secret not found' message", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			// No error should be returned - configuration issues are reported via status conditions
			Expect(err).NotTo(HaveOccurred())

			// Verify MCPServer status has Accepted=False
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
			Expect(acceptedCondition.Message).To(ContainSubstring("nonexistent-secret"))
			Expect(acceptedCondition.Message).To(ContainSubstring("not found"))
		})
	})

	Context("When ConfigMap is optional and doesn't exist", func() {
		const resourceName = "test-resource-optional-configmap"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			// Don't create the ConfigMap - it should be optional
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path: "/etc/config",
								Source: mcpv1alpha1.StorageSource{
									Type: mcpv1alpha1.StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "optional-configmap",
										},
										Optional: ptr.To(true),
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should succeed reconciliation even when ConfigMap doesn't exist", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify deployment was created
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify volume is created with optional ConfigMap reference
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
			volume := deployment.Spec.Template.Spec.Volumes[0]
			Expect(volume.VolumeSource.ConfigMap).NotTo(BeNil())
			Expect(volume.VolumeSource.ConfigMap.Name).To(Equal("optional-configmap"))
			Expect(volume.VolumeSource.ConfigMap.Optional).NotTo(BeNil())
			Expect(*volume.VolumeSource.ConfigMap.Optional).To(BeTrue())
		})
	})

	Context("When Secret is optional and doesn't exist", func() {
		const resourceName = "test-resource-optional-secret"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			// Don't create the Secret - it should be optional
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path: "/etc/secret",
								Source: mcpv1alpha1.StorageSource{
									Type: mcpv1alpha1.StorageTypeSecret,
									Secret: &corev1.SecretVolumeSource{
										SecretName: "optional-secret",
										Optional:   ptr.To(true),
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should succeed reconciliation even when Secret doesn't exist", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify deployment was created
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify volume is created with optional Secret reference
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
			volume := deployment.Spec.Template.Spec.Volumes[0]
			Expect(volume.VolumeSource.Secret).NotTo(BeNil())
			Expect(volume.VolumeSource.Secret.SecretName).To(Equal("optional-secret"))
			Expect(volume.VolumeSource.Secret.Optional).NotTo(BeNil())
			Expect(*volume.VolumeSource.Secret.Optional).To(BeTrue())
		})
	})

	Context("When ConfigMap name is empty", func() {
		const resourceName = "test-resource-empty-configmap-name"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path: "/etc/config",
								Source: mcpv1alpha1.StorageSource{
									Type: mcpv1alpha1.StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "", // Empty name
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should set Accepted=False when ConfigMap name is empty", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			// No error should be returned - configuration issues are reported via status conditions
			Expect(err).NotTo(HaveOccurred())

			// Verify MCPServer status has Accepted=False
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
			Expect(acceptedCondition.Message).To(ContainSubstring("ConfigMap name must not be empty"))
			Expect(acceptedCondition.Message).To(ContainSubstring("index 0"))
		})
	})

	Context("When Secret name is empty", func() {
		const resourceName = "test-resource-empty-secret-name"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path: "/etc/secret",
								Source: mcpv1alpha1.StorageSource{
									Type: mcpv1alpha1.StorageTypeSecret,
									Secret: &corev1.SecretVolumeSource{
										SecretName: "", // Empty name
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should set Accepted=False when Secret name is empty", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			// No error should be returned - configuration issues are reported via status conditions
			Expect(err).NotTo(HaveOccurred())

			// Verify MCPServer status has Accepted=False
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
			Expect(acceptedCondition.Message).To(ContainSubstring("Secret name must not be empty"))
			Expect(acceptedCondition.Message).To(ContainSubstring("index 0"))
		})
	})

	Context("setAcceptedCondition validation", func() {
		ctx := context.Background()

		It("should reject EmptyDir with nil EmptyDir configuration", func() {
			scheme := runtime.NewScheme()
			Expect(mcpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			reconciler := &MCPServerReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-server",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Config: mcpv1alpha1.ServerConfig{
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path: "/data",
								Source: mcpv1alpha1.StorageSource{
									Type:     mcpv1alpha1.StorageTypeEmptyDir,
									EmptyDir: nil, // Intentionally nil
								},
							},
						},
					},
				},
			}

			condition, valid := reconciler.setAcceptedCondition(ctx, mcpServer)
			Expect(valid).To(BeFalse())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(ReasonInvalid))
			Expect(condition.Message).To(ContainSubstring("EmptyDir must be set"))
		})

		It("should reject unknown storage type", func() {
			scheme := runtime.NewScheme()
			Expect(mcpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			reconciler := &MCPServerReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-server",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Config: mcpv1alpha1.ServerConfig{
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path: "/data",
								Source: mcpv1alpha1.StorageSource{
									Type: "UnknownType", // Invalid storage type
								},
							},
						},
					},
				},
			}

			condition, valid := reconciler.setAcceptedCondition(ctx, mcpServer)
			Expect(valid).To(BeFalse())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(ReasonInvalid))
			Expect(condition.Message).To(ContainSubstring("Unsupported storage type"))
			Expect(condition.Message).To(ContainSubstring("UnknownType"))
		})
	})

	Context("When reconciling a resource with resources", func() {
		const resourceName = "test-resource-resources"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should create deployment with resources", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("100m")))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("256Mi")))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("500m")))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("512Mi")))
			// Verify replicas defaults to 1 even when runtime is specified with other fields
			Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
		})

		It("should update deployment when resources change", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling to create the initial deployment with resources")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("100m")))

			By("Updating resources")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			mcpServer.Spec.Runtime.Resources = &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			}
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			By("Reconciling again to pick up the change")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("200m")))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("512Mi")))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("1")))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("1Gi")))
		})

		It("should update deployment when resources are removed", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling to create the initial deployment with resources")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("100m")))

			By("Removing resources")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			mcpServer.Spec.Runtime.Resources = nil
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			By("Reconciling again to pick up the change")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(BeEmpty())
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(BeEmpty())
		})

		It("should handle resources with only requests (no limits)", func() {
			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-only-requests",
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
			defer func() {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-only-requests", Namespace: "default"}, mcpServer)
				if err == nil {
					Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
				}
			}()

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-only-requests", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      "test-only-requests",
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("100m")))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("128Mi")))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(BeEmpty())
		})

		It("should handle resources with only limits (no requests)", func() {
			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-only-limits",
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Resources: &corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
			defer func() {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-only-limits", Namespace: "default"}, mcpServer)
				if err == nil {
					Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
				}
			}()

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-only-limits", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      "test-only-limits",
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(BeEmpty())
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("500m")))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("512Mi")))
		})

		It("should handle resources with only CPU (no memory)", func() {
			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-only-cpu",
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("100m"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("200m"),
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
			defer func() {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-only-cpu", Namespace: "default"}, mcpServer)
				if err == nil {
					Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
				}
			}()

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-only-cpu", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      "test-only-cpu",
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("100m")))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).NotTo(HaveKey(corev1.ResourceMemory))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("200m")))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).NotTo(HaveKey(corev1.ResourceMemory))
		})

		It("should handle resources with only memory (no CPU)", func() {
			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-only-memory",
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
			defer func() {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-only-memory", Namespace: "default"}, mcpServer)
				if err == nil {
					Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
				}
			}()

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-only-memory", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      "test-only-memory",
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).NotTo(HaveKey(corev1.ResourceCPU))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("256Mi")))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).NotTo(HaveKey(corev1.ResourceCPU))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("512Mi")))
		})
	})

	Context("Server-side apply for status updates", func() {
		const resourceName = "test-ssa-status"
		const subResourceStatus = "status"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should use SubResourceApply for all status updates and never SubResourceUpdate or SubResourcePatch", func() {
			applyCallCount := 0
			updateCalled := false
			patchCalled := false

			wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
			Expect(err).NotTo(HaveOccurred())

			interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
				SubResourceApply: func(ctx context.Context, c client.Client, subResourceName string, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
					if subResourceName == subResourceStatus {
						applyCallCount++
					}
					return c.SubResource(subResourceName).Apply(ctx, obj, opts...)
				},
				SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					if subResourceName == subResourceStatus {
						updateCalled = true
					}
					return c.SubResource(subResourceName).Update(ctx, obj, opts...)
				},
				SubResourcePatch: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					if subResourceName == subResourceStatus {
						patchCalled = true
					}
					return c.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
				},
			})

			controllerReconciler := &MCPServerReconciler{
				Client: interceptedClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(applyCallCount).To(BeNumerically(">", 0), "expected status updates to use SubResourceApply (SSA)")
			Expect(updateCalled).To(BeFalse(), "status should not use SubResourceUpdate")
			Expect(patchCalled).To(BeFalse(), "status should not use SubResourcePatch")
		})
	})

	Context("When reconciling a resource with health probes", func() {
		const resourceName = "test-resource-probes"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Health: mcpv1alpha1.HealthConfig{
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       30,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		AfterEach(func() {
			mcpServer := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, mcpServer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		})

		It("should create deployment with health probes", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))

			// Verify liveness probe
			Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe).NotTo(BeNil())
			Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet).NotTo(BeNil())
			Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Path).To(Equal("/health"))
			Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Port.IntVal).To(Equal(int32(8080)))
			Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.InitialDelaySeconds).To(Equal(int32(10)))
			Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.PeriodSeconds).To(Equal(int32(30)))

			// Verify readiness probe
			Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe).NotTo(BeNil())
			Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket).NotTo(BeNil())
			Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket.Port.IntVal).To(Equal(int32(8080)))
			Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.InitialDelaySeconds).To(Equal(int32(5)))
			Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.PeriodSeconds).To(Equal(int32(10)))
		})

		It("should update deployment when probes change", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling to create the initial deployment with probes")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.InitialDelaySeconds).To(Equal(int32(10)))

			By("Updating probes")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			mcpServer.Spec.Runtime.Health.LivenessProbe = &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/healthz",
						Port: intstr.FromInt(8080),
					},
				},
				InitialDelaySeconds: 15,
				PeriodSeconds:       60,
			}
			mcpServer.Spec.Runtime.Health.ReadinessProbe = &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/ready",
						Port: intstr.FromInt(8080),
					},
				},
				InitialDelaySeconds: 3,
				PeriodSeconds:       5,
			}
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			By("Reconciling again to pick up the change")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify updated liveness probe
			Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Path).To(Equal("/healthz"))
			Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.InitialDelaySeconds).To(Equal(int32(15)))
			Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.PeriodSeconds).To(Equal(int32(60)))

			// Verify updated readiness probe
			Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet).NotTo(BeNil())
			Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Path).To(Equal("/ready"))
			Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.InitialDelaySeconds).To(Equal(int32(3)))
			Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.PeriodSeconds).To(Equal(int32(5)))
		})

		It("should update deployment when probes are removed", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling to create the initial deployment with probes")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe).NotTo(BeNil())

			By("Removing probes")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			mcpServer.Spec.Runtime.Health.LivenessProbe = nil
			mcpServer.Spec.Runtime.Health.ReadinessProbe = nil
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			By("Reconciling again to pick up the change")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe).To(BeNil())
			Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe).To(BeNil())
		})

		It("should handle only liveness probe (no readiness)", func() {
			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-only-liveness",
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Health: mcpv1alpha1.HealthConfig{
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 10,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
			defer func() {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-only-liveness", Namespace: "default"}, mcpServer)
				if err == nil {
					Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
				}
			}()

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-only-liveness", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      "test-only-liveness",
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe).NotTo(BeNil())
			Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet).NotTo(BeNil())
			Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe).To(BeNil())
		})

		It("should handle only readiness probe (no liveness)", func() {
			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-only-readiness",
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
					Runtime: mcpv1alpha1.RuntimeConfig{
						Health: mcpv1alpha1.HealthConfig{
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 5,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
			defer func() {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-only-readiness", Namespace: "default"}, mcpServer)
				if err == nil {
					Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
				}
			}()

			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-only-readiness", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      "test-only-readiness",
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe).To(BeNil())
			Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe).NotTo(BeNil())
			Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket).NotTo(BeNil())
		})
	})
})

var _ = Describe("MCPServer Controller - Owned Resource Cleanup", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-ownerref"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}
			service := &corev1.Service{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)
			if err == nil {
				Expect(k8sClient.Delete(ctx, service)).To(Succeed())
			}
		})

		It("should set controller owner reference on created Deployment", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())

			Expect(deployment.OwnerReferences).To(HaveLen(1))
			ownerRef := deployment.OwnerReferences[0]
			Expect(ownerRef.Name).To(Equal(mcpServer.Name))
			Expect(ownerRef.UID).To(Equal(mcpServer.UID))
			Expect(*ownerRef.Controller).To(BeTrue())
			Expect(ownerRef.Kind).To(Equal("MCPServer"))
			Expect(ownerRef.APIVersion).To(Equal("mcp.x-k8s.io/v1alpha1"))
		})

		It("should set controller owner reference on created Service", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			service := &corev1.Service{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)).To(Succeed())

			Expect(service.OwnerReferences).To(HaveLen(1))
			ownerRef := service.OwnerReferences[0]
			Expect(ownerRef.Name).To(Equal(mcpServer.Name))
			Expect(ownerRef.UID).To(Equal(mcpServer.UID))
			Expect(*ownerRef.Controller).To(BeTrue())
			Expect(ownerRef.Kind).To(Equal("MCPServer"))
			Expect(ownerRef.APIVersion).To(Equal("mcp.x-k8s.io/v1alpha1"))
		})

		It("should preserve owner references across reconciliation updates", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling to create initial resources")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			originalUID := mcpServer.UID

			By("Updating the MCPServer port to trigger a Service update")
			mcpServer.Spec.Config.Port = 9090
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			By("Reconciling again after the update")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying Deployment owner reference is preserved")
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			Expect(deployment.OwnerReferences[0].UID).To(Equal(originalUID))
			Expect(*deployment.OwnerReferences[0].Controller).To(BeTrue())

			By("Verifying Service owner reference is preserved")
			service := &corev1.Service{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)).To(Succeed())
			Expect(service.OwnerReferences).To(HaveLen(1))
			Expect(service.OwnerReferences[0].UID).To(Equal(originalUID))
			Expect(*service.OwnerReferences[0].Controller).To(BeTrue())
		})
	})
})

var _ = Describe("MCPServer Controller - Error Recovery", func() {
	ctx := context.Background()

	Context("When missing envFrom ConfigMap is created after failure", func() {
		const resourceName = "test-recovery-cm"
		const configMapName = "recovery-configmap"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						EnvFrom: []corev1.EnvFromSource{
							{
								ConfigMapRef: &corev1.ConfigMapEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: "default"}, cm)
			if err == nil {
				Expect(k8sClient.Delete(ctx, cm)).To(Succeed())
			}
		})

		It("should recover from Failed to Pending when missing ConfigMap is created", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("First reconcile fails due to missing ConfigMap")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status is Failed")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal("Invalid"))
			Expect(acceptedCondition.Message).To(ContainSubstring("recovery-configmap"))
			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal("ConfigurationInvalid"))

			By("Verifying no Deployment was created")
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("Creating the missing ConfigMap")
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: "default"},
				Data:       map[string]string{"key": "value"},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			By("Second reconcile succeeds after ConfigMap is available")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status recovered to Pending")
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(acceptedCondition.Reason).To(Equal("Valid"))
			readyCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Reason).To(Equal("Initializing"))

			By("Verifying Deployment was created on recovery")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
		})
	})

	Context("When missing envFrom Secret is created after failure", func() {
		const resourceName = "test-recovery-secret"
		const secretName = "recovery-secret"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						EnvFrom: []corev1.EnvFromSource{
							{
								SecretRef: &corev1.SecretEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: secretName, Namespace: "default"}, secret)
			if err == nil {
				Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			}
		})

		It("should recover from Failed to Pending when missing Secret is created", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("First reconcile fails due to missing Secret")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status is Failed")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal("Invalid"))
			Expect(acceptedCondition.Message).To(ContainSubstring("recovery-secret"))
			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal("ConfigurationInvalid"))

			By("Verifying no Deployment was created")
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("Creating the missing Secret")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "default"},
				Data:       map[string][]byte{"key": []byte("value")},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			By("Second reconcile succeeds after Secret is available")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status recovered to Pending")
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(acceptedCondition.Reason).To(Equal("Valid"))
			readyCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Reason).To(Equal("Initializing"))

			By("Verifying Deployment was created on recovery")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
		})
	})

	Context("When missing storage ConfigMap is created after failure", func() {
		const resourceName = "test-recovery-storage"
		const configMapName = "recovery-storage-cm"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path: "/etc/config",
								Source: mcpv1alpha1.StorageSource{
									Type: mcpv1alpha1.StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: "default"}, cm)
			if err == nil {
				Expect(k8sClient.Delete(ctx, cm)).To(Succeed())
			}
		})

		It("should recover from Failed to Pending when missing storage ConfigMap is created", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("First reconcile fails due to missing storage ConfigMap")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status is Failed")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal("Invalid"))
			Expect(acceptedCondition.Message).To(ContainSubstring("recovery-storage-cm"))
			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal("ConfigurationInvalid"))

			By("Creating the missing ConfigMap")
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: "default"},
				Data:       map[string]string{"config.yaml": "data: value"},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			By("Second reconcile succeeds after ConfigMap is available")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status recovered to Pending")
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(acceptedCondition.Reason).To(Equal("Valid"))
			readyCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Reason).To(Equal("Initializing"))

			By("Verifying Deployment was created on recovery")
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
		})
	})
})

var _ = Describe("MCPServer Controller - Optimistic Locking Conflicts", func() {
	const resourceName = "test-conflict"

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	BeforeEach(func() {
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
			},
			Spec: mcpv1alpha1.MCPServerSpec{
				Source: mcpv1alpha1.Source{
					Type: mcpv1alpha1.SourceTypeContainerImage,
					ContainerImage: &mcpv1alpha1.ContainerImageSource{
						Ref: "docker.io/library/test-image:latest",
					},
				},
				Config: mcpv1alpha1.ServerConfig{
					Port: 8080,
				},
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
	})

	AfterEach(func() {
		resource := &mcpv1alpha1.MCPServer{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		if err == nil {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		}
		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
		if err == nil {
			Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
		}
		service := &corev1.Service{}
		err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)
		if err == nil {
			Expect(k8sClient.Delete(ctx, service)).To(Succeed())
		}
	})

	It("should return conflict error when deployment update encounters optimistic locking conflict", func() {
		By("Initial reconcile to create resources")
		initialReconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		_, err := initialReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Updating MCPServer spec to trigger a deployment update")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Config.Env = []corev1.EnvVar{{Name: "CONFLICT_VAR", Value: "value"}}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Creating interceptor that returns conflict on deployment Update")
		updateCallCount := 0
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*appsv1.Deployment); ok {
					updateCallCount++
					return errors.NewConflict(
						schema.GroupResource{Group: "apps", Resource: "deployments"},
						obj.GetName(),
						fmt.Errorf("the object has been modified"),
					)
				}
				return c.Update(ctx, obj, opts...)
			},
		})

		conflictReconciler := &MCPServerReconciler{
			Client: interceptedClient,
			Scheme: k8sClient.Scheme(),
		}

		By("Reconciling with conflict interceptor")
		_, err = conflictReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).To(HaveOccurred())
		Expect(errors.IsConflict(err)).To(BeTrue())
		Expect(updateCallCount).To(Equal(1))
	})

	It("should succeed on retry after conflict is resolved", func() {
		By("Initial reconcile to create resources")
		initialReconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		_, err := initialReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Updating MCPServer spec to trigger a deployment update")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Config.Env = []corev1.EnvVar{{Name: "RETRY_VAR", Value: "value"}}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Creating interceptor that returns conflict only on first Update")
		updateCallCount := 0
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*appsv1.Deployment); ok {
					updateCallCount++
					if updateCallCount == 1 {
						return errors.NewConflict(
							schema.GroupResource{Group: "apps", Resource: "deployments"},
							obj.GetName(),
							fmt.Errorf("the object has been modified"),
						)
					}
				}
				return c.Update(ctx, obj, opts...)
			},
		})

		conflictReconciler := &MCPServerReconciler{
			Client: interceptedClient,
			Scheme: k8sClient.Scheme(),
		}

		By("First reconcile fails with conflict")
		_, err = conflictReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).To(HaveOccurred())
		Expect(errors.IsConflict(err)).To(BeTrue())

		By("Second reconcile succeeds (conflict resolved)")
		_, err = conflictReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying deployment was updated with the new env var")
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(ContainElement(
			corev1.EnvVar{Name: "RETRY_VAR", Value: "value"},
		))
	})

	It("should return conflict error when service update encounters optimistic locking conflict", func() {
		By("Initial reconcile to create resources")
		initialReconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		_, err := initialReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Updating MCPServer port to trigger a service update")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Config.Port = 9090
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Creating interceptor that returns conflict on service Update")
		updateCallCount := 0
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*corev1.Service); ok {
					updateCallCount++
					return errors.NewConflict(
						schema.GroupResource{Group: "", Resource: "services"},
						obj.GetName(),
						fmt.Errorf("the object has been modified"),
					)
				}
				return c.Update(ctx, obj, opts...)
			},
		})

		conflictReconciler := &MCPServerReconciler{
			Client: interceptedClient,
			Scheme: k8sClient.Scheme(),
		}

		By("Reconciling with conflict interceptor")
		_, err = conflictReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).To(HaveOccurred())
		Expect(errors.IsConflict(err)).To(BeTrue())
		Expect(updateCallCount).To(Equal(1))
	})
})

var _ = Describe("MCPServer Controller - Foreign Owned Resources", func() {
	ctx := context.Background()

	Context("When a Deployment already exists with a different owner", func() {
		const resourceName = "test-foreign-deploy"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("Pre-creating a Deployment owned by a different controller")
			foreignDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "SomeOtherController",
							Name:       "foreign-owner",
							UID:        types.UID("foreign-controller-uid"),
							Controller: ptr.To(true),
						},
					},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"mcp-server": resourceName},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app":        "mcp-server",
								"mcp-server": resourceName,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "other", Image: "other-image:latest"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, foreignDeployment)).To(Succeed())

			By("Creating the MCPServer CR")
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}
		})

		It("should update deployment spec even when owned by another controller", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the deployment spec was overwritten")
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("docker.io/library/test-image:latest"))

			By("Verifying the original foreign owner reference is still present")
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			Expect(deployment.OwnerReferences[0].Name).To(Equal("foreign-owner"))
			Expect(*deployment.OwnerReferences[0].Controller).To(BeTrue())
		})
	})

	Context("When a Service already exists with a different owner", func() {
		const resourceName = "test-foreign-svc"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("Pre-creating a Service owned by a different controller")
			foreignService := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "SomeOtherController",
							Name:       "foreign-svc-owner",
							UID:        types.UID("foreign-svc-controller-uid"),
							Controller: ptr.To(true),
						},
					},
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"mcp-server": resourceName},
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Port:       9999,
							TargetPort: intstr.FromInt32(9999),
							Protocol:   corev1.ProtocolTCP,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, foreignService)).To(Succeed())

			By("Creating the MCPServer CR")
			resource := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			service := &corev1.Service{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)
			if err == nil {
				Expect(k8sClient.Delete(ctx, service)).To(Succeed())
			}
		})

		It("should update service spec even when owned by another controller", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the service port was updated to match MCPServer config")
			service := &corev1.Service{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)).To(Succeed())
			Expect(service.Spec.Ports).To(HaveLen(1))
			Expect(service.Spec.Ports[0].Port).To(Equal(int32(8080)))

			By("Verifying the original foreign owner reference is still present")
			Expect(service.OwnerReferences).To(HaveLen(1))
			Expect(service.OwnerReferences[0].Name).To(Equal("foreign-svc-owner"))
			Expect(*service.OwnerReferences[0].Controller).To(BeTrue())
		})
	})
})
