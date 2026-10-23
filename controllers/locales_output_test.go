package controllers

import (
	"context"
	"time"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type localeConfigMapFailureClient struct {
	client.Client
	failCreate bool
	failUpdate bool
}

type localePruneRaceClient struct {
	client.Client
}

func (c localePruneRaceClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if err := c.Client.List(ctx, list, opts...); err != nil {
		return err
	}
	configMaps, ok := list.(*v1.ConfigMapList)
	if !ok || len(configMaps.Items) == 0 {
		return nil
	}
	for i := range configMaps.Items {
		stale := configMaps.Items[i].DeepCopy()
		latest := &v1.ConfigMap{}
		if err := c.Client.Get(ctx, client.ObjectKeyFromObject(stale), latest); err != nil {
			return err
		}
		if latest.Annotations == nil {
			latest.Annotations = map[string]string{}
		}
		delete(latest.Annotations, unadvertisedSinceAnnotation)
		latest.Annotations["test/re-published"] = "true"
		if err := c.Client.Update(ctx, latest); err != nil {
			return err
		}
		configMaps.Items[i] = *stale
	}
	return nil
}

func (c localeConfigMapFailureClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if c.failCreate && isLocaleMessagesConfigMap(obj) {
		return k8serr.NewServiceUnavailable("temporary write failure")
	}
	return c.Client.Create(ctx, obj, opts...)
}

func (c localeConfigMapFailureClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if c.failUpdate && isLocaleMessagesConfigMap(obj) {
		return k8serr.NewServiceUnavailable("temporary write failure")
	}
	return c.Client.Update(ctx, obj, opts...)
}

func isLocaleMessagesConfigMap(obj client.Object) bool {
	_, ok := obj.(*v1.ConfigMap)
	return ok && obj.GetLabels()[messagesLocaleLabel] != ""
}

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

		ginkgo.By("a disabled locale is dropped from locales.json first and pruned on a later reconcile")
		gomega.Eventually(func() error {
			got := &crd.FrontendEnvironment{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(env), got); err != nil {
				return err
			}
			got.Spec.EnabledLocales = []crd.EnabledLocale{english, {ID: "zh-CN", Name: "简体中文"}}
			return k8sClient.Update(ctx, got)
		}, timeout, interval).Should(gomega.Succeed())
		gomega.Eventually(func() string { return baseData(env.Name)()["locales.json"] }, timeout, interval).ShouldNot(gomega.ContainSubstring(`"fr"`))
		// the controller schedules its own return: nothing else watches ConfigMaps
		res, err := (&FrontendReconciler{Client: k8sManagerClient, Scheme: scheme, Log: logr.Discard()}).
			Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(fe)})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(res.RequeueAfter).To(gomega.And(gomega.BeNumerically(">", 0), gomega.BeNumerically("<=", localePruneGrace)))
		// the removal order itself is covered by TestLocaleConfigMapRemovalOrder;
		// here only check that pruning happens once the grace period is over
		grace := localePruneGrace
		localePruneGrace = 0
		ginkgo.DeferCleanup(func() { localePruneGrace = grace })
		gomega.Eventually(func() bool {
			touch(fe)
			_, err := messagesConfigMap("locale-target", "fr")()
			return k8serr.IsNotFound(err)
		}, timeout, interval).Should(gomega.BeTrue())
		_, err = messagesConfigMap("locale-target", "zh-cn")()
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("retains a published locale after a transient message write failure", func() {
		createNamespace("locale-transient-target")
		env := mockFrontendEnv("test-locale-transient-env", namespace)
		env.Spec.TargetNamespaces = []string{"locale-transient-target"}
		env.Spec.EnabledLocales = []crd.EnabledLocale{english, {ID: "fr", Name: "Français"}}
		fe := createEnv(env, crd.LocaleCatalogs{"fr": {moduleTitleKey: "Application"}})
		gomega.Eventually(func() error {
			_, err := messagesConfigMapWithBase(namespace, env.Name, "fr")()
			return err
		}, timeout, interval).Should(gomega.Succeed())
		gomega.Eventually(func() error {
			_, err := messagesConfigMap("locale-transient-target", "fr")()
			return err
		}, timeout, interval).Should(gomega.Succeed())

		storedEnv := &crd.FrontendEnvironment{}
		gomega.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(env), storedEnv)).To(gomega.Succeed())
		storedFrontend := &crd.Frontend{}
		gomega.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(fe), storedFrontend)).To(gomega.Succeed())
		storedFrontend.Spec.Locales = crd.LocaleCatalogs{"fr": {moduleTitleKey: "Application mise à jour"}}
		reconciliation := &FrontendReconciliation{
			Ctx:                 ctx,
			Client:              localeConfigMapFailureClient{Client: k8sClient, failUpdate: true},
			Log:                 logr.Discard(),
			FrontendEnvironment: storedEnv,
			Frontend:            storedFrontend,
		}
		entries, err := reconciliation.setupLocaleConfigMaps(&crd.FrontendList{Items: []crd.Frontend{*storedFrontend}}, "feo-context-cfg")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(entries).To(gomega.ContainElement(gomega.HaveField("ID", "fr")))
		gomega.Expect(reconciliation.requeueAfter).To(gomega.Equal(localeRetryInterval))
		for _, get := range []func() (*v1.ConfigMap, error){
			messagesConfigMapWithBase(namespace, env.Name, "fr"),
			messagesConfigMap("locale-transient-target", "fr"),
		} {
			cm, err := get()
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cm.Data[messagesFileName("fr")]).To(gomega.ContainSubstring("Application"))
		}
	})

	ginkgo.It("does not advertise a locale whose first publication fails", func() {
		createNamespace("locale-never-published-target")
		env := mockFrontendEnv("test-locale-never-published-env", namespace)
		env.Spec.TargetNamespaces = []string{"locale-never-published-target"}
		env.Spec.EnabledLocales = []crd.EnabledLocale{english, {ID: "fr", Name: "Français"}}
		fe := &crd.Frontend{
			ObjectMeta: metav1.ObjectMeta{Name: "never-published", Namespace: namespace},
			Spec:       crd.FrontendSpec{EnvName: env.Name, Locales: crd.LocaleCatalogs{"fr": {moduleTitleKey: "Application"}}},
		}
		reconciliation := &FrontendReconciliation{
			Ctx:                 ctx,
			Client:              localeConfigMapFailureClient{Client: k8sClient, failCreate: true},
			Log:                 logr.Discard(),
			FrontendEnvironment: env,
			Frontend:            fe,
		}
		entries, err := reconciliation.setupLocaleConfigMaps(&crd.FrontendList{Items: []crd.Frontend{*fe}}, "feo-context-cfg")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(entries).To(gomega.HaveLen(1))
		gomega.Expect(entries[0].ID).To(gomega.Equal("en"))
		gomega.Expect(reconciliation.requeueAfter).To(gomega.Equal(localeRetryInterval))
	})

	ginkgo.It("skips stale prune stamps and deletes after a messages ConfigMap is republished", func() {
		createNamespace("locale-prune-race-target")
		env := mockFrontendEnv("test-locale-prune-race-env", namespace)
		env.Spec.TargetNamespaces = []string{"locale-prune-race-target"}
		gomega.Expect(k8sClient.Create(ctx, env)).To(gomega.Succeed())
		ginkgo.DeferCleanup(func() { gomega.Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, env))).To(gomega.Succeed()) })
		// Create clears TypeMeta, which MakeOwnerReference needs
		env.SetGroupVersionKind(crd.GroupVersion.WithKind("FrontendEnvironment"))

		stamping := &v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: messagesConfigMapName("feo-context-cfg", "fr"), Namespace: "locale-prune-race-target",
			Labels:          map[string]string{"frontendenv": env.Name, messagesLocaleLabel: "fr"},
			OwnerReferences: []metav1.OwnerReference{env.MakeOwnerReference()},
		}, Data: map[string]string{messagesFileName("fr"): "{}"}}
		deleting := &v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: messagesConfigMapName("feo-context-cfg", "ja"), Namespace: "locale-prune-race-target",
			Labels: map[string]string{"frontendenv": env.Name, messagesLocaleLabel: "ja"},
			Annotations: map[string]string{
				unadvertisedSinceAnnotation: time.Now().Add(-localePruneGrace - time.Minute).UTC().Format(time.RFC3339),
			},
			OwnerReferences: []metav1.OwnerReference{env.MakeOwnerReference()},
		}, Data: map[string]string{messagesFileName("ja"): "{}"}}
		gomega.Expect(k8sClient.Create(ctx, stamping)).To(gomega.Succeed())
		gomega.Expect(k8sClient.Create(ctx, deleting)).To(gomega.Succeed())

		frontend := &crd.Frontend{ObjectMeta: metav1.ObjectMeta{Name: "prune-race", Namespace: namespace}, Spec: crd.FrontendSpec{EnvName: env.Name}}
		reconciliation := &FrontendReconciliation{
			Ctx:                 ctx,
			Client:              localePruneRaceClient{Client: k8sClient},
			Log:                 logr.Discard(),
			FrontendEnvironment: env,
			Frontend:            frontend,
		}
		gomega.Expect(reconciliation.pruneLocaleConfigMaps("feo-context-cfg", map[string]bool{})).To(gomega.Succeed())
		gomega.Expect(reconciliation.requeueAfter).To(gomega.BeNumerically(">", 0))

		for _, name := range []string{stamping.Name, deleting.Name} {
			got := &v1.ConfigMap{}
			gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "locale-prune-race-target"}, got)).To(gomega.Succeed())
			gomega.Expect(got.Annotations).To(gomega.HaveKeyWithValue("test/re-published", "true"))
			gomega.Expect(got.Annotations).NotTo(gomega.HaveKey(unadvertisedSinceAnnotation))
		}
	})

	ginkgo.It("repairs an externally changed messages ConfigMap without a Frontend change", func() {
		createNamespace("locale-repair-target")
		env := mockFrontendEnv("test-locale-repair-env", namespace)
		env.Spec.TargetNamespaces = []string{"locale-repair-target"}
		env.Spec.EnabledLocales = []crd.EnabledLocale{english, {ID: "fr", Name: "Français"}}
		env.Spec.Bundles = &[]crd.FrontendBundles{{ID: "iam"}}
		env.Spec.Locales = crd.LocaleCatalogs{"fr": {"bundles.iam.title": "IAM"}}
		createEnv(env, nil)
		get := messagesConfigMap("locale-repair-target", "fr")
		gomega.Eventually(func() error { _, err := get(); return err }, timeout, interval).Should(gomega.Succeed())
		gomega.Consistently(func() string {
			cm, err := get()
			if err != nil {
				return ""
			}
			return cm.Data[messagesFileName("fr")]
		}, time.Second, interval).Should(gomega.Equal(`{"bundle.iam.title":"IAM"}`))

		gomega.Eventually(func() error {
			cm, err := get()
			if err != nil {
				return err
			}
			cm.Data[messagesFileName("fr")] = `{"bundle.iam.title":"external edit"}`
			return k8sClient.Update(ctx, cm)
		}, timeout, interval).Should(gomega.Succeed())
		gomega.Eventually(func() string {
			cm, err := get()
			if err != nil {
				return ""
			}
			return cm.Data[messagesFileName("fr")]
		}, timeout, interval).Should(gomega.Equal(`{"bundle.iam.title":"IAM"}`))
	})

	ginkgo.It("drops a key that collides with another Frontend's and republishes it once the other is gone", func() {
		createNamespace("locale-peer-target")
		createNamespace("locale-peer-owner")
		env := mockFrontendEnv("test-locale-peer-env", namespace)
		env.Spec.TargetNamespaces = []string{"locale-peer-target"}
		env.Spec.EnabledLocales = []crd.EnabledLocale{english, {ID: "fr", Name: "Français"}}
		createEnv(env, crd.LocaleCatalogs{"fr": {"module.defaultDocumentTitle": "Application"}})
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
		gomega.Eventually(messages, timeout, interval).Should(gomega.Equal(`{"module.app.defaultDocumentTitle":"Application"}`))
	})

	ginkgo.It("advertises only English when there is no target namespace to write messages to", func() {
		env := mockFrontendEnv("test-locale-no-target-env", namespace)
		// fr is the default but cannot be advertised, so English takes over
		env.Spec.EnabledLocales = []crd.EnabledLocale{{ID: "en", Name: "English"}, {ID: "fr", Name: "Français", Default: true}}
		createEnv(env, nil)
		gomega.Eventually(baseData(env.Name), timeout, interval).Should(gomega.HaveKeyWithValue("locales.json",
			`[{"id":"en","name":"English","default":true}]`))
	})

	ginkgo.It("writes no locale output when no locales are enabled", func() {
		createNamespace("locale-target-off")
		env := mockFrontendEnv("test-no-locales-env", namespace)
		env.Spec.TargetNamespaces = []string{"locale-target-off"}
		env.Spec.Locales = crd.LocaleCatalogs{"fr": {"bundles.iam.title": "IAM"}}
		createEnv(env, nil)
		gomega.Eventually(baseData(env.Name), timeout, interval).Should(gomega.HaveKey("sso-config.json"))
		gomega.Expect(baseData(env.Name)()).NotTo(gomega.HaveKey("locales.json"))
		list := &v1.ConfigMapList{}
		gomega.Expect(k8sClient.List(ctx, list, client.InNamespace("locale-target-off"), client.HasLabels{"frontend.cloud.redhat.com/messages-locale"})).To(gomega.Succeed())
		gomega.Expect(list.Items).To(gomega.BeEmpty())
	})
})
