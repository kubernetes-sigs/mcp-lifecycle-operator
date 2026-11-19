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

package sandbox

import (
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// EndpointHTTPClient keeps MCP traffic and any bearer token on the configured
// backend's origin, including when the backend returns a redirect.
func EndpointHTTPClient(endpoint string, timeout time.Duration, bearerToken string) (*http.Client, error) {
	origin, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing backend endpoint: %w", err)
	}
	if (origin.Scheme != "http" && origin.Scheme != "https") || origin.Hostname() == "" {
		return nil, fmt.Errorf("backend endpoint must be an absolute HTTP(S) URL")
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: endpointTransport{origin: origin, bearerToken: bearerToken},
	}, nil
}

type endpointTransport struct {
	origin      *url.URL
	bearerToken string
}

func (t endpointTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != t.origin.Scheme || req.URL.Host != t.origin.Host {
		return nil, fmt.Errorf("backend request leaves the permitted endpoint origin")
	}
	if t.bearerToken != "" {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+t.bearerToken)
	}
	return http.DefaultTransport.RoundTrip(req)
}
