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
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	acv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1/applyconfiguration/api/v1alpha1"
)

const (
	fieldManager = "mcpserver-controller"

	// defaultMCPPath is the default HTTP path for MCP endpoints, matching the
	// kubebuilder default on ServerConfig.Path.
	defaultMCPPath = "/mcp"

	// mcpClientName is the client name sent during MCP handshake.
	mcpClientName = "mcp-lifecycle-operator"
)

// MCPClientVersion is the version sent during MCP handshake. Bump with releases.
var MCPClientVersion = "v0.1.0"

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
	ReasonAvailable                = "Available"
	ReasonConfigurationInvalid     = "ConfigurationInvalid"
	ReasonDeploymentUnavailable    = "DeploymentUnavailable"
	ReasonServiceUnavailable       = "ServiceUnavailable"
	ReasonNetworkPolicyUnavailable = "NetworkPolicyUnavailable"
	ReasonScaledToZero             = "ScaledToZero"
	ReasonInitializing             = "Initializing"
	ReasonMCPEndpointUnavailable   = "MCPEndpointUnavailable"
)

// Container waiting reasons from Kubernetes pod status.
const (
	WaitingReasonImagePullBackOff           = "ImagePullBackOff"
	WaitingReasonErrImagePull               = "ErrImagePull"
	WaitingReasonCrashLoopBackOff           = "CrashLoopBackOff"
	WaitingReasonCreateContainerConfigError = "CreateContainerConfigError"
)

// Container terminated reasons from Kubernetes pod status.
const (
	TerminatedReasonOOMKilled = "OOMKilled"
)

// Reconciliation constants.
const (
	// requeueDelayDeploymentUnavailable is the delay before requeuing when a deployment is not yet available.
	requeueDelayDeploymentUnavailable = 15 * time.Second

	// eventActionConfigurationValidation is the reporting action for configuration validation outcomes.
	eventActionConfigurationValidation = "ConfigurationValidation"
	// eventActionConfigurationAccepted is the reporting action when Accepted becomes True.
	eventActionConfigurationAccepted = "ConfigurationAccepted"
	// eventActionServerReady is the reporting action when Ready becomes True with reason Available.
	eventActionServerReady = "ServerReady"
	// eventActionMCPHandshakeFailed is the reporting action when the MCP handshake fails.
	eventActionMCPHandshakeFailed = "MCPHandshakeFailed"
	// eventActionMCPHandshakeRetriesExhausted is the reporting action when handshake retries are exhausted.
	eventActionMCPHandshakeRetriesExhausted = "MCPHandshakeRetriesExhausted"
	// eventActionDeploymentReconcileFailed is the reporting action when Deployment reconciliation fails.
	eventActionDeploymentReconcileFailed = "DeploymentReconcileFailed"
	// eventActionServiceReconcileFailed is the reporting action when Service reconciliation fails.
	eventActionServiceReconcileFailed = "ServiceReconcileFailed"
	// eventActionNetworkPolicyReconcileFailed is the reporting action when NetworkPolicy reconciliation fails.
	eventActionNetworkPolicyReconcileFailed = "NetworkPolicyReconcileFailed"

	// requeueDelayMCPHandshake is the initial delay before requeuing when an MCP handshake fails.
	requeueDelayMCPHandshake = 10 * time.Second
	// maxRequeueDelayMCPHandshake is the maximum requeue delay after exponential backoff.
	maxRequeueDelayMCPHandshake = 2 * time.Minute
	// mcpHandshakeTimeout is the context timeout for a single MCP handshake attempt.
	mcpHandshakeTimeout = 15 * time.Second
	// maxMCPHandshakeRetries is the maximum number of MCP handshake failures before
	// the controller stops requeuing. The status will remain MCPEndpointUnavailable
	// until the next spec change triggers a new reconciliation.
	maxMCPHandshakeRetries = 10
)

// configHashAnnotation is the pod template annotation key used to trigger
// rolling updates when referenced ConfigMap or Secret data changes.
const configHashAnnotation = "mcp.x-k8s.io/config-hash"

// Index keys for field indexing.
const (
	// configMapIndexKey is the index key for finding MCPServers by ConfigMap reference.
	configMapIndexKey = "spec.configMapRefs"
	// secretIndexKey is the index key for finding MCPServers by Secret reference.
	secretIndexKey = "spec.secretRefs"
)

// Custom metadata annotations
const (
	// managedExtraLabels tracks custom labels added via .spec.ExtraLabels
	managedExtraLabels = "mcp.x-k8s.io/managed-extra-labels"
	// managedExtraAnnotations tracks custom annotations added via .spec.Extra
	managedExtraAnnotations = "mcp.x-k8s.io/managed-extra-annotations"
)

// MCPServerReconciler reconciles a MCPServer object
type MCPServerReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Recorder  events.EventRecorder
	MCPDialer func(ctx context.Context, url string) (*mcpv1alpha1.MCPServerInfo, error) // nil = use real MCP handshake
	APIReader client.Reader
}

// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcpservers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcpservers/finalizers,verbs=update
// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcpservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

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
			cleanupMetrics(req.Name, req.Namespace)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get MCPServer")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling MCPServer", "name", mcpServer.Name, "namespace", mcpServer.Namespace)

	pendingAcceptedEvent := !acceptedConditionIsTrue(mcpServer.Status.Conditions)
	pendingServerReadyEvent := !readyConditionIsAvailable(mcpServer.Status.Conditions)

	// Validate configuration
	validationStart := time.Now()
	if err := r.validateConfig(ctx, mcpServer); err != nil {
		reconcileDuration.With(prometheus.Labels{"phase": ReconcilePhaseValidation}).Observe(time.Since(validationStart).Seconds())

		var validationErr *ValidationError
		if errors.As(err, &validationErr) {
			return ctrl.Result{}, r.reconcilePermanentValidationError(ctx, mcpServer, validationErr)
		}

		// Transient error - log and return to trigger retry with exponential backoff
		logger.Error(err, "Transient error during configuration validation, will retry")
		// Don't update status - preserve existing Accepted condition
		return ctrl.Result{}, err
	}
	reconcileDuration.With(prometheus.Labels{"phase": ReconcilePhaseValidation}).Observe(time.Since(validationStart).Seconds())

	// Configuration is valid - create Accepted=True condition
	acceptedCondition := newCondition(
		ConditionTypeAccepted,
		metav1.ConditionTrue,
		ReasonValid,
		"Configuration is valid",
		mcpServer.Generation,
	)
	preserveLastTransitionTime(&acceptedCondition, mcpServer.Status.Conditions)

	// Record Accepted condition metric
	recordCondition(mcpServer.Name, mcpServer.Namespace,
		acceptedCondition.Type, string(acceptedCondition.Status), acceptedCondition.Reason)

	// Normal Event once per Accepted transition (single site); not transactional with applyStatus — PR #118.
	if pendingAcceptedEvent {
		r.emitConfigurationAccepted(mcpServer)
	}

	// Configuration is valid, proceed with deployment reconciliation
	deploymentStart := time.Now()
	existingDeployment, err := r.reconcileDeployment(ctx, mcpServer)
	reconcileDuration.With(prometheus.Labels{"phase": ReconcilePhaseDeployment}).Observe(time.Since(deploymentStart).Seconds())
	if err != nil {
		deploymentFailuresTotal.With(prometheus.Labels{
			"name":      mcpServer.Name,
			"namespace": mcpServer.Namespace,
			"reason":    MetricReasonReconcileError,
		}).Inc()
		// Deployment reconciliation failed - update status
		readyCondition := newCondition(
			ConditionTypeReady,
			metav1.ConditionFalse,
			ReasonDeploymentUnavailable,
			fmt.Sprintf("Failed to reconcile Deployment: %v", err),
			mcpServer.Generation,
		)
		preserveLastTransitionTime(&readyCondition, mcpServer.Status.Conditions)

		recordCondition(mcpServer.Name, mcpServer.Namespace,
			readyCondition.Type, string(readyCondition.Status), readyCondition.Reason)

		if !duplicateDeploymentUnavailable(mcpServer.Status.Conditions, readyCondition.Message) {
			r.emitDeploymentReconcileFailed(mcpServer, readyCondition.Message)
		}

		status := acv1alpha1.MCPServerStatus().
			WithObservedGeneration(mcpServer.Generation).
			WithServiceName(mcpServer.Name).
			WithHandshakeRetryCount(0).
			WithReplicas(mcpServer.Status.Replicas).
			WithReadyReplicas(mcpServer.Status.ReadyReplicas).
			WithConditions(
				conditionToAC(acceptedCondition),
				conditionToAC(readyCondition),
			)

		if statusErr := r.applyStatus(ctx, mcpServer, status); statusErr != nil {
			logger.Error(statusErr, "Failed to update MCPServer status")
			return ctrl.Result{}, statusErr
		}
		if IsOwnershipConflict(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Reconcile Service
	serviceStart := time.Now()
	if err := r.reconcileService(ctx, mcpServer); err != nil {
		reconcileDuration.With(prometheus.Labels{"phase": ReconcilePhaseService}).Observe(time.Since(serviceStart).Seconds())
		return r.handleResourceFailure(ctx, mcpServer, existingDeployment, acceptedCondition, err, resourceFailureParams{
			counter:     serviceFailuresTotal,
			reason:      ReasonServiceUnavailable,
			resource:    "Service",
			isDuplicate: duplicateServiceUnavailable,
			emitEvent:   r.emitServiceReconcileFailed,
		})
	}

	reconcileDuration.With(prometheus.Labels{"phase": ReconcilePhaseService}).Observe(time.Since(serviceStart).Seconds())

	// Reconcile NetworkPolicy
	networkPolicyStart := time.Now()
	if err := r.reconcileNetworkPolicy(ctx, mcpServer); err != nil {
		reconcileDuration.With(prometheus.Labels{"phase": ReconcilePhaseNetworkPolicy}).Observe(time.Since(networkPolicyStart).Seconds())
		return r.handleResourceFailure(ctx, mcpServer, existingDeployment, acceptedCondition, err, resourceFailureParams{
			counter:     networkPolicyFailuresTotal,
			reason:      ReasonNetworkPolicyUnavailable,
			resource:    "NetworkPolicy",
			isDuplicate: duplicateNetworkPolicyUnavailable,
			emitEvent:   r.emitNetworkPolicyReconcileFailed,
		})
	}
	reconcileDuration.With(prometheus.Labels{"phase": ReconcilePhaseNetworkPolicy}).Observe(time.Since(networkPolicyStart).Seconds())

	// Determine Ready condition based on deployment status
	readyCondition := r.reconcileReadyCondition(
		ctx,
		existingDeployment,
		acceptedCondition,
		mcpServer.Generation,
		mcpServer.Status.Conditions,
	)

	// Record Ready condition metric
	recordCondition(mcpServer.Name, mcpServer.Namespace,
		readyCondition.Type, string(readyCondition.Status), readyCondition.Reason)

	// Build status
	path := mcpServer.Spec.Config.Path
	if path == "" {
		path = defaultMCPPath
	}

	mcpURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d%s",
		mcpServer.Name, mcpServer.Namespace, mcpServer.Spec.Config.Port, path)

	// If deployment-level readiness reports Available, verify the MCP endpoint.
	var serverInfo *mcpv1alpha1.MCPServerInfo
	readyCondition, serverInfo = r.reconcileHandshake(ctx, mcpServer, mcpURL, readyCondition)

	// Normal Event once per Ready transition to Available after a successful handshake.
	if pendingServerReadyEvent &&
		readyCondition.Status == metav1.ConditionTrue &&
		readyCondition.Reason == ReasonAvailable {
		r.emitServerReady(mcpServer)
	}

	handshakeRetryCount := r.reconcileHandshakeEventsAndRetryCount(mcpServer, readyCondition)

	status := acv1alpha1.MCPServerStatus().
		WithObservedGeneration(mcpServer.Generation).
		WithDeploymentName(existingDeployment.Name).
		WithServiceName(mcpServer.Name).
		WithHandshakeRetryCount(handshakeRetryCount).
		WithReplicas(ptr.Deref(existingDeployment.Spec.Replicas, 1)).
		WithReadyReplicas(existingDeployment.Status.ReadyReplicas).
		WithAddress(acv1alpha1.MCPServerAddress().
			WithURL(mcpURL)).
		WithConditions(
			conditionToAC(acceptedCondition),
			conditionToAC(readyCondition),
		)

	if serverInfo != nil {
		si := acv1alpha1.MCPServerInfo()
		if serverInfo.Name != "" {
			si = si.WithName(serverInfo.Name)
		}
		if serverInfo.Version != "" {
			si = si.WithVersion(serverInfo.Version)
		}
		if serverInfo.ProtocolVersion != "" {
			si = si.WithProtocolVersion(serverInfo.ProtocolVersion)
		}
		if serverInfo.Instructions != "" {
			si = si.WithInstructions(serverInfo.Instructions)
		}
		if serverInfo.Capabilities != nil {
			si = si.WithCapabilities(acv1alpha1.MCPServerCapabilities().
				WithTools(serverInfo.Capabilities.Tools).
				WithResources(serverInfo.Capabilities.Resources).
				WithPrompts(serverInfo.Capabilities.Prompts).
				WithLogging(serverInfo.Capabilities.Logging).
				WithCompletions(serverInfo.Capabilities.Completions))
		}
		status = status.WithServerInfo(si)
	}

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

	// If MCP endpoint is not yet reachable, requeue with exponential backoff up to a max retry count.
	if readyCondition.Status == metav1.ConditionFalse && readyCondition.Reason == ReasonMCPEndpointUnavailable {
		retryCount := int(handshakeRetryCount)
		if retryCount >= maxMCPHandshakeRetries {
			logger.Info("MCP handshake retries exhausted, not requeuing",
				"retries", retryCount, "max", maxMCPHandshakeRetries)
			return ctrl.Result{}, nil
		}
		// retryCount is 1-based (already incremented); backoff expects 0-based
		delay := mcpHandshakeBackoff(retryCount - 1)
		logger.Info("MCP endpoint not yet reachable, requeuing with backoff",
			"requeueAfter", delay, "retry", retryCount, "maxRetries", maxMCPHandshakeRetries)
		return ctrl.Result{RequeueAfter: delay}, nil
	}

	return ctrl.Result{}, nil
}

func (r *MCPServerReconciler) reconcilePermanentValidationError(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
	validationErr *ValidationError,
) error {
	logger := log.FromContext(ctx)

	acceptedCondition := newCondition(
		ConditionTypeAccepted,
		metav1.ConditionFalse,
		validationErr.Reason,
		validationErr.Message,
		mcpServer.Generation,
	)
	preserveLastTransitionTime(&acceptedCondition, mcpServer.Status.Conditions)

	recordCondition(mcpServer.Name, mcpServer.Namespace,
		acceptedCondition.Type, string(acceptedCondition.Status), acceptedCondition.Reason)

	validationFailuresTotal.With(prometheus.Labels{
		"name":      mcpServer.Name,
		"namespace": mcpServer.Namespace,
		"reason":    validationErr.Reason,
	}).Inc()

	readyCondition := newCondition(
		ConditionTypeReady,
		metav1.ConditionFalse,
		ReasonConfigurationInvalid,
		"Configuration must be fixed before server can start",
		mcpServer.Generation,
	)
	preserveLastTransitionTime(&readyCondition, mcpServer.Status.Conditions)

	prevAccepted := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAccepted)

	status := acv1alpha1.MCPServerStatus().
		WithObservedGeneration(mcpServer.Generation).
		WithServiceName(mcpServer.Name).
		WithHandshakeRetryCount(0).
		WithReplicas(mcpServer.Status.Replicas).
		WithReadyReplicas(mcpServer.Status.ReadyReplicas).
		WithConditions(
			conditionToAC(acceptedCondition),
			conditionToAC(readyCondition),
		)

	if err := r.applyStatus(ctx, mcpServer, status); err != nil {
		logger.Error(err, "Failed to update MCPServer status")
		return err
	}

	duplicateInvalid := prevAccepted != nil && prevAccepted.Status == metav1.ConditionFalse &&
		prevAccepted.Reason == validationErr.Reason && prevAccepted.Message == validationErr.Message
	if !duplicateInvalid {
		r.emitConfigurationInvalid(mcpServer, validationErr)
	}

	logger.Info("MCPServer configuration is invalid", "reason", validationErr.Reason)
	recordCondition(mcpServer.Name, mcpServer.Namespace,
		readyCondition.Type, string(readyCondition.Status), readyCondition.Reason)
	return nil
}

func (r *MCPServerReconciler) emitConfigurationInvalid(mcpServer *mcpv1alpha1.MCPServer, validationErr *ValidationError) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(mcpServer, nil, corev1.EventTypeWarning, validationErr.Reason, eventActionConfigurationValidation,
		"MCPServer %s: %s", mcpServer.Name, validationErr.Message)
}

func (r *MCPServerReconciler) emitConfigurationAccepted(mcpServer *mcpv1alpha1.MCPServer) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(mcpServer, nil, corev1.EventTypeNormal, ReasonValid, eventActionConfigurationAccepted,
		"MCPServer %s configuration is valid; Accepted=True", mcpServer.Name)
}

func (r *MCPServerReconciler) emitServerReady(mcpServer *mcpv1alpha1.MCPServer) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(mcpServer, nil, corev1.EventTypeNormal, ReasonAvailable, eventActionServerReady, "MCPServer %s is ready; Ready=True", mcpServer.Name)
}

func (r *MCPServerReconciler) emitDeploymentReconcileFailed(mcpServer *mcpv1alpha1.MCPServer, message string) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(mcpServer, nil, corev1.EventTypeWarning, ReasonDeploymentUnavailable, eventActionDeploymentReconcileFailed,
		"MCPServer %s: %s", mcpServer.Name, message)
}

func (r *MCPServerReconciler) emitServiceReconcileFailed(mcpServer *mcpv1alpha1.MCPServer, message string) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(mcpServer, nil, corev1.EventTypeWarning, ReasonServiceUnavailable, eventActionServiceReconcileFailed,
		"MCPServer %s: %s", mcpServer.Name, message)
}

type resourceFailureParams struct {
	counter     *prometheus.CounterVec
	reason      string
	resource    string
	isDuplicate func([]metav1.Condition, string) bool
	emitEvent   func(*mcpv1alpha1.MCPServer, string)
}

func (r *MCPServerReconciler) handleResourceFailure(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
	existingDeployment *appsv1.Deployment,
	acceptedCondition metav1.Condition,
	reconcileErr error,
	params resourceFailureParams,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	params.counter.With(prometheus.Labels{
		"name":      mcpServer.Name,
		"namespace": mcpServer.Namespace,
		"reason":    MetricReasonReconcileError,
	}).Inc()

	readyCondition := newCondition(
		ConditionTypeReady,
		metav1.ConditionFalse,
		params.reason,
		fmt.Sprintf("Failed to reconcile %s: %v", params.resource, reconcileErr),
		mcpServer.Generation,
	)
	preserveLastTransitionTime(&readyCondition, mcpServer.Status.Conditions)

	recordCondition(mcpServer.Name, mcpServer.Namespace,
		readyCondition.Type, string(readyCondition.Status), readyCondition.Reason)

	if !params.isDuplicate(mcpServer.Status.Conditions, readyCondition.Message) {
		params.emitEvent(mcpServer, readyCondition.Message)
	}

	status := acv1alpha1.MCPServerStatus().
		WithObservedGeneration(mcpServer.Generation).
		WithDeploymentName(existingDeployment.Name).
		WithServiceName(mcpServer.Name).
		WithHandshakeRetryCount(0).
		WithReplicas(ptr.Deref(existingDeployment.Spec.Replicas, 1)).
		WithReadyReplicas(existingDeployment.Status.ReadyReplicas).
		WithConditions(
			conditionToAC(acceptedCondition),
			conditionToAC(readyCondition),
		)

	if statusErr := r.applyStatus(ctx, mcpServer, status); statusErr != nil {
		logger.Error(statusErr, "Failed to update MCPServer status")
		return ctrl.Result{}, statusErr
	}
	if IsOwnershipConflict(reconcileErr) {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, reconcileErr
}

func (r *MCPServerReconciler) emitNetworkPolicyReconcileFailed(mcpServer *mcpv1alpha1.MCPServer, message string) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(mcpServer, nil, corev1.EventTypeWarning, ReasonNetworkPolicyUnavailable, eventActionNetworkPolicyReconcileFailed,
		"MCPServer %s: %s", mcpServer.Name, message)
}

func (r *MCPServerReconciler) emitMCPHandshakeFailed(mcpServer *mcpv1alpha1.MCPServer, message string) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(mcpServer, nil, corev1.EventTypeWarning, ReasonMCPEndpointUnavailable, eventActionMCPHandshakeFailed,
		"MCP handshake failed for MCPServer %s: %s", mcpServer.Name, message)
}

func (r *MCPServerReconciler) emitMCPHandshakeRetriesExhausted(mcpServer *mcpv1alpha1.MCPServer, retryCount int32) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(mcpServer, nil, corev1.EventTypeWarning, ReasonMCPEndpointUnavailable, eventActionMCPHandshakeRetriesExhausted,
		"MCP handshake retries exhausted for MCPServer %s after %d attempts; fix the MCP endpoint or update spec to retry",
		mcpServer.Name, retryCount)
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
		For(&mcpv1alpha1.MCPServer{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
			predicate.LabelChangedPredicate{},
		))).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.NetworkPolicy{}).
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
