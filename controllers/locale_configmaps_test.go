package controllers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// Unit tests for setupLocaleConfigMaps against a fake API server. The
// reconciling Frontend lives in "boot", so the source base ConfigMap is
// boot/stage; target namespaces hold feo-context-cfg and each base has messages.

const localeTestBase = "feo-context-cfg"

var english = crd.EnabledLocale{ID: "en", Name: "English", Default: true}

func localeConfigMapTestEnv(targets []string, enabled ...crd.EnabledLocale) *crd.FrontendEnvironment {
	return &crd.FrontendEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "stage", UID: "stage-uid"},
		Spec: crd.FrontendEnvironmentSpec{
			TargetNamespaces: targets,
			EnabledLocales:   enabled,
		},
	}
}

func newLocaleReconciliation(c client.Client, env *crd.FrontendEnvironment) *FrontendReconciliation {
	fe := &crd.Frontend{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "boot"}, Spec: crd.FrontendSpec{EnvName: env.Name}}
	return &FrontendReconciliation{Ctx: context.Background(), Client: c, Log: logr.Discard(), FrontendEnvironment: env, Frontend: fe}
}

// messagesConfigMap is a messages ConfigMap as the operator writes it for env.
func messagesConfigMap(env *crd.FrontendEnvironment, ns, baseName, locale string, annotations map[string]string) *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: messagesConfigMapName(baseName, locale), Namespace: ns,
			Labels:          map[string]string{"frontendenv": env.Name, messagesLocaleLabel: locale},
			Annotations:     annotations,
			OwnerReferences: []metav1.OwnerReference{env.MakeOwnerReference()},
		},
		Data: map[string]string{messagesFileName(locale): "{}"},
	}
}

// baseConfigMap is a stored base ConfigMap advertising locales.
func baseConfigMap(ns, name string, locales ...string) *v1.ConfigMap {
	entries := []string{}
	for _, l := range locales {
		entries = append(entries, `{"id":"`+l+`","name":"`+l+`"}`)
	}
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Data:       map[string]string{"locales.json": "[" + strings.Join(entries, ",") + "]"},
	}
}

func staleSince() map[string]string {
	return map[string]string{unadvertisedSinceAnnotation: time.Now().Add(-localePruneGrace - time.Minute).UTC().Format(time.RFC3339)}
}

func getConfigMap(t *testing.T, c client.Client, ns, name string) (*v1.ConfigMap, error) {
	t.Helper()
	cm := &v1.ConfigMap{}
	return cm, c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: ns}, cm)
}

func setupLocales(t *testing.T, r *FrontendReconciliation, feList *crd.FrontendList) []localeEntry {
	t.Helper()
	if feList == nil {
		feList = &crd.FrontendList{}
	}
	entries, err := r.setupLocaleConfigMaps(feList, localeTestBase)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// Removal order: a messages ConfigMap is pruned only after no stored
// locales.json lists its locale and the grace period has passed, and the
// reconcile is requeued for that deadline.
func TestLocaleConfigMapRemovalOrder(t *testing.T) {
	ctx := context.Background()
	env := localeConfigMapTestEnv([]string{"console"}, english)
	source := baseConfigMap("boot", "stage", "en", "fr")
	fr := messagesConfigMap(env, "console", localeTestBase, "fr", nil)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(source, fr).Build()
	frName := messagesConfigMapName(localeTestBase, "fr")

	// fr is disabled but the stored source locales.json still advertises it
	r := newLocaleReconciliation(c, env)
	setupLocales(t, r, nil)
	cm, err := getConfigMap(t, c, "console", frName)
	if err != nil || cm.Annotations[unadvertisedSinceAnnotation] != "" {
		t.Fatalf("still advertised: want kept and unstamped, got %v, %v", cm.Annotations, err)
	}
	if r.requeueAfter != localeRetryInterval {
		t.Errorf("still advertised by a stored base: want a requeue to start the grace period, got %v", r.requeueAfter)
	}

	// the source base no longer lists fr: start the grace period
	source.Data["locales.json"] = `[{"id":"en","name":"English","default":true}]`
	if err := c.Update(ctx, source); err != nil {
		t.Fatal(err)
	}
	r = newLocaleReconciliation(c, env)
	setupLocales(t, r, nil)
	cm, err = getConfigMap(t, c, "console", frName)
	if err != nil || cm.Annotations[unadvertisedSinceAnnotation] == "" {
		t.Fatalf("unadvertised: want kept and stamped, got %v, %v", cm.Annotations, err)
	}
	if r.requeueAfter != localePruneGrace {
		t.Errorf("stamped: want requeue after the grace period, got %v", r.requeueAfter)
	}

	r = newLocaleReconciliation(c, env)
	setupLocales(t, r, nil)
	if _, err := getConfigMap(t, c, "console", frName); err != nil {
		t.Fatalf("within grace: want kept, got %v", err)
	}
	if r.requeueAfter <= 0 || r.requeueAfter > localePruneGrace {
		t.Errorf("within grace: want requeue for the remaining time, got %v", r.requeueAfter)
	}

	// grace period over
	cm.Annotations = staleSince()
	if err := c.Update(ctx, cm); err != nil {
		t.Fatal(err)
	}
	setupLocales(t, newLocaleReconciliation(c, env), nil)
	if _, err := getConfigMap(t, c, "console", frName); !k8serr.IsNotFound(err) {
		t.Fatalf("after grace: want pruned, got %v", err)
	}
}

// A target namespace whose base ConfigMap still advertises a locale keeps its
// messages, even when the source base does not, and an unreadable base keeps
// everything in that namespace.
func TestLocaleConfigMapTargetBaseStillAdvertises(t *testing.T) {
	env := localeConfigMapTestEnv([]string{"console", "console-beta", "console-broken"}, english)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		baseConfigMap("boot", "stage", "en"),
		// this target's base update failed: it still lists fr
		baseConfigMap("console", localeTestBase, "en", "fr"),
		baseConfigMap("console-beta", localeTestBase, "en"),
		messagesConfigMap(env, "console", localeTestBase, "fr", staleSince()),
		messagesConfigMap(env, "console-beta", localeTestBase, "fr", staleSince()),
		messagesConfigMap(env, "console-broken", localeTestBase, "fr", staleSince()),
	).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if key.Namespace == "console-broken" && key.Name == localeTestBase {
				return errors.New("unavailable")
			}
			return c.Get(ctx, key, obj, opts...)
		},
	}).Build()

	setupLocales(t, newLocaleReconciliation(c, env), nil)

	frName := messagesConfigMapName(localeTestBase, "fr")
	if _, err := getConfigMap(t, c, "console", frName); err != nil {
		t.Errorf("still advertised by the target base: want kept, got %v", err)
	}
	if _, err := getConfigMap(t, c, "console-broken", frName); err != nil {
		t.Errorf("unreadable target base: want kept, got %v", err)
	}
	if _, err := getConfigMap(t, c, "console-beta", frName); !k8serr.IsNotFound(err) {
		t.Errorf("advertised nowhere, past grace: want pruned, got %v", err)
	}
}

// An unreadable target base alone schedules a retry: nothing else would come
// back to prune once it is readable again.
func TestLocaleConfigMapUnreadableTargetBaseRetries(t *testing.T) {
	env := localeConfigMapTestEnv([]string{"console"}, english)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		baseConfigMap("boot", "stage", "en"),
		messagesConfigMap(env, "console", localeTestBase, "fr", staleSince()),
	).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if key.Namespace == "console" && key.Name == localeTestBase {
				return errors.New("unavailable")
			}
			return c.Get(ctx, key, obj, opts...)
		},
	}).Build()

	r := newLocaleReconciliation(c, env)
	setupLocales(t, r, nil)
	if _, err := getConfigMap(t, c, "console", messagesConfigMapName(localeTestBase, "fr")); err != nil {
		t.Errorf("unreadable target base: want kept, got %v", err)
	}
	if r.requeueAfter != localeRetryInterval {
		t.Errorf("unreadable target base: want a retry after %v, got %v", localeRetryInterval, r.requeueAfter)
	}
}

func TestLocaleConfigMapUnparsableSourceBaseKeepsAll(t *testing.T) {
	env := localeConfigMapTestEnv([]string{"console"}, english)
	source := baseConfigMap("boot", "stage")
	source.Data["locales.json"] = "not json"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(source, messagesConfigMap(env, "console", localeTestBase, "fr", staleSince())).Build()

	r := newLocaleReconciliation(c, env)
	setupLocales(t, r, nil)
	if _, err := getConfigMap(t, c, "console", messagesConfigMapName(localeTestBase, "fr")); err != nil {
		t.Errorf("unparsable source base: want kept, got %v", err)
	}
	if r.requeueAfter != localeRetryInterval {
		t.Errorf("unparsable source base: want a retry after %v, got %v", localeRetryInterval, r.requeueAfter)
	}
}

func TestLocaleConfigMapReenabledCancelsPruning(t *testing.T) {
	env := localeConfigMapTestEnv([]string{"console"}, english, crd.EnabledLocale{ID: "fr", Name: "Français"})
	fr := messagesConfigMap(env, "console", localeTestBase, "fr", map[string]string{unadvertisedSinceAnnotation: time.Now().UTC().Format(time.RFC3339)})
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(fr).Build()

	entries := setupLocales(t, newLocaleReconciliation(c, env), nil)
	if len(entries) != 2 || entries[1].File != "messages.fr.json" {
		t.Errorf("entries: got %v", entries)
	}
	got, err := getConfigMap(t, c, "console", fr.Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Annotations[unadvertisedSinceAnnotation]; ok {
		t.Errorf("re-enabled locale is still scheduled for pruning: %v", got.Annotations)
	}
}

// A locale whose messages cannot be written to every target namespace is not
// advertised, the other locales are unaffected, the Frontend's condition says
// so, and the reconcile is retried.
func TestLocaleConfigMapFailedWriteNotAdvertised(t *testing.T) {
	env := localeConfigMapTestEnv([]string{"console", "console-beta"}, crd.EnabledLocale{ID: "en", Name: "English"},
		crd.EnabledLocale{ID: "fr", Name: "Français", Default: true}, crd.EnabledLocale{ID: "ja", Name: "日本語"})
	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			// fr reaches the first target namespace, then fails in the second
			if obj.GetName() == messagesConfigMapName(localeTestBase, "fr") && obj.GetNamespace() == "console-beta" {
				return k8serr.NewForbidden(v1.Resource("configmaps"), obj.GetName(), errors.New("denied"))
			}
			return c.Create(ctx, obj, opts...)
		},
	}).Build()
	r := newLocaleReconciliation(c, env)

	entries := setupLocales(t, r, nil)
	want := []localeEntry{
		// fr was the default; English takes over
		{ID: "en", Name: "English", Default: true},
		{ID: "ja", Name: "日本語", File: "messages.ja.json"},
	}
	if !slices.Equal(entries, want) {
		t.Errorf("entries: got %v, want %v", entries, want)
	}
	if r.requeueAfter != localeRetryInterval {
		t.Errorf("want a retry after %v, got %v", localeRetryInterval, r.requeueAfter)
	}
}

// A conflict is a stale read, not a bad locale: the reconcile is retried rather
// than the locale dropped from locales.json.
func TestLocaleConfigMapConflictRetries(t *testing.T) {
	env := localeConfigMapTestEnv([]string{"console"}, english, crd.EnabledLocale{ID: "fr", Name: "Français"})
	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			return k8serr.NewConflict(v1.Resource("configmaps"), obj.GetName(), errors.New("stale"))
		},
	}).Build()

	if _, err := newLocaleReconciliation(c, env).setupLocaleConfigMaps(&crd.FrontendList{}, localeTestBase); !k8serr.IsConflict(err) {
		t.Errorf("want the conflict returned for a retry, got %v", err)
	}
}

func TestLocaleConfigMapOversizeNotAdvertised(t *testing.T) {
	env := localeConfigMapTestEnv([]string{"console"}, english, crd.EnabledLocale{ID: "fr", Name: "Français"})
	env.Spec.Locales = crd.LocaleCatalogs{"fr": {}}
	env.Spec.Bundles = &[]crd.FrontendBundles{}
	// the CRD caps a catalog at 2048 keys of 4096 characters; 300 of 4096 is
	// already over the 900 KB messages limit
	for i := 0; i < 300; i++ {
		*env.Spec.Bundles = append(*env.Spec.Bundles, crd.FrontendBundles{ID: fmt.Sprintf("b%d", i)})
		env.Spec.Locales["fr"][fmt.Sprintf("bundles.b%d.title", i)] = crd.LocaleMessage(strings.Repeat("é", 4096))
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := newLocaleReconciliation(c, env)

	entries := setupLocales(t, r, nil)
	if !slices.Equal(entries, []localeEntry{{ID: "en", Name: "English", Default: true}}) {
		t.Errorf("entries: got %v", entries)
	}
	if _, err := getConfigMap(t, c, "console", messagesConfigMapName(localeTestBase, "fr")); !k8serr.IsNotFound(err) {
		t.Errorf("oversize messages were written: %v", err)
	}
}

// Reconciling unchanged input does not touch the messages ConfigMaps.
func TestLocaleConfigMapNoOpKeepsResourceVersion(t *testing.T) {
	env := localeConfigMapTestEnv([]string{"console"}, english, crd.EnabledLocale{ID: "fr", Name: "Français"})
	env.Spec.Bundles = &[]crd.FrontendBundles{{ID: "iam"}}
	env.Spec.Locales = crd.LocaleCatalogs{"fr": {"bundles.iam.title": "IAM"}}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	name := messagesConfigMapName(localeTestBase, "fr")

	setupLocales(t, newLocaleReconciliation(c, env), nil)
	first, err := getConfigMap(t, c, "console", name)
	if err != nil {
		t.Fatal(err)
	}
	setupLocales(t, newLocaleReconciliation(c, env), nil)
	second, err := getConfigMap(t, c, "console", name)
	if err != nil {
		t.Fatal(err)
	}
	if first.ResourceVersion != second.ResourceVersion {
		t.Errorf("no-op reconcile rewrote the ConfigMap: %s -> %s", first.ResourceVersion, second.ResourceVersion)
	}
}

// Updating with a stale cached ConfigMap must conflict instead of overwriting
// a newer value with the APIReader bypass removed.
func TestLocaleConfigMapStaleUpdateConflicts(t *testing.T) {
	ctx := context.Background()
	env := localeConfigMapTestEnv([]string{"console"}, english, crd.EnabledLocale{ID: "fr", Name: "Français"})
	name := messagesConfigMapName(localeTestBase, "fr")
	stored := messagesConfigMap(env, "console", localeTestBase, "fr", nil)
	stored.Data[messagesFileName("fr")] = `{"key":"old"}`
	server := fake.NewClientBuilder().WithScheme(scheme).WithObjects(stored).Build()
	stale := stored.DeepCopy()
	current := stored.DeepCopy()
	current.Data[messagesFileName("fr")] = `{"key":"newer"}`
	if err := server.Update(ctx, current); err != nil {
		t.Fatal(err)
	}
	cached := interceptor.NewClient(server, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if key.Namespace == "console" && key.Name == name {
				stale.DeepCopyInto(obj.(*v1.ConfigMap))
				return nil
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})

	r := newLocaleReconciliation(cached, env)
	err := r.writeLocaleConfigMap(types.NamespacedName{Name: name, Namespace: "console"}, "fr", `{"key":"desired"}`)
	if !k8serr.IsConflict(err) {
		t.Fatalf("want stale resourceVersion conflict, got %v", err)
	}
	got, err := getConfigMap(t, server, "console", name)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"key":"newer"}`; got.Data[messagesFileName("fr")] != want {
		t.Errorf("stale update overwrote current data: got %s, want %s", got.Data[messagesFileName("fr")], want)
	}
}

// Messages are written beside the source and target base ConfigMaps and contain
// only this environment's Frontends. Pruning only considers ConfigMaps owned by
// this environment under those base names.
func TestLocaleConfigMapNamespaceIsolation(t *testing.T) {
	env := localeConfigMapTestEnv([]string{"console"}, english, crd.EnabledLocale{ID: "fr", Name: "Français"})
	other := localeConfigMapTestEnv(nil)
	other.Name, other.UID = "prod", "prod-uid"

	elsewhere := messagesConfigMap(env, "elsewhere", localeTestBase, "de", staleSince())
	foreignOwner := messagesConfigMap(other, "console", localeTestBase, "it", staleSince())
	foreignOwner.Labels["frontendenv"] = env.Name
	otherEnv := messagesConfigMap(other, "console", localeTestBase, "de", staleSince())
	otherBase := messagesConfigMap(env, "console", localeTestBase+"-beta", "de", staleSince())
	// positive control: our own stale ConfigMap is pruned
	ours := messagesConfigMap(env, "console", localeTestBase, "es", staleSince())
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(elsewhere, foreignOwner, otherEnv, otherBase, ours).Build()

	feList := &crd.FrontendList{Items: []crd.Frontend{
		withSearch(localeTestFrontend("app", true, crd.LocaleCatalogs{"fr": {"searchEntries.x.title": "X"}}), "x"),
		withSearch(localeTestFrontend("prod-app", true, crd.LocaleCatalogs{"fr": {"searchEntries.y.title": "Y"}}), "y"),
	}}
	feList.Items[1].Spec.EnvName = "prod"
	setupLocales(t, newLocaleReconciliation(c, env), feList)

	list := &v1.ConfigMapList{}
	if err := c.List(context.Background(), list, client.HasLabels{messagesLocaleLabel}); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, cm := range list.Items {
		got[cm.Namespace+"/"+cm.Name] = cm.Data[messagesFileName(cm.Labels[messagesLocaleLabel])]
	}
	want := map[string]string{
		"console/" + messagesConfigMapName(localeTestBase, "fr"): `{"search.app-stage-x.title":"X"}`,
		"boot/" + messagesConfigMapName("stage", "fr"):           `{"search.app-stage-x.title":"X"}`,
		"elsewhere/" + elsewhere.Name:                            "{}",
		"console/" + foreignOwner.Name:                           "{}",
		"console/" + otherEnv.Name:                               "{}",
		"console/" + otherBase.Name:                              "{}",
	}
	if len(got) != len(want) {
		t.Errorf("messages ConfigMaps: got %v, want %v", got, want)
	}
	for k, v := range want {
		if data, ok := got[k]; !ok || data != v {
			t.Errorf("%s: got %q (present %v), want %q", k, data, ok, v)
		}
	}
}

// With several pending prunes, the reconcile is requeued for the earliest
// deadline.
func TestLocaleConfigMapRequeuesForEarliestDeadline(t *testing.T) {
	env := localeConfigMapTestEnv([]string{"console"}, english)
	since := func(ago time.Duration) map[string]string {
		return map[string]string{unadvertisedSinceAnnotation: time.Now().Add(-ago).UTC().Format(time.RFC3339)}
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		messagesConfigMap(env, "console", localeTestBase, "fr", since(localePruneGrace-10*time.Minute)),
		messagesConfigMap(env, "console", localeTestBase, "ja", since(localePruneGrace-3*time.Minute)),
		messagesConfigMap(env, "console", localeTestBase, "ko", since(localePruneGrace-7*time.Minute)),
	).Build()
	r := newLocaleReconciliation(c, env)
	setupLocales(t, r, nil)
	if r.requeueAfter <= 2*time.Minute || r.requeueAfter > 3*time.Minute {
		t.Errorf("want a requeue at the earliest deadline (~3m), got %v", r.requeueAfter)
	}
}

// A messages file moved into BinaryData must be moved back, not duplicated:
// the API server rejects a key present in both Data and BinaryData.
func TestLocaleConfigMapWriteClearsBinaryData(t *testing.T) {
	env := localeConfigMapTestEnv([]string{"console"}, english)
	stored := messagesConfigMap(env, "console", localeTestBase, "fr", nil)
	stored.Data = nil
	stored.BinaryData = map[string][]byte{messagesFileName("fr"): []byte("{}")}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(stored).Build()
	r := newLocaleReconciliation(c, env)

	nn := types.NamespacedName{Name: stored.Name, Namespace: stored.Namespace}
	if err := r.writeLocaleConfigMap(nn, "fr", "{}"); err != nil {
		t.Fatal(err)
	}
	got, err := getConfigMap(t, c, nn.Namespace, nn.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.BinaryData) != 0 {
		t.Errorf("BinaryData: got %v, want none", got.BinaryData)
	}
	if got.Data[messagesFileName("fr")] != "{}" {
		t.Errorf("Data: got %v", got.Data)
	}
}
