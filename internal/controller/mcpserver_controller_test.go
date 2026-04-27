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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

// newTestMCPServer returns an MCPServer with standard test defaults:
// namespace "default", SourceTypeContainerImage with ref
// "docker.io/library/test-image:latest", and port 8080.
// Callers mutate the returned struct for scenario-specific fields.
func newTestMCPServer(name string) *mcpv1alpha1.MCPServer {
	return &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
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
}

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
				resource := newTestMCPServer(resourceName)
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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Env = []corev1.EnvVar{
				{Name: "TOKEN", Value: "test-token"},
				{Name: "LOG_LEVEL", Value: "debug"},
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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Arguments = []string{"--verbose", "--port=8080"}
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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Runtime = mcpv1alpha1.RuntimeConfig{
				Security: mcpv1alpha1.SecurityConfig{
					ServiceAccountName: "my-sa",
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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Runtime = mcpv1alpha1.RuntimeConfig{
				Security: mcpv1alpha1.SecurityConfig{
					SecurityContext: &corev1.SecurityContext{
						RunAsUser:  &runAsUser,
						RunAsGroup: &runAsGroup,
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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Runtime = mcpv1alpha1.RuntimeConfig{
				Security: mcpv1alpha1.SecurityConfig{
					PodSecurityContext: &corev1.PodSecurityContext{
						RunAsUser: &runAsUser,
						FSGroup:   &fsGroup,
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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Runtime = mcpv1alpha1.RuntimeConfig{
				Security: mcpv1alpha1.SecurityConfig{
					PodSecurityContext: &corev1.PodSecurityContext{
						RunAsUser: &runAsUser,
						FSGroup:   &fsGroup,
					},
					SecurityContext: &corev1.SecurityContext{
						ReadOnlyRootFilesystem: &readOnly,
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
			resource := newTestMCPServer(resourceName + "-none")
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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Runtime.Replicas = ptr.To(int32(3))
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
			// No Runtime section - replicas should default to 1
			resource := newTestMCPServer(resourceName)
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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Runtime.Replicas = ptr.To(int32(0))
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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Runtime.Replicas = ptr.To(int32(2))
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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Runtime.Replicas = ptr.To(int32(3))
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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Runtime.Replicas = ptr.To(int32(1))
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
			resource := newTestMCPServer(resourceName)
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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.EnvFrom = []corev1.EnvFromSource{
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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.EnvFrom = []corev1.EnvFromSource{
				{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "nonexistent-configmap"},
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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.EnvFrom = []corev1.EnvFromSource{
				{
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "nonexistent-secret"},
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

	Context("When reconciling a resource with a missing env valueFrom ConfigMap reference", func() {
		const resourceName = "test-resource-env-valuefrom-missing-cm"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Env = []corev1.EnvVar{
				{
					Name: "MY_CONFIG_VAR",
					ValueFrom: &corev1.EnvVarSource{
						ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "nonexistent-env-configmap"},
							Key:                  "some-key",
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

		It("should set Accepted=False when env valueFrom references a missing ConfigMap", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
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
			Expect(acceptedCondition.Message).To(ContainSubstring("nonexistent-env-configmap"))
			Expect(acceptedCondition.Message).To(ContainSubstring("MY_CONFIG_VAR"))

			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal(ReasonConfigurationInvalid))
		})

		It("should skip validation when env valueFrom ConfigMap reference is optional", func() {
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			optional := true
			mcpServer.Spec.Config.Env[0].ValueFrom.ConfigMapKeyRef.Optional = &optional
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

	Context("When reconciling a resource with a missing env valueFrom Secret reference", func() {
		const resourceName = "test-resource-env-valuefrom-missing-secret"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Env = []corev1.EnvVar{
				{
					Name: "MY_SECRET_VAR",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "nonexistent-env-secret"},
							Key:                  "some-key",
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

		It("should set Accepted=False when env valueFrom references a missing Secret", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
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
			Expect(acceptedCondition.Message).To(ContainSubstring("nonexistent-env-secret"))
			Expect(acceptedCondition.Message).To(ContainSubstring("MY_SECRET_VAR"))

			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal(ReasonConfigurationInvalid))
		})

		It("should skip validation when env valueFrom Secret reference is optional", func() {
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			optional := true
			mcpServer.Spec.Config.Env[0].ValueFrom.SecretKeyRef.Optional = &optional
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
})
