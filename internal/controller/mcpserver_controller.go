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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	v1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	acv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1/applyconfiguration/api/v1alpha1"
)

// Phase constants for MCPServer status.
const (
	PhasePending = "Pending"
	PhaseRunning = "Running"
	PhaseFailed  = "Failed"

	fieldManager = "mcpserver-controller"
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

	// Reconcile Deployment
	existingDeployment, err := r.reconcileDeployment(ctx, mcpServer)
	if err != nil {
		r.updateStatusFailed(ctx, mcpServer, "Failed to reconcile Deployment")
		return ctrl.Result{}, err
	}

	// Reconcile Service
	if err := r.reconcileService(ctx, mcpServer); err != nil {
		r.updateStatusFailed(ctx, mcpServer, "Failed to reconcile Service")
		return ctrl.Result{}, err
	}

	phase, condition := determinePhase(existingDeployment, mcpServer.Generation, mcpServer.Status.Conditions)

	path := mcpServer.Spec.Config.Path
	if path == "" {
		path = "/mcp"
	}

	status := acv1alpha1.MCPServerStatus().
		WithPhase(phase).
		WithDeploymentName(existingDeployment.Name).
		WithServiceName(mcpServer.Name).
		WithAddress(acv1alpha1.MCPServerAddress().WithURL(fmt.Sprintf("http://%s.%s.svc.cluster.local:%d%s", mcpServer.Name, mcpServer.Namespace, mcpServer.Spec.Config.Port, path))).
		WithConditions(conditionToAC(condition))

	if err := r.applyStatus(ctx, mcpServer, status); err != nil {
		logger.Error(err, "Failed to apply new MCPServer status")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled MCPServer", "phase", mcpServer.Status.Phase)
	return ctrl.Result{}, nil
}

// determinePhase maps deployment status to an MCPServer phase and condition.
func determinePhase(
	deployment *appsv1.Deployment,
	generation int64,
	existingConditions []metav1.Condition,
) (string, metav1.Condition) {
	deploymentAvailable := false
	deploymentProgressing := false
	deploymentReplicaFailure := false
	var deploymentMessage string

	for _, condition := range deployment.Status.Conditions {
		switch condition.Type {
		case appsv1.DeploymentAvailable:
			if condition.Status == corev1.ConditionTrue {
				deploymentAvailable = true
			}
		case appsv1.DeploymentProgressing:
			if condition.Status == corev1.ConditionTrue {
				deploymentProgressing = true
			}
			if condition.Status == corev1.ConditionFalse {
				deploymentMessage = condition.Message
			}
		case appsv1.DeploymentReplicaFailure:
			if condition.Status == corev1.ConditionTrue {
				deploymentReplicaFailure = true
				deploymentMessage = condition.Message
			}
		}
	}

	if len(deployment.Status.Conditions) == 0 && deployment.Status.ReadyReplicas == 0 {
		condition := metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "DeploymentPending",
			Message:            "Waiting for Deployment to report status",
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}

		preserveLastTransitionTime(&condition, existingConditions)

		return PhasePending, condition
	}

	if deploymentAvailable && deployment.Status.ReadyReplicas > 0 {
		condition := metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "DeploymentAvailable",
			Message:            "Deployment is available and ready",
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}

		preserveLastTransitionTime(&condition, existingConditions)

		return PhaseRunning, condition
	}

	if deploymentReplicaFailure || (!deploymentProgressing && !deploymentAvailable) {
		message := "Deployment failed"
		if deploymentMessage != "" {
			message = deploymentMessage
		}

		condition := metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "DeploymentFailed",
			Message:            message,
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}

		preserveLastTransitionTime(&condition, existingConditions)

		return PhaseFailed, condition
	}

	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "DeploymentProgressing",
		Message:            "Deployment is progressing",
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}

	preserveLastTransitionTime(&condition, existingConditions)

	return PhasePending, condition
}

// reconcileDeployment creates or updates the Deployment for the MCPServer
// and returns the current state of the deployment.
func (r *MCPServerReconciler) reconcileDeployment(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
) (*appsv1.Deployment, error) {
	logger := log.FromContext(ctx)

	deployment, err := r.createDeployment(ctx, mcpServer)
	if err != nil {
		logger.Error(err, "Failed to create Deployment")
		r.updateStatusFailed(ctx, mcpServer, "Failed to create Deployment")
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
		if err := r.Get(ctx, client.ObjectKey{
			Name: deployment.Name, Namespace: deployment.Namespace,
		}, existingDeployment); err != nil {
			logger.Error(err, "Failed to get newly created Deployment")
			return nil, err
		}
		return existingDeployment, nil
	} else if err != nil {
		logger.Error(err, "Failed to get Deployment")
		return nil, err
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
			!equality.Semantic.DeepEqual(existingDeployment.Labels, deployment.Labels) ||
			!equality.Semantic.DeepEqual(existingDeployment.Annotations, deployment.Annotations) ||
			!equality.Semantic.DeepEqual(existingDeployment.Spec.Template.Labels, deployment.Spec.Template.Labels) ||
			!equality.Semantic.DeepEqual(existingDeployment.Spec.Template.Annotations, deployment.Spec.Template.Annotations) ||
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
			!equality.Semantic.DeepEqual(existingDeployment.Spec.Replicas, deployment.Spec.Replicas)
	}
	if needsUpdate {
		logger.Info("Updating Deployment", "name", existingDeployment.Name)
		existingDeployment.Spec.Replicas = deployment.Spec.Replicas
		existingDeployment.Spec.Template.Spec = deployment.Spec.Template.Spec
		existingDeployment.Labels = deployment.Labels
		existingDeployment.Annotations = deployment.Annotations
		existingDeployment.Spec.Template.Labels = deployment.Spec.Template.Labels
		existingDeployment.Spec.Template.Annotations = deployment.Spec.Template.Annotations
		if err := r.Update(ctx, existingDeployment); err != nil {
			logger.Error(err, "Failed to update Deployment")
			return nil, err
		}
	} else {
		logger.Info("Deployment already exists and is up to date", "name", deployment.Name)
	}

	return existingDeployment, nil
}

// createDeployment creates a Deployment for the MCPServer
func (r *MCPServerReconciler) createDeployment(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) (*appsv1.Deployment, error) {
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
	mergedLabels := mergeStringMaps(labels, mcpServer.Spec.Labels)
	mergedAnnotations := mergeStringMaps(mcpServer.Spec.Annotations)

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
		if err := r.validateEnvFrom(ctx, mcpServer); err != nil {
			return nil, err
		}
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
	volumes, volumeMounts, err := r.processStorageMounts(ctx, mcpServer)
	if err != nil {
		return nil, err
	}
	container.VolumeMounts = volumeMounts

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        mcpServer.Name,
			Namespace:   mcpServer.Namespace,
			Labels:      mergedLabels,
			Annotations: mergedAnnotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      mergedLabels,
					Annotations: mergedAnnotations,
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

// validateEnvFrom verifies that referenced ConfigMaps and Secrets exist for non-optional envFrom entries.
func (r *MCPServerReconciler) validateEnvFrom(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) error {
	for i, envFrom := range mcpServer.Spec.Config.EnvFrom {
		if ref := envFrom.ConfigMapRef; ref != nil {
			if ref.Optional == nil || !*ref.Optional {
				configMap := &corev1.ConfigMap{}
				if err := r.Get(ctx, client.ObjectKey{
					Name:      ref.Name,
					Namespace: mcpServer.Namespace,
				}, configMap); err != nil {
					return fmt.Errorf("failed to get ConfigMap %s for envFrom at index %d: %w", ref.Name, i, err)
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
					return fmt.Errorf("failed to get Secret %s for envFrom at index %d: %w", ref.Name, i, err)
				}
			}
		}
	}
	return nil
}

// processStorageMounts builds volumes and volume mounts from the MCPServer storage configuration,
// validating that referenced ConfigMaps and Secrets exist for non-optional entries.
func (r *MCPServerReconciler) processStorageMounts(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
) ([]corev1.Volume, []corev1.VolumeMount, error) {
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
			if storage.Source.ConfigMap == nil {
				return nil, nil, fmt.Errorf("configMap must be set when type is ConfigMap for storage mount at index %d", i)
			}
			if storage.Source.ConfigMap.Name == "" {
				return nil, nil, fmt.Errorf("configMap name must not be empty for storage mount at index %d", i)
			}
			if storage.Source.ConfigMap.Optional == nil || !*storage.Source.ConfigMap.Optional {
				configMap := &corev1.ConfigMap{}
				if err := r.Get(ctx, client.ObjectKey{
					Name:      storage.Source.ConfigMap.Name,
					Namespace: mcpServer.Namespace,
				}, configMap); err != nil {
					return nil, nil, fmt.Errorf("failed to get ConfigMap %s for storage mount at index %d: %w", storage.Source.ConfigMap.Name, i, err)
				}
			}
			volume.ConfigMap = storage.Source.ConfigMap
		case mcpv1alpha1.StorageTypeSecret:
			if storage.Source.Secret == nil {
				return nil, nil, fmt.Errorf("secret must be set when type is Secret for storage mount at index %d", i)
			}
			if storage.Source.Secret.SecretName == "" {
				return nil, nil, fmt.Errorf("secret name must not be empty for storage mount at index %d", i)
			}
			if storage.Source.Secret.Optional == nil || !*storage.Source.Secret.Optional {
				secret := &corev1.Secret{}
				if err := r.Get(ctx, client.ObjectKey{
					Name:      storage.Source.Secret.SecretName,
					Namespace: mcpServer.Namespace,
				}, secret); err != nil {
					return nil, nil, fmt.Errorf("failed to get Secret %s for storage mount at index %d: %w", storage.Source.Secret.SecretName, i, err)
				}
			}
			volume.Secret = storage.Source.Secret
		case mcpv1alpha1.StorageTypeEmptyDir:
			if storage.Source.EmptyDir == nil {
				return nil, nil, fmt.Errorf("emptyDir must be set when type is EmptyDir for storage mount at index %d", i)
			}
			// No existence validation needed - EmptyDir is created by Kubernetes
			volume.EmptyDir = storage.Source.EmptyDir
		default:
			return nil, nil, fmt.Errorf("unsupported storage type %s at index %d", storage.Source.Type, i)
		}

		volumes = append(volumes, volume)
	}

	return volumes, volumeMounts, nil
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
	} else if err != nil {
		logger.Error(err, "Failed to get Service")
		return err
	} else if !equality.Semantic.DeepEqual(service.Spec.Ports, existingService.Spec.Ports) ||
		!equality.Semantic.DeepEqual(service.Labels, existingService.Labels) ||
		!equality.Semantic.DeepEqual(service.Annotations, existingService.Annotations) {
		logger.Info("Updating Service", "name", existingService.Name)
		existingService.Spec.Ports = service.Spec.Ports
		existingService.Labels = service.Labels
		existingService.Annotations = service.Annotations
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
	mergedLabels := mergeStringMaps(labels, mcpServer.Spec.Labels)
	mergedAnnotations := mergeStringMaps(mcpServer.Spec.Annotations)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        mcpServer.Name,
			Namespace:   mcpServer.Namespace,
			Labels:      mergedLabels,
			Annotations: mergedAnnotations,
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

// updateStatusFailed updates the MCPServer status to Failed
func (r *MCPServerReconciler) updateStatusFailed(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer, message string) {
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "ReconciliationFailed",
		Message:            message,
		ObservedGeneration: mcpServer.Generation,
		LastTransitionTime: metav1.Now(),
	}

	preserveLastTransitionTime(&condition, mcpServer.Status.Conditions)

	status := acv1alpha1.MCPServerStatus().
		WithPhase(PhaseFailed).
		WithServiceName(mcpServer.Name).
		WithConditions(conditionToAC(condition))

	if mcpServer.Status.DeploymentName != "" {
		status = status.WithDeploymentName(mcpServer.Status.DeploymentName)
	}

	if mcpServer.Status.Address != nil {
		status = status.WithAddress(acv1alpha1.MCPServerAddress().WithURL(mcpServer.Status.Address.URL))
	}

	if err := r.applyStatus(ctx, mcpServer, status); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update MCPServer status to Failed")
	}
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

// SetupWithManager sets up the controller with the Manager.
func (r *MCPServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcpv1alpha1.MCPServer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Named("mcpserver").
		Complete(r)
}

// mergeStringMaps merges multiple string maps into a single map.
// Maps passed later take precedence in case of duplicate keys.

func mergeStringMaps(maps ...map[string]string) map[string]string {
	out := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
