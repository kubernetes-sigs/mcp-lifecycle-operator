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
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Runtime.Replicas = new(int32(1))
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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Port = 3001
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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Path = "/sse"
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
			resource := newTestMCPServer(resourceName)
			resource.Spec.Runtime.Replicas = new(int32(1))
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

var _ = Describe("MCPServer Controller - reconcileService", func() {
	const resourceName = "test-reconcile-service"

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
		Expect(svc.Spec.SessionAffinity).To(Equal(corev1.ServiceAffinityClientIP))
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

var _ = Describe("MCPServer Controller - Stateless Service", func() {
	const resourceName = "test-stateless-service"

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

	It("should create a service with no session affinity when stateless is true", func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.MCP.Stateless = new(true)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		err := reconciler.reconcileService(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())

		svc := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, svc)).To(Succeed())
		Expect(svc.Spec.SessionAffinity).To(Equal(corev1.ServiceAffinityNone))
	})

	It("should create a service with ClientIP session affinity when mcp is not set", func() {
		resource := newTestMCPServer(resourceName)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		err := reconciler.reconcileService(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())

		svc := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, svc)).To(Succeed())
		Expect(svc.Spec.SessionAffinity).To(Equal(corev1.ServiceAffinityClientIP))
	})

	It("should create a service with ClientIP session affinity when stateless is false", func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.MCP.Stateless = new(false)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		err := reconciler.reconcileService(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())

		svc := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, svc)).To(Succeed())
		Expect(svc.Spec.SessionAffinity).To(Equal(corev1.ServiceAffinityClientIP))
	})

	It("should update session affinity when stateless changes from false to true", func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.MCP.Stateless = new(false)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		controllerReconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		By("Reconciling to create the service with ClientIP affinity")
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		svc := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, svc)).To(Succeed())
		Expect(svc.Spec.SessionAffinity).To(Equal(corev1.ServiceAffinityClientIP))

		By("Updating stateless to true")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.MCP.Stateless = new(true)
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Reconciling again to pick up the stateless change")
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying session affinity was updated to None")
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, svc)).To(Succeed())
		Expect(svc.Spec.SessionAffinity).To(Equal(corev1.ServiceAffinityNone))
	})

	It("should update session affinity when stateless changes from true to false", func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.MCP.Stateless = new(true)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		controllerReconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		By("Reconciling to create the service with no session affinity")
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		svc := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, svc)).To(Succeed())
		Expect(svc.Spec.SessionAffinity).To(Equal(corev1.ServiceAffinityNone))

		By("Updating stateless to false")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.MCP.Stateless = new(false)
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Reconciling again to pick up the stateless change")
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying session affinity was updated to ClientIP")
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, svc)).To(Succeed())
		Expect(svc.Spec.SessionAffinity).To(Equal(corev1.ServiceAffinityClientIP))
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
		resource := newTestMCPServer(resourceName)
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

var _ = Describe("MCPServer Controller - Server-Side Apply for Status", func() {
	const resourceName = "test-ssa-status"
	const subResourceStatus = "status"

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
