package controllers

import (
	"context"
	"fmt"
	"time"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Frontend controller with image", func() {
	const (
		FrontendName      = "test-frontend"
		FrontendNamespace = "default"
		FrontendEnvName   = "test-env"
		BundleName        = "test-bundle"

		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When creating a Frontend Resource", func() {
		It("Should create a deployment with the correct items", func() {
			By("By creating a new Frontend")
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
					API: crd.ApiInfo{
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
							Id:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/test/href",
							}},
						}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, frontend)).Should(Succeed())

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
				},
			}
			Expect(k8sClient.Create(ctx, frontendEnvironment)).Should(Succeed())

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
			Expect(k8sClient.Create(ctx, bundle)).Should(Succeed())

			deploymentLookupKey := types.NamespacedName{Name: frontend.Name, Namespace: FrontendNamespace}
			ingressLookupKey := types.NamespacedName{Name: frontend.Name, Namespace: FrontendNamespace}
			configMapLookupKey := types.NamespacedName{Name: frontendEnvironment.Name, Namespace: FrontendNamespace}
			configSSOMapLookupKey := types.NamespacedName{Name: fmt.Sprintf("%s-sso", frontendEnvironment.Name), Namespace: FrontendNamespace}
			serviceLookupKey := types.NamespacedName{Name: frontend.Name, Namespace: FrontendNamespace}

			createdDeployment := &apps.Deployment{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, deploymentLookupKey, createdDeployment)
				return err == nil
			}, timeout, interval).Should(BeTrue())
			Expect(createdDeployment.Name).Should(Equal(FrontendName))
			fmt.Printf("\n%v\n", createdDeployment.GetAnnotations())
			Expect(createdDeployment.Spec.Template.GetAnnotations()["ssoHash"]).ShouldNot(Equal(""))
			Expect(createdDeployment.Spec.Template.GetAnnotations()["configHash"]).ShouldNot(Equal(""))

			createdIngress := &networking.Ingress{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, ingressLookupKey, createdIngress)
				return err == nil
			}, timeout, interval).Should(BeTrue())
			Expect(createdIngress.Name).Should(Equal(FrontendName))

			createdService := &v1.Service{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, serviceLookupKey, createdService)
				return err == nil
			}, timeout, interval).Should(BeTrue())
			Expect(createdIngress.Name).Should(Equal(FrontendName))

			createdConfigMap := &v1.ConfigMap{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
				return err == nil
			}, timeout, interval).Should(BeTrue())
			Expect(createdConfigMap.Name).Should(Equal(FrontendEnvName))
			Expect(createdConfigMap.Data).Should(Equal(map[string]string{
				"fed-modules.json": "{\"testFrontend\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}]}}",
				"test-env.json":    "{\"id\":\"test-bundle\",\"title\":\"\",\"navItems\":[{\"title\":\"Test\",\"href\":\"/test/href\"}]}",
			}))
			createdSSOConfigMap := &v1.ConfigMap{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, configSSOMapLookupKey, createdSSOConfigMap)
				return err == nil
			}, timeout, interval).Should(BeTrue())
		})
	})
})

var _ = Describe("Frontend controller with service", func() {
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

	Context("When creating a Frontend Resource", func() {
		It("Should create a deployment with the correct items", func() {
			By("By creating a new Frontend")
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
					API: crd.ApiInfo{
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
							Id:     "test",
							Module: "./RootApp",
							Routes: []crd.Route{{
								Pathname: "/test/href",
							}},
						}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, frontend)).Should(Succeed())

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
				},
			}
			Expect(k8sClient.Create(ctx, &frontendEnvironment)).Should(Succeed())

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
			Expect(k8sClient.Create(ctx, &bundle)).Should(Succeed())

			ingressLookupKey := types.NamespacedName{Name: frontend.Name, Namespace: FrontendNamespace}
			configMapLookupKey := types.NamespacedName{Name: frontendEnvironment.Name, Namespace: FrontendNamespace}
			configSSOMapLookupKey := types.NamespacedName{Name: fmt.Sprintf("%s-sso", frontendEnvironment.Name), Namespace: FrontendNamespace}

			createdIngress := &networking.Ingress{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, ingressLookupKey, createdIngress)
				return err == nil
			}, timeout, interval).Should(BeTrue())
			Expect(createdIngress.Name).Should(Equal(FrontendName))
			Expect(createdIngress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).Should(Equal(ServiceName))

			createdConfigMap := &v1.ConfigMap{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
				return err == nil
			}, timeout, interval).Should(BeTrue())
			Expect(createdConfigMap.Name).Should(Equal(FrontendEnvName))
			Expect(createdConfigMap.Data).Should(Equal(map[string]string{
				"fed-modules.json":      "{\"testFrontendService\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}]}}",
				"test-env-service.json": "{\"id\":\"test-service-bundle\",\"title\":\"\",\"navItems\":[{\"title\":\"Test\",\"href\":\"/test/href\"},{\"title\":\"Test2\",\"href\":\"/test/href2\"}]}",
			}))

			createdSSOConfigMap := &v1.ConfigMap{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, configSSOMapLookupKey, createdSSOConfigMap)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			Eventually(func() bool {
				nfe := &crd.Frontend{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: frontend.Name, Namespace: frontend.Namespace}, nfe)
				if err != nil {
					return false
				}
				Expect(nfe.Status.Conditions[0].Type).Should(Equal(crd.FrontendsReady))
				Expect(nfe.Status.Conditions[0].Status).Should(Equal(v1.ConditionTrue))
				Expect(nfe.Status.Conditions[1].Type).Should(Equal(crd.ReconciliationFailed))
				Expect(nfe.Status.Conditions[1].Status).Should(Equal(v1.ConditionFalse))
				Expect(nfe.Status.Conditions[2].Type).Should(Equal(crd.ReconciliationSuccessful))
				Expect(nfe.Status.Conditions[2].Status).Should(Equal(v1.ConditionTrue))
				Expect(nfe.Status.Ready).Should(Equal(true))
				return true
			}, timeout, interval).Should(BeTrue())
		})
	})
})
