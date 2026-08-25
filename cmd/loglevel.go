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

package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	uzap "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	defaultLoggingConfigMapName = "mcp-lifecycle-operator-config"
	defaultLoggingConfigMapKey  = "log-level"
	podNamespaceEnvVar          = "POD_NAMESPACE"
)

var serviceAccountNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// resolveLoggingNamespace returns the namespace for the logging ConfigMap watch.
func resolveLoggingNamespace(namespaceOverride string) string {
	if ns := strings.TrimSpace(namespaceOverride); ns != "" {
		return ns
	}
	return detectOperatorNamespace()
}

// extractAtomicLevel returns the zap.AtomicLevel used by the logger options.
// When --zap-log-level is not set, this matches controller-runtime defaults:
// Debug in development mode (--zap-devel) and Info otherwise.
func extractAtomicLevel(opts *crzap.Options) uzap.AtomicLevel {
	if opts.Level != nil {
		switch level := opts.Level.(type) {
		case uzap.AtomicLevel:
			return level
		case *uzap.AtomicLevel:
			return *level
		}
	}
	if opts.Development {
		return uzap.NewAtomicLevelAt(uzap.DebugLevel)
	}
	return uzap.NewAtomicLevelAt(uzap.InfoLevel)
}

// parseLogLevel parses a zap log level from the same values accepted by --zap-log-level.
func parseLogLevel(value string) (zapcore.Level, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("log level must not be empty")
	}

	if level, ok := map[string]zapcore.Level{
		"debug": uzap.DebugLevel,
		"info":  uzap.InfoLevel,
		"error": uzap.ErrorLevel,
		"panic": uzap.PanicLevel,
	}[strings.ToLower(value)]; ok {
		return level, nil
	}

	logLevel, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid log level %q", value)
	}
	// zapcore.Level is int8; verbosity N becomes Level(-N), so N must fit in [-128, -1].
	if logLevel <= 0 || logLevel > 128 {
		return 0, fmt.Errorf("invalid log level %q", value)
	}
	return zapcore.Level(int8(-logLevel)), nil
}

// detectOperatorNamespace returns the namespace the operator pod is running in.
func detectOperatorNamespace() string {
	if ns := strings.TrimSpace(os.Getenv(podNamespaceEnvVar)); ns != "" {
		return ns
	}

	data, err := os.ReadFile(serviceAccountNamespaceFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

type logLevelReconciler struct {
	client.Client
	atomicLevel uzap.AtomicLevel
	key         string
}

func (r *logLevelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.Log.WithName("loglevel")

	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, req.NamespacedName, cm); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	levelStr, ok := cm.Data[r.key]
	if !ok {
		log.Info("ConfigMap is missing log level key", "key", r.key)
		return ctrl.Result{}, nil
	}

	level, err := parseLogLevel(levelStr)
	if err != nil {
		log.Error(err, "Ignoring invalid log level in ConfigMap", "key", r.key, "value", levelStr)
		return ctrl.Result{}, nil
	}

	if r.atomicLevel.Level() != level {
		r.atomicLevel.SetLevel(level)
		log.Info("Updated log level from ConfigMap", "level", level.String())
	}

	return ctrl.Result{}, nil
}

// setupLogLevelFromConfigMap watches a ConfigMap and updates the logger level at runtime.
func setupLogLevelFromConfigMap(mgr ctrl.Manager, atomicLevel uzap.AtomicLevel, namespace, name, key string) error {
	if name == "" {
		setupLog.Info("Runtime log level ConfigMap watch is disabled")
		return nil
	}
	if namespace == "" {
		setupLog.Info("Unable to determine operator namespace; runtime log level ConfigMap watch is disabled")
		return nil
	}
	if key == "" {
		return fmt.Errorf("logging configmap key must not be empty")
	}

	setupLog.Info("Watching ConfigMap for runtime log level changes",
		"namespace", namespace, "name", name, "key", key)

	reconciler := &logLevelReconciler{
		Client:      mgr.GetClient(),
		atomicLevel: atomicLevel,
		key:         key,
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("logging-config").
		For(&corev1.ConfigMap{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(obj client.Object) bool {
			return obj.GetNamespace() == namespace && obj.GetName() == name
		})).
		WithOptions(controller.Options{NeedLeaderElection: new(false)}).
		Complete(reconciler)
}
