package controllers

import (
	"context"
	"time"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// locales.json output of the environment ConfigMap (RHCLOUD-52083).
var _ = ginkgo.Describe("locales.json", func() {
	const (
		namespace = "default"
		timeout   = time.Second * 10
		interval  = time.Millisecond * 250
	)
	ctx := context.Background()

	// configMapFor creates the environment and one Frontend in it, and returns
	// the environment ConfigMap once the reconciler has written its sso-config.json.
	configMapFor := func(env *crd.FrontendEnvironment) *v1.ConfigMap {
		fe := &crd.Frontend{
			ObjectMeta: metav1.ObjectMeta{Name: env.Name + "-fe", Namespace: namespace},
			Spec: crd.FrontendSpec{
				EnvName:  env.Name,
				Image:    "my-image:version",
				Frontend: crd.FrontendInfo{Paths: []string{"/"}},
				Module:   &crd.FedModule{Modules: []crd.Module{}},
			},
		}
		gomega.Expect(k8sClient.Create(ctx, fe)).To(gomega.Succeed())
		ginkgo.DeferCleanup(func() { gomega.Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, fe))).To(gomega.Succeed()) })
		gomega.Expect(k8sClient.Create(ctx, env)).To(gomega.Succeed())
		ginkgo.DeferCleanup(func() { gomega.Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, env))).To(gomega.Succeed()) })

		cm := &v1.ConfigMap{}
		gomega.Eventually(func() bool {
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: env.Name, Namespace: namespace}, cm); err != nil {
				return false
			}
			_, ok := cm.Data["sso-config.json"]
			return ok
		}, timeout, interval).Should(gomega.BeTrue())
		return cm
	}

	ginkgo.It("lists the enabled locales, English included", func() {
		env := mockFrontendEnv("test-locales-json-env", namespace)
		env.Spec.EnabledLocales = []crd.EnabledLocale{
			{ID: "en", Name: "English", Default: true},
			{ID: "fr", Name: "Français"},
		}
		cm := configMapFor(env)
		gomega.Expect(cm.Data).To(gomega.HaveKeyWithValue("locales.json",
			`[{"id":"en","name":"English","default":true},{"id":"fr","name":"Français"}]`))
	})

	ginkgo.It("is absent when no locales are enabled", func() {
		cm := configMapFor(mockFrontendEnv("test-no-locales-json-env", namespace))
		gomega.Expect(cm.Data).NotTo(gomega.HaveKey("locales.json"))
	})
})
