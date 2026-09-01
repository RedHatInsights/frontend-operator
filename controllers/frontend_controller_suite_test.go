package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"github.com/gobeam/stringy"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	prom "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apps "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func intPtr(i int) *int {
	return &i
}

var _ = ginkgo.Describe("Frontend controller with image", func() {
	const (
		FrontendName       = "test-frontend"
		FrontendNamespace  = "default"
		FrontendEnvName    = "test-env"
		FrontendName2      = "test-frontend2"
		FrontendNamespace2 = "default"
		FrontendEnvName2   = "test-env"
		BundleName         = "test-bundle"

		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	ginkgo.Context("When creating a Frontend Resource", func() {
		ginkgo.It("Should create a deployment with the correct items", func() {
			ginkgo.By("ginkgo.By creating a new Frontend")
			ctx := context.Background()

			var customConfig apiextensions.JSON
			err := customConfig.UnmarshalJSON([]byte(`{"apple":"pie"}`))
			gomega.Expect(err).Should(gomega.BeNil())

			var customConfig2 apiextensions.JSON
			err = customConfig2.UnmarshalJSON([]byte(`{"cheese":"pasty"}`))
			gomega.Expect(err).Should(gomega.BeNil())

			frontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName:        FrontendEnvName,
					Title:          "",
					DeploymentRepo: "",
					API: &crd.APIInfo{
						Versions: []string{"v1"},
						Specs: []crd.APISpecInfo{
							{
								URL:          "https://console.redhat.com/api/inventory/v1/openapi.json",
								BundleLabels: []string{"insights"},
								FrontendName: "inventory-deployment-abcdefg", // will be overridden
							},
						},
					},
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/test"},
					},
					Image: "my-image:version",
					NavItems: []*crd.BundleNavItem{{
						Title:   "Test",
						GroupID: "",
						Href:    "/test/href",
					}},
					Module: &crd.FedModule{
						ManifestLocation: "/apps/inventory/fed-mods.json",
						FullProfile:      crd.TruePtr(),
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/test/href",
							}},
						}},
						Config: &customConfig,
					},
					FeoConfigEnabled: true,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())

			frontend2 := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendName2,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName:        FrontendEnvName,
					Title:          "",
					DeploymentRepo: "",
					API: &crd.APIInfo{
						Versions: []string{"v1"},
						Specs: []crd.APISpecInfo{
							{
								URL:          "https://console.redhat.com/api/inventory/v1/openapi.json",
								BundleLabels: []string{"insights"},
								FrontendName: "inventory-deployment-abcdefg", // will be overridden
							},
						},
					},
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/test"},
					},
					Image: "my-image:version",
					NavItems: []*crd.BundleNavItem{{
						Title:   "Test",
						GroupID: "",
						Href:    "/test/href",
					}},
					Module: &crd.FedModule{
						ManifestLocation: "/apps/inventory/fed-mods.json",
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",

							Routes: []crd.Route{{
								Pathname: "/test/href",
							}},
						}},
						Config:      &customConfig2,
						FullProfile: crd.FalsePtr(),
					},
					FeoConfigEnabled: true,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend2)).Should(gomega.Succeed())

			frontendEnvironment := &crd.FrontendEnvironment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "FrontendEnvironment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendEnvironmentSpec{
					SSO:      "https://something-auth",
					Hostname: "something",
					Monitoring: &crd.MonitoringConfig{
						Mode: "app-interface",
					},
					GenerateNavJSON:      true,
					OverwriteCaddyConfig: true,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())

			bundle := &crd.Bundle{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Bundle",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.BundleSpec{
					ID:      BundleName,
					Title:   "",
					AppList: []string{FrontendName, FrontendName2},
					EnvName: FrontendEnvName,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, bundle)).Should(gomega.Succeed())

			deploymentLookupKey := types.NamespacedName{Name: frontend.Name + "-frontend", Namespace: FrontendNamespace}
			ingressLookupKey := types.NamespacedName{Name: frontend.Name, Namespace: FrontendNamespace}
			configMapLookupKey := types.NamespacedName{Name: frontendEnvironment.Name, Namespace: FrontendNamespace}
			serviceLookupKey := types.NamespacedName{Name: frontend.Name, Namespace: FrontendNamespace}
			createdDeployment := &apps.Deployment{}

			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, deploymentLookupKey, createdDeployment)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())
			expectedVolumeMounts := []v1.VolumeMount{
				{
					Name:      "config",
					MountPath: "/opt/app-root/src/build/chrome",
				},
				{
					Name:      "config",
					MountPath: "/opt/app-root/src/build/stable/operator-generated",
				},
				{
					Name:      "caddy",
					MountPath: "/opt/app-root/src/Caddyfile",
					SubPath:   "Caddyfile",
				},
				{
					Name:             "config-chrome",
					ReadOnly:         false,
					MountPath:        "/srv/dist/operator-generated/fed-modules.json",
					SubPath:          "fed-modules.json",
					MountPropagation: nil,
					SubPathExpr:      "",
				},
			}
			gomega.Expect(createdDeployment.Name).Should(gomega.Equal(FrontendName + "-frontend"))
			gomega.Expect(createdDeployment.Spec.Template.Spec.Containers[0].VolumeMounts).Should(gomega.Equal(expectedVolumeMounts))
			fmt.Printf("\n%v\n", createdDeployment.GetAnnotations())
			gomega.Expect(createdDeployment.Spec.Template.GetAnnotations()["configHash"]).ShouldNot(gomega.Equal(""))

			createdIngress := &networking.Ingress{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, ingressLookupKey, createdIngress)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())
			gomega.Expect(createdIngress.Name).Should(gomega.Equal(FrontendName))

			createdService := &v1.Service{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, serviceLookupKey, createdService)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())
			gomega.Expect(createdService.Name).Should(gomega.Equal(FrontendName))

			createdConfigMap := &v1.ConfigMap{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
				if err != nil {
					return err == nil
				}
				if len(createdConfigMap.Data) != 4 {
					return false
				}
				return true
			}, timeout, interval).Should(gomega.BeTrue())
			gomega.Expect(createdConfigMap.Name).Should(gomega.Equal(FrontendEnvName))
			gomega.Expect(createdConfigMap.Data).Should(gomega.Equal(map[string]string{
				"api-specs.json":   "[{\"url\":\"https://console.redhat.com/api/inventory/v1/openapi.json\",\"bundleLabels\":[\"insights\"],\"frontendName\":\"test-frontend\"},{\"url\":\"https://console.redhat.com/api/inventory/v1/openapi.json\",\"bundleLabels\":[\"insights\"],\"frontendName\":\"test-frontend2\"}]",
				"Caddyfile":        caddyFileTemplate,
				"fed-modules.json": "{\"testFrontend\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"config\":{\"apple\":\"pie\"},\"fullProfile\":true,\"cdnPath\":\"/things/test/\"},\"testFrontend2\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"config\":{\"cheese\":\"pasty\"},\"fullProfile\":false,\"cdnPath\":\"/things/test/\"}}",
				"sso-config.json":  "{\"environment\":\"test-env\",\"ssoUrl\":\"https://something-auth\"}",
			}))
			gomega.Expect(createdConfigMap.ObjectMeta.OwnerReferences[0].Name).Should(gomega.Equal(FrontendEnvName))

		})
	})
})

var _ = ginkgo.Describe("Frontend controller with service", func() {
	const (
		FrontendName      = "test-frontend-service"
		FrontendNamespace = "default"
		FrontendEnvName   = "test-env-service"
		ServiceName       = "test-service"
		BundleName        = "test-service-bundle"

		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	ginkgo.Context("When creating a Frontend Resource", func() {
		ginkgo.It("Should create a deployment with the correct items", func() {
			ginkgo.By("ginkgo.By creating a new Frontend")
			ctx := context.Background()

			frontendEnvironment := crd.FrontendEnvironment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "FrontendEnvironment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendEnvironmentSpec{
					SSO:      "https://something-auth",
					Hostname: "something",
					Whitelist: []string{
						"192.168.0.0/24",
						"10.10.0.0/24",
					},
					Monitoring: &crd.MonitoringConfig{
						Mode: "local",
					},
					GenerateNavJSON: false,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, &frontendEnvironment)).Should(gomega.Succeed())

			frontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName:        FrontendEnvName,
					Title:          "",
					DeploymentRepo: "",
					API: &crd.APIInfo{
						Versions: []string{"v1"},
						Specs: []crd.APISpecInfo{
							{
								URL:          "https://console.redhat.com/api/inventory/v1/openapi.json",
								BundleLabels: []string{"insights"},
								FrontendName: "inventory-deployment-abcdefg",
							},
						},
					},
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/test"},
					},
					Service: ServiceName,
					NavItems: []*crd.BundleNavItem{
						{
							Title:   "Test",
							GroupID: "",
							Href:    "/test/href",
						},
						{
							Title:   "Test2",
							GroupID: "",
							Href:    "/test/href2",
						},
					},
					Module: &crd.FedModule{
						ManifestLocation: "/apps/inventory/fed-mods.json",
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/test/href",
							}},
						}},
					},
					FeoConfigEnabled: true,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())

			bundle := crd.Bundle{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Bundle",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.BundleSpec{
					ID:      BundleName,
					Title:   "",
					AppList: []string{FrontendName},
					EnvName: FrontendEnvName,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, &bundle)).Should(gomega.Succeed())

			ingressLookupKey := types.NamespacedName{Name: frontend.Name, Namespace: FrontendNamespace}
			configMapLookupKey := types.NamespacedName{Name: frontendEnvironment.Name, Namespace: FrontendNamespace}

			createdIngress := &networking.Ingress{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, ingressLookupKey, createdIngress)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())
			gomega.Expect(createdIngress.Name).Should(gomega.Equal(FrontendName))
			gomega.Expect(createdIngress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).Should(gomega.Equal(ServiceName))
			gomega.Expect(createdIngress.Annotations["nginx.ingress.kubernetes.io/whitelist-source-range"]).Should(gomega.Equal("192.168.0.0/24,10.10.0.0/24"))
			gomega.Expect(createdIngress.Annotations["haproxy.router.openshift.io/ip_whitelist"]).Should(gomega.Equal("192.168.0.0/24 10.10.0.0/24"))

			createdConfigMap := &v1.ConfigMap{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())
			gomega.Expect(createdConfigMap.Name).Should(gomega.Equal(FrontendEnvName))
			gomega.Expect(createdConfigMap.Data).Should(gomega.Equal(map[string]string{
				"Caddyfile":        caddyFileTemplate,
				"api-specs.json":   "[{\"url\":\"https://console.redhat.com/api/inventory/v1/openapi.json\",\"bundleLabels\":[\"insights\"],\"frontendName\":\"test-frontend-service\"}]",
				"fed-modules.json": "{\"testFrontendService\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"fullProfile\":false,\"cdnPath\":\"/things/test/\"}}",
				"sso-config.json":  "{\"environment\":\"test-env-service\",\"ssoUrl\":\"https://something-auth\"}",
			}))

			gomega.Eventually(func() bool {
				fmt.Printf("TESTING..............")
				nfe := &crd.Frontend{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: frontend.Name, Namespace: frontend.Namespace}, nfe)
				if err != nil {
					fmt.Printf("ERRRRORRRRR: %s", err)
					return false
				}
				fmt.Printf("SO GO HERE.....")
				fmt.Printf("%v", nfe.Status.Conditions)
				// Check the length of Conditions slice before accessing by index
				if len(nfe.Status.Conditions) > 2 {
					fmt.Printf("I GOT TRUE???")
					gomega.Expect(nfe.Status.Conditions[0].Type).Should(gomega.Equal(crd.ReconciliationSuccessful))
					gomega.Expect(nfe.Status.Conditions[0].Status).Should(gomega.Equal(metav1.ConditionTrue))
					gomega.Expect(nfe.Status.Conditions[1].Type).Should(gomega.Equal(crd.ReconciliationFailed))
					gomega.Expect(nfe.Status.Conditions[1].Status).Should(gomega.Equal(metav1.ConditionFalse))
					gomega.Expect(nfe.Status.Conditions[2].Type).Should(gomega.Equal(crd.FrontendsReady))
					gomega.Expect(nfe.Status.Conditions[2].Status).Should(gomega.Equal(metav1.ConditionTrue))
					gomega.Expect(nfe.Status.Ready).Should(gomega.Equal(true))
					return true
				}
				return false
			}, timeout, interval).Should(gomega.BeTrue())

		})
	})
})

var _ = ginkgo.Describe("Frontend controller with chrome", func() {
	const (
		FrontendName      = "chrome"
		FrontendNamespace = "default"
		FrontendEnvName   = "test-chrome-env"
		FrontendName2     = "non-chrome"
		FrontendName3     = "no-config"
		BundleName        = "test-chrome-bundle"

		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	ginkgo.Context("When creating a chrome Frontend Resource", func() {
		ginkgo.It("Should create a deployment with the correct items", func() {
			ginkgo.By("ginkgo.By creating a new Frontend")
			ctx := context.Background()

			var customConfig apiextensions.JSON
			err := customConfig.UnmarshalJSON([]byte(`{"apple":"pie"}`))
			gomega.Expect(err).Should(gomega.BeNil())

			frontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName:        FrontendEnvName,
					Title:          "",
					DeploymentRepo: "",
					API: &crd.APIInfo{
						Versions: []string{"v1"},
					},
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/test"},
					},
					Image: "my-image:version",
					NavItems: []*crd.BundleNavItem{{
						Title:   "Test",
						GroupID: "",
						Href:    "/test/href",
					}},
					Module: &crd.FedModule{
						ManifestLocation: "/apps/inventory/fed-mods.json",
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/test/href",
							}},
						}},
						Config: &customConfig,
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())

			frontend2 := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendName2,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName:        FrontendEnvName,
					Title:          "",
					DeploymentRepo: "",
					API: &crd.APIInfo{
						Versions: []string{"v1"},
					},
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/test"},
					},
					Image: "my-image:version",
					NavItems: []*crd.BundleNavItem{{
						Title:   "Test",
						GroupID: "",
						Href:    "/test/href",
					}},
					Module: &crd.FedModule{
						ManifestLocation: "/apps/inventory/fed-mods.json",
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/test/href",
							}},
						}},
						Config: &customConfig,
					},
					FeoConfigEnabled: true,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend2)).Should(gomega.Succeed())

			frontend3 := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendName3,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName:        FrontendEnvName,
					Title:          "",
					DeploymentRepo: "",
					API: &crd.APIInfo{
						Versions: []string{"v1"},
					},
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/test"},
					},
					Image: "my-image:version",
					NavItems: []*crd.BundleNavItem{{
						Title:   "Test",
						GroupID: "",
						Href:    "/test/href",
					}},
					Module: &crd.FedModule{
						ManifestLocation: "/apps/inventory/fed-mods.json",
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/test/href",
							}},
						}},
					},
					FeoConfigEnabled: true,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend3)).Should(gomega.Succeed())

			frontendEnvironment := &crd.FrontendEnvironment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "FrontendEnvironment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendEnvironmentSpec{
					SSO:      "https://something-auth",
					Hostname: "something",
					Monitoring: &crd.MonitoringConfig{
						Mode: "app-interface",
					},
					GenerateNavJSON: true,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())

			bundle := &crd.Bundle{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Bundle",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.BundleSpec{
					ID:      BundleName,
					Title:   "",
					AppList: []string{FrontendName, FrontendName2, FrontendName3},
					EnvName: FrontendEnvName,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, bundle)).Should(gomega.Succeed())

			deploymentLookupKey := types.NamespacedName{Name: frontend.Name + "-frontend", Namespace: FrontendNamespace}
			ingressLookupKey := types.NamespacedName{Name: frontend.Name, Namespace: FrontendNamespace}
			configMapLookupKey := types.NamespacedName{Name: frontendEnvironment.Name, Namespace: FrontendNamespace}
			serviceLookupKey := types.NamespacedName{Name: frontend.Name, Namespace: FrontendNamespace}
			createdDeployment := &apps.Deployment{}

			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, deploymentLookupKey, createdDeployment)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())
			expectedVolumeMounts := []v1.VolumeMount{
				{
					Name:      "config",
					MountPath: "/opt/app-root/src/build/chrome",
				},
				{
					Name:      "config",
					MountPath: "/opt/app-root/src/build/stable/operator-generated",
				},
				{
					Name:             "config-chrome",
					ReadOnly:         false,
					MountPath:        "/srv/dist/operator-generated/fed-modules.json",
					SubPath:          "fed-modules.json",
					MountPropagation: nil,
					SubPathExpr:      "",
				},
			}
			gomega.Expect(createdDeployment.Name).Should(gomega.Equal(FrontendName + "-frontend"))
			gomega.Expect(createdDeployment.Spec.Template.Spec.Containers[0].VolumeMounts).Should(gomega.Equal(expectedVolumeMounts))
			fmt.Printf("\n%v\n", createdDeployment.GetAnnotations())
			gomega.Expect(createdDeployment.Spec.Template.GetAnnotations()["configHash"]).ShouldNot(gomega.Equal(""))

			createdIngress := &networking.Ingress{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, ingressLookupKey, createdIngress)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())
			gomega.Expect(createdIngress.Name).Should(gomega.Equal(FrontendName))

			createdService := &v1.Service{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, serviceLookupKey, createdService)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())
			gomega.Expect(createdService.Name).Should(gomega.Equal(FrontendName))

			createdConfigMap := &v1.ConfigMap{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
				if err != nil {
					return err == nil
				}
				if len(createdConfigMap.Data) != 3 {
					return false
				}
				return true
			}, timeout, interval).Should(gomega.BeTrue())
			gomega.Expect(createdConfigMap.Name).Should(gomega.Equal(FrontendEnvName))
			gomega.Expect(createdConfigMap.Data).Should(gomega.Equal(map[string]string{
				"Caddyfile":        caddyFileTemplate,
				"fed-modules.json": "{\"chrome\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"config\":{\"apple\":\"pie\",\"ssoUrl\":\"https://something-auth\"},\"fullProfile\":false,\"cdnPath\":\"/things/test/\"},\"noConfig\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"fullProfile\":false,\"cdnPath\":\"/things/test/\"},\"nonChrome\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"config\":{\"apple\":\"pie\"},\"fullProfile\":false,\"cdnPath\":\"/things/test/\"}}",
				"sso-config.json":  "{\"environment\":\"test-chrome-env\",\"ssoUrl\":\"https://something-auth\"}",
			}))
			gomega.Expect(createdConfigMap.ObjectMeta.OwnerReferences[0].Name).Should(gomega.Equal(FrontendEnvName))

		})
	})
})

var _ = ginkgo.Describe("ServiceMonitor Creation", func() {
	const (
		FrontendName      = "test-service-monitor"
		FrontendNamespace = "default"
		FrontendEnvName   = "test-service-env"
		BundleName        = "test-bundle"

		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	ginkgo.Context("When creating a Frontend Resource", func() {
		ginkgo.It("Should create a ServiceMonitor", func() {
			ginkgo.By("Reading the FrontendEnvironment")
			ctx := context.Background()

			frontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName:        FrontendEnvName,
					Title:          "",
					DeploymentRepo: "",
					API: &crd.APIInfo{
						Versions: []string{"v1"},
						Specs: []crd.APISpecInfo{
							{
								URL:          "https://console.redhat.com/api/inventory/v1/openapi.json",
								BundleLabels: []string{"insights"},
								FrontendName: "inventory-deployment-abcdefg",
							},
						},
					},
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/test"},
					},
					Image: "my-image:version",
					NavItems: []*crd.BundleNavItem{{
						Title:   "Test",
						GroupID: "",
						Href:    "/test/href",
					}},
					Module: &crd.FedModule{
						ManifestLocation: "/apps/inventory/fed-mods.json",
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/test/href",
							}},
						}},
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())

			frontendEnvironment := &crd.FrontendEnvironment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "FrontendEnvironment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendEnvironmentSpec{
					SSO:      "https://something-auth",
					Hostname: "something",
					Monitoring: &crd.MonitoringConfig{
						Mode: "app-interface",
					},
					GenerateNavJSON: true,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())

			bundle := &crd.Bundle{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Bundle",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.BundleSpec{
					ID:      BundleName,
					Title:   "",
					AppList: []string{FrontendName},
					EnvName: FrontendEnvName,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, bundle)).Should(gomega.Succeed())

			serviceLookupKey := types.NamespacedName{Name: frontend.Name, Namespace: FrontendNamespace}
			monitorLookupKey := types.NamespacedName{Name: frontend.Name, Namespace: MonitoringNamespace}

			createdService := &v1.Service{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, serviceLookupKey, createdService)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())
			gomega.Expect(createdService.Name).Should(gomega.Equal(FrontendName))

			createdServiceMonitor := &prom.ServiceMonitor{}
			ls := metav1.LabelSelector{
				MatchLabels: map[string]string{
					"frontend": FrontendName,
				},
			}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, monitorLookupKey, createdServiceMonitor)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())
			gomega.Expect(createdServiceMonitor.Name).Should(gomega.Equal(FrontendName))
			gomega.Expect(createdServiceMonitor.Spec.Selector).Should(gomega.Equal(ls))
		})
	})
})

var _ = ginkgo.Describe("Dependencies", func() {
	const (
		FrontendName      = "test-dependencies"
		FrontendName2     = "test-optional-dependencies"
		FrontendName3     = "test-no-dependencies"
		FrontendNamespace = "default"
		FrontendEnvName   = "test-dependencies-env"
		BundleName        = "test-dependencies-bundle"

		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	ginkgo.Context("When creating a Frontend Resource with dependencies", func() {
		ginkgo.It("Should create the right config", func() {
			ginkgo.By("Setting up dependencies and optionaldependencies")
			ctx := context.Background()

			configMapLookupKey := types.NamespacedName{Name: FrontendEnvName, Namespace: FrontendNamespace}

			frontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName:        FrontendEnvName,
					Title:          "",
					DeploymentRepo: "",
					API: &crd.APIInfo{
						Versions: []string{"v1"},
					},
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/test"},
					},
					Image: "my-image:version",
					NavItems: []*crd.BundleNavItem{{
						Title:   "Test",
						GroupID: "",
						Href:    "/test/href",
					}},
					Module: &crd.FedModule{
						ManifestLocation: "/apps/inventory/fed-mods.json",
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/test/href",
							}},
							Dependencies: []string{"depstring"},
						}},
					},
					FeoConfigEnabled: true,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())

			frontend2 := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendName2,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName:        FrontendEnvName,
					Title:          "",
					DeploymentRepo: "",
					API: &crd.APIInfo{
						Versions: []string{"v1"},
					},
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/test"},
					},
					Image: "my-image:version",
					NavItems: []*crd.BundleNavItem{{
						Title:   "Test",
						GroupID: "",
						Href:    "/test/href",
					}},
					Module: &crd.FedModule{
						ManifestLocation: "/apps/inventory/fed-mods.json",
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/test/href",
							}},
							OptionalDependencies: []string{"depstring-op"},
						}},
					},
					FeoConfigEnabled: true,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend2)).Should(gomega.Succeed())

			frontend3 := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendName3,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName:        FrontendEnvName,
					Title:          "",
					DeploymentRepo: "",
					API: &crd.APIInfo{
						Versions: []string{"v1"},
					},
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/test"},
					},
					Image: "my-image:version",
					NavItems: []*crd.BundleNavItem{{
						Title:   "Test",
						GroupID: "",
						Href:    "/test/href",
					}},
					Module: &crd.FedModule{
						ManifestLocation: "/apps/inventory/fed-mods.json",
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/test/href",
							}},
						}},
					},
					FeoConfigEnabled: true,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend3)).Should(gomega.Succeed())

			frontendEnvironment := &crd.FrontendEnvironment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "FrontendEnvironment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendEnvironmentSpec{
					SSO:      "https://something-auth",
					Hostname: "something",
					Monitoring: &crd.MonitoringConfig{
						Mode: "app-interface",
					},
					GenerateNavJSON: true,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())

			bundle := &crd.Bundle{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Bundle",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.BundleSpec{
					ID:      BundleName,
					Title:   "",
					AppList: []string{FrontendName, FrontendName2, FrontendName3},
					EnvName: FrontendEnvName,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, bundle)).Should(gomega.Succeed())

			createdConfigMap := &v1.ConfigMap{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
				if err != nil {
					return err == nil
				}
				if len(createdConfigMap.Data) != 3 {
					return false
				}
				return true
			}, timeout, interval).Should(gomega.BeTrue())
			gomega.Expect(createdConfigMap.Name).Should(gomega.Equal(FrontendEnvName))
			gomega.Expect(createdConfigMap.Data).Should(gomega.Equal(map[string]string{
				"Caddyfile":        caddyFileTemplate,
				"fed-modules.json": "{\"testDependencies\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}],\"dependencies\":[\"depstring\"]}],\"fullProfile\":false,\"cdnPath\":\"/things/test/\"},\"testNoDependencies\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"fullProfile\":false,\"cdnPath\":\"/things/test/\"},\"testOptionalDependencies\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}],\"optionalDependencies\":[\"depstring-op\"]}],\"fullProfile\":false,\"cdnPath\":\"/things/test/\"}}",
				"sso-config.json":  "{\"environment\":\"test-dependencies-env\",\"ssoUrl\":\"https://something-auth\"}",
			}))
			gomega.Expect(createdConfigMap.ObjectMeta.OwnerReferences[0].Name).Should(gomega.Equal(FrontendEnvName))

		})
	})

})

type SearchFrontendEntry struct {
	Name          string
	SearchEntries []*crd.SearchEntry
}

type SearchIndexCase struct {
	SearchFrontendEntries []SearchFrontendEntry
	Env                   string
	ExpectedResult        string
	Namespace             string
}

func frontendFromSearchEntry(tc SearchIndexCase, entry SearchFrontendEntry) *crd.Frontend {
	frontend := &crd.Frontend{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cloud.redhat.com/v1",
			Kind:       "Frontend",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      entry.Name,
			Namespace: tc.Namespace,
		},
		Spec: crd.FrontendSpec{
			EnvName:        tc.Env,
			Title:          "",
			DeploymentRepo: "",
			Frontend: crd.FrontendInfo{
				Paths: []string{"/"},
			},
			Image: "my-image:version",
			Module: &crd.FedModule{
				ManifestLocation: "",
				Modules:          []crd.Module{},
			},
			SearchEntries:    entry.SearchEntries,
			FeoConfigEnabled: true,
		},
	}

	return frontend
}

func mockFrontendEnv(env string, namespace string) *crd.FrontendEnvironment {
	return &crd.FrontendEnvironment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cloud.redhat.com/v1",
			Kind:       "FrontendEnvironment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      env,
			Namespace: namespace,
		},
		Spec: crd.FrontendEnvironmentSpec{
			SSO:      "https://something-auth",
			Hostname: "something",
			Monitoring: &crd.MonitoringConfig{
				Mode: "app-interface",
			},
			GenerateNavJSON: false,
		},
	}
}

var _ = ginkgo.Describe("Search index", func() {
	const (
		FrontendName      = "test-search-index"
		FrontendName2     = "test-search-index2"
		FrontendName3     = "test-search-index3"
		FrontendNamespace = "default"
		FrontendEnvName   = "test-search-index-env"
		FrontendEnvName2  = "test-search-index-env2"

		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	ginkgo.Context("When creating frontend with search entries", func() {
		ginkgo.It("Should create the search index", func() {
			ginkgo.By("from single Frontend resource", func() {
				ctx := context.Background()

				testCase := SearchIndexCase{
					Env:            FrontendEnvName,
					Namespace:      FrontendNamespace,
					ExpectedResult: fmt.Sprintf("[{\"id\":\"test-search-index-test-search-index-env-test\",\"href\":\"/test/href\",\"title\":\"Test\",\"description\":\"Test description\",\"frontendRef\":\"%s\"},{\"id\":\"test-search-index-test-search-index-env-test2\",\"href\":\"/test2/href\",\"title\":\"Test2\",\"description\":\"Test2 description\",\"frontendRef\":\"%s\"}]", FrontendName, FrontendName),
					SearchFrontendEntries: []SearchFrontendEntry{{
						Name: FrontendName,
						SearchEntries: []*crd.SearchEntry{{
							ID:          "test2",
							Href:        "/test2/href",
							Title:       "Test2",
							Description: "Test2 description",
						}, {
							ID:          "test",
							Href:        "/test/href",
							Title:       "Test",
							Description: "Test description",
						}},
					}},
				}
				configMapLookupKey := types.NamespacedName{Name: testCase.Env, Namespace: testCase.Namespace}
				for _, tc := range testCase.SearchFrontendEntries {
					frontend := frontendFromSearchEntry(testCase, tc)
					gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())
				}
				frontendEnvironment := mockFrontendEnv(testCase.Env, testCase.Namespace)
				gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())
				createdConfigMap := &v1.ConfigMap{}
				gomega.Eventually(func() bool {
					err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
					if err != nil {
						return err == nil
					}
					if len(createdConfigMap.Data) != 4 {
						return false
					}
					return true
				}, timeout, interval).Should(gomega.BeTrue())
				gomega.Expect(createdConfigMap.Name).Should(gomega.Equal(FrontendEnvName))

				searchIndexMap, ok := createdConfigMap.Data["search-index.json"]
				gomega.Expect(ok).Should(gomega.BeTrue())
				gomega.Expect(searchIndexMap).Should(gomega.Equal(testCase.ExpectedResult))
				gomega.Expect(createdConfigMap.ObjectMeta.OwnerReferences[0].Name).Should(gomega.Equal(FrontendEnvName))
			})

			ginkgo.By("from multiple Frontend resources", func() {
				ctx := context.Background()

				testCase := SearchIndexCase{
					Env:            FrontendEnvName2,
					Namespace:      FrontendNamespace,
					ExpectedResult: fmt.Sprintf("[{\"id\":\"test-search-index2-test-search-index-env2-test-search-index2\",\"href\":\"/test/href\",\"title\":\"Test\",\"description\":\"Test description\",\"frontendRef\":\"%s\"},{\"id\":\"test-search-index3-test-search-index-env2-test-search-index3\",\"href\":\"/test/href\",\"title\":\"Test\",\"description\":\"Test description\",\"frontendRef\":\"%s\"}]", FrontendName2, FrontendName3),
					SearchFrontendEntries: []SearchFrontendEntry{{
						Name: FrontendName2,
						SearchEntries: []*crd.SearchEntry{{
							ID:          FrontendName2,
							Href:        "/test/href",
							Title:       "Test",
							Description: "Test description",
						}},
					}, {
						Name: FrontendName3,
						SearchEntries: []*crd.SearchEntry{{
							ID:          FrontendName3,
							Href:        "/test/href",
							Title:       "Test",
							Description: "Test description",
						}},
					}},
				}

				configMapLookupKey := types.NamespacedName{Name: testCase.Env, Namespace: testCase.Namespace}
				for _, tc := range testCase.SearchFrontendEntries {
					frontend := frontendFromSearchEntry(testCase, tc)
					gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())
				}

				frontendEnvironment := mockFrontendEnv(testCase.Env, testCase.Namespace)
				gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())
				createdConfigMap := &v1.ConfigMap{}
				gomega.Eventually(func() bool {
					err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
					if err != nil {
						return err == nil
					}
					if len(createdConfigMap.Data) != 4 {
						return false
					}
					return true
				}, timeout, interval).Should(gomega.BeTrue())
				searchIndexMap, ok := createdConfigMap.Data["search-index.json"]
				gomega.Expect(ok).Should(gomega.BeTrue())

				ssoConfigMap, ok := createdConfigMap.Data["sso-config.json"]
				gomega.Expect(ok).Should(gomega.BeTrue())
				gomega.Expect(ssoConfigMap).Should(gomega.Equal(`{"environment":"test-search-index-env2","ssoUrl":"https://something-auth"}`))

				// Make sure the order does not break the tests
				var sortedSearchIndex []crd.SearchEntry
				err := json.Unmarshal([]byte(searchIndexMap), &sortedSearchIndex)
				gomega.Expect(err).Should(gomega.BeNil())
				sort.Slice(sortedSearchIndex, func(i, j int) bool {
					return sortedSearchIndex[i].ID < sortedSearchIndex[j].ID
				})
				var expectedIndex []crd.SearchEntry
				err = json.Unmarshal([]byte(testCase.ExpectedResult), &expectedIndex)
				gomega.Expect(err).Should(gomega.BeNil())
				gomega.Expect(createdConfigMap.Name).Should(gomega.Equal(testCase.Env))

				for _, expectedCase := range expectedIndex {
					gomega.Expect(sortedSearchIndex).Should(gomega.ContainElement(expectedCase))
				}
				gomega.Expect(createdConfigMap.ObjectMeta.OwnerReferences[0].Name).Should(gomega.Equal(testCase.Env))
			})

			ginkgo.By("with identical content but different IDs to test sorting", func() {
				ctx := context.Background()

				testCase := SearchIndexCase{
					Env:            "test-search-index-env-sorting",
					Namespace:      FrontendNamespace,
					ExpectedResult: fmt.Sprintf("[{\"id\":\"entry-a\",\"href\":\"/identical/href\",\"title\":\"Identical Title\",\"description\":\"Identical description\",\"frontendRef\":\"%s\"},{\"id\":\"entry-z\",\"href\":\"/identical/href\",\"title\":\"Identical Title\",\"description\":\"Identical description\",\"frontendRef\":\"%s\"}]", "test-frontend-id", "test-frontend-id"),
					SearchFrontendEntries: []SearchFrontendEntry{{
						Name: "test-frontend-id",
						SearchEntries: []*crd.SearchEntry{{
							ID:          "entry-z", // Later alphabetically
							Href:        "/identical/href",
							Title:       "Identical Title",
							Description: "Identical description",
						}, {
							ID:          "entry-a", // Earlier alphabetically
							Href:        "/identical/href",
							Title:       "Identical Title",
							Description: "Identical description",
						}},
					}},
				}
				configMapLookupKey := types.NamespacedName{Name: testCase.Env, Namespace: testCase.Namespace}
				for _, tc := range testCase.SearchFrontendEntries {
					frontend := frontendFromSearchEntry(testCase, tc)
					fmt.Println("frontend", frontend)
					gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())
				}
				frontendEnvironment := mockFrontendEnv(testCase.Env, testCase.Namespace)
				gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())
				createdConfigMap := &v1.ConfigMap{}
				gomega.Eventually(func() bool {
					err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
					if err != nil {
						return err == nil
					}
					if len(createdConfigMap.Data) != 4 {
						return false
					}
					return true
				}, timeout, interval).Should(gomega.BeTrue())
				gomega.Expect(createdConfigMap.Name).Should(gomega.Equal(testCase.Env))

				searchIndexMap, ok := createdConfigMap.Data["search-index.json"]
				gomega.Expect(ok).Should(gomega.BeTrue())

				// Parse the actual result and verify it's sorted correctly by ID
				var actualSearchIndex []crd.SearchEntry
				err := json.Unmarshal([]byte(searchIndexMap), &actualSearchIndex)
				gomega.Expect(err).Should(gomega.BeNil())

				// Should have exactly 2 entries
				gomega.Expect(len(actualSearchIndex)).Should(gomega.Equal(2))

				// Verify that entries are sorted by ID (entry-a should come before entry-z)
				gomega.Expect(actualSearchIndex[0].ID).Should(gomega.ContainSubstring("entry-a"))
				gomega.Expect(actualSearchIndex[1].ID).Should(gomega.ContainSubstring("entry-z"))

				// Verify both have identical non-ID fields
				gomega.Expect(actualSearchIndex[0].Title).Should(gomega.Equal("Identical Title"))
				gomega.Expect(actualSearchIndex[0].Href).Should(gomega.Equal("/identical/href"))
				gomega.Expect(actualSearchIndex[0].Description).Should(gomega.Equal("Identical description"))
				gomega.Expect(actualSearchIndex[0].FrontendRef).Should(gomega.Equal("test-frontend-id"))
				gomega.Expect(actualSearchIndex[1].Title).Should(gomega.Equal("Identical Title"))
				gomega.Expect(actualSearchIndex[1].Href).Should(gomega.Equal("/identical/href"))
				gomega.Expect(actualSearchIndex[1].Description).Should(gomega.Equal("Identical description"))
				gomega.Expect(actualSearchIndex[1].FrontendRef).Should(gomega.Equal("test-frontend-id"))

				gomega.Expect(createdConfigMap.ObjectMeta.OwnerReferences[0].Name).Should(gomega.Equal(testCase.Env))
			})

			ginkgo.By("with identical content but different FrontendRefs to test sorting", func() {
				ctx := context.Background()

				testCase := SearchIndexCase{
					Env:            "test-search-index-env-sorting-2",
					Namespace:      FrontendNamespace,
					ExpectedResult: fmt.Sprintf("[{\"id\":\"entry-a\",\"href\":\"/identical/href\",\"title\":\"Identical Title\",\"description\":\"Identical description\",\"frontendRef\":\"%s\"},{\"id\":\"entry-z\",\"href\":\"/identical/href\",\"title\":\"Identical Title\",\"description\":\"Identical description\",\"frontendRef\":\"%s\"}]", "test-frontend-ref-1", "test-frontend-ref-2"),
					SearchFrontendEntries: []SearchFrontendEntry{{
						Name: "test-frontend-ref-1",
						SearchEntries: []*crd.SearchEntry{{
							ID:          "entry-a",
							Href:        "/identical/href",
							Title:       "Identical Title",
							Description: "Identical description",
							FrontendRef: "test-frontend-ref-1",
						}},
					}, {
						Name: "test-frontend-ref-2",
						SearchEntries: []*crd.SearchEntry{{
							ID:          "entry-a",
							Href:        "/identical/href",
							Title:       "Identical Title",
							Description: "Identical description",
							FrontendRef: "test-frontend-ref-2",
						}},
					}},
				}
				configMapLookupKey := types.NamespacedName{Name: testCase.Env, Namespace: testCase.Namespace}
				for _, tc := range testCase.SearchFrontendEntries {
					frontend := frontendFromSearchEntry(testCase, tc)
					gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())
				}
				frontendEnvironment := mockFrontendEnv(testCase.Env, testCase.Namespace)
				gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())
				createdConfigMap := &v1.ConfigMap{}
				gomega.Eventually(func() bool {
					err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
					if err != nil {
						return err == nil
					}
					if len(createdConfigMap.Data) != 4 {
						return false
					}
					return true
				}, timeout, interval).Should(gomega.BeTrue())
				gomega.Expect(createdConfigMap.Name).Should(gomega.Equal(testCase.Env))

				searchIndexMap, ok := createdConfigMap.Data["search-index.json"]
				gomega.Expect(ok).Should(gomega.BeTrue())

				// Parse the actual result and verify it's sorted correctly by ID
				var actualSearchIndex []crd.SearchEntry
				err := json.Unmarshal([]byte(searchIndexMap), &actualSearchIndex)
				gomega.Expect(err).Should(gomega.BeNil())

				// Should have exactly 2 entries
				gomega.Expect(len(actualSearchIndex)).Should(gomega.Equal(2))

				gomega.Expect(actualSearchIndex[0].FrontendRef).Should(gomega.Equal("test-frontend-ref-1"))
				gomega.Expect(actualSearchIndex[1].FrontendRef).Should(gomega.Equal("test-frontend-ref-2"))

				// Verify both have identical non-FrontendRef fields
				gomega.Expect(actualSearchIndex[0].ID).Should(gomega.ContainSubstring("entry-a"))
				gomega.Expect(actualSearchIndex[1].ID).Should(gomega.ContainSubstring("entry-a"))
				gomega.Expect(actualSearchIndex[0].Title).Should(gomega.Equal("Identical Title"))
				gomega.Expect(actualSearchIndex[0].Href).Should(gomega.Equal("/identical/href"))
				gomega.Expect(actualSearchIndex[0].Description).Should(gomega.Equal("Identical description"))
				gomega.Expect(actualSearchIndex[1].Title).Should(gomega.Equal("Identical Title"))
				gomega.Expect(actualSearchIndex[1].Href).Should(gomega.Equal("/identical/href"))
				gomega.Expect(actualSearchIndex[1].Description).Should(gomega.Equal("Identical description"))

				gomega.Expect(createdConfigMap.ObjectMeta.OwnerReferences[0].Name).Should(gomega.Equal(testCase.Env))
			})
		})
	})
})

type WidgetFrontendTestEntry struct {
	Widgets      []*crd.WidgetModuleFederationMetadata
	FrontendName string
}

type WidgetCase struct {
	WidgetsFrontend        []WidgetFrontendTestEntry
	Namespace              string
	Environment            string
	ExpectedConfigMapEntry []crd.WidgetModuleFederationMetadata
}

func frontendFromWidget(wc WidgetCase, wf WidgetFrontendTestEntry) *crd.Frontend {
	frontend := &crd.Frontend{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cloud.redhat.com/v1",
			Kind:       "Frontend",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      wf.FrontendName,
			Namespace: wc.Namespace,
		},
		Spec: crd.FrontendSpec{
			EnvName:        wc.Environment,
			Title:          "",
			DeploymentRepo: "",
			Frontend: crd.FrontendInfo{
				Paths: []string{""},
			},
			Image: "my-image:version",
			Module: &crd.FedModule{
				ManifestLocation: "",
				Modules:          []crd.Module{},
			},
			WidgetRegistry:   wf.Widgets,
			FeoConfigEnabled: true,
		},
	}
	return frontend
}

var _ = ginkgo.Describe("Widget registry", func() {
	const (
		FrontendName      = "test-widget-registry"
		FrontendName2     = "test-widget-registry2"
		FrontendNamespace = "default"
		FrontendEnvName   = "test-widget-registry-env"

		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	var (
		WidgetDefaults = crd.WidgetBaseDimensions{
			Width:     intPtr(1),
			Height:    intPtr(1),
			MaxHeight: intPtr(2),
			MinHeight: intPtr(1),
		}
		Widget1 = &crd.WidgetModuleFederationMetadata{
			Scope:  "test",
			Module: "./foo",
			Config: crd.WidgetConfiguration{
				Icon:  "icon",
				Title: "title",
			},
			Defaults: WidgetDefaults,
		}
		Widget2 = &crd.WidgetModuleFederationMetadata{
			Scope:  "test",
			Module: "./bar",
			Config: crd.WidgetConfiguration{
				Icon:  "icon-bar",
				Title: "Bar",
			},
			Defaults: WidgetDefaults,
		}
		Widget3 = &crd.WidgetModuleFederationMetadata{
			Scope:  "baz",
			Module: "./default",
			Config: crd.WidgetConfiguration{
				Icon:  "baz",
				Title: "Baz",
			},
			Defaults: WidgetDefaults,
		}
	)

	ginkgo.It("Should create widget registry", func() {
		ginkgo.By("collection entries from Frontend resources", func() {
			expectedResult := []crd.WidgetModuleFederationMetadata{{
				FrontendRef: FrontendName,
				Scope:       Widget1.Scope,
				Module:      Widget1.Module,
				Config:      Widget1.Config,
				Defaults:    WidgetDefaults,
			}, {
				FrontendRef: FrontendName,
				Scope:       Widget2.Scope,
				Module:      Widget2.Module,
				Config:      Widget2.Config,
				Defaults:    WidgetDefaults,
			}, {
				FrontendRef: FrontendName2,
				Scope:       Widget3.Scope,
				Module:      Widget3.Module,
				Config:      Widget3.Config,
				Defaults:    WidgetDefaults,
			}}
			widgetCases := []WidgetCase{{
				WidgetsFrontend: []WidgetFrontendTestEntry{{
					Widgets:      []*crd.WidgetModuleFederationMetadata{Widget1, Widget2},
					FrontendName: FrontendName,
				}, {
					Widgets:      []*crd.WidgetModuleFederationMetadata{Widget3},
					FrontendName: FrontendName2,
				},
				},
				Namespace:              FrontendNamespace,
				Environment:            FrontendEnvName,
				ExpectedConfigMapEntry: expectedResult,
			}}

			for _, widgetCase := range widgetCases {
				ctx := context.Background()
				configMapLookupKey := types.NamespacedName{Name: widgetCase.Environment + "-widget-registry", Namespace: widgetCase.Namespace}
				for _, wf := range widgetCase.WidgetsFrontend {
					frontend := frontendFromWidget(widgetCase, wf)
					gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())
				}

				frontendEnvironment := mockFrontendEnv(widgetCase.Environment, widgetCase.Namespace)
				gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())
				createdConfigMap := &v1.ConfigMap{}
				gomega.Eventually(func() bool {
					err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
					if err != nil {
						return err == nil
					}
					if len(createdConfigMap.Data) != 1 {
						return false
					}
					return true
				}, timeout, interval).Should(gomega.BeTrue())

				widgetRegistryMap := createdConfigMap.Data["widget-registry.json"]
				var widgetRegistry []crd.WidgetModuleFederationMetadata
				err := json.Unmarshal([]byte(widgetRegistryMap), &widgetRegistry)
				gomega.Expect(err).Should(gomega.BeNil())

				gomega.Expect(createdConfigMap.Name).Should(gomega.Equal(widgetCase.Environment + "-widget-registry"))
				for _, w := range expectedResult {
					gomega.Expect(widgetRegistry).Should(gomega.ContainElement(w))
				}
				gomega.Expect(createdConfigMap.ObjectMeta.OwnerReferences[0].Name).Should(gomega.Equal(widgetCase.Environment))
			}
		})
	})
})

type ServiceTileTestEntry struct {
	ServiceTiles []*crd.ServiceTile
	FrontendName string
}

type ServiceTileCase struct {
	ServiceTiles           []*ServiceTileTestEntry
	Namespace              string
	Environment            string
	ExpectedConfigMapEntry string
}

func frontendFromServiceTile(sct ServiceTileCase, ste ServiceTileTestEntry) *crd.Frontend {
	frontend := &crd.Frontend{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cloud.redhat.com/v1",
			Kind:       "Frontend",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ste.FrontendName,
			Namespace: sct.Namespace,
		},
		Spec: crd.FrontendSpec{
			EnvName:        sct.Environment,
			Title:          "",
			DeploymentRepo: "",
			Frontend: crd.FrontendInfo{
				Paths: []string{""},
			},
			Image: "my-image:version",
			Module: &crd.FedModule{
				ManifestLocation: "",
				Modules:          []crd.Module{},
			},
			ServiceTiles:     ste.ServiceTiles,
			FeoConfigEnabled: true,
		},
	}
	return frontend
}

var _ = ginkgo.Describe("Service tiles", func() {
	const (
		FrontendName           = "test-service-tile"
		FrontendName2          = "test-service-tile2"
		FrontendNamespace      = "default"
		FrontendEnvName        = "test-service-tile-env"
		FrontendEnvName2       = "test-service-tile-env2"
		ServiceSectionID       = "test-service-section"
		ServiceSectionGroupID1 = "test-service-section-group1"
		ServiceSectionGroupID2 = "test-service-section-group2"

		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	var (
		ServiceTile1 = &crd.ServiceTile{
			Section:     ServiceSectionID,
			Group:       ServiceSectionGroupID1,
			ID:          "test-service-tile1",
			Href:        "/foo",
			Title:       "bar",
			Description: "",
			Icon:        "",
		}
		ServiceTile2 = &crd.ServiceTile{
			Section:     ServiceSectionID,
			Group:       ServiceSectionGroupID1,
			ID:          "test-service-tile2",
			Href:        "/bar",
			Title:       "bar",
			Description: "",
			Icon:        "",
		}
		ServiceTile3 = &crd.ServiceTile{
			Section:     ServiceSectionID,
			Group:       ServiceSectionGroupID2,
			ID:          "test-service-tile3",
			Href:        "/baz",
			Title:       "baz",
			Description: "",
			Icon:        "",
		}
		ServiceTile4 = &crd.ServiceTile{
			Section:     ServiceSectionID,
			Group:       ServiceSectionGroupID2,
			ID:          "test-service-tile4",
			Href:        "/baz",
			Title:       "z",
			Description: "z",
		}
		ServiceTile5 = &crd.ServiceTile{
			Section:     ServiceSectionID,
			Group:       ServiceSectionGroupID2,
			ID:          "test-service-tile5",
			Href:        "/baz",
			Title:       "z",
			Description: "a",
		}
		ServiceTile6 = &crd.ServiceTile{
			Section:     ServiceSectionID,
			Group:       ServiceSectionGroupID2,
			ID:          "test-service-tile6",
			Href:        "/baz",
			Title:       "a",
			Description: "a",
		}
		ExpectedServiceTiles1 = []crd.FrontendServiceCategoryGenerated{
			{
				ID:    ServiceSectionID,
				Title: "Service Section",
				Groups: []crd.FrontendServiceCategoryGroupGenerated{{
					ID:    ServiceSectionGroupID1,
					Title: "Service Section Group 1",
					Tiles: &[]crd.ServiceTile{{
						Section:     ServiceTile1.Section,
						Group:       ServiceTile1.Group,
						ID:          ServiceTile1.ID,
						Href:        ServiceTile1.Href,
						Title:       ServiceTile1.Title,
						Description: ServiceTile1.Description,
						Icon:        ServiceTile1.Icon,
						FrontendRef: FrontendName,
					}, {
						Section:     ServiceTile2.Section,
						Group:       ServiceTile2.Group,
						ID:          ServiceTile2.ID,
						Href:        ServiceTile2.Href,
						Title:       ServiceTile2.Title,
						Description: ServiceTile2.Description,
						Icon:        ServiceTile2.Icon,
						FrontendRef: FrontendName,
					}},
				}, {
					ID:    ServiceSectionGroupID2,
					Title: "Service Section Group 2",
					Tiles: &[]crd.ServiceTile{{
						Section:     ServiceTile3.Section,
						Group:       ServiceTile3.Group,
						ID:          ServiceTile3.ID,
						Href:        ServiceTile3.Href,
						Title:       ServiceTile3.Title,
						Description: ServiceTile3.Description,
						Icon:        ServiceTile3.Icon,
						FrontendRef: FrontendName,
					}},
				}},
			},
		}
		ExpectedServiceTiles2 = []crd.FrontendServiceCategoryGenerated{
			{
				ID:    ServiceSectionID,
				Title: "Service Section",
				Groups: []crd.FrontendServiceCategoryGroupGenerated{{
					ID:    ServiceSectionGroupID1,
					Title: "Service Section Group 1",
					Tiles: &[]crd.ServiceTile{},
				}, {
					ID:    ServiceSectionGroupID2,
					Title: "Service Section Group 2",
					Tiles: &[]crd.ServiceTile{
						{
							Section:     ServiceTile4.Section,
							Group:       ServiceTile4.Group,
							ID:          ServiceTile4.ID,
							Href:        ServiceTile4.Href,
							Title:       ServiceTile4.Title,
							Description: ServiceTile4.Description,
							FrontendRef: FrontendName2,
						},
						{
							Section:     ServiceTile5.Section,
							Group:       ServiceTile5.Group,
							ID:          ServiceTile5.ID,
							Href:        ServiceTile5.Href,
							Title:       ServiceTile5.Title,
							Description: ServiceTile5.Description,
							FrontendRef: FrontendName2,
						},
						{
							Section:     ServiceTile6.Section,
							Group:       ServiceTile6.Group,
							ID:          ServiceTile6.ID,
							Href:        ServiceTile6.Href,
							Title:       ServiceTile6.Title,
							Description: ServiceTile6.Description,
							FrontendRef: FrontendName2,
						},
					},
				}},
			},
		}
	)

	ginkgo.It("Should create service tiles", func() {
		ginkgo.By("collection entries from Frontend resources", func() {
			expectedResult1, err := json.Marshal(ExpectedServiceTiles1)
			gomega.Expect(err).Should(gomega.BeNil())
			expectedResult2, err := json.Marshal(ExpectedServiceTiles2)
			gomega.Expect(err).Should(gomega.BeNil())
			serviceTileCases := []ServiceTileCase{{
				Namespace:              FrontendNamespace,
				Environment:            FrontendEnvName,
				ExpectedConfigMapEntry: string(expectedResult1),
				ServiceTiles: []*ServiceTileTestEntry{{
					ServiceTiles: []*crd.ServiceTile{ServiceTile1, ServiceTile2, ServiceTile3},
					FrontendName: FrontendName,
				}},
			},
				{
					Namespace:              FrontendNamespace,
					Environment:            FrontendEnvName2,
					ExpectedConfigMapEntry: string(expectedResult2),
					ServiceTiles: []*ServiceTileTestEntry{{
						ServiceTiles: []*crd.ServiceTile{ServiceTile4, ServiceTile5, ServiceTile6},
						FrontendName: FrontendName2,
					}},
				}}

			for _, serviceCase := range serviceTileCases {
				ctx := context.Background()
				configMapLookupKey := types.NamespacedName{Name: serviceCase.Environment, Namespace: serviceCase.Namespace}
				for _, sc := range serviceCase.ServiceTiles {
					frontend := frontendFromServiceTile(serviceCase, *sc)
					gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())
				}

				frontendEnvironment := mockFrontendEnv(serviceCase.Environment, serviceCase.Namespace)
				frontendEnvironment.Spec.ServiceCategories = &[]crd.FrontendServiceCategory{
					{
						ID:    ServiceSectionID,
						Title: "Service Section",
						Groups: []crd.FrontendServiceCategoryGroup{
							{
								ID:    ServiceSectionGroupID1,
								Title: "Service Section Group 1",
							},
							{
								ID:    ServiceSectionGroupID2,
								Title: "Service Section Group 2",
							},
						},
					},
				}
				gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())
				createdConfigMap := &v1.ConfigMap{}
				gomega.Eventually(func() bool {
					err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
					if err != nil {
						return err == nil
					}
					if len(createdConfigMap.Data) != 4 {
						return false
					}
					return true
				}, timeout, interval).Should(gomega.BeTrue())

				serviceTileRegistryMap := createdConfigMap.Data["service-tiles.json"]

				ssoConfigMap, ok := createdConfigMap.Data["sso-config.json"]
				gomega.Expect(ok).Should(gomega.BeTrue())
				expectedSSO := fmt.Sprintf(`{"environment":"%s","ssoUrl":"https://something-auth"}`, serviceCase.Environment)
				gomega.Expect(ssoConfigMap).Should(gomega.Equal(expectedSSO))

				gomega.Expect(createdConfigMap.Name).Should(gomega.Equal(serviceCase.Environment))
				gomega.Expect(serviceTileRegistryMap).Should(gomega.Equal(serviceCase.ExpectedConfigMapEntry))
				gomega.Expect(createdConfigMap.ObjectMeta.OwnerReferences[0].Name).Should(gomega.Equal(serviceCase.Environment))
			}
		})
	})
})

var _ = ginkgo.Describe("Navigation nesting", func() {
	const (
		FrontendName      = "test-nested-nav"
		FrontendNamespace = "default"
		FrontendEnvName   = "test-nested-nav-env"

		timeout  = time.Second * 20
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)
	ginkgo.It("Should stop navigation nesting if the limit is exceeded", func() {
		ctx := context.Background()
		configMapLookupKey := types.NamespacedName{Name: FrontendEnvName, Namespace: FrontendNamespace}
		frontendEnvironment := mockFrontendEnv(FrontendEnvName, FrontendNamespace)
		frontendEnvironment.Spec.Bundles = &[]crd.FrontendBundles{
			{
				ID:    "nested-bundle",
				Title: "Nested Bundle",
			},
		}
		frontend := &crd.Frontend{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "cloud.redhat.com/v1",
				Kind:       "Frontend",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      FrontendName,
				Namespace: FrontendNamespace,
			},
			Spec: crd.FrontendSpec{
				EnvName:        FrontendEnvName,
				Title:          "",
				DeploymentRepo: "",
				API: &crd.APIInfo{
					Versions: []string{"v1"},
				},
				Frontend: crd.FrontendInfo{
					Paths: []string{"/things/test"},
				},
				Image: "my-image:version",
				Module: &crd.FedModule{
					ManifestLocation: "/apps/inventory/fed-mods.json",
					Modules: []crd.Module{{
						ID:     "test",
						Module: "./RootApp",
						Routes: []crd.Route{{
							Pathname: "/test/href",
						}},
						Dependencies: []string{"depstring"},
					}},
				},
				FeoConfigEnabled: true,
				// deliberately create a circular references to test the depth limit
				NavigationSegments: []*crd.NavigationSegment{{
					SegmentID: "first-segment",
					NavItems: &[]crd.ChromeNavItem{{
						SegmentRef: &crd.SegmentRef{
							FrontendName: FrontendName,
							SegmentID:    "second-segment",
						},
					}},
				}, {
					SegmentID: "second-segment",
					NavItems: &[]crd.ChromeNavItem{{
						SegmentRef: &crd.SegmentRef{
							FrontendName: FrontendName,
							SegmentID:    "first-segment",
						},
					}},
				}},
				BundleSegments: []*crd.BundleSegment{{
					SegmentID: "test",
					BundleID:  "nested-bundle",
					Position:  100,
					NavItems: &[]crd.ChromeNavItem{{
						SegmentRef: &crd.SegmentRef{
							FrontendName: FrontendName,
							SegmentID:    "first-segment",
						},
					}},
				}},
			},
		}
		gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())
		gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())
		createdConfigMap := &v1.ConfigMap{}
		var depthError error
		gomega.Eventually(func() string {
			err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
			if err != nil {
				if strings.Contains(err.Error(), `configmaps "test-nested-nav-env" not found`) {
					depthError = err
					return depthError.Error()
				}
				return ""
			}
			return ""
		}, timeout, interval).Should(gomega.Equal(`configmaps "test-nested-nav-env" not found`))
	})
})

type CDNTestEntry struct {
	Paths           []string
	ExpectedPath    string
	FrontendName    string
	Namespace       string
	CaseDescription string
}

func frontendFromCDN(tc CDNTestEntry) *crd.Frontend {
	frontend := &crd.Frontend{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cloud.redhat.com/v1",
			Kind:       "Frontend",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      tc.FrontendName,
			Namespace: tc.Namespace,
		},
		Spec: crd.FrontendSpec{
			EnvName:        tc.FrontendName,
			Title:          "",
			DeploymentRepo: "",
			API: &crd.APIInfo{
				Versions: []string{"v1"},
			},
			Frontend: crd.FrontendInfo{
				Paths: tc.Paths,
			},
			Module: &crd.FedModule{
				ManifestLocation: "/foo/bar.json",
				Modules:          []crd.Module{},
			},
			FeoConfigEnabled: true,
		},
	}

	return frontend
}

var _ = ginkgo.Describe("CDN Path", func() {
	const (
		FrontendNamespace = "default"
		FrontendName      = "test-cdn-path"
		FrontendName2     = "test-cdn-path-2"
		FrontendName3     = "test-cdn-path-3"
		FrontendName4     = "test-cdn-path-4"

		timeout  = time.Second * 20
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	cdnTestCases := []CDNTestEntry{
		{
			Paths:           []string{"/apps/inventory/"},
			ExpectedPath:    "/apps/inventory/",
			FrontendName:    FrontendName,
			Namespace:       FrontendNamespace,
			CaseDescription: "Should move the path to the config map with no modifications",
		},
		{
			Paths:           []string{"apps/inventory"},
			ExpectedPath:    "/apps/inventory/",
			FrontendName:    FrontendName2,
			Namespace:       FrontendNamespace,
			CaseDescription: "Should move the path to the config map and add leading and trailing slashes",
		},
		{
			Paths:           []string{"/apps/inventory/", "/foo/bar"},
			ExpectedPath:    "/apps/inventory/",
			FrontendName:    FrontendName3,
			Namespace:       FrontendNamespace,
			CaseDescription: "Should move the first path to the config map",
		},
		{
			Paths:           []string{},
			ExpectedPath:    "",
			FrontendName:    FrontendName4,
			Namespace:       FrontendNamespace,
			CaseDescription: "Should not move the path to the config map",
		},
	}

	for _, cdnTestCase := range cdnTestCases {
		ginkgo.It(cdnTestCase.CaseDescription, func() {
			ctx := context.Background()

			frontendEnvironment := crd.FrontendEnvironment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "FrontendEnvironment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      cdnTestCase.FrontendName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendEnvironmentSpec{
					SSO:      "https://something-auth",
					Hostname: "something",
					Whitelist: []string{
						"192.168.0.0/24",
						"10.10.0.0/24",
					},
					Monitoring: &crd.MonitoringConfig{
						Mode: "local",
					},
					GenerateNavJSON: false,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, &frontendEnvironment)).Should(gomega.Succeed())

			frontend := frontendFromCDN(cdnTestCase)

			gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())

			configMapLookupKey := types.NamespacedName{Name: frontendEnvironment.Name, Namespace: cdnTestCase.Namespace}

			createdConfigMap := &v1.ConfigMap{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())

			fedModules := createdConfigMap.Data["fed-modules.json"]

			// unmarshal the fed-modules.json to check the CDN path
			var fedModulesMap map[string]crd.FedModule
			err := json.Unmarshal([]byte(fedModules), &fedModulesMap)
			gomega.Expect(err).Should(gomega.BeNil())
			fn := stringy.New(cdnTestCase.FrontendName).CamelCase().Get()
			gomega.Expect(fedModulesMap[fn].CDNPath).Should(gomega.Equal(cdnTestCase.ExpectedPath))
		})
	}
})

var _ = ginkgo.Describe("APIInfo Schema Validation", func() {
	const (
		SchemaTestFrontendName      = "schema-test-frontend"
		SchemaTestFrontendNamespace = "default"
		SchemaTestEnvName           = "schema-test-env"

		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	ginkgo.Context("When using the new Specs field", func() {
		ginkgo.It("Should correctly populate the Specs field with single spec", func() {
			ginkgo.By("Creating a Frontend with single API spec")
			ctx := context.Background()

			frontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1alpha1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      SchemaTestFrontendName,
					Namespace: SchemaTestFrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName: SchemaTestEnvName,
					API: &crd.APIInfo{
						Versions: []string{"v1"},
						Specs: []crd.APISpecInfo{
							{
								URL:          "https://console.redhat.com/api/test/v1/openapi.json",
								BundleLabels: []string{"insights"},
								FrontendName: "test-service-deployment",
							},
						},
					},
					Frontend: crd.FrontendInfo{
						Paths: []string{"/test/single-spec"},
					},
					Image: "test-image:latest",
				},
			}

			gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())

			ginkgo.By("Verifying the API spec is correctly stored")
			frontendLookupKey := types.NamespacedName{Name: SchemaTestFrontendName, Namespace: SchemaTestFrontendNamespace}
			createdFrontend := &crd.Frontend{}

			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, frontendLookupKey, createdFrontend)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())

			gomega.Expect(createdFrontend.Spec.API).ShouldNot(gomega.BeNil())
			gomega.Expect(createdFrontend.Spec.API.Specs).Should(gomega.HaveLen(1))
			gomega.Expect(createdFrontend.Spec.API.Specs[0].URL).Should(gomega.Equal("https://console.redhat.com/api/test/v1/openapi.json"))
			gomega.Expect(createdFrontend.Spec.API.Specs[0].BundleLabels).Should(gomega.Equal([]string{"insights"}))
			gomega.Expect(createdFrontend.Spec.API.Specs[0].FrontendName).Should(gomega.Equal("test-service-deployment"))

			ginkgo.By("Cleaning up")
			gomega.Expect(k8sClient.Delete(ctx, frontend)).Should(gomega.Succeed())
		})

		ginkgo.It("Should handle multiple API specs in the Specs array", func() {
			ginkgo.By("Creating a Frontend with multiple API specs")
			ctx := context.Background()

			multiSpecFrontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1alpha1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      SchemaTestFrontendName + "-multi",
					Namespace: SchemaTestFrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName: SchemaTestEnvName,
					API: &crd.APIInfo{
						Versions: []string{"v1", "v2"},
						Specs: []crd.APISpecInfo{
							{
								URL:          "https://console.redhat.com/api/inventory/v1/openapi.json",
								BundleLabels: []string{"insights"},
								FrontendName: "inventory-service",
							},
							{
								URL:          "https://console.redhat.com/api/compliance/v1/openapi.json",
								BundleLabels: []string{"insights", "compliance"},
								FrontendName: "compliance-service",
							},
							{
								URL:          "https://console.redhat.com/api/automation/v1/openapi.json",
								BundleLabels: []string{"ansible"},
								FrontendName: "automation-service",
							},
						},
					},
					Frontend: crd.FrontendInfo{
						Paths: []string{"/test/multi-spec"},
					},
					Image: "test-image:latest",
				},
			}

			gomega.Expect(k8sClient.Create(ctx, multiSpecFrontend)).Should(gomega.Succeed())

			ginkgo.By("Verifying all API specs are correctly stored")
			frontendLookupKey := types.NamespacedName{Name: SchemaTestFrontendName + "-multi", Namespace: SchemaTestFrontendNamespace}
			createdFrontend := &crd.Frontend{}

			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, frontendLookupKey, createdFrontend)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())

			gomega.Expect(createdFrontend.Spec.API).ShouldNot(gomega.BeNil())
			gomega.Expect(createdFrontend.Spec.API.Versions).Should(gomega.Equal([]string{"v1", "v2"}))
			gomega.Expect(createdFrontend.Spec.API.Specs).Should(gomega.HaveLen(3))

			// Verify first spec
			gomega.Expect(createdFrontend.Spec.API.Specs[0].URL).Should(gomega.Equal("https://console.redhat.com/api/inventory/v1/openapi.json"))
			gomega.Expect(createdFrontend.Spec.API.Specs[0].BundleLabels).Should(gomega.Equal([]string{"insights"}))
			gomega.Expect(createdFrontend.Spec.API.Specs[0].FrontendName).Should(gomega.Equal("inventory-service"))

			// Verify second spec
			gomega.Expect(createdFrontend.Spec.API.Specs[1].URL).Should(gomega.Equal("https://console.redhat.com/api/compliance/v1/openapi.json"))
			gomega.Expect(createdFrontend.Spec.API.Specs[1].BundleLabels).Should(gomega.Equal([]string{"insights", "compliance"}))
			gomega.Expect(createdFrontend.Spec.API.Specs[1].FrontendName).Should(gomega.Equal("compliance-service"))

			// Verify third spec
			gomega.Expect(createdFrontend.Spec.API.Specs[2].URL).Should(gomega.Equal("https://console.redhat.com/api/automation/v1/openapi.json"))
			gomega.Expect(createdFrontend.Spec.API.Specs[2].BundleLabels).Should(gomega.Equal([]string{"ansible"}))
			gomega.Expect(createdFrontend.Spec.API.Specs[2].FrontendName).Should(gomega.Equal("automation-service"))

			ginkgo.By("Cleaning up")
			gomega.Expect(k8sClient.Delete(ctx, multiSpecFrontend)).Should(gomega.Succeed())
		})

		ginkgo.It("Should handle empty Specs array", func() {
			ginkgo.By("Creating a Frontend with empty API specs")
			ctx := context.Background()

			emptySpecFrontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1alpha1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      SchemaTestFrontendName + "-empty",
					Namespace: SchemaTestFrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName: SchemaTestEnvName,
					API: &crd.APIInfo{
						Versions: []string{"v1"},
						Specs:    []crd.APISpecInfo{}, // Empty specs array
					},
					Frontend: crd.FrontendInfo{
						Paths: []string{"/test/empty-spec"},
					},
					Image: "test-image:latest",
				},
			}

			gomega.Expect(k8sClient.Create(ctx, emptySpecFrontend)).Should(gomega.Succeed())

			ginkgo.By("Verifying the empty API specs array is handled correctly")
			frontendLookupKey := types.NamespacedName{Name: SchemaTestFrontendName + "-empty", Namespace: SchemaTestFrontendNamespace}
			createdFrontend := &crd.Frontend{}

			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, frontendLookupKey, createdFrontend)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())

			gomega.Expect(createdFrontend.Spec.API).ShouldNot(gomega.BeNil())
			gomega.Expect(createdFrontend.Spec.API.Specs).Should(gomega.HaveLen(0))

			ginkgo.By("Cleaning up")
			gomega.Expect(k8sClient.Delete(ctx, emptySpecFrontend)).Should(gomega.Succeed())
		})

		ginkgo.It("Should handle nil API field gracefully", func() {
			ginkgo.By("Creating a Frontend with nil API field")
			ctx := context.Background()

			nilAPIFrontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1alpha1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      SchemaTestFrontendName + "-nil-api",
					Namespace: SchemaTestFrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName: SchemaTestEnvName,
					API:     nil, // Nil API field
					Frontend: crd.FrontendInfo{
						Paths: []string{"/test/nil-api"},
					},
					Image: "test-image:latest",
				},
			}

			gomega.Expect(k8sClient.Create(ctx, nilAPIFrontend)).Should(gomega.Succeed())

			ginkgo.By("Verifying the nil API field is handled correctly")
			frontendLookupKey := types.NamespacedName{Name: SchemaTestFrontendName + "-nil-api", Namespace: SchemaTestFrontendNamespace}
			createdFrontend := &crd.Frontend{}

			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, frontendLookupKey, createdFrontend)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())

			gomega.Expect(createdFrontend.Spec.API).Should(gomega.BeNil())

			ginkgo.By("Cleaning up")
			gomega.Expect(k8sClient.Delete(ctx, nilAPIFrontend)).Should(gomega.Succeed())
		})
	})

	ginkgo.Context("When validating APISpecInfo fields", func() {
		ginkgo.It("Should properly validate all APISpecInfo fields", func() {
			ginkgo.By("Creating a Frontend with comprehensive APISpecInfo")
			ctx := context.Background()

			validationFrontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1alpha1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      SchemaTestFrontendName + "-validation",
					Namespace: SchemaTestFrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName: SchemaTestEnvName,
					API: &crd.APIInfo{
						Versions: []string{"v1", "v2", "v3"},
						Specs: []crd.APISpecInfo{
							{
								URL:          "https://console.redhat.com/api/detailed-test/v1/openapi.json",
								BundleLabels: []string{"insights", "testing", "validation"},
								FrontendName: "detailed-test-service-deployment-12345",
							},
						},
					},
					Frontend: crd.FrontendInfo{
						Paths: []string{"/test/validation"},
					},
					Image: "test-image:validation",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, validationFrontend)).Should(gomega.Succeed())

			ginkgo.By("Verifying all APISpecInfo fields are correctly preserved")
			frontendLookupKey := types.NamespacedName{Name: SchemaTestFrontendName + "-validation", Namespace: SchemaTestFrontendNamespace}
			createdFrontend := &crd.Frontend{}

			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, frontendLookupKey, createdFrontend)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())

			gomega.Expect(createdFrontend.Spec.API).ShouldNot(gomega.BeNil())
			gomega.Expect(createdFrontend.Spec.API.Versions).Should(gomega.Equal([]string{"v1", "v2", "v3"}))
			gomega.Expect(createdFrontend.Spec.API.Specs).Should(gomega.HaveLen(1))
			spec := createdFrontend.Spec.API.Specs[0]
			gomega.Expect(spec.URL).Should(gomega.Equal("https://console.redhat.com/api/detailed-test/v1/openapi.json"))
			gomega.Expect(spec.BundleLabels).Should(gomega.HaveLen(3))
			gomega.Expect(spec.BundleLabels).Should(gomega.ContainElements("insights", "testing", "validation"))
			gomega.Expect(spec.FrontendName).Should(gomega.Equal("detailed-test-service-deployment-12345"))

			ginkgo.By("Cleaning up")
			gomega.Expect(k8sClient.Delete(ctx, validationFrontend)).Should(gomega.Succeed())
		})

		ginkgo.It("Should allow API with empty specs array", func() {
			ginkgo.By("Creating a Frontend with versions and empty specs should succeed")
			ctx := context.Background()

			validFrontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1alpha1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      SchemaTestFrontendName + "-empty-specs",
					Namespace: SchemaTestFrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName: SchemaTestEnvName,
					API: &crd.APIInfo{
						Versions: []string{"v1"},
						Specs:    []crd.APISpecInfo{}, // Empty specs array
					},
					Frontend: crd.FrontendInfo{
						Paths: []string{"/test/empty-specs"},
					},
					Image: "test-image:latest",
				},
			}

			ginkgo.By("API validation is handled by Kubernetes CRD validation")

			gomega.Expect(k8sClient.Create(ctx, validFrontend)).Should(gomega.Succeed())
		})
	})
})

var _ = ginkgo.Describe("DisableContainerDeployments", func() {
	const (
		FrontendName      = "test-frontend-disable"
		FrontendNamespace = "default"
		FrontendEnvName   = "test-env-disable"
		BundleName        = "test-bundle-disable"

		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	ginkgo.Context("When DisableContainerDeployments is true from the start", func() {
		ginkgo.It("Should create ConfigMap and Ingress but not Deployment or Service", func() {
			ctx := context.Background()

			frontendEnvironment := &crd.FrontendEnvironment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "FrontendEnvironment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendEnvironmentSpec{
					SSO:                         "https://something-auth",
					Hostname:                    "something",
					GenerateNavJSON:             true,
					DisableContainerDeployments: true,
					Monitoring: &crd.MonitoringConfig{
						Mode: "app-interface",
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())

			frontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FrontendName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName: FrontendEnvName,
					Title:   "Disable Test",
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/disable-test"},
					},
					Image:   "my-image:version",
					Service: "external-disable-service",
					Module: &crd.FedModule{
						ManifestLocation: "/apps/disable-test/fed-mods.json",
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/things/disable-test",
							}},
						}},
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())

			bundle := &crd.Bundle{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Bundle",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      BundleName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.BundleSpec{
					ID:      BundleName,
					Title:   "",
					AppList: []string{FrontendName},
					EnvName: FrontendEnvName,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, bundle)).Should(gomega.Succeed())

			ginkgo.By("Verifying Ingress is created")
			ingressLookupKey := types.NamespacedName{Name: frontend.Name, Namespace: FrontendNamespace}
			createdIngress := &networking.Ingress{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, ingressLookupKey, createdIngress)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())
			gomega.Expect(createdIngress.Name).Should(gomega.Equal(FrontendName))

			ginkgo.By("Verifying Ingress backend points to Spec.Service, not operator-managed service")
			gomega.Expect(createdIngress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).Should(gomega.Equal("external-disable-service"))

			ginkgo.By("Verifying ConfigMap is created")
			configMapLookupKey := types.NamespacedName{Name: frontendEnvironment.Name, Namespace: FrontendNamespace}
			createdConfigMap := &v1.ConfigMap{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying Deployment is NOT created")
			deploymentLookupKey := types.NamespacedName{Name: frontend.Name + "-frontend", Namespace: FrontendNamespace}
			gomega.Consistently(func() bool {
				err := k8sClient.Get(ctx, deploymentLookupKey, &apps.Deployment{})
				return k8serrors.IsNotFound(err)
			}, time.Second*3, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying Service is NOT created")
			serviceLookupKey := types.NamespacedName{Name: frontend.Name, Namespace: FrontendNamespace}
			gomega.Consistently(func() bool {
				err := k8sClient.Get(ctx, serviceLookupKey, &v1.Service{})
				return k8serrors.IsNotFound(err)
			}, time.Second*3, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying ServiceMonitor is NOT created when container deployments disabled")
			monitorLookupKey := types.NamespacedName{Name: frontend.Name, Namespace: "openshift-customer-monitoring"}
			gomega.Consistently(func() bool {
				err := k8sClient.Get(ctx, monitorLookupKey, &prom.ServiceMonitor{})
				return k8serrors.IsNotFound(err)
			}, time.Second*3, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying Frontend status shows successful reconciliation")
			updatedFrontend := &crd.Frontend{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: FrontendName, Namespace: FrontendNamespace}, updatedFrontend)
				if err != nil {
					return false
				}
				for _, c := range updatedFrontend.Status.Conditions {
					if c.Type == "ReconciliationSuccessful" && c.Status == "True" {
						return true
					}
				}
				return false
			}, timeout, interval).Should(gomega.BeTrue())
		})
	})

	ginkgo.Context("When DisableContainerDeployments is toggled true after initial deployment", func() {
		ginkgo.It("Should clean up existing Deployment and Service", func() {
			ctx := context.Background()

			const (
				ToggleFrontendName = "test-frontend-toggle"
				ToggleEnvName      = "test-env-toggle"
				ToggleBundleName   = "test-bundle-toggle"
			)

			frontendEnvironment := &crd.FrontendEnvironment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "FrontendEnvironment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      ToggleEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendEnvironmentSpec{
					SSO:             "https://something-auth",
					Hostname:        "something",
					GenerateNavJSON: true,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())

			frontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      ToggleFrontendName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName: ToggleEnvName,
					Title:   "Toggle Test",
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/toggle-test"},
					},
					Image:   "my-image:version",
					Service: "external-toggle-service",
					Module: &crd.FedModule{
						ManifestLocation: "/apps/toggle-test/fed-mods.json",
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/things/toggle-test",
							}},
						}},
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())

			bundle := &crd.Bundle{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Bundle",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      ToggleBundleName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.BundleSpec{
					ID:      ToggleBundleName,
					Title:   "",
					AppList: []string{ToggleFrontendName},
					EnvName: ToggleEnvName,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, bundle)).Should(gomega.Succeed())

			ginkgo.By("Verifying Deployment exists initially")
			deploymentLookupKey := types.NamespacedName{Name: ToggleFrontendName + "-frontend", Namespace: FrontendNamespace}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, deploymentLookupKey, &apps.Deployment{})
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying Service exists initially")
			serviceLookupKey := types.NamespacedName{Name: ToggleFrontendName, Namespace: FrontendNamespace}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, serviceLookupKey, &v1.Service{})
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())

			ginkgo.By("Toggling DisableContainerDeployments to true")
			updatedEnv := &crd.FrontendEnvironment{}
			gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ToggleEnvName}, updatedEnv)).Should(gomega.Succeed())
			updatedEnv.Spec.DisableContainerDeployments = true
			gomega.Expect(k8sClient.Update(ctx, updatedEnv)).Should(gomega.Succeed())

			ginkgo.By("Verifying Deployment is cleaned up")
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, deploymentLookupKey, &apps.Deployment{})
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying Service is cleaned up")
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, serviceLookupKey, &v1.Service{})
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying Ingress still exists and backend switches to Spec.Service")
			ingressLookupKey := types.NamespacedName{Name: ToggleFrontendName, Namespace: FrontendNamespace}
			createdIngress := &networking.Ingress{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, ingressLookupKey, createdIngress)
				if err != nil {
					return false
				}
				return createdIngress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name == "external-toggle-service"
			}, timeout, interval).Should(gomega.BeTrue())

			ginkgo.By("Re-enabling container deployments")
			gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ToggleEnvName}, updatedEnv)).Should(gomega.Succeed())
			updatedEnv.Spec.DisableContainerDeployments = false
			gomega.Expect(k8sClient.Update(ctx, updatedEnv)).Should(gomega.Succeed())

			ginkgo.By("Verifying Deployment is recreated")
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, deploymentLookupKey, &apps.Deployment{})
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying Service is recreated")
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, serviceLookupKey, &v1.Service{})
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying Ingress backend switches back to operator-managed Service")
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, ingressLookupKey, createdIngress)
				if err != nil {
					return false
				}
				return createdIngress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name == ToggleFrontendName
			}, timeout, interval).Should(gomega.BeTrue())
		})
	})

	ginkgo.Context("When DisableContainerDeployments is true with cache bust and push cache enabled", func() {
		ginkgo.It("Should not create Jobs, Deployment, or Service", func() {
			ctx := context.Background()

			const (
				JobsFrontendName = "test-frontend-jobs"
				JobsEnvName      = "test-env-jobs"
				JobsBundleName   = "test-bundle-jobs"
			)

			frontendEnvironment := &crd.FrontendEnvironment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "FrontendEnvironment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      JobsEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendEnvironmentSpec{
					SSO:                         "https://something-auth",
					Hostname:                    "something",
					GenerateNavJSON:             true,
					DisableContainerDeployments: true,
					EnableAkamaiCacheBust:       true,
					AkamaiCacheBustImage:        "quay.io/cachebust:latest",
					EnablePushCache:             true,
					ValpopImage:                 "quay.io/valpop:latest",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())

			frontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      JobsFrontendName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName: JobsEnvName,
					Title:   "Jobs Test",
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/jobs-test"},
					},
					Image:   "my-image:version",
					Service: "external-jobs-service",
					Module: &crd.FedModule{
						ManifestLocation: "/apps/jobs-test/fed-mods.json",
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/things/jobs-test",
							}},
						}},
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())

			bundle := &crd.Bundle{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Bundle",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      JobsBundleName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.BundleSpec{
					ID:      JobsBundleName,
					Title:   "",
					AppList: []string{JobsFrontendName},
					EnvName: JobsEnvName,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, bundle)).Should(gomega.Succeed())

			ginkgo.By("Waiting for reconciliation to succeed")
			updatedFrontend := &crd.Frontend{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: JobsFrontendName, Namespace: FrontendNamespace}, updatedFrontend)
				if err != nil {
					return false
				}
				for _, c := range updatedFrontend.Status.Conditions {
					if c.Type == "ReconciliationSuccessful" && c.Status == "True" {
						return true
					}
				}
				return false
			}, timeout, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying cache bust Job is NOT created")
			cacheBustJobKey := types.NamespacedName{Name: JobsFrontendName + "-frontend-cachebust", Namespace: FrontendNamespace}
			gomega.Consistently(func() bool {
				err := k8sClient.Get(ctx, cacheBustJobKey, &batchv1.Job{})
				return k8serrors.IsNotFound(err)
			}, time.Second*3, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying push cache Job is NOT created")
			pushCacheJobKey := types.NamespacedName{Name: JobsFrontendName + "-frontend-pushcache", Namespace: FrontendNamespace}
			gomega.Consistently(func() bool {
				err := k8sClient.Get(ctx, pushCacheJobKey, &batchv1.Job{})
				return k8serrors.IsNotFound(err)
			}, time.Second*3, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying Deployment is NOT created")
			deploymentKey := types.NamespacedName{Name: JobsFrontendName + "-frontend", Namespace: FrontendNamespace}
			gomega.Consistently(func() bool {
				err := k8sClient.Get(ctx, deploymentKey, &apps.Deployment{})
				return k8serrors.IsNotFound(err)
			}, time.Second*3, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying Service is NOT created")
			serviceKey := types.NamespacedName{Name: JobsFrontendName, Namespace: FrontendNamespace}
			gomega.Consistently(func() bool {
				err := k8sClient.Get(ctx, serviceKey, &v1.Service{})
				return k8serrors.IsNotFound(err)
			}, time.Second*3, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying Ingress IS created with backend pointing at Spec.Service")
			ingressKey := types.NamespacedName{Name: JobsFrontendName, Namespace: FrontendNamespace}
			createdIngress := &networking.Ingress{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, ingressKey, createdIngress)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())
			gomega.Expect(createdIngress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).Should(gomega.Equal("external-jobs-service"))
		})
	})

	ginkgo.Context("When toggling DisableContainerDeployments with cache bust and push cache enabled", func() {
		ginkgo.It("Should clean up Jobs when disabled and recreate when re-enabled", func() {
			ctx := context.Background()

			const (
				ToggleJobsFrontendName = "test-frontend-toggle-jobs"
				ToggleJobsEnvName      = "test-env-toggle-jobs"
				ToggleJobsBundleName   = "test-bundle-toggle-jobs"
			)

			os.Setenv("PUSHCACHE_AWS_ACCESS_KEY_ID", "test-access-key")
			os.Setenv("PUSHCACHE_AWS_SECRET_ACCESS_KEY", "test-secret-key")
			os.Setenv("PUSHCACHE_AWS_REGION", "us-east-1")
			os.Setenv("PUSHCACHE_AWS_ENDPOINT", "minio-service.minio-env.svc.cluster.local")
			os.Setenv("PUSHCACHE_AWS_PORT", "9000")
			os.Setenv("PUSHCACHE_AWS_BUCKET_NAME", "frontend")
			defer func() {
				os.Unsetenv("PUSHCACHE_AWS_ACCESS_KEY_ID")
				os.Unsetenv("PUSHCACHE_AWS_SECRET_ACCESS_KEY")
				os.Unsetenv("PUSHCACHE_AWS_REGION")
				os.Unsetenv("PUSHCACHE_AWS_ENDPOINT")
				os.Unsetenv("PUSHCACHE_AWS_PORT")
				os.Unsetenv("PUSHCACHE_AWS_BUCKET_NAME")
			}()

			akamaiSecret := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "akamai",
					Namespace: FrontendNamespace,
				},
				Data: map[string][]byte{
					"host":          []byte("test.purge.akamai.net"),
					"access_token":  []byte("test-access-token"),
					"client_token":  []byte("test-client-token"),
					"client_secret": []byte("test-client-secret"),
				},
			}
			err := k8sClient.Create(ctx, akamaiSecret)
			if err != nil && !k8serrors.IsAlreadyExists(err) {
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
			}

			frontendEnvironment := &crd.FrontendEnvironment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "FrontendEnvironment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      ToggleJobsEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendEnvironmentSpec{
					SSO:                   "https://something-auth",
					Hostname:              "something",
					GenerateNavJSON:       true,
					EnableAkamaiCacheBust: true,
					AkamaiCacheBustImage:  "quay.io/cachebust:latest",
					EnablePushCache:       true,
					ValpopImage:           "quay.io/valpop:latest",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())

			frontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      ToggleJobsFrontendName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName: ToggleJobsEnvName,
					Title:   "Toggle Jobs Test",
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/toggle-jobs-test"},
					},
					Image:   "my-image:version",
					Service: "external-toggle-jobs-service",
					Module: &crd.FedModule{
						ManifestLocation: "/apps/toggle-jobs-test/fed-mods.json",
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/things/toggle-jobs-test",
							}},
						}},
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())

			bundle := &crd.Bundle{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Bundle",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      ToggleJobsBundleName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.BundleSpec{
					ID:      ToggleJobsBundleName,
					Title:   "",
					AppList: []string{ToggleJobsFrontendName},
					EnvName: ToggleJobsEnvName,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, bundle)).Should(gomega.Succeed())

			ginkgo.By("Verifying cache bust and push cache Jobs exist initially")
			cacheBustJobKey := types.NamespacedName{Name: ToggleJobsFrontendName + "-frontend-cachebust", Namespace: FrontendNamespace}
			pushCacheJobKey := types.NamespacedName{Name: ToggleJobsFrontendName + "-frontend-pushcache", Namespace: FrontendNamespace}
			jobKeys := []types.NamespacedName{cacheBustJobKey, pushCacheJobKey}
			const testJobFinalizer = "frontend-operator.test/hold-deletion"
			originalJobUIDs := map[types.NamespacedName]types.UID{}
			for _, key := range jobKeys {
				gomega.Eventually(func() error {
					j := &batchv1.Job{}
					if err := k8sClient.Get(ctx, key, j); err != nil {
						return err
					}
					originalJobUIDs[key] = j.UID
					if !slices.Contains(j.Finalizers, testJobFinalizer) {
						j.Finalizers = append(j.Finalizers, testJobFinalizer)
						return k8sClient.Update(ctx, j)
					}
					return nil
				}, timeout, interval).Should(gomega.Succeed())
			}

			ginkgo.By("Toggling DisableContainerDeployments to true")
			updatedEnv := &crd.FrontendEnvironment{}
			gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ToggleJobsEnvName}, updatedEnv)).Should(gomega.Succeed())
			updatedEnv.Spec.DisableContainerDeployments = true
			gomega.Expect(k8sClient.Update(ctx, updatedEnv)).Should(gomega.Succeed())

			ginkgo.By("Verifying both Jobs remain terminating behind the test finalizer")
			for _, key := range jobKeys {
				gomega.Eventually(func() bool {
					j := &batchv1.Job{}
					err := k8sClient.Get(ctx, key, j)
					return err == nil && !j.GetDeletionTimestamp().IsZero() && slices.Contains(j.Finalizers, testJobFinalizer)
				}, timeout, interval).Should(gomega.BeTrue())
			}

			// Re-enable WHILE the Jobs are still terminating. This exercises the
			// terminating-Job guard in manageExistingJob: the reconcile must not
			// treat the terminating Job as up-to-date (which would succeed-and-skip
			// and never recreate); it should error and requeue until the Job is gone.
			ginkgo.By("Re-enabling container deployments while Jobs are terminating")
			gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ToggleJobsEnvName}, updatedEnv)).Should(gomega.Succeed())
			updatedEnv.Spec.DisableContainerDeployments = false
			gomega.Expect(k8sClient.Update(ctx, updatedEnv)).Should(gomega.Succeed())

			ginkgo.By("Verifying the terminating-Job guard fails reconciliation before deletion completes")
			gomega.Eventually(func() bool {
				updatedFrontend := &crd.Frontend{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: ToggleJobsFrontendName, Namespace: FrontendNamespace}, updatedFrontend); err != nil {
					return false
				}
				for _, condition := range updatedFrontend.Status.Conditions {
					if condition.Type == crd.ReconciliationFailed && condition.Status == metav1.ConditionTrue {
						return strings.Contains(condition.Message, "is terminating, will retry")
					}
				}
				return false
			}, timeout, interval).Should(gomega.BeTrue())

			ginkgo.By("Simulating garbage collection so deletion can complete")
			for _, key := range jobKeys {
				gomega.Eventually(func() error {
					j := &batchv1.Job{}
					if err := k8sClient.Get(ctx, key, j); err != nil {
						return err
					}
					// envtest has no Job controller or garbage collector. Clear the
					// finalizers and repeat the delete request to simulate their work.
					j.Finalizers = nil
					if err := k8sClient.Update(ctx, j); err != nil {
						return err
					}
					if err := k8sClient.Delete(ctx, j); err != nil && !k8serrors.IsNotFound(err) {
						return err
					}
					return nil
				}, timeout, interval).Should(gomega.Succeed())
			}

			ginkgo.By("Verifying cache bust Job is recreated")
			gomega.Eventually(func() bool {
				j := &batchv1.Job{}
				err := k8sClient.Get(ctx, cacheBustJobKey, j)
				return err == nil && j.DeletionTimestamp == nil && j.UID != originalJobUIDs[cacheBustJobKey]
			}, timeout, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying push cache Job is recreated")
			gomega.Eventually(func() bool {
				j := &batchv1.Job{}
				err := k8sClient.Get(ctx, pushCacheJobKey, j)
				return err == nil && j.DeletionTimestamp == nil && j.UID != originalJobUIDs[pushCacheJobKey]
			}, timeout, interval).Should(gomega.BeTrue())
		})
	})

	ginkgo.Context("When toggling DisableContainerDeployments with image but no Spec.Service", func() {
		ginkgo.It("Should delete the existing Ingress when no backing Service remains", func() {
			ctx := context.Background()

			const (
				FallbackFrontendName = "test-frontend-fallback"
				FallbackEnvName      = "test-env-fallback"
				FallbackBundleName   = "test-bundle-fallback"
			)

			frontendEnvironment := &crd.FrontendEnvironment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "FrontendEnvironment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FallbackEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendEnvironmentSpec{
					SSO:             "https://something-auth",
					Hostname:        "something",
					GenerateNavJSON: true,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())

			frontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FallbackFrontendName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName: FallbackEnvName,
					Title:   "Fallback Test",
					Image:   "my-image:version",
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/fallback-test"},
					},
					Module: &crd.FedModule{
						ManifestLocation: "/apps/fallback-test/fed-mods.json",
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/things/fallback-test",
							}},
						}},
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())

			bundle := &crd.Bundle{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Bundle",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FallbackBundleName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.BundleSpec{
					ID:      FallbackBundleName,
					Title:   "",
					AppList: []string{FallbackFrontendName},
					EnvName: FallbackEnvName,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, bundle)).Should(gomega.Succeed())

			ingressKey := types.NamespacedName{Name: FallbackFrontendName, Namespace: FrontendNamespace}
			ginkgo.By("Verifying the initial Ingress points to the operator-managed Service")
			gomega.Eventually(func() bool {
				createdIngress := &networking.Ingress{}
				if err := k8sClient.Get(ctx, ingressKey, createdIngress); err != nil {
					return false
				}
				return createdIngress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name == FallbackFrontendName
			}, timeout, interval).Should(gomega.BeTrue())

			ginkgo.By("Disabling container deployments without providing Spec.Service")
			updatedEnv := &crd.FrontendEnvironment{}
			gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: FallbackEnvName}, updatedEnv)).Should(gomega.Succeed())
			updatedEnv.Spec.DisableContainerDeployments = true
			gomega.Expect(k8sClient.Update(ctx, updatedEnv)).Should(gomega.Succeed())

			ginkgo.By("Verifying the Ingress is deleted with its backing Service")
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, ingressKey, &networking.Ingress{})
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(gomega.BeTrue())
		})
	})

	ginkgo.Context("When DisableContainerDeployments is true with no image set", func() {
		ginkgo.It("Should reconcile successfully without creating container resources", func() {
			ctx := context.Background()

			const (
				NoImgFrontendName = "test-frontend-noimg"
				NoImgEnvName      = "test-env-noimg"
				NoImgBundleName   = "test-bundle-noimg"
			)

			frontendEnvironment := &crd.FrontendEnvironment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "FrontendEnvironment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      NoImgEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendEnvironmentSpec{
					SSO:                         "https://something-auth",
					Hostname:                    "something",
					GenerateNavJSON:             true,
					DisableContainerDeployments: true,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())

			frontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      NoImgFrontendName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName: NoImgEnvName,
					Title:   "No Image Test",
					Service: "external-noimg-service",
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/noimg-test"},
					},
					Module: &crd.FedModule{
						ManifestLocation: "/apps/noimg-test/fed-mods.json",
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/things/noimg-test",
							}},
						}},
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())

			bundle := &crd.Bundle{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Bundle",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      NoImgBundleName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.BundleSpec{
					ID:      NoImgBundleName,
					Title:   "",
					AppList: []string{NoImgFrontendName},
					EnvName: NoImgEnvName,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, bundle)).Should(gomega.Succeed())

			ginkgo.By("Verifying reconciliation succeeds")
			updatedFrontend := &crd.Frontend{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: NoImgFrontendName, Namespace: FrontendNamespace}, updatedFrontend)
				if err != nil {
					return false
				}
				for _, c := range updatedFrontend.Status.Conditions {
					if c.Type == "ReconciliationSuccessful" && c.Status == "True" {
						return true
					}
				}
				return false
			}, timeout, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying no Deployment exists")
			gomega.Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: NoImgFrontendName + "-frontend", Namespace: FrontendNamespace}, &apps.Deployment{})
				return k8serrors.IsNotFound(err)
			}, time.Second*3, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying Ingress is created with backend pointing at Spec.Service")
			createdIngress := &networking.Ingress{}
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: NoImgFrontendName, Namespace: FrontendNamespace}, createdIngress)
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())
			gomega.Expect(createdIngress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).Should(gomega.Equal("external-noimg-service"))
		})
	})

	ginkgo.Context("When DisableContainerDeployments is toggled with monitoring enabled", func() {
		ginkgo.It("Should clean up existing ServiceMonitor", func() {
			ctx := context.Background()

			const (
				SMToggleFrontendName = "test-frontend-sm-toggle"
				SMToggleEnvName      = "test-env-sm-toggle"
				SMToggleBundleName   = "test-bundle-sm-toggle"
			)

			frontendEnvironment := &crd.FrontendEnvironment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "FrontendEnvironment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      SMToggleEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendEnvironmentSpec{
					SSO:             "https://something-auth",
					Hostname:        "something",
					GenerateNavJSON: true,
					Monitoring: &crd.MonitoringConfig{
						Mode: "app-interface",
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())

			frontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      SMToggleFrontendName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName: SMToggleEnvName,
					Title:   "SM Toggle Test",
					Image:   "my-image:version",
					Service: "external-sm-toggle-service",
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/sm-toggle-test"},
					},
					Module: &crd.FedModule{
						ManifestLocation: "/apps/sm-toggle-test/fed-mods.json",
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/things/sm-toggle-test",
							}},
						}},
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())

			bundle := &crd.Bundle{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Bundle",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      SMToggleBundleName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.BundleSpec{
					ID:      SMToggleBundleName,
					Title:   "",
					AppList: []string{SMToggleFrontendName},
					EnvName: SMToggleEnvName,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, bundle)).Should(gomega.Succeed())

			monitorLookupKey := types.NamespacedName{Name: SMToggleFrontendName, Namespace: MonitoringNamespace}
			ginkgo.By("Verifying ServiceMonitor exists initially")
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, monitorLookupKey, &prom.ServiceMonitor{})
				return err == nil
			}, timeout, interval).Should(gomega.BeTrue())

			ginkgo.By("Toggling DisableContainerDeployments to true")
			updatedEnv := &crd.FrontendEnvironment{}
			gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: SMToggleEnvName}, updatedEnv)).Should(gomega.Succeed())
			updatedEnv.Spec.DisableContainerDeployments = true
			gomega.Expect(k8sClient.Update(ctx, updatedEnv)).Should(gomega.Succeed())

			ginkgo.By("Verifying ServiceMonitor is cleaned up")
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, monitorLookupKey, &prom.ServiceMonitor{})
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(gomega.BeTrue())
		})
	})

	ginkgo.Context("When Frontend.Spec.Disabled and DisableContainerDeployments are both true", func() {
		ginkgo.It("Should let Frontend.Spec.Disabled take precedence and skip reconciliation", func() {
			ctx := context.Background()

			const (
				DisabledFrontendName = "test-frontend-disabled-precedence"
				DisabledEnvName      = "test-env-disabled-precedence"
				DisabledBundleName   = "test-bundle-disabled-precedence"
			)

			frontendEnvironment := &crd.FrontendEnvironment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "FrontendEnvironment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      DisabledEnvName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendEnvironmentSpec{
					SSO:                         "https://something-auth",
					Hostname:                    "something",
					GenerateNavJSON:             true,
					DisableContainerDeployments: true,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(gomega.Succeed())

			frontend := &crd.Frontend{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Frontend",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      DisabledFrontendName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.FrontendSpec{
					EnvName:  DisabledEnvName,
					Title:    "Disabled Precedence Test",
					Disabled: true,
					Image:    "my-image:version",
					Service:  "external-disabled-service",
					Frontend: crd.FrontendInfo{
						Paths: []string{"/things/disabled-test"},
					},
					Module: &crd.FedModule{
						ManifestLocation: "/apps/disabled-test/fed-mods.json",
						Modules: []crd.Module{{
							ID:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/things/disabled-test",
							}},
						}},
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())

			bundle := &crd.Bundle{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cloud.redhat.com/v1",
					Kind:       "Bundle",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      DisabledBundleName,
					Namespace: FrontendNamespace,
				},
				Spec: crd.BundleSpec{
					ID:      DisabledBundleName,
					Title:   "",
					AppList: []string{DisabledFrontendName},
					EnvName: DisabledEnvName,
				},
			}
			gomega.Expect(k8sClient.Create(ctx, bundle)).Should(gomega.Succeed())

			// DisableContainerDeployments alone would still create Ingress; Disabled skips all reconcile.
			ginkgo.By("Verifying Ingress is NOT created when Frontend is disabled")
			gomega.Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: DisabledFrontendName, Namespace: FrontendNamespace}, &networking.Ingress{})
				return k8serrors.IsNotFound(err)
			}, time.Second*3, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying Deployment is NOT created")
			gomega.Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: DisabledFrontendName + "-frontend", Namespace: FrontendNamespace}, &apps.Deployment{})
				return k8serrors.IsNotFound(err)
			}, time.Second*3, interval).Should(gomega.BeTrue())

			ginkgo.By("Verifying ReconciliationSuccessful is not set")
			gomega.Consistently(func() bool {
				updatedFrontend := &crd.Frontend{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: DisabledFrontendName, Namespace: FrontendNamespace}, updatedFrontend)
				if err != nil {
					return false
				}
				for _, c := range updatedFrontend.Status.Conditions {
					if c.Type == "ReconciliationSuccessful" && c.Status == "True" {
						return false
					}
				}
				return true
			}, time.Second*3, interval).Should(gomega.BeTrue())
		})
	})
})
