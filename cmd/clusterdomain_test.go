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
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestParseClusterDomain(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
	}{
		{name: "default", input: "cluster.local", want: "cluster.local"},
		{name: "custom", input: "cozy.local", want: "cozy.local"},
		{name: "single label", input: "local", want: "local"},
		{name: "hyphens and digits", input: "cluster-1.example", want: "cluster-1.example"},
		{name: "empty defaults", want: "cluster.local"},
		{name: "whitespace defaults", input: " \t", want: "cluster.local"},
		{name: "normalization", input: "  Cozy.LOCAL.  ", want: "cozy.local"},
		{name: "maximum label", input: strings.Repeat("a", 63) + ".local", want: strings.Repeat("a", 63) + ".local"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseClusterDomain(tc.input)
			if err != nil || got != tc.want {
				t.Fatalf("parseClusterDomain(%q) = %q, %v; want %q", tc.input, got, err, tc.want)
			}
		})
	}
}

func TestParseClusterDomainRejectsURLSyntax(t *testing.T) {
	for _, input := range []string{
		"cluster.local@127.0.0.1",
		"user:password@example.com",
		"https://cluster.local",
		"cluster.local:8080",
		"cluster.local/path",
		"cluster.local?query=1",
		"cluster.local#fragment",
		"cluster.local\\path",
		"cluster%2elocal",
		"[::1]",
		"cluster local",
		"cluster\nlocal",
		"cluster\tlocal",
		"cluster\x00local",
		".cluster.local",
		"cluster..local",
		"cluster.local..",
		".",
		"-cluster.local",
		"cluster-.local",
		"cluster_local",
		strings.Repeat("a", 64) + ".local",
		strings.Repeat("a.", 127) + "a",
	} {
		t.Run(input, func(t *testing.T) {
			if got, err := parseClusterDomain(input); err == nil || got != "" {
				t.Fatalf("parseClusterDomain(%q) = %q, %v; want rejection", input, got, err)
			}
		})
	}
}

func TestInvalidClusterDomainStopsStartup(t *testing.T) {
	const helperEnv = "MCP_TEST_INVALID_CLUSTER_DOMAIN"
	if domain := os.Getenv(helperEnv); domain != "" {
		os.Args = []string{os.Args[0], "--cluster-domain=" + domain}
		main()
		return
	}
	for _, domain := range []string{
		"cluster.local@127.0.0.1",
		"cluster.local:8080",
		"cluster.local/path",
	} {
		t.Run(domain, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestInvalidClusterDomainStopsStartup$")
			cmd.Env = append(os.Environ(), helperEnv+"="+domain)
			output, err := cmd.CombinedOutput()
			exitErr, ok := err.(*exec.ExitError)
			if !ok || exitErr.ExitCode() != 1 {
				t.Fatalf("manager exit = %v; want exit 1; output: %s", err, output)
			}
			if !strings.Contains(string(output), "invalid --cluster-domain value") {
				t.Fatalf("manager did not report the invalid flag before starting: %s", output)
			}
		})
	}
}
