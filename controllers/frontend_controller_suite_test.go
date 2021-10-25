package controllers

import (
	"context"
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

// +kubebuilder:docs-gen:collapse=Imports

/*
The first step to writing a simple integration test is to actually create an instance of CronJob you can run tests against.
Note that to create a CronJob, you’ll need to create a stub CronJob struct that contains your CronJob’s specifications.
Note that when we create a stub CronJob, the CronJob also needs stubs of its required downstream objects.
Without the stubbed Job template spec and the Pod template spec below, the Kubernetes API will not be able to
create the CronJob.
*/
var _ = Describe("Frontend controller", func() {

	// Define utility constants for object names and testing timeouts/durations and intervals.
	const (
		FrontendName      = "test-frontend"
		FrontendNamespace = "default"
		FrontendEnvName   = "test-env"

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
					NavItem: &crd.BundleNavItem{
						Title:       "Test",
						GroupID:     "",
						NavItems:    []crd.LeafBundleNavItem{},
						AppId:       "",
						Href:        "/test/href",
						Product:     "",
						IsExternal:  false,
						Filterable:  false,
						Permissions: []crd.BundlePermission{},
						Routes:      []crd.LeafBundleNavItem{},
						Expandable:  false,
					},
					Module: crd.FedModule{
						ManifestLocation: "/apps/inventory/fed-mods.json",
						Modules: []crd.Module{{
							Id:     "test",
							Module: "./RootApp",
							Routes: []crd.Routes{{
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
					Kind:       "Frontend",
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

			/*
				After creating this CronJob, let's check that the CronJob's Spec fields match what we passed in.
				Note that, because the k8s apiserver may not have finished creating a CronJob after our `Create()` call from earlier, we will use Gomega’s Eventually() testing function instead of Expect() to give the apiserver an opportunity to finish creating our CronJob.
				`Eventually()` will repeatedly run the function provided as an argument every interval seconds until
				(a) the function’s output matches what’s expected in the subsequent `Should()` call, or
				(b) the number of attempts * interval period exceed the provided timeout value.
				In the examples below, timeout and interval are Go Duration values of our choosing.
			*/

			deploymentLookupKey := types.NamespacedName{Name: frontend.Name, Namespace: FrontendNamespace}
			ingressLookupKey := types.NamespacedName{Name: frontend.Name, Namespace: FrontendNamespace}
			configMapLookupKey := types.NamespacedName{Name: frontendEnvironment.Name, Namespace: FrontendNamespace}

			createdDeployment := &apps.Deployment{}

			// We'll need to retry getting this newly created CronJob, given that creation may not immediately happen.
			Eventually(func() bool {
				err := k8sClient.Get(ctx, deploymentLookupKey, createdDeployment)
				if err != nil {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())
			// Let's make sure our Schedule string value was properly converted/handled.
			Expect(createdDeployment.Name).Should(Equal(FrontendName))

			createdIngress := &networking.Ingress{}
			// We'll need to retry getting this newly created CronJob, given that creation may not immediately happen.
			Eventually(func() bool {
				err := k8sClient.Get(ctx, ingressLookupKey, createdIngress)
				if err != nil {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())
			// Let's make sure our Schedule string value was properly converted/handled.
			Expect(createdIngress.Name).Should(Equal(FrontendName))

			createdConfigMap := &v1.ConfigMap{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
				if err != nil {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())
			// Let's make sure our Schedule string value was properly converted/handled.
			Expect(createdConfigMap.Name).Should(Equal(FrontendEnvName))
			Expect(createdConfigMap.Data).Should(Equal(map[string]string{
				"fed-modules.json": "{\"test-frontend\":{\"manifestLocation\":\"/apps/inventory/fed-mods.json\",\"modules\":[{\"id\":\"test\",\"module\":\"./RootApp\",\"routes\":[{\"pathname\":\"/test/href\"}]}]}}",
			}))
		})
	})
})
