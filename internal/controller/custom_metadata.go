package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var ErrNilMap = errors.New("destination map not initialized")

const jsonNull = "null"

var reservedLabelKeys = map[string]bool{
	LabelKeyApp:       true,
	LabelKeyMCPServer: true,
}

const reservedAnnotationPrefix = "mcp.x-k8s.io/"

func filterReservedKeys(m map[string]string) map[string]string {
	if len(m) == 0 {
		return m
	}
	filtered := make(map[string]string, len(m))
	for k, v := range m {
		if reservedLabelKeys[k] {
			continue
		}
		filtered[k] = v
	}
	return filtered
}

func filterReservedAnnotationKeys(m map[string]string) map[string]string {
	if len(m) == 0 {
		return m
	}
	filtered := make(map[string]string, len(m))
	for k, v := range m {
		if strings.HasPrefix(k, reservedAnnotationPrefix) {
			continue
		}
		filtered[k] = v
	}
	return filtered
}

// mergeMaps merges all entries from src into dst.
// Callers are responsible for filtering reserved keys before calling.
func mergeMaps(dst, src map[string]string) error {
	if dst == nil {
		return ErrNilMap
	}
	maps.Copy(dst, src)
	return nil
}

func applyCustomObjectMetadata(mcpServer *mcpv1alpha1.MCPServer, obj client.Object) error {
	annotations := obj.GetAnnotations()

	currentLabels := make(map[string]string)
	if annotations != nil {
		if managedLabels, ok := annotations[managedExtraLabels]; ok {
			if err := json.Unmarshal([]byte(managedLabels), &currentLabels); err != nil {
				return fmt.Errorf("retrieving current custom labels failed; %w", err)
			}
		}
	}

	effectiveLabels := filterReservedKeys(mcpServer.Spec.ExtraLabels)

	if !maps.Equal(effectiveLabels, currentLabels) {
		labels := obj.GetLabels()
		for key := range currentLabels {
			delete(labels, key)
		}
		if annotations != nil {
			delete(annotations, managedExtraLabels)
		}
	}

	effectiveAnnotations := filterReservedAnnotationKeys(mcpServer.Spec.ExtraAnnotations)

	currentAnnotations := make(map[string]string)
	if annotations != nil {
		if managedAnnotations, ok := annotations[managedExtraAnnotations]; ok {
			if err := json.Unmarshal([]byte(managedAnnotations), &currentAnnotations); err != nil {
				return fmt.Errorf("retrieving current custom annotations failed; %w", err)
			}
		}
	}

	if !maps.Equal(effectiveAnnotations, currentAnnotations) {
		for key := range currentAnnotations {
			delete(annotations, key)
		}
		if annotations != nil {
			delete(annotations, managedExtraAnnotations)
		}
	}

	if len(effectiveLabels) == 0 &&
		len(effectiveAnnotations) == 0 &&
		len(currentLabels) == 0 &&
		len(currentAnnotations) == 0 {
		return nil
	}

	if obj.GetLabels() == nil {
		obj.SetLabels(make(map[string]string))
	}
	if obj.GetAnnotations() == nil {
		obj.SetAnnotations(make(map[string]string))
	}

	if len(effectiveLabels) > 0 {
		if err := mergeMaps(obj.GetLabels(), effectiveLabels); err != nil {
			return fmt.Errorf("appending labels failed; %w", err)
		}
	}

	if len(effectiveAnnotations) > 0 {
		if err := mergeMaps(obj.GetAnnotations(), effectiveAnnotations); err != nil {
			return fmt.Errorf("appending annotations failed; %w", err)
		}
	}

	extraLabelsByte, err := json.Marshal(effectiveLabels)
	if err != nil {
		return fmt.Errorf("marshaling .spec.extraLabels failed; %w", err)
	}
	if len(extraLabelsByte) != 0 && string(extraLabelsByte) != jsonNull {
		obj.GetAnnotations()[managedExtraLabels] = string(extraLabelsByte)
	}

	extraAnnotationsByte, err := json.Marshal(effectiveAnnotations)
	if err != nil {
		return fmt.Errorf("marshaling .spec.extraAnnotations failed; %w", err)
	}
	if len(extraAnnotationsByte) != 0 && string(extraAnnotationsByte) != jsonNull {
		obj.GetAnnotations()[managedExtraAnnotations] = string(extraAnnotationsByte)
	}

	return nil
}

func applyCustomDeploymentMetadata(mcpServer *mcpv1alpha1.MCPServer, deployment *appsv1.Deployment) error {
	var oldLabels map[string]string
	if v, ok := deployment.Annotations[managedExtraLabels]; ok {
		_ = json.Unmarshal([]byte(v), &oldLabels)
	}
	var oldAnnotations map[string]string
	if v, ok := deployment.Annotations[managedExtraAnnotations]; ok {
		_ = json.Unmarshal([]byte(v), &oldAnnotations)
	}

	if err := applyCustomObjectMetadata(mcpServer, deployment); err != nil {
		return err
	}

	effectiveLabels := filterReservedKeys(mcpServer.Spec.ExtraLabels)
	effectiveAnnotations := filterReservedAnnotationKeys(mcpServer.Spec.ExtraAnnotations)

	if !maps.Equal(effectiveLabels, oldLabels) {
		for key := range oldLabels {
			delete(deployment.Spec.Template.Labels, key)
		}
	}
	if len(effectiveLabels) > 0 {
		if deployment.Spec.Template.Labels == nil {
			deployment.Spec.Template.Labels = make(map[string]string)
		}
		if err := mergeMaps(deployment.Spec.Template.Labels, effectiveLabels); err != nil {
			return fmt.Errorf("appending pod template labels failed; %w", err)
		}
	}

	if !maps.Equal(effectiveAnnotations, oldAnnotations) {
		for key := range oldAnnotations {
			delete(deployment.Spec.Template.Annotations, key)
		}
	}
	if len(effectiveAnnotations) > 0 {
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = make(map[string]string)
		}
		if err := mergeMaps(deployment.Spec.Template.Annotations, effectiveAnnotations); err != nil {
			return fmt.Errorf("appending pod template annotations failed; %w", err)
		}
	}

	return nil
}

func applyCustomServiceMetadata(mcpServer *mcpv1alpha1.MCPServer, obj client.Object) error {
	return applyCustomObjectMetadata(mcpServer, obj)
}

func applyCustomNetworkPolicyMetadata(mcpServer *mcpv1alpha1.MCPServer, obj client.Object) error {
	return applyCustomObjectMetadata(mcpServer, obj)
}

func objectLabelsChanged(mcpServer *mcpv1alpha1.MCPServer, obj client.Object, extraLabelMaps ...map[string]string) bool {
	effectiveLabels := filterReservedKeys(mcpServer.Spec.ExtraLabels)

	var currentLabels map[string]string
	annotations := obj.GetAnnotations()
	if annotations != nil {
		vals, ok := annotations[managedExtraLabels]
		if ok {
			if err := json.Unmarshal([]byte(vals), &currentLabels); err != nil {
				return true
			}

			if len(currentLabels) > 0 {
				if len(effectiveLabels) != 0 &&
					!maps.Equal(currentLabels, effectiveLabels) {
					return true
				}

				if len(effectiveLabels) == 0 {
					return true
				}

				labels := obj.GetLabels()
				for k := range currentLabels {
					if _, ok := labels[k]; !ok {
						return true
					}
					for _, m := range extraLabelMaps {
						if _, ok := m[k]; !ok {
							return true
						}
					}
				}
			}
		}
	}

	if len(currentLabels) == 0 &&
		len(effectiveLabels) != 0 {
		return true
	}

	return false
}

func objectAnnotationsChanged(mcpServer *mcpv1alpha1.MCPServer, obj client.Object, extraAnnotationMaps ...map[string]string) bool {
	effectiveAnnotations := filterReservedAnnotationKeys(mcpServer.Spec.ExtraAnnotations)

	var currentAnnotations map[string]string
	annotations := obj.GetAnnotations()
	if annotations != nil {
		vals, ok := annotations[managedExtraAnnotations]
		if ok {
			if err := json.Unmarshal([]byte(vals), &currentAnnotations); err != nil {
				return true
			}

			if len(currentAnnotations) > 0 {
				if len(effectiveAnnotations) != 0 &&
					!maps.Equal(currentAnnotations, effectiveAnnotations) {
					return true
				}

				if len(effectiveAnnotations) == 0 {
					return true
				}

				for k := range currentAnnotations {
					if _, ok := annotations[k]; !ok {
						return true
					}
					for _, m := range extraAnnotationMaps {
						if _, ok := m[k]; !ok {
							return true
						}
					}
				}
			}
		}
	}

	if len(currentAnnotations) == 0 &&
		len(effectiveAnnotations) != 0 {
		return true
	}

	return false
}

func deploymentLabelsChanged(mcpServer *mcpv1alpha1.MCPServer, deployment *appsv1.Deployment) bool {
	return objectLabelsChanged(mcpServer, deployment, deployment.Spec.Template.Labels)
}

func deploymentAnnotationsChanged(mcpServer *mcpv1alpha1.MCPServer, deployment *appsv1.Deployment) bool {
	return objectAnnotationsChanged(mcpServer, deployment, deployment.Spec.Template.Annotations)
}

func serviceLabelsChanged(mcpServer *mcpv1alpha1.MCPServer, obj client.Object) bool {
	return objectLabelsChanged(mcpServer, obj)
}

func serviceAnnotationsChanged(mcpServer *mcpv1alpha1.MCPServer, obj client.Object) bool {
	return objectAnnotationsChanged(mcpServer, obj)
}

func networkPolicyLabelsChanged(mcpServer *mcpv1alpha1.MCPServer, obj client.Object) bool {
	return objectLabelsChanged(mcpServer, obj)
}

func networkPolicyAnnotationsChanged(mcpServer *mcpv1alpha1.MCPServer, obj client.Object) bool {
	return objectAnnotationsChanged(mcpServer, obj)
}
