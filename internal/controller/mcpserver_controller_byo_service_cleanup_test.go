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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

var _ = Describe("MCPServer Controller - BYO Service cleanup", func() {
	ctx := context.Background()

	Context("When serviceRef is added to an MCPServer with a managed Service", func() {
		const resourceName = "test-cleanup-add-ref"
		const externalName = "external-mcp-svc"

		typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			ext := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: externalName, Namespace: "default"},
				Spec: corev1.ServiceSpec{
					Ports:    []corev1.ServicePort{{Name: mcpPortName, Port: 9090, Protocol: corev1.ProtocolTCP}},
					Selector: map[string]string{"app": "external"},
				},
			}
			Expect(k8sClient.Create(ctx, ext)).To(Succeed())

			server := newTestMCPServer(resourceName)
			server.Spec.Config.Port = 8080
			server.Spec.Config.Path = DefaultMCPPath
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			cleanupObject(ctx, &mcpv1beta1.MCPServer{}, typeNamespacedName)
			cleanupObject(ctx, &corev1.Service{}, types.NamespacedName{Name: externalName, Namespace: "default"})
			cleanupObject(ctx, &corev1.Service{}, typeNamespacedName)
		})

		It("should delete the operator-owned managed Service and resolve via serviceRef", func() {
			r := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			managed := &corev1.Service{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, managed)).To(Succeed())
			owner := metav1.GetControllerOf(managed)
			Expect(owner).NotTo(BeNil())

			server := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, server)).To(Succeed())
			Expect(owner.UID).To(Equal(server.UID))

			server.Spec.ServiceRef = &mcpv1beta1.ServiceReference{Name: externalName}
			Expect(k8sClient.Update(ctx, server)).To(Succeed())

			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, typeNamespacedName, &corev1.Service{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "stale managed Service should be deleted after serviceRef was added")

			Expect(k8sClient.Get(ctx, typeNamespacedName, server)).To(Succeed())
			Expect(server.Status.ServiceName).To(Equal(externalName))
		})
	})

	Context("When serviceRef is removed from an MCPServer", func() {
		const resourceName = "test-cleanup-remove-ref"
		const externalName = "external-mcp-svc-2"

		typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			ext := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: externalName, Namespace: "default"},
				Spec: corev1.ServiceSpec{
					Ports:    []corev1.ServicePort{{Name: mcpPortName, Port: 9090, Protocol: corev1.ProtocolTCP}},
					Selector: map[string]string{"app": "external"},
				},
			}
			Expect(k8sClient.Create(ctx, ext)).To(Succeed())

			server := newTestMCPServer(resourceName)
			server.Spec.Config.Port = 8080
			server.Spec.Config.Path = DefaultMCPPath
			server.Spec.ServiceRef = &mcpv1beta1.ServiceReference{Name: externalName}
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			cleanupObject(ctx, &mcpv1beta1.MCPServer{}, typeNamespacedName)
			cleanupObject(ctx, &corev1.Service{}, types.NamespacedName{Name: externalName, Namespace: "default"})
			cleanupObject(ctx, &corev1.Service{}, typeNamespacedName)
		})

		It("should resume managed-Service reconciliation", func() {
			r := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, typeNamespacedName, &corev1.Service{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "no managed Service while serviceRef is set")

			server := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, server)).To(Succeed())
			server.Spec.ServiceRef = nil
			Expect(k8sClient.Update(ctx, server)).To(Succeed())

			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			managed := &corev1.Service{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, managed)).To(Succeed())
			owner := metav1.GetControllerOf(managed)
			Expect(owner).NotTo(BeNil())
			Expect(owner.UID).To(Equal(server.UID))
		})
	})

	Context("When serviceRef points at the MCPServer's own managed Service (self-reference)", func() {
		const resourceName = "test-cleanup-self-ref"

		typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			server := newTestMCPServer(resourceName)
			server.Spec.Config.Port = 8080
			server.Spec.Config.Path = DefaultMCPPath
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			cleanupObject(ctx, &mcpv1beta1.MCPServer{}, typeNamespacedName)
			cleanupObject(ctx, &corev1.Service{}, typeNamespacedName)
		})

		It("should preserve the managed Service", func() {
			r := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, &corev1.Service{})).To(Succeed())

			server := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, server)).To(Succeed())
			server.Spec.ServiceRef = &mcpv1beta1.ServiceReference{Name: resourceName}
			Expect(k8sClient.Update(ctx, server)).To(Succeed())

			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, typeNamespacedName, &corev1.Service{})).To(Succeed(),
				"self-referenced Service must not be deleted")
		})
	})
})

var _ = Describe("cleanupManagedServiceForBYO - unit tests", func() {
	ctx := context.Background()

	const name = "srv-name"
	const namespace = "default"
	const externalName = "external-svc"
	const ownerUID = types.UID("mcpserver-uid")

	newServer := func(serviceRefName string) *mcpv1beta1.MCPServer {
		return &mcpv1beta1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: ownerUID},
			Spec: mcpv1beta1.MCPServerSpec{
				ServiceRef: &mcpv1beta1.ServiceReference{Name: serviceRefName},
			},
		}
	}

	// managedService builds a Service named after the MCPServer with a controller
	// owner reference carrying the given UID, mimicking an operator-managed Service.
	managedService := func(controllerUID types.UID) *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: mcpv1beta1.GroupVersion.String(),
					Kind:       mcpv1beta1.MCPServerKind,
					Name:       name,
					UID:        controllerUID,
					Controller: new(true),
				}},
			},
			Spec: corev1.ServiceSpec{
				Ports:    []corev1.ServicePort{{Name: mcpPortName, Port: 8080, Protocol: corev1.ProtocolTCP}},
				Selector: map[string]string{"app": name},
			},
		}
	}

	externalService := func() *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: externalName, Namespace: namespace},
			Spec: corev1.ServiceSpec{
				Ports:    []corev1.ServicePort{{Name: mcpPortName, Port: 9090}},
				Selector: map[string]string{"app": "external"},
			},
		}
	}

	It("does not delete the managed Service when the external Service cannot be resolved", func() {
		managed := managedService(ownerUID)
		fakeClient := fake.NewClientBuilder().
			WithScheme(k8sClient.Scheme()).
			WithObjects(managed).
			Build()
		r := &MCPServerReconciler{Client: fakeClient, Scheme: k8sClient.Scheme(), APIReader: fakeClient}

		err := r.cleanupManagedServiceForBYO(ctx, newServer("missing-external"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("missing-external"))
		Expect(err.Error()).To(ContainSubstring("not found"))

		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &corev1.Service{})).
			To(Succeed(), "managed Service must survive an unresolvable external reference")
	})

	It("does not delete a Service owned by a different controller", func() {
		managed := managedService("some-other-uid")
		fakeClient := fake.NewClientBuilder().
			WithScheme(k8sClient.Scheme()).
			WithObjects(managed, externalService()).
			Build()
		r := &MCPServerReconciler{Client: fakeClient, Scheme: k8sClient.Scheme(), APIReader: fakeClient}

		Expect(r.cleanupManagedServiceForBYO(ctx, newServer("external-svc"))).To(Succeed())

		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &corev1.Service{})).
			To(Succeed(), "foreign-owned Service must not be deleted")
	})

	It("does not delete a Service that has no controller owner", func() {
		managed := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: corev1.ServiceSpec{
				Ports:    []corev1.ServicePort{{Name: mcpPortName, Port: 8080}},
				Selector: map[string]string{"app": name},
			},
		}
		fakeClient := fake.NewClientBuilder().
			WithScheme(k8sClient.Scheme()).
			WithObjects(managed, externalService()).
			Build()
		r := &MCPServerReconciler{Client: fakeClient, Scheme: k8sClient.Scheme(), APIReader: fakeClient}

		Expect(r.cleanupManagedServiceForBYO(ctx, newServer("external-svc"))).To(Succeed())

		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &corev1.Service{})).
			To(Succeed(), "user-owned Service must not be deleted")
	})

	It("does not delete when the Service was replaced between read and delete (precondition race)", func() {
		managed := managedService(ownerUID)
		base := fake.NewClientBuilder().
			WithScheme(k8sClient.Scheme()).
			WithObjects(managed, externalService()).
			Build()
		// Intercept Get of the managed Service to hand back a stale resourceVersion,
		// simulating a concurrent replacement. The Delete precondition then mismatches
		// the record and the fake API server rejects the delete with a conflict.
		racingClient := interceptor.NewClient(base, interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if err := c.Get(ctx, key, obj, opts...); err != nil {
					return err
				}
				if svc, ok := obj.(*corev1.Service); ok && svc.Name == name {
					svc.ResourceVersion = "stale-0"
				}
				return nil
			},
		})
		r := &MCPServerReconciler{Client: racingClient, Scheme: k8sClient.Scheme(), APIReader: racingClient}

		Expect(r.cleanupManagedServiceForBYO(ctx, newServer("external-svc"))).To(Succeed())

		Expect(base.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &corev1.Service{})).
			To(Succeed(), "a replaced Service must not be deleted by a stale precondition")
	})

	It("deletes the managed Service owned by this MCPServer once the external Service resolves", func() {
		managed := managedService(ownerUID)
		fakeClient := fake.NewClientBuilder().
			WithScheme(k8sClient.Scheme()).
			WithObjects(managed, externalService()).
			Build()
		r := &MCPServerReconciler{Client: fakeClient, Scheme: k8sClient.Scheme(), APIReader: fakeClient}

		Expect(r.cleanupManagedServiceForBYO(ctx, newServer("external-svc"))).To(Succeed())

		err := fakeClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &corev1.Service{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "owned managed Service should be deleted")
	})

	It("preserves a self-referencing Service without resolving an external Service", func() {
		managed := managedService(ownerUID)
		// No external Service object; a self-reference must short-circuit before any
		// resolution or deletion.
		fakeClient := fake.NewClientBuilder().
			WithScheme(k8sClient.Scheme()).
			WithObjects(managed).
			Build()
		r := &MCPServerReconciler{Client: fakeClient, Scheme: k8sClient.Scheme(), APIReader: fakeClient}

		Expect(r.cleanupManagedServiceForBYO(ctx, newServer(name))).To(Succeed())

		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &corev1.Service{})).
			To(Succeed(), "self-referenced Service must be preserved")
	})
})

// cleanupObject deletes obj identified by key, ignoring not-found.
func cleanupObject(ctx context.Context, obj client.Object, key types.NamespacedName) {
	if err := k8sClient.Get(ctx, key, obj); err == nil {
		Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
	}
}
