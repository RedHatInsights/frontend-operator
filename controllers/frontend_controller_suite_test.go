package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	prom "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

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
					Name:             "config",
					ReadOnly:         false,
					MountPath:        "/srv/dist/operator-generated",
					SubPath:          "operator-generated",
					MountPropagation: nil,
					SubPathExpr:      "",
				},
				{
					Name:      "caddy",
					MountPath: "/opt/app-root/src/Caddyfile",
					SubPath:   "Caddyfile",
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
				if len(createdConfigMap.Data) != 2 {
					return false
				}
				return true
			}, timeout, interval).Should(gomega.BeTrue())
			gomega.Expect(createdConfigMap.Name).Should(gomega.Equal(FrontendEnvName))
			gomega.Expect(createdConfigMap.Data).Should(gomega.Equal(map[string]string{
				"Caddyfile":        caddyFileTemplate,
				"fed-modules.json": "{\"testFrontend\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"config\":{\"apple\":\"pie\"},\"fullProfile\":true},\"testFrontend2\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"config\":{\"cheese\":\"pasty\"},\"fullProfile\":false}}",
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
				"fed-modules.json": "{\"testFrontendService\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"fullProfile\":false}}",
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
					Name:             "config",
					ReadOnly:         false,
					MountPath:        "/srv/dist/operator-generated",
					SubPath:          "operator-generated",
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
				if len(createdConfigMap.Data) != 2 {
					return false
				}
				return true
			}, timeout, interval).Should(gomega.BeTrue())
			gomega.Expect(createdConfigMap.Name).Should(gomega.Equal(FrontendEnvName))
			gomega.Expect(createdConfigMap.Data).Should(gomega.Equal(map[string]string{
				"Caddyfile":        caddyFileTemplate,
				"fed-modules.json": "{\"chrome\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"config\":{\"apple\":\"pie\",\"ssoUrl\":\"https://something-auth\"},\"fullProfile\":false},\"noConfig\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"fullProfile\":false},\"nonChrome\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"config\":{\"apple\":\"pie\"},\"fullProfile\":false}}",
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
				if len(createdConfigMap.Data) != 2 {
					return false
				}
				return true
			}, timeout, interval).Should(gomega.BeTrue())
			gomega.Expect(createdConfigMap.Name).Should(gomega.Equal(FrontendEnvName))
			gomega.Expect(createdConfigMap.Data).Should(gomega.Equal(map[string]string{
				"Caddyfile":        caddyFileTemplate,
				"fed-modules.json": "{\"testDependencies\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}],\"dependencies\":[\"depstring\"]}],\"fullProfile\":false},\"testNoDependencies\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"fullProfile\":false},\"testOptionalDependencies\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}],\"optionalDependencies\":[\"depstring-op\"]}],\"fullProfile\":false}}",
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
							ID:          "test",
							Href:        "/test/href",
							Title:       "Test",
							Description: "Test description",
						}, {
							ID:          "test2",
							Href:        "/test2/href",
							Title:       "Test2",
							Description: "Test2 description",
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
					if len(createdConfigMap.Data) != 3 {
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
					if len(createdConfigMap.Data) != 3 {
						return false
					}
					return true
				}, timeout, interval).Should(gomega.BeTrue())
				searchIndexMap, ok := createdConfigMap.Data["search-index.json"]
				gomega.Expect(ok).Should(gomega.BeTrue())
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
		})
	})
})

type WidgetFrontendTestEntry struct {
	Widgets      []*crd.WidgetEntry
	FrontendName string
}

type WidgetCase struct {
	WidgetsFrontend        []WidgetFrontendTestEntry
	Namespace              string
	Environment            string
	ExpectedConfigMapEntry []crd.WidgetEntry
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
		DefaultWidgetVariant = crd.WidgetDefaultVariant{
			Width:     1,
			Height:    1,
			MaxHeight: 2,
			MinHeight: 1,
		}
		WidgetDefaults = crd.WidgetDefaults{
			Small:  DefaultWidgetVariant,
			Medium: DefaultWidgetVariant,
			Large:  DefaultWidgetVariant,
			XLarge: DefaultWidgetVariant,
		}
		Widget1 = &crd.WidgetEntry{
			Scope:  "test",
			Module: "./foo",
			Config: crd.WidgetConfig{
				Icon:  "icon",
				Title: "title",
			},
			Defaults: WidgetDefaults,
		}
		Widget2 = &crd.WidgetEntry{
			Scope:  "test",
			Module: "./bar",
			Config: crd.WidgetConfig{
				Icon:  "icon-bar",
				Title: "Bar",
			},
			Defaults: WidgetDefaults,
		}
		Widget3 = &crd.WidgetEntry{
			Scope:  "baz",
			Module: "./default",
			Config: crd.WidgetConfig{
				Icon:  "baz",
				Title: "Baz",
			},
			Defaults: WidgetDefaults,
		}
	)

	ginkgo.It("Should create widget registry", func() {
		ginkgo.By("collection entries from Frontend resources", func() {
			expectedResult := []crd.WidgetEntry{{
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
					Widgets:      []*crd.WidgetEntry{Widget1, Widget2},
					FrontendName: FrontendName,
				}, {
					Widgets:      []*crd.WidgetEntry{Widget3},
					FrontendName: FrontendName2,
				},
				},
				Namespace:              FrontendNamespace,
				Environment:            FrontendEnvName,
				ExpectedConfigMapEntry: expectedResult,
			}}

			for _, widgetCase := range widgetCases {
				ctx := context.Background()
				configMapLookupKey := types.NamespacedName{Name: widgetCase.Environment, Namespace: widgetCase.Namespace}
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
					if len(createdConfigMap.Data) != 3 {
						return false
					}
					return true
				}, timeout, interval).Should(gomega.BeTrue())

				widgetRegistryMap := createdConfigMap.Data["widget-registry.json"]
				var widgetRegistry []crd.WidgetEntry
				err := json.Unmarshal([]byte(widgetRegistryMap), &widgetRegistry)
				gomega.Expect(err).Should(gomega.BeNil())

				gomega.Expect(createdConfigMap.Name).Should(gomega.Equal(widgetCase.Environment))
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
	)

	ginkgo.It("Should create service tiles", func() {
		ginkgo.By("collection entries from Frontend resources", func() {
			expectedResult, err := json.Marshal(ExpectedServiceTiles1)
			gomega.Expect(err).Should(gomega.BeNil())
			serviceTileCases := []ServiceTileCase{{
				Namespace:              FrontendNamespace,
				Environment:            FrontendEnvName,
				ExpectedConfigMapEntry: string(expectedResult),
				ServiceTiles: []*ServiceTileTestEntry{{
					ServiceTiles: []*crd.ServiceTile{ServiceTile1, ServiceTile2, ServiceTile3},
					FrontendName: FrontendName,
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
					if len(createdConfigMap.Data) != 3 {
						return false
					}
					return true
				}, timeout, interval).Should(gomega.BeTrue())

				serviceTileRegistryMap := createdConfigMap.Data["service-tiles.json"]

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
