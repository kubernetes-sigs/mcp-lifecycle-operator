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
// https://github.com/kubernetes-sigs/kubebuilder/blob/v4.11.1/pkg/plugins/golang/v4/scaffolds/internal/templates/controllers/controller.go

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	v1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	acv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1/applyconfiguration/api/v1alpha1"
)

const (
	fieldManager = "mcpserver-controller"
)

// Condition types for MCPServer status.
const (
	// ConditionTypeAccepted indicates the MCPServer configuration is valid.
	ConditionTypeAccepted = "Accepted"
	// ConditionTypeReady indicates the MCPServer is ready to serve requests.
	ConditionTypeReady = "Ready"
)

// Reasons for Accepted condition.
const (
	ReasonValid   = "Valid"
	ReasonInvalid = "Invalid"
	ReasonUnknown = "Unknown"
)

// Reasons for Ready condition.
const (
	ReasonAvailable             = "Available"
	ReasonConfigurationInvalid  = "ConfigurationInvalid"
	ReasonDeploymentUnavailable = "DeploymentUnavailable"
	ReasonServiceUnavailable    = "ServiceUnavailable"
	ReasonScaledToZero          = "ScaledToZero"
	ReasonInitializing          = "Initializing"
)

// Reconciliation constants.
const (
	// requeueDelayDeploymentUnavailable is the delay before requeuing when a deployment is not yet available.
	requeueDelayDeploymentUnavailable = 15 * time.Second
)

// Index keys for field indexing.
const (
	// configMapIndexKey is the index key for finding MCPServers by ConfigMap reference.
	configMapIndexKey = "spec.configMapRefs"
	// secretIndexKey is the index key for finding MCPServers by Secret reference.
	secretIndexKey = "spec.secretRefs"
)

// MCPServerReconciler reconciles a MCPServer object
type MCPServerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcpservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcpservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcpservers/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.1/pkg/reconcile
func (r *MCPServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the MCPServer instance
	mcpServer := &mcpv1alpha1.MCPServer{}
	if err := r.Get(ctx, req.NamespacedName, mcpServer); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("MCPServer resource not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get MCPServer")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling MCPServer", "name", mcpServer.Name, "namespace", mcpServer.Namespace)

	// Validate configuration and set Accepted condition
	acceptedCondition, configValid := r.setAcceptedCondition(ctx, mcpServer)
	preserveLastTransitionTime(&acceptedCondition, mcpServer.Status.Conditions)

	// If configuration is not valid, update status and stop
	if !configValid {
		readyCondition := newCondition(
			ConditionTypeReady,
			metav1.ConditionFalse,
			ReasonConfigurationInvalid,
			"Configuration must be fixed before server can start",
			mcpServer.Generation,
		)
		preserveLastTransitionTime(&readyCondition, mcpServer.Status.Conditions)

		status := acv1alpha1.MCPServerStatus().
			WithObservedGeneration(mcpServer.Generation).
			WithServiceName(mcpServer.Name).
			WithConditions(
				conditionToAC(acceptedCondition),
				conditionToAC(readyCondition),
			)

		if err := r.applyStatus(ctx, mcpServer, status); err != nil {
			logger.Error(err, "Failed to update MCPServer status")
			return ctrl.Result{}, err
		}

		logger.Info("MCPServer configuration is invalid", "reason", acceptedCondition.Reason)
		return ctrl.Result{}, nil
	}

	// Configuration is valid, proceed with deployment reconciliation
	existingDeployment, err := r.reconcileDeployment(ctx, mcpServer)
	if err != nil {
		// Deployment reconciliation failed - update status
		readyCondition := newCondition(
			ConditionTypeReady,
			metav1.ConditionFalse,
			ReasonDeploymentUnavailable,
			fmt.Sprintf("Failed to reconcile Deployment: %v", err),
			mcpServer.Generation,
		)
		preserveLastTransitionTime(&readyCondition, mcpServer.Status.Conditions)

		status := acv1alpha1.MCPServerStatus().
			WithObservedGeneration(mcpServer.Generation).
			WithServiceName(mcpServer.Name).
			WithConditions(
				conditionToAC(acceptedCondition),
				conditionToAC(readyCondition),
			)

		if err := r.applyStatus(ctx, mcpServer, status); err != nil {
			logger.Error(err, "Failed to update MCPServer status")
		}
		return ctrl.Result{}, err
	}

	// Reconcile Service
	if err := r.reconcileService(ctx, mcpServer); err != nil {
		// Service reconciliation failed - update status
		readyCondition := newCondition(
			ConditionTypeReady,
			metav1.ConditionFalse,
			ReasonServiceUnavailable,
			fmt.Sprintf("Failed to reconcile Service: %v", err),
			mcpServer.Generation,
		)
		preserveLastTransitionTime(&readyCondition, mcpServer.Status.Conditions)

		status := acv1alpha1.MCPServerStatus().
			WithObservedGeneration(mcpServer.Generation).
			WithDeploymentName(existingDeployment.Name).
			WithServiceName(mcpServer.Name).
			WithConditions(
				conditionToAC(acceptedCondition),
				conditionToAC(readyCondition),
			)

		if err := r.applyStatus(ctx, mcpServer, status); err != nil {
			logger.Error(err, "Failed to update MCPServer status")
		}
		return ctrl.Result{}, err
	}

	// Determine Ready condition based on deployment status
	readyCondition := determineReadyCondition(
		existingDeployment,
		acceptedCondition,
		mcpServer.Generation,
		mcpServer.Status.Conditions,
	)

	// Build status
	path := mcpServer.Spec.Config.Path
	if path == "" {
		path = "/mcp"
	}

	status := acv1alpha1.MCPServerStatus().
		WithObservedGeneration(mcpServer.Generation).
		WithDeploymentName(existingDeployment.Name).
		WithServiceName(mcpServer.Name).
		WithAddress(acv1alpha1.MCPServerAddress().
			WithURL(fmt.Sprintf("http://%s.%s.svc.cluster.local:%d%s",
				mcpServer.Name, mcpServer.Namespace, mcpServer.Spec.Config.Port, path))).
		WithConditions(
			conditionToAC(acceptedCondition),
			conditionToAC(readyCondition),
		)

	if err := r.applyStatus(ctx, mcpServer, status); err != nil {
		logger.Error(err, "Failed to apply MCPServer status")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled MCPServer",
		"accepted", acceptedCondition.Status,
		"ready", readyCondition.Status)

	// If Deployment is not yet available, requeue to check again later
	if readyCondition.Status == metav1.ConditionFalse && readyCondition.Reason == ReasonDeploymentUnavailable {
		logger.Info("Deployment not yet available, requeuing to check again",
			"requeueAfter", requeueDelayDeploymentUnavailable)
		return ctrl.Result{RequeueAfter: requeueDelayDeploymentUnavailable}, nil
	}

	return ctrl.Result{}, nil
}

// determineReadyCondition analyzes deployment status and accepted condition to determine
// the Ready condition.
func determineReadyCondition(
	deployment *appsv1.Deployment,
	acceptedCondition metav1.Condition,
	generation int64,
	existingConditions []metav1.Condition,
) metav1.Condition {
	// If configuration is not accepted, Ready=False
	if acceptedCondition.Status == metav1.ConditionFalse {
		condition := newCondition(
			ConditionTypeReady,
			metav1.ConditionFalse,
			ReasonConfigurationInvalid,
			"Configuration must be fixed before server can start",
			generation,
		)
		preserveLastTransitionTime(&condition, existingConditions)
		return condition
	}

	// Check if scaled to zero
	// Note: Following Kubernetes Deployment semantics, we set Ready=True when scaled to 0.
	// Scaling to zero is an intentional, valid desired state (not a failure).
	// This prevents false alerts and aligns with core K8s resource conventions where
	// conditions indicate "is the system in its desired state?" rather than "is it doing work?".
	// Users can check the ScaledToZero reason or status.replicas for operational state.
	if deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == 0 {
		condition := newCondition(
			ConditionTypeReady,
			metav1.ConditionTrue,
			ReasonScaledToZero,
			"Server is ready (scaled to 0 replicas)",
			generation,
		)
		preserveLastTransitionTime(&condition, existingConditions)
		return condition
	}

	// Extract deployment conditions
	deploymentAvailable := false
	deploymentProgressing := false
	deploymentReplicaFailure := false
	var deploymentMessage string

	for _, cond := range deployment.Status.Conditions {
		switch cond.Type {
		case appsv1.DeploymentAvailable:
			if cond.Status == corev1.ConditionTrue {
				deploymentAvailable = true
			}
		case appsv1.DeploymentProgressing:
			if cond.Status == corev1.ConditionTrue {
				deploymentProgressing = true
			}
			if cond.Status == corev1.ConditionFalse {
				deploymentMessage = cond.Message
			}
		case appsv1.DeploymentReplicaFailure:
			if cond.Status == corev1.ConditionTrue {
				deploymentReplicaFailure = true
				deploymentMessage = cond.Message
			}
		}
	}

	// Deployment has no status yet
	if len(deployment.Status.Conditions) == 0 && deployment.Status.ReadyReplicas == 0 {
		condition := newCondition(
			ConditionTypeReady,
			metav1.ConditionUnknown,
			ReasonInitializing,
			"Waiting for Deployment to report status",
			generation,
		)
		preserveLastTransitionTime(&condition, existingConditions)
		return condition
	}

	// Deployment hasn't processed the latest spec yet
	// Only check if ObservedGeneration is non-zero (0 is the initial state)
	if deployment.Status.ObservedGeneration > 0 && deployment.Status.ObservedGeneration < deployment.Generation {
		condition := newCondition(
			ConditionTypeReady,
			metav1.ConditionFalse,
			ReasonDeploymentUnavailable,
			"Deployment is processing spec update",
			generation,
		)
		preserveLastTransitionTime(&condition, existingConditions)
		return condition
	}

	// At least one replica is ready - SUCCESS
	if deploymentAvailable && deployment.Status.ReadyReplicas > 0 {
		message := fmt.Sprintf("MCP server is ready (%d of %d instances healthy)",
			deployment.Status.ReadyReplicas,
			ptr.Deref(deployment.Spec.Replicas, 1))
		condition := newCondition(
			ConditionTypeReady,
			metav1.ConditionTrue,
			ReasonAvailable,
			message,
			generation,
		)
		preserveLastTransitionTime(&condition, existingConditions)
		return condition
	}

	// Deployment exists but no replicas ready - FAILURE
	// This covers: ImagePullBackOff, CrashLoop, OOM, Security errors, Probe failures, etc.
	if deploymentReplicaFailure || (!deploymentProgressing && !deploymentAvailable) {
		message := analyzeDeploymentFailure(deploymentMessage)
		condition := newCondition(
			ConditionTypeReady,
			metav1.ConditionFalse,
			ReasonDeploymentUnavailable,
			message,
			generation,
		)
		preserveLastTransitionTime(&condition, existingConditions)
		return condition
	}

	// Still progressing (no replicas ready yet)
	condition := newCondition(
		ConditionTypeReady,
		metav1.ConditionFalse,
		ReasonDeploymentUnavailable,
		"Waiting for instances to become healthy",
		generation,
	)
	preserveLastTransitionTime(&condition, existingConditions)
	return condition
}

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

	existingDeployment := &appsv1.Deployment{}
	err = r.Get(ctx, client.ObjectKey{Name: deployment.Name, Namespace: deployment.Namespace}, existingDeployment)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Creating Deployment", "name", deployment.Name)
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

	oldPodSpec := existingDeployment.Spec.Template.Spec
	newPodSpec := deployment.Spec.Template.Spec

	var needsUpdate bool
	if len(oldPodSpec.Containers) == 0 {
		logger.Info("Recovering deployment with empty containers list", "name", existingDeployment.Name)
		needsUpdate = true
	} else {
		needsUpdate = !equality.Semantic.DeepDerivative(newPodSpec, oldPodSpec) ||
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
			!equality.Semantic.DeepEqual(oldPodSpec.Containers[0].LivenessProbe, newPodSpec.Containers[0].LivenessProbe) ||
			!equality.Semantic.DeepEqual(oldPodSpec.Containers[0].ReadinessProbe, newPodSpec.Containers[0].ReadinessProbe) ||
			oldPodSpec.ServiceAccountName != newPodSpec.ServiceAccountName ||
			!equality.Semantic.DeepEqual(existingDeployment.Spec.Replicas, deployment.Spec.Replicas) ||
			ownershipChanged
	}
	if needsUpdate {
		logger.Info("Updating Deployment", "name", existingDeployment.Name)
		existingDeployment.Spec.Replicas = deployment.Spec.Replicas
		existingDeployment.Spec.Template.Spec = deployment.Spec.Template.Spec
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
	labels := map[string]string{
		"app":        "mcp-server",
		"mcp-server": mcpServer.Name,
	}

	container := corev1.Container{
		Name:  "mcp-server",
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

	// Apply health probes if specified.
	// The probes are passed directly to the container spec without any transformation,
	// providing full compatibility with the Kubernetes Probe API. This allows users to
	// configure all probe types (httpGet, tcpSocket, exec, grpc) and all parameters
	// (delays, periods, thresholds) using standard Kubernetes probe configuration.
	if mcpServer.Spec.Runtime.Health.LivenessProbe != nil {
		container.LivenessProbe = mcpServer.Spec.Runtime.Health.LivenessProbe
	}
	if mcpServer.Spec.Runtime.Health.ReadinessProbe != nil {
		container.ReadinessProbe = mcpServer.Spec.Runtime.Health.ReadinessProbe
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
				MatchLabels: map[string]string{
					"mcp-server": mcpServer.Name,
				},
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
	deployment.Spec.Template.Spec.SecurityContext = mcpServer.Spec.Runtime.Security.PodSecurityContext

	return deployment, nil
}

// processStorageMounts builds volumes and volume mounts from the MCPServer storage configuration.
// Validation of referenced ConfigMaps and Secrets is done in setAcceptedCondition.
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
			volumeMount.RecursiveReadOnly = ptr.To(corev1.RecursiveReadOnlyEnabled)
		}

		volumeMounts = append(volumeMounts, volumeMount)

		volume := corev1.Volume{
			Name: volumeName,
		}

		switch storage.Source.Type {
		case mcpv1alpha1.StorageTypeConfigMap:
			// Validation already done in setAcceptedCondition
			volume.ConfigMap = storage.Source.ConfigMap
		case mcpv1alpha1.StorageTypeSecret:
			// Validation already done in setAcceptedCondition
			volume.Secret = storage.Source.Secret
		case mcpv1alpha1.StorageTypeEmptyDir:
			// No validation needed - EmptyDir is created by Kubernetes
			volume.EmptyDir = storage.Source.EmptyDir
		}

		volumes = append(volumes, volume)
	}

	return volumes, volumeMounts
}

// reconcileService creates or updates the Service for the MCPServer.
func (r *MCPServerReconciler) reconcileService(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
) error {
	logger := log.FromContext(ctx)

	service := r.createService(mcpServer)
	if err := controllerutil.SetControllerReference(mcpServer, service, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference for Service")
		return err
	}

	existingService := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, existingService)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Creating Service", "name", service.Name)
		if err := r.Create(ctx, service); err != nil {
			logger.Error(err, "Failed to create Service")
			return err
		}
		return nil
	} else if err != nil {
		logger.Error(err, "Failed to get Service")
		return err
	}

	// Validate ownership before updating
	if err := r.validateOwnership(existingService, mcpServer); err != nil {
		logger.Error(err, "Service ownership validation failed")
		return err
	}

	// Check if we need to adopt an orphaned resource by comparing owner UIDs before updating
	oldOwnerUID := ""
	if oldOwner := metav1.GetControllerOf(existingService); oldOwner != nil {
		oldOwnerUID = string(oldOwner.UID)
	}

	// Update ownerReferences to establish/refresh controller ownership.
	// This is safe because validateOwnership has confirmed we can manage this resource.
	// For orphaned resources, this adopts them by updating the stale UID.
	if err := controllerutil.SetControllerReference(mcpServer, existingService, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference for existing Service")
		return err
	}

	// Check if we actually adopted an orphaned resource (owner UID changed)
	ownershipChanged := false
	if newOwner := metav1.GetControllerOf(existingService); newOwner != nil {
		ownershipChanged = oldOwnerUID != string(newOwner.UID)
	}

	// Update if ports changed OR if we adopted an orphaned resource
	needsUpdate := !equality.Semantic.DeepEqual(service.Spec.Ports, existingService.Spec.Ports) || ownershipChanged
	if needsUpdate {
		logger.Info("Updating Service", "name", existingService.Name)
		existingService.Spec.Ports = service.Spec.Ports
		if err := r.Update(ctx, existingService); err != nil {
			logger.Error(err, "Failed to update Service")
			return err
		}
	} else {
		logger.Info("Service already exists and is up to date", "name", service.Name)
	}

	return nil
}

// createService creates a Service for the MCPServer
func (r *MCPServerReconciler) createService(mcpServer *mcpv1alpha1.MCPServer) *corev1.Service {
	labels := map[string]string{
		"app":        "mcp-server",
		"mcp-server": mcpServer.Name,
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"mcp-server": mcpServer.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "mcp",
					Port:       mcpServer.Spec.Config.Port,
					TargetPort: intstr.FromString("mcp"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	return service
}

// isSameGroupKind checks if an owner reference matches the expected API group and kind,
// ignoring the API version to support cross-version adoption scenarios.
func isSameGroupKind(ownerRef *metav1.OwnerReference, expectedGroup, expectedKind string) bool {
	if ownerRef.Kind != expectedKind {
		return false
	}

	ownerGV, err := schema.ParseGroupVersion(ownerRef.APIVersion)
	if err != nil {
		return false
	}

	return ownerGV.Group == expectedGroup
}

// validateOwnership checks if a resource is owned by a different controller.
// Returns an error if the resource has a controller owner that is not the given MCPServer,
// or if the resource has no controller owner (preventing silent adoption of unowned resources).
func (r *MCPServerReconciler) validateOwnership(
	obj client.Object,
	mcpServer *mcpv1alpha1.MCPServer,
) error {
	// Get the controller owner reference from the existing resource
	controllerOwner := metav1.GetControllerOf(obj)
	if controllerOwner == nil {
		// No controller owner - reject to prevent silent adoption
		// User must delete the existing resource or choose a different name for their MCPServer
		return fmt.Errorf("resource %s/%s exists but has no controller owner; "+
			"delete the resource first or choose a different name for the MCPServer",
			obj.GetNamespace(), obj.GetName())
	}

	// Check if the controller owner is this MCPServer by UID
	if controllerOwner.UID == mcpServer.UID {
		// Owned by this exact MCPServer instance - safe to update
		return nil
	}

	// Check if the owner is an MCPServer with the same name/namespace/group
	// This handles the case where the MCPServer was deleted and recreated
	// with the same name, and we want to adopt the orphaned resources.
	// We validate the API group but allow different versions to support upgrades.
	if isSameGroupKind(controllerOwner, mcpv1alpha1.GroupVersion.Group, mcpv1alpha1.MCPServerKind) &&
		controllerOwner.Name == mcpServer.Name &&
		obj.GetNamespace() == mcpServer.Namespace {
		// Owner is an MCPServer with same group/name/namespace but different UID
		// This means the old MCPServer was deleted and this is a new one
		// Safe to adopt the resources (version may differ during upgrades)
		return nil
	}

	// Resource is owned by a different controller
	return fmt.Errorf("resource %s/%s is owned by %s/%s (UID: %s), cannot be managed by MCPServer %s/%s (UID: %s)",
		obj.GetNamespace(), obj.GetName(),
		controllerOwner.Kind, controllerOwner.Name, controllerOwner.UID,
		mcpServer.Namespace, mcpServer.Name, mcpServer.UID)
}

func (r *MCPServerReconciler) applyStatus(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
	status *acv1alpha1.MCPServerStatusApplyConfiguration,
) error {
	return r.Status().Apply(ctx,
		acv1alpha1.MCPServer(mcpServer.Name, mcpServer.Namespace).WithStatus(status),
		client.FieldOwner(fieldManager),
		client.ForceOwnership,
	)
}

func conditionToAC(condition metav1.Condition) *v1ac.ConditionApplyConfiguration {
	return v1ac.Condition().
		WithType(condition.Type).
		WithStatus(condition.Status).
		WithReason(condition.Reason).
		WithMessage(condition.Message).
		WithObservedGeneration(condition.ObservedGeneration).
		WithLastTransitionTime(condition.LastTransitionTime)
}

// preserveLastTransitionTime keeps the existing LastTransitionTime when the
// condition status has not changed, so that timestamps reflect actual transitions.
func preserveLastTransitionTime(condition *metav1.Condition, existingConditions []metav1.Condition) {
	if existing := meta.FindStatusCondition(existingConditions, condition.Type); existing != nil && existing.Status == condition.Status {
		condition.LastTransitionTime = existing.LastTransitionTime
	}
}

// defaultContainerSecurityContext returns the "restricted" Pod Security Standard
// security context applied to MCP server containers by default.
func defaultContainerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(true),
		RunAsNonRoot:             ptr.To(true),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
}

// newCondition creates a new metav1.Condition with the current timestamp.
func newCondition(
	condType string,
	status metav1.ConditionStatus,
	reason string,
	message string,
	observedGeneration int64,
) metav1.Condition {
	return metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: observedGeneration,
		LastTransitionTime: metav1.Now(),
	}
}

// analyzeDeploymentFailure examines the deployment message to build a detailed message
// about why pods are not healthy. Returns a message with specifics.
func analyzeDeploymentFailure(deploymentMessage string) string {
	if deploymentMessage == "" {
		return "No healthy instances available"
	}

	// Extract the most relevant error information
	msg := deploymentMessage

	// Common patterns - extract the key info
	if strings.Contains(msg, "ImagePullBackOff") || strings.Contains(msg, "ErrImagePull") {
		return fmt.Sprintf("No healthy instances: ImagePullBackOff - %s", msg)
	}
	if strings.Contains(msg, "runAsNonRoot") || strings.Contains(msg, "CreateContainerConfigError") {
		return fmt.Sprintf("No healthy instances: CreateContainerConfigError - %s", msg)
	}
	if strings.Contains(msg, "OOMKilled") {
		return fmt.Sprintf("No healthy instances: OOMKilled - %s", msg)
	}
	if strings.Contains(msg, "CrashLoopBackOff") {
		return fmt.Sprintf("No healthy instances: CrashLoopBackOff - %s", msg)
	}
	if strings.Contains(msg, "Liveness probe failed") || strings.Contains(msg, "Readiness probe failed") {
		return fmt.Sprintf("No healthy instances: Probe failed - %s", msg)
	}

	// Generic failure
	return fmt.Sprintf("No healthy instances: %s", msg)
}

// validateStorageMount validates a single storage mount configuration.
// Returns an error condition and false if validation fails, otherwise returns zero condition and true.
func (r *MCPServerReconciler) validateStorageMount(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
	storage mcpv1alpha1.StorageMount,
	index int,
) (metav1.Condition, bool) {
	switch storage.Source.Type {
	case mcpv1alpha1.StorageTypeConfigMap:
		if storage.Source.ConfigMap == nil {
			return newCondition(
				ConditionTypeAccepted,
				metav1.ConditionFalse,
				ReasonInvalid,
				fmt.Sprintf("ConfigMap must be set for storage mount at index %d", index),
				mcpServer.Generation,
			), false
		}
		if storage.Source.ConfigMap.Name == "" {
			return newCondition(
				ConditionTypeAccepted,
				metav1.ConditionFalse,
				ReasonInvalid,
				fmt.Sprintf("ConfigMap name must not be empty for storage mount at index %d", index),
				mcpServer.Generation,
			), false
		}
		// Skip validation if optional
		if storage.Source.ConfigMap.Optional != nil && *storage.Source.ConfigMap.Optional {
			return metav1.Condition{}, true
		}
		// Validate ConfigMap exists
		configMap := &corev1.ConfigMap{}
		if err := r.Get(ctx, client.ObjectKey{
			Name:      storage.Source.ConfigMap.Name,
			Namespace: mcpServer.Namespace,
		}, configMap); err != nil {
			if apierrors.IsNotFound(err) {
				return newCondition(
					ConditionTypeAccepted,
					metav1.ConditionFalse,
					ReasonInvalid,
					fmt.Sprintf("ConfigMap '%s' not found in namespace '%s'",
						storage.Source.ConfigMap.Name, mcpServer.Namespace),
					mcpServer.Generation,
				), false
			}
			// Other errors (permissions, etc.) - still mark as not accepted
			return newCondition(
				ConditionTypeAccepted,
				metav1.ConditionFalse,
				ReasonInvalid,
				fmt.Sprintf("Failed to validate ConfigMap '%s': %v",
					storage.Source.ConfigMap.Name, err),
				mcpServer.Generation,
			), false
		}

	case mcpv1alpha1.StorageTypeSecret:
		if storage.Source.Secret == nil {
			return newCondition(
				ConditionTypeAccepted,
				metav1.ConditionFalse,
				ReasonInvalid,
				fmt.Sprintf("Secret must be set for storage mount at index %d", index),
				mcpServer.Generation,
			), false
		}
		if storage.Source.Secret.SecretName == "" {
			return newCondition(
				ConditionTypeAccepted,
				metav1.ConditionFalse,
				ReasonInvalid,
				fmt.Sprintf("Secret name must not be empty for storage mount at index %d", index),
				mcpServer.Generation,
			), false
		}
		// Skip validation if optional
		if storage.Source.Secret.Optional != nil && *storage.Source.Secret.Optional {
			return metav1.Condition{}, true
		}
		// Validate Secret exists
		secret := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{
			Name:      storage.Source.Secret.SecretName,
			Namespace: mcpServer.Namespace,
		}, secret); err != nil {
			if apierrors.IsNotFound(err) {
				return newCondition(
					ConditionTypeAccepted,
					metav1.ConditionFalse,
					ReasonInvalid,
					fmt.Sprintf("Secret '%s' not found in namespace '%s'",
						storage.Source.Secret.SecretName, mcpServer.Namespace),
					mcpServer.Generation,
				), false
			}
			return newCondition(
				ConditionTypeAccepted,
				metav1.ConditionFalse,
				ReasonInvalid,
				fmt.Sprintf("Failed to validate Secret '%s': %v",
					storage.Source.Secret.SecretName, err),
				mcpServer.Generation,
			), false
		}

	case mcpv1alpha1.StorageTypeEmptyDir:
		// Validate EmptyDir configuration is present
		if storage.Source.EmptyDir == nil {
			return newCondition(
				ConditionTypeAccepted,
				metav1.ConditionFalse,
				ReasonInvalid,
				fmt.Sprintf("EmptyDir must be set for storage mount at index %d", index),
				mcpServer.Generation,
			), false
		}

	default:
		// Unknown/unsupported storage type
		return newCondition(
			ConditionTypeAccepted,
			metav1.ConditionFalse,
			ReasonInvalid,
			fmt.Sprintf("Unsupported storage type '%s' at index %d", storage.Source.Type, index),
			mcpServer.Generation,
		), false
	}
	return metav1.Condition{}, true
}

// validateEnvFrom validates a single envFrom configuration.
// Returns an error condition and false if validation fails, otherwise returns zero condition and true.
func (r *MCPServerReconciler) validateEnvFrom(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
	envFrom corev1.EnvFromSource,
	index int,
) (metav1.Condition, bool) {
	if ref := envFrom.ConfigMapRef; ref != nil {
		if ref.Optional == nil || !*ref.Optional {
			configMap := &corev1.ConfigMap{}
			if err := r.Get(ctx, client.ObjectKey{
				Name:      ref.Name,
				Namespace: mcpServer.Namespace,
			}, configMap); err != nil {
				if apierrors.IsNotFound(err) {
					return newCondition(
						ConditionTypeAccepted,
						metav1.ConditionFalse,
						ReasonInvalid,
						fmt.Sprintf("ConfigMap '%s' (envFrom index %d) not found in namespace '%s'",
							ref.Name, index, mcpServer.Namespace),
						mcpServer.Generation,
					), false
				}
				return newCondition(
					ConditionTypeAccepted,
					metav1.ConditionFalse,
					ReasonInvalid,
					fmt.Sprintf("Failed to validate ConfigMap '%s': %v", ref.Name, err),
					mcpServer.Generation,
				), false
			}
		}
	}
	if ref := envFrom.SecretRef; ref != nil {
		if ref.Optional == nil || !*ref.Optional {
			secret := &corev1.Secret{}
			if err := r.Get(ctx, client.ObjectKey{
				Name:      ref.Name,
				Namespace: mcpServer.Namespace,
			}, secret); err != nil {
				if apierrors.IsNotFound(err) {
					return newCondition(
						ConditionTypeAccepted,
						metav1.ConditionFalse,
						ReasonInvalid,
						fmt.Sprintf("Secret '%s' (envFrom index %d) not found in namespace '%s'",
							ref.Name, index, mcpServer.Namespace),
						mcpServer.Generation,
					), false
				}
				return newCondition(
					ConditionTypeAccepted,
					metav1.ConditionFalse,
					ReasonInvalid,
					fmt.Sprintf("Failed to validate Secret '%s': %v", ref.Name, err),
					mcpServer.Generation,
				), false
			}
		}
	}
	return metav1.Condition{}, true
}

// setAcceptedCondition validates the MCPServer configuration and sets the Accepted condition.
// Returns the condition and a boolean indicating if configuration is valid.
func (r *MCPServerReconciler) setAcceptedCondition(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
) (metav1.Condition, bool) {
	// Validate storage mounts
	for i, storage := range mcpServer.Spec.Config.Storage {
		if condition, valid := r.validateStorageMount(ctx, mcpServer, storage, i); !valid {
			return condition, false
		}
	}

	// Validate envFrom references
	for i, envFrom := range mcpServer.Spec.Config.EnvFrom {
		if condition, valid := r.validateEnvFrom(ctx, mcpServer, envFrom, i); !valid {
			return condition, false
		}
	}

	// All validation passed
	return newCondition(
		ConditionTypeAccepted,
		metav1.ConditionTrue,
		ReasonValid,
		"Configuration is valid",
		mcpServer.Generation,
	), true
}

// extractConfigMapNames is an index extractor that returns all ConfigMap names
// referenced by an MCPServer. Used for efficient ConfigMap watch lookups.
// This returns both required and optional ConfigMap references, matching Kubernetes
// semantics where optional resources are still used when available.
func extractConfigMapNames(obj client.Object) []string {
	mcpServer := obj.(*mcpv1alpha1.MCPServer)
	var configMaps []string
	seen := make(map[string]bool)

	// Extract from storage mounts
	for _, storage := range mcpServer.Spec.Config.Storage {
		if storage.Source.Type == mcpv1alpha1.StorageTypeConfigMap &&
			storage.Source.ConfigMap != nil {
			name := storage.Source.ConfigMap.Name
			if !seen[name] {
				configMaps = append(configMaps, name)
				seen[name] = true
			}
		}
	}

	// Extract from envFrom
	for _, envFrom := range mcpServer.Spec.Config.EnvFrom {
		if envFrom.ConfigMapRef != nil {
			name := envFrom.ConfigMapRef.Name
			if !seen[name] {
				configMaps = append(configMaps, name)
				seen[name] = true
			}
		}
	}

	// Extract from env valueFrom
	for _, env := range mcpServer.Spec.Config.Env {
		if env.ValueFrom != nil && env.ValueFrom.ConfigMapKeyRef != nil {
			name := env.ValueFrom.ConfigMapKeyRef.Name
			if !seen[name] {
				configMaps = append(configMaps, name)
				seen[name] = true
			}
		}
	}

	return configMaps
}

// extractSecretNames is an index extractor that returns all Secret names
// referenced by an MCPServer. Used for efficient Secret watch lookups.
// This returns both required and optional Secret references, matching Kubernetes
// semantics where optional resources are still used when available.
func extractSecretNames(obj client.Object) []string {
	mcpServer := obj.(*mcpv1alpha1.MCPServer)
	var secrets []string
	seen := make(map[string]bool)

	// Extract from storage mounts
	for _, storage := range mcpServer.Spec.Config.Storage {
		if storage.Source.Type == mcpv1alpha1.StorageTypeSecret &&
			storage.Source.Secret != nil {
			name := storage.Source.Secret.SecretName
			if !seen[name] {
				secrets = append(secrets, name)
				seen[name] = true
			}
		}
	}

	// Extract from envFrom
	for _, envFrom := range mcpServer.Spec.Config.EnvFrom {
		if envFrom.SecretRef != nil {
			name := envFrom.SecretRef.Name
			if !seen[name] {
				secrets = append(secrets, name)
				seen[name] = true
			}
		}
	}

	// Extract from env valueFrom
	for _, env := range mcpServer.Spec.Config.Env {
		if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
			name := env.ValueFrom.SecretKeyRef.Name
			if !seen[name] {
				secrets = append(secrets, name)
				seen[name] = true
			}
		}
	}

	return secrets
}

// findMCPServersForResource is a generic helper that finds all MCPServers
// referencing a given resource by name using the specified field index.
func (r *MCPServerReconciler) findMCPServersForResource(
	ctx context.Context,
	resourceName string,
	namespace string,
	indexKey string,
) []reconcile.Request {
	logger := log.FromContext(ctx)
	var mcpServers mcpv1alpha1.MCPServerList

	// Use the index to find MCPServers that reference this resource
	if err := r.List(ctx, &mcpServers,
		client.InNamespace(namespace),
		client.MatchingFields{indexKey: resourceName},
	); err != nil {
		logger.Error(err, "Failed to list MCPServers for resource",
			"resourceName", resourceName,
			"namespace", namespace,
			"indexKey", indexKey)
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0, len(mcpServers.Items))
	for _, mcpServer := range mcpServers.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&mcpServer),
		})
	}
	return requests
}

// findMCPServersForConfigMap finds all MCPServers that reference the given ConfigMap
// using the field index for efficient lookup.
func (r *MCPServerReconciler) findMCPServersForConfigMap(ctx context.Context, configMap client.Object) []reconcile.Request {
	return r.findMCPServersForResource(ctx, configMap.GetName(), configMap.GetNamespace(), configMapIndexKey)
}

// findMCPServersForSecret finds all MCPServers that reference the given Secret
// using the field index for efficient lookup.
func (r *MCPServerReconciler) findMCPServersForSecret(ctx context.Context, secret client.Object) []reconcile.Request {
	return r.findMCPServersForResource(ctx, secret.GetName(), secret.GetNamespace(), secretIndexKey)
}

// SetupWithManager sets up the controller with the Manager.
func (r *MCPServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctx := context.Background()

	// Register ConfigMap index for efficient lookups
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&mcpv1alpha1.MCPServer{},
		configMapIndexKey,
		extractConfigMapNames,
	); err != nil {
		return fmt.Errorf("failed to setup ConfigMap index: %w", err)
	}

	// Register Secret index for efficient lookups
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&mcpv1alpha1.MCPServer{},
		secretIndexKey,
		extractSecretNames,
	); err != nil {
		return fmt.Errorf("failed to setup Secret index: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&mcpv1alpha1.MCPServer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.findMCPServersForConfigMap),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.findMCPServersForSecret),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Named("mcpserver").
		Complete(r)
}
