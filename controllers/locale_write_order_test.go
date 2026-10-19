package controllers

import (
	"context"
	"slices"
	"testing"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	resCache "github.com/RedHatInsights/rhc-osdk-utils/resourceCache"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// Activation order: every messages ConfigMap is written before any base
// ConfigMap that advertises it, so locales.json never points at a missing file.
func TestLocaleWriteOrder(t *testing.T) {
	ctx := context.Background()
	env := &crd.FrontendEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "stage", UID: "stage-uid"},
		Spec: crd.FrontendEnvironmentSpec{
			SSO:              "https://sso.example.com/auth",
			TargetNamespaces: []string{"console", "console-2"},
			EnabledLocales:   []crd.EnabledLocale{{ID: "en", Name: "English", Default: true}, {ID: "fr", Name: "Français"}},
			Locales:          crd.LocaleCatalogs{"fr": {"bundles.iam.title": "IAM"}},
		},
	}
	fe := crd.Frontend{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "boot"},
		Spec:       crd.FrontendSpec{EnvName: "stage", FeoConfigEnabled: true, Module: &crd.FedModule{}},
	}

	writes := []string{}
	record := func(obj client.Object) {
		if _, ok := obj.(*v1.ConfigMap); ok {
			writes = append(writes, obj.GetNamespace()+"/"+obj.GetName())
		}
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(fe.DeepCopy()).
		WithIndex(&crd.Frontend{}, "spec.envName", func(o client.Object) []string { return []string{o.(*crd.Frontend).Spec.EnvName} }).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				record(obj)
				return c.Create(ctx, obj, opts...)
			},
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				record(obj)
				return c.Update(ctx, obj, opts...)
			},
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				record(obj)
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).Build()

	log := logr.Discard()
	cache := resCache.NewObjectCache(ctx, c, &log, resCache.NewCacheConfig(scheme, nil, nil, resCache.Options{}))
	cache.AddPossibleGVKFromIdent(CoreConfig)
	r := &FrontendReconciliation{
		Ctx: ctx, Client: c, Log: log, Cache: cache,
		FRE:                 &FrontendReconciler{Client: c},
		FrontendEnvironment: env,
		Frontend:            &fe,
	}
	if _, err := r.setupConfigMaps(); err != nil {
		t.Fatal(err)
	}
	if err := cache.ApplyAll(); err != nil {
		t.Fatal(err)
	}

	lastMessages, firstBase := -1, -1
	for i, w := range writes {
		switch w {
		case "console/feo-context-cfg-messages-fr", "console-2/feo-context-cfg-messages-fr":
			lastMessages = i
		case "boot/stage", "console/feo-context-cfg", "console-2/feo-context-cfg":
			if firstBase < 0 {
				firstBase = i
			}
		}
	}
	if lastMessages < 0 || firstBase < 0 || lastMessages > firstBase {
		t.Errorf("messages must be written before any base ConfigMap, writes: %v", writes)
	}
	for _, want := range []string{"console/feo-context-cfg-messages-fr", "console-2/feo-context-cfg-messages-fr", "boot/stage", "console/feo-context-cfg", "console-2/feo-context-cfg"} {
		if !slices.Contains(writes, want) {
			t.Errorf("missing write %s in %v", want, writes)
		}
	}
}
