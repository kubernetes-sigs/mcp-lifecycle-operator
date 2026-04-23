//go:build e2e
// +build e2e

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
// https://github.com/kubernetes-sigs/kubebuilder/blob/v4.11.1/pkg/plugins/golang/v4/scaffolds/internal/templates/test/e2e/test.go

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/utils"
)

// namespace where the project is deployed in
const namespace = "mcp-lifecycle-operator-system"

// serviceAccountName created for the project
const serviceAccountName = "mcp-lifecycle-operator-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "mcp-lifecycle-operator-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "mcp-lifecycle-operator-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", managerImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=mcp-lifecycle-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("ensuring the controller pod is ready")
			verifyControllerPodReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", controllerPodName, "-n", namespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "Controller pod not ready")
			}
			Eventually(verifyControllerPodReady, 3*time.Minute, time.Second).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Serving metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted, 3*time.Minute, time.Second).Should(Succeed())

			// +kubebuilder:scaffold:e2e-metrics-webhooks-readiness

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": ["curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
							"securityContext": {
								"readOnlyRootFilesystem": true,
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccountName": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			verifyMetricsAvailable := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
				g.Expect(metricsOutput).NotTo(BeEmpty())
				g.Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
			}
			Eventually(verifyMetricsAvailable, 2*time.Minute).Should(Succeed())
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks
	})

	Context("MCPServer - Everything MCP Server", func() {
		const (
			mcpTestNamespace    = "e2e-mcpserver-test"
			mcpServerName       = "everything-mcp-server"
			mcpServerPort       = 3001
		)
		var manifestFile string

		BeforeAll(func() {
			By("creating a test namespace for MCPServer")
			cmd := exec.Command("kubectl", "create", "ns", mcpTestNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create test namespace")

			By("writing the MCPServer manifest to a temp file")
			manifest := fmt.Sprintf(`apiVersion: mcp.x-k8s.io/v1alpha1
kind: MCPServer
metadata:
  name: %s
  namespace: %s
spec:
  source:
    type: ContainerImage
    containerImage:
      ref: quay.io/matzew/mcp-everything:latest
  config:
    port: %d
    path: /mcp
`, mcpServerName, mcpTestNamespace, mcpServerPort)

			manifestFile = filepath.Join(os.TempDir(), "e2e-mcpserver.yaml")
			err = os.WriteFile(manifestFile, []byte(manifest), 0644)
			Expect(err).NotTo(HaveOccurred(), "Failed to write manifest file")

			By("deploying the everything-mcp-server MCPServer resource")
			cmd = exec.Command("kubectl", "apply", "-f", manifestFile)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply MCPServer resource")
		})

		AfterAll(func() {
			By("deleting the MCPServer resource")
			cmd := exec.Command("kubectl", "delete", "mcpserver",
				mcpServerName, "-n", mcpTestNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("deleting the MCPServer test namespace")
			cmd = exec.Command("kubectl", "delete", "ns", mcpTestNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			if manifestFile != "" {
				_ = os.Remove(manifestFile)
			}
		})

		It("should become ready and serve tools via MCP protocol", func() {
			By("waiting for the MCPServer to have Ready=True condition")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "mcpserver",
					mcpServerName, "-n", mcpTestNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type==\"Ready\")].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"),
					"MCPServer not yet Ready")
			}, 3*time.Minute, 2*time.Second).Should(Succeed())

			By("verifying the MCPServer has Accepted=True condition")
			cmd := exec.Command("kubectl", "get", "mcpserver",
				mcpServerName, "-n", mcpTestNamespace,
				"-o", "jsonpath={.status.conditions[?(@.type==\"Accepted\")].status}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("True"), "MCPServer not Accepted")

			By("verifying the MCPServer status address is populated")
			cmd = exec.Command("kubectl", "get", "mcpserver",
				mcpServerName, "-n", mcpTestNamespace,
				"-o", "jsonpath={.status.address.url}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).NotTo(BeEmpty(), "MCPServer address URL should be set")
			expectedURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/mcp",
				mcpServerName, mcpTestNamespace, mcpServerPort)
			Expect(output).To(Equal(expectedURL))

			By("waiting for the server pod to be running")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods",
					"-l", fmt.Sprintf("mcp-server=%s", mcpServerName),
					"-n", mcpTestNamespace,
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}, 3*time.Minute, 2*time.Second).Should(Succeed())

			By("setting up port-forward to the MCP server service")
			localPort := findFreePort()
			portForwardCmd := exec.Command("kubectl", "port-forward",
				fmt.Sprintf("svc/%s", mcpServerName),
				fmt.Sprintf("%d:%d", localPort, mcpServerPort),
				"-n", mcpTestNamespace)
			portForwardCmd.Stdout = GinkgoWriter
			portForwardCmd.Stderr = GinkgoWriter
			err = portForwardCmd.Start()
			Expect(err).NotTo(HaveOccurred(), "Failed to start port-forward")
			defer func() {
				if portForwardCmd.Process != nil {
					_ = portForwardCmd.Process.Kill()
					_ = portForwardCmd.Wait()
				}
			}()

			// Wait for port-forward to be ready by polling the local port.
			Eventually(func() error {
				conn, dialErr := net.DialTimeout("tcp",
					fmt.Sprintf("localhost:%d", localPort), time.Second)
				if dialErr != nil {
					return dialErr
				}
				conn.Close()
				return nil
			}, 30*time.Second, time.Second).Should(Succeed(),
				"Port-forward did not become ready")

			By("connecting an MCP client and initializing the session")
			serverURL := fmt.Sprintf("http://localhost:%d/mcp", localPort)

			mcpClient := mcp.NewClient(
				&mcp.Implementation{
					Name:    "e2e-test-client",
					Version: "v0.0.1",
				},
				nil,
			)

			transport := &mcp.StreamableClientTransport{
				Endpoint:   serverURL,
				HTTPClient: &http.Client{Timeout: 30 * time.Second},
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			session, err := mcpClient.Connect(ctx, transport, nil)
			Expect(err).NotTo(HaveOccurred(), "Failed to connect MCP client")
			defer session.Close()

			initResult := session.InitializeResult()
			Expect(initResult).NotTo(BeNil())
			_, _ = fmt.Fprintf(GinkgoWriter,
				"Connected to MCP server: %s (version %s)\n",
				initResult.ServerInfo.Name,
				initResult.ServerInfo.Version)

			By("listing available tools from the MCP server")
			toolsResult, err := session.ListTools(ctx, nil)
			Expect(err).NotTo(HaveOccurred(), "Failed to list MCP tools")
			Expect(toolsResult).NotTo(BeNil())
			Expect(toolsResult.Tools).NotTo(BeEmpty(),
				"Expected the everything-mcp-server to expose at least one tool")

			_, _ = fmt.Fprintf(GinkgoWriter,
				"Found %d tools:\n", len(toolsResult.Tools))
			for _, tool := range toolsResult.Tools {
				_, _ = fmt.Fprintf(GinkgoWriter,
					"  - %s: %s\n", tool.Name, tool.Description)
			}
		})
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// Temporary file to store the token request
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		// Execute kubectl command to create the token
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		// Parse the JSON output to extract the token
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	return utils.Run(cmd)
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}

// findFreePort asks the OS for a free port to use for port-forwarding.
func findFreePort() int {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		// Fallback to a fixed port if we can't get a free one.
		return 13001
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
