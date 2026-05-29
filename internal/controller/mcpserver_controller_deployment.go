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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

// reconcileDeployment creates or updates the Deployment for the MCPServer
// and returns the current state of the deployment.
func (r *MCPServerReconciler) reconcileDeployment(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
) (*appsv1.Deployment, error) {
	logger := log.FromContext(ctx)

	deployment, err := r.createDeployment(mcpServer)
	if err != nil {
		logger.Error(err, "Failed to create Deployment")
		return nil, err
	}
	if err := controllerutil.SetControllerReference(mcpServer, deployment, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference for Deployment")
		return nil, err
	}

	if err := r.applyConfigHash(ctx, mcpServer, deployment); err != nil {
		return nil, err
	}

	existingDeployment := &appsv1.Deployment{}
	err = r.Get(ctx, client.ObjectKey{Name: deployment.Name, Namespace: deployment.Namespace}, existingDeployment)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Creating Deployment", "name", deployment.Name)
		if err := applyCustomDeploymentMetadata(mcpServer, deployment); err != nil {
			return nil, fmt.Errorf("applying custom metadata failed; %w", err)
		}
		if err := r.Create(ctx, deployment); err != nil {
			logger.Error(err, "Failed to create Deployment")
			return nil, err
		}
		// Return the deployment object we just created.
		// Don't try to Get it immediately - this can cause a race condition
		// where the API server hasn't fully processed the creation yet.
		// The deployment status will be empty, which is fine - the Ready condition
		// will be set to Unknown/Initializing, and we'll requeue to check again.
		return deployment, nil
	} else if err != nil {
		logger.Error(err, "Failed to get Deployment")
		return nil, err
	}

	// Validate ownership before updating
	if err := r.validateOwnership(existingDeployment, mcpServer); err != nil {
		logger.Error(err, "Deployment ownership validation failed")
		return nil, err
	}

	// Check if we need to adopt an orphaned resource by comparing owner UIDs before updating
	oldOwnerUID := ""
	if oldOwner := metav1.GetControllerOf(existingDeployment); oldOwner != nil {
		oldOwnerUID = string(oldOwner.UID)
	}

	// Update ownerReferences to establish/refresh controller ownership.
	// This is safe because validateOwnership has confirmed we can manage this resource.
	// For orphaned resources, this adopts them by updating the stale UID.
	if err := controllerutil.SetControllerReference(mcpServer, existingDeployment, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference for existing Deployment")
		return nil, err
	}

	// Check if we actually adopted an orphaned resource (owner UID changed)
	ownershipChanged := false
	if newOwner := metav1.GetControllerOf(existingDeployment); newOwner != nil {
		ownershipChanged = oldOwnerUID != string(newOwner.UID)
	}

	needsUpdate := deploymentNeedsUpdate(mcpServer, existingDeployment, deployment, ownershipChanged)
	if needsUpdate && len(existingDeployment.Spec.Template.Spec.Containers) == 0 {
		logger.Info("Recovering deployment with empty containers list", "name", existingDeployment.Name)
	}
	if needsUpdate {
		logger.Info("Updating Deployment", "name", existingDeployment.Name)
		existingDeployment.Spec.Replicas = deployment.Spec.Replicas
		existingDeployment.Spec.Template.Annotations = deployment.Spec.Template.Annotations
		existingDeployment.Spec.Template.Spec = deployment.Spec.Template.Spec
		if err := applyCustomDeploymentMetadata(mcpServer, existingDeployment); err != nil {
			return nil, fmt.Errorf("applying custom metadata failed; %w", err)
		}
		if err := r.Update(ctx, existingDeployment); err != nil {
			logger.Error(err, "Failed to update Deployment")
			return nil, err
		}
		// Re-fetch deployment to get current status after update
		if err := r.Get(ctx, client.ObjectKey{
			Name: existingDeployment.Name, Namespace: existingDeployment.Namespace,
		}, existingDeployment); err != nil {
			logger.Error(err, "Failed to get updated Deployment")
			return nil, err
		}
	} else {
		logger.Info("Deployment already exists and is up to date", "name", deployment.Name)
	}

	return existingDeployment, nil
}

func deploymentNeedsUpdate(mcpServer *mcpv1alpha1.MCPServer, existing, desired *appsv1.Deployment, ownershipChanged bool) bool {
	oldPodSpec := existing.Spec.Template.Spec
	newPodSpec := desired.Spec.Template.Spec

	if len(oldPodSpec.Containers) == 0 {
		return true
	}

	if len(newPodSpec.Containers) == 0 {
		return false // update would be rejected anyways by api server
	}

	return !equality.Semantic.DeepDerivative(newPodSpec, oldPodSpec) ||
		// Explicit DeepEqual checks for fields that can be zeroed/removed by the user.
		// DeepDerivative skips zero-value fields in the desired spec, so removals
		// (clearing args, env, volumes, etc.) would go undetected without these.
		!equality.Semantic.DeepEqual(oldPodSpec.Containers[0].Args, newPodSpec.Containers[0].Args) ||
		!equality.Semantic.DeepEqual(oldPodSpec.Containers[0].Env, newPodSpec.Containers[0].Env) ||
		!equality.Semantic.DeepEqual(oldPodSpec.Containers[0].EnvFrom, newPodSpec.Containers[0].EnvFrom) ||
		!equality.Semantic.DeepEqual(oldPodSpec.SecurityContext, newPodSpec.SecurityContext) ||
		!equality.Semantic.DeepEqual(oldPodSpec.Volumes, newPodSpec.Volumes) ||
		!equality.Semantic.DeepEqual(oldPodSpec.Containers[0].VolumeMounts, newPodSpec.Containers[0].VolumeMounts) ||
		!equality.Semantic.DeepEqual(oldPodSpec.Containers[0].Resources, newPodSpec.Containers[0].Resources) ||
		!equality.Semantic.DeepEqual(oldPodSpec.Containers[0].Ports, newPodSpec.Containers[0].Ports) ||
		!equality.Semantic.DeepEqual(oldPodSpec.Containers[0].SecurityContext, newPodSpec.Containers[0].SecurityContext) ||
		!equality.Semantic.DeepEqual(oldPodSpec.Containers[0].LivenessProbe, newPodSpec.Containers[0].LivenessProbe) ||
		!equality.Semantic.DeepEqual(oldPodSpec.Containers[0].ReadinessProbe, newPodSpec.Containers[0].ReadinessProbe) ||
		oldPodSpec.ServiceAccountName != newPodSpec.ServiceAccountName ||
		!equality.Semantic.DeepEqual(existing.Spec.Replicas, desired.Spec.Replicas) ||
		!equality.Semantic.DeepEqual(existing.Spec.Template.Annotations, desired.Spec.Template.Annotations) ||
		deploymentAnnotationsChanged(mcpServer, existing) ||
		deploymentLabelsChanged(mcpServer, existing) ||
		ownershipChanged
}

func managedWorkloadLabels(mcpServerName string) map[string]string {
	return map[string]string{
		LabelKeyApp:       ManagedWorkloadName,
		LabelKeyMCPServer: mcpServerName,
	}
}

func managedWorkloadSelector(mcpServerName string) map[string]string {
	return map[string]string{LabelKeyMCPServer: mcpServerName}
}

// createDeployment creates a Deployment for the MCPServer
func (r *MCPServerReconciler) createDeployment(mcpServer *mcpv1alpha1.MCPServer) (*appsv1.Deployment, error) {
	// Validate source type and extract image reference
	var imageRef string
	switch mcpServer.Spec.Source.Type {
	case mcpv1alpha1.SourceTypeContainerImage:
		if mcpServer.Spec.Source.ContainerImage == nil {
			return nil, fmt.Errorf("containerImage must be set when source type is ContainerImage")
		}
		imageRef = mcpServer.Spec.Source.ContainerImage.Ref
	default:
		return nil, fmt.Errorf("unsupported source type: %s", mcpServer.Spec.Source.Type)
	}

	// Replicas defaults to 1 when not specified (nil)
	replicas := int32(1)
	if mcpServer.Spec.Runtime.Replicas != nil {
		replicas = *mcpServer.Spec.Runtime.Replicas
	}
	labels := managedWorkloadLabels(mcpServer.Name)

	container := corev1.Container{
		Name:  ManagedWorkloadName,
		Image: imageRef,
		Ports: []corev1.ContainerPort{
			{
				Name:          "mcp",
				ContainerPort: mcpServer.Spec.Config.Port,
				Protocol:      corev1.ProtocolTCP,
			},
		},
	}

	// Add args if specified
	if len(mcpServer.Spec.Config.Arguments) > 0 {
		container.Args = mcpServer.Spec.Config.Arguments
	}

	// Add env vars if specified
	if len(mcpServer.Spec.Config.Env) > 0 {
		container.Env = mcpServer.Spec.Config.Env
	}
	if len(mcpServer.Spec.Config.EnvFrom) > 0 {
		container.EnvFrom = mcpServer.Spec.Config.EnvFrom
	}

	// Apply security context: use user-specified if provided, otherwise apply restricted defaults
	if mcpServer.Spec.Runtime.Security.SecurityContext != nil {
		container.SecurityContext = mcpServer.Spec.Runtime.Security.SecurityContext
	} else {
		container.SecurityContext = defaultContainerSecurityContext()
	}

	// Apply resource requirements if specified
	if mcpServer.Spec.Runtime.Resources != nil {
		container.Resources = *mcpServer.Spec.Runtime.Resources
	}

	// Apply health probes. Zero-valued timing fields are filled with Kubernetes
	// API server defaults via withProbeDefaults so that DeepEqual comparisons in
	// deploymentNeedsUpdate match the API-server-defaulted existing spec.
	//
	// When no readiness probe is specified, a default TCP socket probe is injected
	// targeting the configured MCP port.
	if mcpServer.Spec.Runtime.Health.LivenessProbe != nil {
		container.LivenessProbe = withProbeDefaults(mcpServer.Spec.Runtime.Health.LivenessProbe)
	}
	if mcpServer.Spec.Runtime.Health.ReadinessProbe != nil {
		container.ReadinessProbe = withProbeDefaults(mcpServer.Spec.Runtime.Health.ReadinessProbe)
	} else {
		container.ReadinessProbe = defaultMCPReadinessProbe(mcpServer.Spec.Config.Port)
	}

	// Process storage mounts
	volumes, volumeMounts := r.processStorageMounts(mcpServer)
	container.VolumeMounts = volumeMounts

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: managedWorkloadSelector(mcpServer.Name),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{container},
					Volumes:    volumes,
				},
			},
		},
	}

	// Add security settings if specified
	// Only set ServiceAccountName if non-empty; otherwise leave unset for Kubernetes to default
	if mcpServer.Spec.Runtime.Security.ServiceAccountName != "" {
		deployment.Spec.Template.Spec.ServiceAccountName = mcpServer.Spec.Runtime.Security.ServiceAccountName
	}
	// Default to an empty PodSecurityContext so the desired spec matches the
	// API server default. Without this, nil vs &PodSecurityContext{} causes
	// deploymentNeedsUpdate to return true on every reconciliation.
	if mcpServer.Spec.Runtime.Security.PodSecurityContext != nil {
		deployment.Spec.Template.Spec.SecurityContext = mcpServer.Spec.Runtime.Security.PodSecurityContext
	} else {
		deployment.Spec.Template.Spec.SecurityContext = &corev1.PodSecurityContext{}
	}

	return deployment, nil
}

// processStorageMounts builds volumes and volume mounts from the MCPServer storage configuration.
// Validation of referenced ConfigMaps and Secrets is done in validateConfig.
func (r *MCPServerReconciler) processStorageMounts(
	mcpServer *mcpv1alpha1.MCPServer,
) ([]corev1.Volume, []corev1.VolumeMount) {
	volumes := make([]corev1.Volume, 0, len(mcpServer.Spec.Config.Storage))
	volumeMounts := make([]corev1.VolumeMount, 0, len(mcpServer.Spec.Config.Storage))

	for i, storage := range mcpServer.Spec.Config.Storage {
		volumeName := fmt.Sprintf("vol-%d", i)

		volumeMount := corev1.VolumeMount{
			Name:      volumeName,
			MountPath: storage.Path,
		}

		// Default to ReadOnly if not specified
		permissions := storage.Permissions
		if permissions == "" {
			permissions = mcpv1alpha1.MountPermissionsReadOnly
		}

		switch permissions {
		case mcpv1alpha1.MountPermissionsReadOnly:
			volumeMount.ReadOnly = true
		case mcpv1alpha1.MountPermissionsReadWrite:
			volumeMount.ReadOnly = false
		case mcpv1alpha1.MountPermissionsRecursiveReadOnly:
			volumeMount.ReadOnly = true
			volumeMount.RecursiveReadOnly = new(corev1.RecursiveReadOnlyEnabled)
		}

		volumeMounts = append(volumeMounts, volumeMount)

		volume := corev1.Volume{
			Name: volumeName,
		}

		switch storage.Source.Type {
		case mcpv1alpha1.StorageTypeConfigMap:
			// Validation already done in validateConfig
			volume.ConfigMap = storage.Source.ConfigMap
		case mcpv1alpha1.StorageTypeSecret:
			// Validation already done in validateConfig
			volume.Secret = storage.Source.Secret
		case mcpv1alpha1.StorageTypeEmptyDir:
			// No validation needed - EmptyDir is created by Kubernetes
			volume.EmptyDir = storage.Source.EmptyDir
		}

		volumes = append(volumes, volume)
	}

	return volumes, volumeMounts
}

// defaultContainerSecurityContext returns the "restricted" Pod Security Standard
// security context applied to MCP server containers by default.
func defaultContainerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: new(false),
		ReadOnlyRootFilesystem:   new(true),
		RunAsNonRoot:             new(true),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
}

// defaultMCPReadinessProbe returns a TCP socket readiness probe targeting the
// configured MCP port. This is injected when the user does not specify a custom
// readiness probe, ensuring that containers not listening on the expected port
// will not report as Ready. A TCP probe is used instead of HTTP GET because
// the MCP Streamable HTTP spec only requires POST support on the endpoint path;
// a GET probe would reject valid MCP servers that do not serve GET.
// The controller-level MCP handshake provides the semantic protocol validation.
func defaultMCPReadinessProbe(port int32) *corev1.Probe {
	return withProbeDefaults(&corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt32(port),
			},
		},
	})
}

// withProbeDefaults fills in zero-valued probe timing fields with the
// Kubernetes API server defaults. Without this, DeepEqual in
// deploymentNeedsUpdate flags a diff between the desired spec and the
// API-server-defaulted existing spec on every reconciliation.
func withProbeDefaults(probe *corev1.Probe) *corev1.Probe {
	out := *probe
	if out.FailureThreshold == 0 {
		out.FailureThreshold = 3
	}
	if out.PeriodSeconds == 0 {
		out.PeriodSeconds = 10
	}
	if out.SuccessThreshold == 0 {
		out.SuccessThreshold = 1
	}
	if out.TimeoutSeconds == 0 {
		out.TimeoutSeconds = 1
	}
	return &out
}
