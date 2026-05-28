package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

func applyCustomDeploymentMetadata(mcpServer *mcpv1alpha1.MCPServer, deployment *appsv1.Deployment) error {
	currentLabels := make(map[string]string)
	if managedLabels, ok := deployment.Annotations[managedExtraLabels]; ok {
		if err := json.Unmarshal([]byte(managedLabels), &currentLabels); err != nil {
			return fmt.Errorf("retrieving current custom labels failed; %w", err)
		}
	}

	effectiveLabels := filterReservedKeys(mcpServer.Spec.ExtraLabels)

	if !maps.Equal(effectiveLabels, currentLabels) {
		for key := range currentLabels {
			delete(deployment.Labels, key)
			delete(deployment.Spec.Template.Labels, key)
		}
		delete(deployment.Annotations, managedExtraLabels)
	}

	effectiveAnnotations := filterReservedAnnotationKeys(mcpServer.Spec.ExtraAnnotations)

	currentAnnotations := make(map[string]string)
	if managedAnnotations, ok := deployment.Annotations[managedExtraAnnotations]; ok {
		if err := json.Unmarshal([]byte(managedAnnotations), &currentAnnotations); err != nil {
			return fmt.Errorf("retrieving current custom annotations failed; %w", err)
		}
	}

	if !maps.Equal(effectiveAnnotations, currentAnnotations) {
		for key := range currentAnnotations {
			delete(deployment.Annotations, key)
			delete(deployment.Spec.Template.Annotations, key)
		}
		delete(deployment.Annotations, managedExtraAnnotations)
	}

	if len(effectiveLabels) == 0 &&
		len(effectiveAnnotations) == 0 &&
		len(currentLabels) == 0 &&
		len(currentAnnotations) == 0 {
		return nil
	}

	if len(effectiveLabels) > 0 {
		if deployment.Labels == nil {
			deployment.Labels = make(map[string]string)
		}
		if err := mergeMaps(
			deployment.Labels,
			effectiveLabels,
		); err != nil {
			return fmt.Errorf("appending deployment labels failed; %w", err)
		}
		if deployment.Spec.Template.Labels == nil {
			deployment.Spec.Template.Labels = make(map[string]string)
		}
		if err := mergeMaps(
			deployment.Spec.Template.Labels,
			effectiveLabels,
		); err != nil {
			return fmt.Errorf("appending pod template labels failed; %w", err)
		}
	}

	if len(effectiveAnnotations) > 0 {
		if deployment.Annotations == nil {
			deployment.Annotations = make(map[string]string)
		}
		if err := mergeMaps(
			deployment.Annotations,
			effectiveAnnotations,
		); err != nil {
			return fmt.Errorf("appending deployment annotations failed; %w", err)
		}
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = make(map[string]string)
		}
		if err := mergeMaps(
			deployment.Spec.Template.Annotations,
			effectiveAnnotations,
		); err != nil {
			return fmt.Errorf("appending pod template annotations failed; %w", err)
		}
	}

	if deployment.Annotations == nil {
		deployment.Annotations = make(map[string]string)
	}

	extraLabelsByte, err := json.Marshal(effectiveLabels)
	if err != nil {
		return fmt.Errorf("marshaling .spec.extraLabels failed; %w", err)
	}
	if len(extraLabelsByte) != 0 && string(extraLabelsByte) != jsonNull {
		deployment.Annotations[managedExtraLabels] = string(extraLabelsByte)
	}

	extraAnnotationsByte, err := json.Marshal(effectiveAnnotations)
	if err != nil {
		return fmt.Errorf("marshaling .spec.extraAnnotations failed; %w", err)
	}
	if len(extraAnnotationsByte) != 0 && string(extraAnnotationsByte) != jsonNull {
		deployment.Annotations[managedExtraAnnotations] = string(extraAnnotationsByte)
	}

	return nil
}

func applyCustomServiceMetadata(mcpServer *mcpv1alpha1.MCPServer, service *corev1.Service) error {
	currentLabels := make(map[string]string)
	if managedLabels, ok := service.Annotations[managedExtraLabels]; ok {
		if err := json.Unmarshal([]byte(managedLabels), &currentLabels); err != nil {
			return fmt.Errorf("retrieving current custom labels failed; %w", err)
		}
	}

	effectiveLabels := filterReservedKeys(mcpServer.Spec.ExtraLabels)

	if !maps.Equal(effectiveLabels, currentLabels) {
		for key := range currentLabels {
			delete(service.Labels, key)
		}
		delete(service.Annotations, managedExtraLabels)
	}

	effectiveAnnotations := filterReservedAnnotationKeys(mcpServer.Spec.ExtraAnnotations)

	currentAnnotations := make(map[string]string)
	if managedAnnotations, ok := service.Annotations[managedExtraAnnotations]; ok {
		if err := json.Unmarshal([]byte(managedAnnotations), &currentAnnotations); err != nil {
			return fmt.Errorf("retrieving current custom annotations failed; %w", err)
		}
	}

	if !maps.Equal(effectiveAnnotations, currentAnnotations) {
		for key := range currentAnnotations {
			delete(service.Annotations, key)
		}
		delete(service.Annotations, managedExtraAnnotations)
	}

	if len(effectiveLabels) == 0 &&
		len(effectiveAnnotations) == 0 &&
		len(currentLabels) == 0 &&
		len(currentAnnotations) == 0 {
		return nil
	}

	if service.Labels == nil {
		service.Labels = make(map[string]string)
	}

	if service.Annotations == nil {
		service.Annotations = make(map[string]string)
	}

	if len(effectiveLabels) > 0 {
		if err := mergeMaps(service.Labels, effectiveLabels); err != nil {
			return fmt.Errorf("appending service labels failed; %w", err)
		}
	}

	if len(effectiveAnnotations) > 0 {
		if err := mergeMaps(service.Annotations, effectiveAnnotations); err != nil {
			return fmt.Errorf("appending service annotations failed; %w", err)
		}
	}

	extraLabelsByte, err := json.Marshal(effectiveLabels)
	if err != nil {
		return fmt.Errorf("marshaling .spec.extraLabels failed; %w", err)
	}
	if len(extraLabelsByte) != 0 && string(extraLabelsByte) != jsonNull {
		service.Annotations[managedExtraLabels] = string(extraLabelsByte)
	}

	extraAnnotationsByte, err := json.Marshal(effectiveAnnotations)
	if err != nil {
		return fmt.Errorf("marshaling .spec.extraAnnotations failed; %w", err)
	}
	if len(extraAnnotationsByte) != 0 && string(extraAnnotationsByte) != jsonNull {
		service.Annotations[managedExtraAnnotations] = string(extraAnnotationsByte)
	}

	return nil
}

func deploymentLabelsChanged(mcpServer *mcpv1alpha1.MCPServer, deployment *appsv1.Deployment) bool {
	effectiveLabels := filterReservedKeys(mcpServer.Spec.ExtraLabels)

	var currentLabels map[string]string
	vals, ok := deployment.Annotations[managedExtraLabels]
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

			// check if current labels and .spec.ExtraLabels match
			for k := range currentLabels {
				if _, ok := deployment.Labels[k]; !ok {
					return true
				}
				if _, ok := deployment.Spec.Template.Labels[k]; !ok {
					return true
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

func deploymentAnnotationsChanged(mcpServer *mcpv1alpha1.MCPServer, deployment *appsv1.Deployment) bool {
	effectiveAnnotations := filterReservedAnnotationKeys(mcpServer.Spec.ExtraAnnotations)

	var currentAnnotations map[string]string
	vals, ok := deployment.Annotations[managedExtraAnnotations]
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

			// check if current annotations and .spec.ExtraAnnotations match
			for k := range currentAnnotations {
				if _, ok := deployment.Annotations[k]; !ok {
					return true
				}
				if _, ok := deployment.Spec.Template.Annotations[k]; !ok {
					return true
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

func serviceLabelsChanged(mcpServer *mcpv1alpha1.MCPServer, service *corev1.Service) bool {
	effectiveLabels := filterReservedKeys(mcpServer.Spec.ExtraLabels)

	var currentLabels map[string]string
	vals, ok := service.Annotations[managedExtraLabels]
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

			// check if current labels and .spec.ExtraLabels match
			for k := range currentLabels {
				if _, ok := service.Labels[k]; !ok {
					return true
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

func serviceAnnotationsChanged(mcpServer *mcpv1alpha1.MCPServer, service *corev1.Service) bool {
	effectiveAnnotations := filterReservedAnnotationKeys(mcpServer.Spec.ExtraAnnotations)

	var currentAnnotations map[string]string
	vals, ok := service.Annotations[managedExtraAnnotations]
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

			// check if current annotations and .spec.ExtraAnnotations match
			for k := range currentAnnotations {
				if _, ok := service.Annotations[k]; !ok {
					return true
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
