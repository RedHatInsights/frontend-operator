package controllers

import (
	"context"
	"time"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// locales.json and the per-locale messages ConfigMaps (RHCLOUD-52083, RHCLOUD-52085).
var _ = ginkgo.Describe("Locale output", func() {
	const (
		namespace = "default"
		timeout   = time.Second * 10
		interval  = time.Millisecond * 250
	)
	ctx := context.Background()

	english := crd.EnabledLocale{ID: "en", Name: "English", Default: true}

	// createEnv creates the environment and one Frontend in it, and returns the
	// Frontend; the base ConfigMap is named after the environment.
	createEnv := func(env *crd.FrontendEnvironment, locales crd.LocaleCatalogs) *crd.Frontend {
		fe := &crd.Frontend{
			ObjectMeta: metav1.ObjectMeta{Name: env.Name + "-fe", Namespace: namespace},
			Spec: crd.FrontendSpec{
				EnvName:          env.Name,
				Image:            "my-image:version",
				Frontend:         crd.FrontendInfo{Paths: []string{"/"}},
				Module:           &crd.FedModule{Modules: []crd.Module{}, ModuleID: "app"},
				FeoConfigEnabled: true,
				Locales:          locales,
			},
		}
		gomega.Expect(k8sClient.Create(ctx, fe)).To(gomega.Succeed())
		ginkgo.DeferCleanup(func() { gomega.Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, fe))).To(gomega.Succeed()) })
		gomega.Expect(k8sClient.Create(ctx, env)).To(gomega.Succeed())
		ginkgo.DeferCleanup(func() { gomega.Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, env))).To(gomega.Succeed()) })
		return fe
	}

	createNamespace := func(name string) {
		ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
		gomega.Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, ns))).To(gomega.Succeed())
	}

	baseData := func(envName string) func() map[string]string {
		return func() map[string]string {
			cm := &v1.ConfigMap{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: envName, Namespace: namespace}, cm); err != nil {
				return nil
			}
			return cm.Data
		}
	}

	messagesConfigMapWithBase := func(ns, baseName, locale string) func() (*v1.ConfigMap, error) {
		return func() (*v1.ConfigMap, error) {
			cm := &v1.ConfigMap{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: messagesConfigMapName(baseName, locale), Namespace: ns}, cm)
			return cm, err
		}
	}
	messagesConfigMap := func(ns, locale string) func() (*v1.ConfigMap, error) {
		return messagesConfigMapWithBase(ns, "feo-context-cfg", locale)
	}

	// touch triggers a reconcile without changing the generated output.
	touch := func(fe *crd.Frontend) {
		gomega.Eventually(func() error {
			got := &crd.Frontend{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(fe), got); err != nil {
				return err
			}
			if got.Annotations == nil {
				got.Annotations = map[string]string{}
			}
			got.Annotations["test/touch"] = time.Now().String()
			return k8sClient.Update(ctx, got)
		}, timeout, interval).Should(gomega.Succeed())
	}

	ginkgo.It("writes messages beside source and target configs and advertises them in locales.json", func() {
		createNamespace("locale-target")
		env := mockFrontendEnv("test-locale-messages-env", namespace)
		env.Spec.TargetNamespaces = []string{"locale-target"}
		env.Spec.EnabledLocales = []crd.EnabledLocale{english, {ID: "fr", Name: "Français"}, {ID: "zh-CN", Name: "简体中文"}}
		env.Spec.Bundles = &[]crd.FrontendBundles{{ID: "iam", Title: "IAM"}}
		env.Spec.Locales = crd.LocaleCatalogs{"fr": {"bundles.iam.title": "IAM"}}
		fe := createEnv(env, crd.LocaleCatalogs{"fr": {"module.defaultDocumentTitle": "Application"}})

		gomega.Eventually(baseData(env.Name), timeout, interval).Should(gomega.HaveKeyWithValue("locales.json",
			`[{"id":"en","name":"English","default":true},{"id":"fr","name":"Français","file":"messages.fr.json"},{"id":"zh-CN","name":"简体中文","file":"messages.zh-CN.json"}]`))

		// the resource cache applies in no particular order
		for _, get := range []func() (*v1.ConfigMap, error){
			messagesConfigMap("locale-target", "fr"), messagesConfigMap("locale-target", "zh-cn"),
			messagesConfigMapWithBase(namespace, env.Name, "fr"), messagesConfigMapWithBase(namespace, env.Name, "zh-cn"),
		} {
			gomega.Eventually(func() error { _, err := get(); return err }, timeout, interval).Should(gomega.Succeed())
		}
		fr, err := messagesConfigMap("locale-target", "fr")()
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(fr.Data).To(gomega.Equal(map[string]string{
			"messages.fr.json": `{"bundle.iam.title":"IAM","module.app.defaultDocumentTitle":"Application"}`,
		}))
		gomega.Expect(fr.Labels).To(gomega.HaveKeyWithValue("frontend.cloud.redhat.com/messages-locale", "fr"))
		gomega.Expect(fr.Labels).To(gomega.HaveKeyWithValue("frontendenv", env.Name))
		gomega.Expect(fr.Annotations).NotTo(gomega.HaveKey("qontract.recycle"))
		gomega.Expect(fr.OwnerReferences).To(gomega.ContainElement(gomega.HaveField("Name", env.Name)))

		sourceFr, err := messagesConfigMapWithBase(namespace, env.Name, "fr")()
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(sourceFr.Data).To(gomega.Equal(fr.Data))

		zh, err := messagesConfigMap("locale-target", "zh-cn")()
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(zh.Data).To(gomega.Equal(map[string]string{"messages.zh-CN.json": "{}"}))
		sourceZH, err := messagesConfigMapWithBase(namespace, env.Name, "zh-cn")()
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(sourceZH.Data).To(gomega.Equal(zh.Data))

		ginkgo.By("a translation-only change updates the messages, not the base ConfigMap or the Deployment")
		before := baseData(env.Name)()
		deploymentHash := func() string {
			d := &apps.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: fe.Name + "-frontend", Namespace: namespace}, d); err != nil {
				return ""
			}
			return d.Spec.Template.Annotations["configHash"]
		}
		gomega.Eventually(deploymentHash, timeout, interval).ShouldNot(gomega.BeEmpty())
		hashBefore := deploymentHash()
		gomega.Eventually(func() error {
			got := &crd.Frontend{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(fe), got); err != nil {
				return err
			}
			got.Spec.Locales = crd.LocaleCatalogs{"fr": {"module.defaultDocumentTitle": "Appli"}}
			return k8sClient.Update(ctx, got)
		}, timeout, interval).Should(gomega.Succeed())
		gomega.Eventually(func() string {
			cm, _ := messagesConfigMap("locale-target", "fr")()
			return cm.Data["messages.fr.json"]
		}, timeout, interval).Should(gomega.ContainSubstring(`"Appli"`))
		gomega.Expect(baseData(env.Name)()).To(gomega.Equal(before))
		gomega.Consistently(deploymentHash, time.Second*2, interval).Should(gomega.Equal(hashBefore))

		ginkgo.By("a disabled locale leaves locales.json but keeps its messages while it has strings")
		gomega.Eventually(func() error {
			got := &crd.FrontendEnvironment{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(env), got); err != nil {
				return err
			}
			got.Spec.EnabledLocales = []crd.EnabledLocale{english, {ID: "fr", Name: "Français"}}
			return k8sClient.Update(ctx, got)
		}, timeout, interval).Should(gomega.Succeed())
		gomega.Eventually(func() string { return baseData(env.Name)()["locales.json"] }, timeout, interval).ShouldNot(gomega.ContainSubstring(`"zh-CN"`))
		_, err = messagesConfigMap("locale-target", "fr")()
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("a locale without strings that is no longer enabled is pruned")
		gomega.Eventually(func() bool {
			_, err := messagesConfigMap("locale-target", "zh-cn")()
			return k8serr.IsNotFound(err)
		}, timeout, interval).Should(gomega.BeTrue())
		_, err = messagesConfigMapWithBase(namespace, env.Name, "zh-cn")()
		gomega.Expect(k8serr.IsNotFound(err)).To(gomega.BeTrue())
	})

	ginkgo.It("drops a key that collides with another Frontend's and republishes it once the other is gone and the environment reconciles", func() {
		createNamespace("locale-peer-target")
		createNamespace("locale-peer-owner")
		env := mockFrontendEnv("test-locale-peer-env", namespace)
		env.Spec.TargetNamespaces = []string{"locale-peer-target"}
		env.Spec.EnabledLocales = []crd.EnabledLocale{english, {ID: "fr", Name: "Français"}}
		fe := createEnv(env, crd.LocaleCatalogs{"fr": {"module.defaultDocumentTitle": "Application"}})
		messages := func() string {
			cm, err := messagesConfigMap("locale-peer-target", "fr")()
			if err != nil {
				return ""
			}
			return cm.Data[messagesFileName("fr")]
		}
		gomega.Eventually(messages, timeout, interval).Should(gomega.Equal(`{"module.app.defaultDocumentTitle":"Application"}`))

		ginkgo.By("another Frontend with the same moduleID makes the translated title collide")
		other := &crd.Frontend{
			ObjectMeta: metav1.ObjectMeta{Name: env.Name + "-other", Namespace: "locale-peer-owner"},
			Spec: crd.FrontendSpec{
				EnvName:          env.Name,
				Image:            "my-image:version",
				Frontend:         crd.FrontendInfo{Paths: []string{"/other"}},
				Module:           &crd.FedModule{Modules: []crd.Module{}, ModuleID: "app"},
				FeoConfigEnabled: true,
			},
		}
		gomega.Expect(k8sClient.Create(ctx, other)).To(gomega.Succeed())
		ginkgo.DeferCleanup(func() { gomega.Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, other))).To(gomega.Succeed()) })
		gomega.Eventually(messages, timeout, interval).Should(gomega.Equal(`{}`))

		ginkgo.By("deleting the other Frontend republishes the surviving title")
		gomega.Expect(k8sClient.Delete(ctx, other)).To(gomega.Succeed())
		touch(fe)
		gomega.Eventually(messages, timeout, interval).Should(gomega.Equal(`{"module.app.defaultDocumentTitle":"Application"}`))
	})

	ginkgo.It("publishes beside the source config when there is no target namespace", func() {
		env := mockFrontendEnv("test-locale-no-target-env", namespace)
		env.Spec.EnabledLocales = []crd.EnabledLocale{english, {ID: "fr", Name: "Français"}}
		createEnv(env, nil)
		gomega.Eventually(baseData(env.Name), timeout, interval).Should(gomega.HaveKeyWithValue("locales.json",
			`[{"id":"en","name":"English","default":true},{"id":"fr","name":"Français","file":"messages.fr.json"}]`))
		gomega.Eventually(func() map[string]string {
			cm, _ := messagesConfigMapWithBase(namespace, env.Name, "fr")()
			return cm.Data
		}, timeout, interval).Should(gomega.Equal(map[string]string{"messages.fr.json": "{}"}))
	})

	ginkgo.It("publishes staged strings but writes no locales.json when no locales are enabled", func() {
		createNamespace("locale-target-off")
		env := mockFrontendEnv("test-no-locales-env", namespace)
		env.Spec.TargetNamespaces = []string{"locale-target-off"}
		env.Spec.Bundles = &[]crd.FrontendBundles{{ID: "iam", Title: "IAM"}}
		env.Spec.Locales = crd.LocaleCatalogs{"fr": {"bundles.iam.title": "IAM"}}
		createEnv(env, nil)
		gomega.Eventually(baseData(env.Name), timeout, interval).Should(gomega.HaveKey("sso-config.json"))
		gomega.Expect(baseData(env.Name)()).NotTo(gomega.HaveKey("locales.json"))
		gomega.Eventually(func() string {
			cm, _ := messagesConfigMap("locale-target-off", "fr")()
			return cm.Data["messages.fr.json"]
		}, timeout, interval).Should(gomega.Equal(`{"bundle.iam.title":"IAM"}`))
	})
})
