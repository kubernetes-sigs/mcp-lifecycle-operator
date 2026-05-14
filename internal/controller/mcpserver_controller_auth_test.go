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
	"fmt"
	"testing"
)

func Test_isHTTPAuthError(t *testing.T) {
	tt := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "401 Unauthorized",
			err:  fmt.Errorf("POST http://example.com/mcp: Unauthorized"),
			want: true,
		},
		{
			name: "403 Forbidden",
			err:  fmt.Errorf("POST http://example.com/mcp: Forbidden"),
			want: true,
		},
		{
			name: "generic error",
			err:  fmt.Errorf("connection refused"),
			want: false,
		},
		{
			name: "error containing Unauthorized but not as suffix",
			err:  fmt.Errorf("Unauthorized access to resource"),
			want: false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			got := isHTTPAuthError(tc.err)
			if got != tc.want {
				t.Errorf("isHTTPAuthError() = %v, want %v", got, tc.want)
			}
		})
	}
}
