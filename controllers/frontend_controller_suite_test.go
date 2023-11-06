package controllers

import (
	"context"
	"fmt"
	"time"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"github.com/onsi/ginkgo"
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
					API: crd.APIInfo{
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
					API: crd.APIInfo{
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
			gomega.Expect(createdDeployment.Name).Should(gomega.Equal(FrontendName + "-frontend"))
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
				"fed-modules.json": "{\"testFrontend\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"config\":{\"apple\":\"pie\"},\"fullProfile\":true},\"testFrontend2\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"config\":{\"cheese\":\"pasty\"},\"fullProfile\":false}}",
				"test-env.json":    "{\"id\":\"test-bundle\",\"title\":\"\",\"navginkgo.Items\":[{\"title\":\"Test\",\"href\":\"/test/href\"},{\"title\":\"Test\",\"href\":\"/test/href\"}]}",
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
					API: crd.APIInfo{
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
				"fed-modules.json": "{\"testFrontendService\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"fullProfile\":false}}",
			}))

			gomega.Eventually(func() bool {
				nfe := &crd.Frontend{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: frontend.Name, Namespace: frontend.Namespace}, nfe)
				if err != nil {
					return false
				}
				gomega.Expect(nfe.Status.Conditions[0].Type).Should(gomega.Equal(crd.FrontendsReady))
				gomega.Expect(nfe.Status.Conditions[0].Status).Should(gomega.Equal(v1.ConditionTrue))
				gomega.Expect(nfe.Status.Conditions[1].Type).Should(gomega.Equal(crd.ReconciliationFailed))
				gomega.Expect(nfe.Status.Conditions[1].Status).Should(gomega.Equal(v1.ConditionFalse))
				gomega.Expect(nfe.Status.Conditions[2].Type).Should(gomega.Equal(crd.ReconciliationSuccessful))
				gomega.Expect(nfe.Status.Conditions[2].Status).Should(gomega.Equal(v1.ConditionTrue))
				gomega.Expect(nfe.Status.Ready).Should(gomega.Equal(true))
				return true
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
					API: crd.APIInfo{
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
					API: crd.APIInfo{
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
					API: crd.APIInfo{
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
			gomega.Expect(createdDeployment.Name).Should(gomega.Equal(FrontendName + "-frontend"))
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
				"fed-modules.json":     "{\"chrome\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"config\":{\"apple\":\"pie\",\"ssoUrl\":\"https://something-auth\"},\"fullProfile\":false},\"noConfig\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"fullProfile\":false},\"nonChrome\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"config\":{\"apple\":\"pie\"},\"fullProfile\":false}}",
				"test-chrome-env.json": "{\"id\":\"test-chrome-bundle\",\"title\":\"\",\"navginkgo.Items\":[{\"title\":\"Test\",\"href\":\"/test/href\"},{\"title\":\"Test\",\"href\":\"/test/href\"},{\"title\":\"Test\",\"href\":\"/test/href\"}]}"}))
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
					API: crd.APIInfo{
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
					API: crd.APIInfo{
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
					API: crd.APIInfo{
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
					API: crd.APIInfo{
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
				"test-dependencies-env.json": "{\"id\":\"test-dependencies-bundle\",\"title\":\"\",\"navginkgo.Items\":[{\"title\":\"Test\",\"href\":\"/test/href\"},{\"title\":\"Test\",\"href\":\"/test/href\"},{\"title\":\"Test\",\"href\":\"/test/href\"}]}",
				"fed-modules.json":           "{\"testDependencies\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}],\"dependencies\":[\"depstring\"]}],\"fullProfile\":false},\"testNoDependencies\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}],\"fullProfile\":false},\"testOptionalDependencies\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}],\"optionalDependencies\":[\"depstring-op\"]}],\"fullProfile\":false}}",
			}))
			gomega.Expect(createdConfigMap.ObjectMeta.OwnerReferences[0].Name).Should(gomega.Equal(FrontendEnvName))

		})
	})
})
