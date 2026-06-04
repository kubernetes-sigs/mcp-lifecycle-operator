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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

const (
	// AnnotationAutoCreatePrerequisites controls whether the operator creates
	// missing ConfigMaps and ServiceAccounts referenced by the MCPServer CR.
	AnnotationAutoCreatePrerequisites = "mcp.x-k8s.io/auto-create-prerequisites"

	// eventActionPrerequisiteCreated is the reporting action when a prerequisite resource is auto-created.
	eventActionPrerequisiteCreated = "PrerequisiteCreated"
)

// ensurePrerequisites creates missing ServiceAccounts and ConfigMaps referenced
// by the MCPServer when the auto-create-prerequisites annotation is set to "true".
// Created resources are owned by the MCPServer so they are cleaned up on deletion.
func (r *MCPServerReconciler) ensurePrerequisites(ctx context.Context, server *mcpv1alpha1.MCPServer) error {
	if server.Annotations[AnnotationAutoCreatePrerequisites] != "true" {
		return nil
	}

	logger := log.FromContext(ctx)
	logger.Info("Auto-creating prerequisites for MCPServer", "name", server.Name)

	if err := r.ensureServiceAccount(ctx, server); err != nil {
		return err
	}

	if err := r.ensureConfigMaps(ctx, server); err != nil {
		return err
	}

	return nil
}

// ensureServiceAccount creates the ServiceAccount referenced by
// spec.runtime.security.serviceAccountName if it does not already exist.
func (r *MCPServerReconciler) ensureServiceAccount(ctx context.Context, server *mcpv1alpha1.MCPServer) error {
	saName := server.Spec.Runtime.Security.ServiceAccountName
	if saName == "" {
		return nil
	}

	sa := &corev1.ServiceAccount{}
	err := r.Get(ctx, client.ObjectKey{Name: saName, Namespace: server.Namespace}, sa)
	if err == nil {
		// Already exists, nothing to do.
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to check ServiceAccount %s: %w", saName, err)
	}

	sa = &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      saName,
			Namespace: server.Namespace,
		},
	}
	if err := controllerutil.SetControllerReference(server, sa, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on ServiceAccount %s: %w", saName, err)
	}
	if err := r.Create(ctx, sa); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("failed to create ServiceAccount %s: %w", saName, err)
	}

	r.emitPrerequisiteCreated(server, "ServiceAccount", saName)
	return nil
}

// ensureConfigMaps creates any ConfigMaps referenced in spec.config.storage
// that do not already exist.
func (r *MCPServerReconciler) ensureConfigMaps(ctx context.Context, server *mcpv1alpha1.MCPServer) error {
	for i, storage := range server.Spec.Config.Storage {
		if storage.Source.Type != mcpv1alpha1.StorageTypeConfigMap {
			continue
		}
		if storage.Source.ConfigMap == nil {
			continue
		}

		cmName := storage.Source.ConfigMap.Name
		if cmName == "" {
			continue
		}

		cm := &corev1.ConfigMap{}
		err := r.Get(ctx, client.ObjectKey{Name: cmName, Namespace: server.Namespace}, cm)
		if err == nil {
			// Already exists, nothing to do.
			continue
		}
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to check ConfigMap %s: %w", cmName, err)
		}

		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: server.Namespace,
			},
			Data: map[string]string{},
		}
		if err := controllerutil.SetControllerReference(server, cm, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on ConfigMap %s: %w", cmName, err)
		}
		if err := r.Create(ctx, cm); err != nil {
			if apierrors.IsAlreadyExists(err) {
				continue
			}
			return fmt.Errorf("failed to create ConfigMap %s at storage index %d: %w", cmName, i, err)
		}

		r.emitPrerequisiteCreated(server, "ConfigMap", cmName)
	}

	return nil
}

func (r *MCPServerReconciler) emitPrerequisiteCreated(server *mcpv1alpha1.MCPServer, kind, name string) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(server, nil, corev1.EventTypeNormal, "CreatedPrerequisite",
		eventActionPrerequisiteCreated, "Created %s %s for MCPServer %s", kind, name, server.Name)
}
