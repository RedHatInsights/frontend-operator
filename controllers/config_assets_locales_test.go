package controllers

import (
	"maps"
	"testing"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	resCache "github.com/RedHatInsights/rhc-osdk-utils/resourceCache"
	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestConfigAssetsLocalePayload(t *testing.T) {
	r := configAssetsReconciliation(t)
	r.configAssets["locales.json"] = `[{"id":"en"},{"id":"fr","file":"messages.fr.json"}]`
	r.configAssets["messages.fr.json"] = `{"bundle.iam.title":"Identité"}`
	// Disabled translations are also staged, matching generated ConfigMaps.
	r.configAssets["messages.ja.json"] = `{"bundle.iam.title":"ID"}`
	job := configAssetsJob(t, r)
	setConfigAssetsJobStatus(t, r, job, batchv1.JobComplete)
	cm := &v1.ConfigMap{}
	key := types.NamespacedName{Name: job.Name, Namespace: job.Namespace}
	if err := r.Client.Get(r.Ctx, key, cm); err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(cm.Data, r.configAssets) {
		t.Fatalf("lost locale payload: %v", cm.Data)
	}
	noop := configAssetsJob(t, r)
	if noop.ResourceVersion != job.ResourceVersion {
		t.Fatal("unchanged locale data recreated Job")
	}
	r.configAssets["messages.fr.json"] = `{"bundle.iam.title":"Comptes"}`
	updated := configAssetsJob(t, r)
	if updated.Spec.Template.Annotations[configAssetsHashAnnotation] == job.Spec.Template.Annotations[configAssetsHashAnnotation] {
		t.Fatal("translation-only change did not refresh upload identity")
	}
	if updated.Spec.Template.Spec.Containers[0].Image != job.Spec.Template.Spec.Containers[0].Image {
		t.Fatal("translation change required a new image")
	}
	setConfigAssetsJobStatus(t, r, updated, batchv1.JobComplete)
	r.configAssets["locales.json"] = `[{"id":"en"}]`
	delete(r.configAssets, "messages.fr.json")
	removed := configAssetsJob(t, r)
	if err := r.Client.Get(r.Ctx, key, cm); err != nil {
		t.Fatal(err)
	}
	if cm.Data["messages.fr.json"] != "" || cm.Data["locales.json"] != `[{"id":"en"}]` || removed.Spec.Template.Annotations[configAssetsHashAnnotation] == updated.Spec.Template.Annotations[configAssetsHashAnnotation] {
		t.Fatal("removed locale remained in publication")
	}
	for _, manifest := range []string{`[{"id":"fr","file":"messages.fr.json"}]`, `[{"id":"fr","file":"../other.json"}]`, `{}`} {
		r.configAssets["locales.json"] = manifest
		if _, err := selectConfigAssets(r.configAssets); err == nil {
			t.Fatalf("accepted invalid/incomplete manifest: %s", manifest)
		}
	}
}

func TestGeneratedLocaleAssetsReachPublication(t *testing.T) {
	// Exercise run(), not a hand-assembled payload, so a missing handoff from
	// setupConfigMaps to publication cannot pass the hash/lifecycle tests.
	r := configAssetsReconciliation(t)
	r.FrontendEnvironment = localeConfigMapTestEnv(nil, english)
	r.FrontendEnvironment.Spec.EnablePushCache = true
	r.FrontendEnvironment.Spec.ValpopImage = "quay.io/valpop:test"
	r.FrontendEnvironment.Spec.DisableContainerDeployments = true
	r.Frontend = localeMessagesTestFrontend(nil)
	r.Frontend.Spec.Locales = crd.LocaleCatalogs{"fr": {moduleTitleKey: "Application"}}
	r.Client = newLocaleTestClient(r.Frontend.DeepCopy())
	log := logr.Discard()
	r.Cache = resCache.NewObjectCache(r.Ctx, r.Client, &log, resCache.NewCacheConfig(scheme, nil, nil, resCache.Options{}))
	r.FRE = &FrontendReconciler{Client: r.Client}
	if err := r.run(); err != nil {
		t.Fatal(err)
	}
	files, err := selectConfigAssets(r.configAssets)
	if err != nil {
		t.Fatal(err)
	}
	if files["messages.fr.json"] != `{"module.app.defaultDocumentTitle":"Application"}` || files["locales.json"] == "" {
		t.Fatalf("generated locale assets did not reach publication: %v", files)
	}
}
