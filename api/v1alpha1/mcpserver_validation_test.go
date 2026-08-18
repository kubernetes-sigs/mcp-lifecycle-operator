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

package v1alpha1

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
)

func TestValidation(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MCPServer Validation Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	var err error
	err = AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	// Retrieve the first found binary directory to allow running tests from IDEs
	if binaryDir := getFirstFoundEnvTestBinaryDir(); binaryDir != "" {
		testEnv.BinaryAssetsDirectory = binaryDir
	}

	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	Eventually(func() error {
		return testEnv.Stop()
	}, time.Minute, time.Second).Should(Succeed())
})

func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		// Directory not existing is normal in many test environments, don't log it as an error
		if !os.IsNotExist(err) {
			logf.Log.Error(err, "Failed to read directory", "path", basePath)
		}
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}

var _ = Describe("MCPServer Validation", func() {
	var namespace *corev1.Namespace

	BeforeEach(func() {
		// Create a unique namespace for each test
		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-validation-",
			},
		}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
	})

	AfterEach(func() {
		// Clean up namespace
		if namespace != nil {
			Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
		}
	})

	Context("Source validation", func() {
		It("should accept valid Source with type=ContainerImage and containerImage set", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-source",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should reject Source with type=ContainerImage but no containerImage", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-source-missing-image",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type:           SourceTypeContainerImage,
						ContainerImage: nil, // Missing containerImage
					},
					Config: ServerConfig{
						Port: 8080,
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("containerImage must be set when type is ContainerImage"))
		})
	})

	Context("StorageMount validation", func() {
		It("should accept valid StorageMount with type=ConfigMap and configMap set", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-configmap-storage",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path: "/etc/config",
								Source: StorageSource{
									Type: StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-config",
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should accept valid StorageMount with type=Secret and secret set", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-secret-storage",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path: "/etc/secret",
								Source: StorageSource{
									Type: StorageTypeSecret,
									Secret: &corev1.SecretVolumeSource{
										SecretName: "test-secret",
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should reject StorageMount with both configMap and secret set", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-both-sources",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path: "/etc/config",
								Source: StorageSource{
									Type: StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-config",
										},
									},
									Secret: &corev1.SecretVolumeSource{
										SecretName: "test-secret",
									},
								},
							},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			// CEL validation ensures only the field matching the type is set
			Expect(err.Error()).To(ContainSubstring("secret must be set when type is Secret and must not be set otherwise"))
		})

		It("should reject StorageMount with type=ConfigMap but secret set instead", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-type-mismatch-configmap",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path: "/etc/config",
								Source: StorageSource{
									Type: StorageTypeConfigMap, // Type says ConfigMap
									Secret: &corev1.SecretVolumeSource{ // But Secret is set
										SecretName: "test-secret",
									},
								},
							},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("configMap must be set when type is ConfigMap"))
		})

		It("should reject StorageMount with type=Secret but configMap set instead", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-type-mismatch-secret",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path: "/etc/secret",
								Source: StorageSource{
									Type: StorageTypeSecret, // Type says Secret
									ConfigMap: &corev1.ConfigMapVolumeSource{ // But ConfigMap is set
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-config",
										},
									},
								},
							},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("secret must be set when type is Secret"))
		})

		It("should reject StorageMount with type=ConfigMap but neither field set", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-no-source",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path: "/etc/config",
								Source: StorageSource{
									Type:      StorageTypeConfigMap,
									ConfigMap: nil,
									Secret:    nil,
								},
							},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("configMap must be set when type is ConfigMap"))
		})

		It("should reject StorageMount with relative path", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-relative-path",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path: "etc/config", // Invalid: relative path
								Source: StorageSource{
									Type: StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-config",
										},
									},
								},
							},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(Or(
				ContainSubstring("path"),
				ContainSubstring("pattern"),
			))
		})

		It("should accept StorageMount with valid absolute path", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-absolute-path",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path: "/etc/config", // Valid: absolute path
								Source: StorageSource{
									Type: StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-config",
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should accept StorageMount with permissions=ReadOnly", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-permissions-readonly",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path:        "/etc/config",
								Permissions: MountPermissionsReadOnly,
								Source: StorageSource{
									Type: StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-config",
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should accept StorageMount with permissions=ReadWrite", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-permissions-readwrite",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path:        "/etc/config",
								Permissions: MountPermissionsReadWrite,
								Source: StorageSource{
									Type: StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-config",
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should accept StorageMount with permissions=RecursiveReadOnly", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-permissions-recursivereadonly",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path:        "/etc/config",
								Permissions: MountPermissionsRecursiveReadOnly,
								Source: StorageSource{
									Type: StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-config",
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should use default permissions (ReadOnly) when not specified", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-permissions-default",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path: "/etc/config",
								// Permissions not specified - should default to ReadOnly
								Source: StorageSource{
									Type: StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-config",
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should reject invalid permissions value", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-permissions",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path:        "/etc/config",
								Permissions: "InvalidValue", // Invalid enum value
								Source: StorageSource{
									Type: StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-config",
										},
									},
								},
							},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("permissions"))
		})

		It("should accept path with special characters", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-path-special-chars",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path: "/etc/config-123_test.dir/sub-dir", // Valid: common special chars
								Source: StorageSource{
									Type: StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-config",
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should reject path not starting with /", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-path-not-absolute",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path: "etc/config", // Invalid: not absolute path
								Source: StorageSource{
									Type: StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-config",
										},
									},
								},
							},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("path"))
		})

		It("should reject path that is too long", func() {
			longPath := "/" + string(make([]byte, 4096)) // 4097 characters total
			for i := range longPath[1:] {
				longPath = longPath[:i+1] + "a" + longPath[i+2:]
			}
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-path-too-long",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path: longPath, // Invalid: >4096 characters
								Source: StorageSource{
									Type: StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-config",
										},
									},
								},
							},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("path"))
		})

		It("should reject path containing colon", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-path-colon",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path: "/etc/config:data", // Invalid: contains colon
								Source: StorageSource{
									Type: StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-config",
										},
									},
								},
							},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("path"))
		})
	})

	Context("ServerConfig validation", func() {
		It("should reject Port below minimum (0)", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-port-low",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 0, // Invalid: below minimum of 1
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("port"))
		})

		It("should reject Port above maximum (65536)", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-port-high",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 65536, // Invalid: above maximum of 65535
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("port"))
		})

		It("should accept valid Port range (1-65535)", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-port",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})
	})

	Context("ContainerImageSource validation", func() {
		It("should reject empty Ref", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-empty-ref",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "", // Invalid: empty string
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(Or(
				ContainSubstring("ref"),
				ContainSubstring("minLength"),
			))
		})

		It("should reject an invalid image pull policy", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-pull-policy",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref:        "docker.io/library/test-image:v1",
							PullPolicy: "Invalid",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("pullPolicy"))
		})

		It("should reject image pull secrets with an empty name", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-image-pull-secret",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:v1",
							ImagePullSecrets: []corev1.LocalObjectReference{
								{Name: ""},
							},
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("imagePullSecrets"))
		})
	})

	Context("Multiple storage mounts", func() {
		It("should accept multiple valid storage mounts", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multiple-storage",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path: "/etc/config",
								Source: StorageSource{
									Type: StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-config",
										},
									},
								},
							},
							{
								Path: "/etc/secret",
								Source: StorageSource{
									Type: StorageTypeSecret,
									Secret: &corev1.SecretVolumeSource{
										SecretName: "test-secret",
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should reject multiple storage mounts with duplicate paths", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "duplicate-storage-path",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Storage: []StorageMount{
							{
								Path: "/etc/config",
								Source: StorageSource{
									Type: StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "config-one",
										},
									},
								},
							},
							{
								Path: "/etc/config",
								Source: StorageSource{
									Type: StorageTypeConfigMap,
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "config-two",
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).NotTo(Succeed())
		})
	})

	Context("RuntimeConfig validation", func() {
		It("should accept omitted RuntimeConfig (defaults will be applied)", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-omitted-runtime",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
					// Runtime is omitted entirely - valid because defaults will be applied
					Runtime: RuntimeConfig{}, // With omitzero tag, this gets omitted from JSON
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should accept RuntimeConfig with replicas set", func() {
			replicas := int32(2)
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-runtime-replicas",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
					Runtime: RuntimeConfig{
						Replicas: &replicas,
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should accept RuntimeConfig with replicas set to 0 for scale-to-zero", func() {
			replicas := int32(0)
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-runtime-replicas-zero",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
					Runtime: RuntimeConfig{
						Replicas: &replicas,
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should accept RuntimeConfig with security set", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-runtime-security",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
					Runtime: RuntimeConfig{
						Security: SecurityConfig{
							ServiceAccountName: "custom-sa",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should accept MCPServer without runtime field (omitted)", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-no-runtime",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
					// Omitted - should use defaults
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})
	})

	Context("SecurityConfig validation", func() {
		It("should accept empty SecurityConfig (security: {})", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-empty-security",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
					Runtime: RuntimeConfig{
						Security: SecurityConfig{}, // Valid: empty struct with zero values
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should accept SecurityConfig with serviceAccountName set", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-security-sa",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
					Runtime: RuntimeConfig{
						Security: SecurityConfig{
							ServiceAccountName: "custom-sa",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should accept SecurityConfig with podSecurityContext set", func() {
			runAsNonRoot := true
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-security-pod-ctx",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
					Runtime: RuntimeConfig{
						Security: SecurityConfig{
							PodSecurityContext: &corev1.PodSecurityContext{
								RunAsNonRoot: &runAsNonRoot,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should accept SecurityConfig with securityContext set", func() {
			allowPrivilegeEscalation := false
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-security-container-ctx",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
					Runtime: RuntimeConfig{
						Security: SecurityConfig{
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: &allowPrivilegeEscalation,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should accept MCPServer without security field (omitted)", func() {
			replicas := int32(1)
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-no-security",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
					Runtime: RuntimeConfig{
						Replicas: &replicas,
						// Omitted - should use defaults
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should accept valid DNS subdomain ServiceAccountName", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-sa-dns-subdomain",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
					Runtime: RuntimeConfig{
						Security: SecurityConfig{
							ServiceAccountName: "my-service-account", // Valid DNS subdomain
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should accept ServiceAccountName with dots", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-sa-with-dots",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
					Runtime: RuntimeConfig{
						Security: SecurityConfig{
							ServiceAccountName: "my.service.account", // Valid with dots
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should reject ServiceAccountName with uppercase letters", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-sa-uppercase",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
					Runtime: RuntimeConfig{
						Security: SecurityConfig{
							ServiceAccountName: "MyServiceAccount", // Invalid: uppercase
						},
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("serviceAccountName"))
		})

		It("should reject ServiceAccountName with invalid characters", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-sa-chars",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
					Runtime: RuntimeConfig{
						Security: SecurityConfig{
							ServiceAccountName: "my_service_account", // Invalid: underscore not allowed
						},
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("serviceAccountName"))
		})

		It("should reject ServiceAccountName starting with hyphen", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-sa-start-hyphen",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
					Runtime: RuntimeConfig{
						Security: SecurityConfig{
							ServiceAccountName: "-invalid-start", // Invalid: starts with hyphen
						},
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("serviceAccountName"))
		})

		It("should reject ServiceAccountName ending with hyphen", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-sa-end-hyphen",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
					Runtime: RuntimeConfig{
						Security: SecurityConfig{
							ServiceAccountName: "invalid-end-", // Invalid: ends with hyphen
						},
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("serviceAccountName"))
		})
	})

	Context("MCPServerSpec.Path validation", func() {
		It("should accept valid HTTP path", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-path",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Path: "/api/v1/mcp",
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should accept path with hyphens and underscores", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-path-chars",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Path: "/mcp-server_v1",
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should use default path /mcp when not specified", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default-path",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
					},
					// Path not specified - should default to /mcp
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should reject path not starting with /", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-path-no-slash",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Path: "relative/path",
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("must start with '/'"))
		})

		It("should reject path containing spaces", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-path-space",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Path: "/mcp server/path",
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("must not contain spaces"))
		})

		It("should reject path containing query string separator", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-path-query",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Path: "/mcp?query=param",
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("must not contain query string separator"))
		})

		It("should reject path containing fragment separator", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-path-fragment",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Path: "/mcp#fragment",
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("must not contain fragment separator"))
		})

		It("should reject path that is too long", func() {
			longPath := "/" + strings.Repeat("a", 253) // 254 chars total
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-path-toolong",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port: 8080,
						Path: longPath,
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(Or(
				ContainSubstring("Too long"),
				ContainSubstring("max length"),
			))
		})

		It("should accept valid arguments array", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-arguments",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port:      8080,
						Arguments: []string{"--config", "/etc/config.toml", "--verbose"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should accept empty arguments array", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "empty-arguments",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port:      8080,
						Arguments: []string{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		It("should reject arguments array containing empty strings", func() {
			mcpServer := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-empty-string-arg",
					Namespace: namespace.Name,
				},
				Spec: MCPServerSpec{
					Source: Source{
						Type: SourceTypeContainerImage,
						ContainerImage: &ContainerImageSource{
							Ref: "docker.io/library/test-image:latest",
						},
					},
					Config: ServerConfig{
						Port:      8080,
						Arguments: []string{"--config", "", "--verbose"},
					},
				},
			}
			err := k8sClient.Create(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("arguments must not contain empty strings"))
		})
	})
})
