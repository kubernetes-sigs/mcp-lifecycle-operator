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

package v1beta1

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	v1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1"
)

var _ conversion.Convertible = &MCPServer{}

// ConvertTo converts the beta representation to the v1 storage version. The
// schemas have the same JSON shape except for the deprecated logging status
// capability, which is intentionally absent from v1.
func (src *MCPServer) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1.MCPServer)
	if err := convertMCPServer(src, dst); err != nil {
		return err
	}
	dst.TypeMeta = metav1.TypeMeta{APIVersion: v1.GroupVersion.String(), Kind: v1.MCPServerKind}
	return nil
}

// ConvertFrom converts the v1 storage version to the beta representation.
func (dst *MCPServer) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1.MCPServer)
	if err := convertMCPServer(src, dst); err != nil {
		return err
	}
	dst.TypeMeta = metav1.TypeMeta{APIVersion: GroupVersion.String(), Kind: MCPServerKind}
	return nil
}

func convertMCPServer(src, dst any) error {
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}
