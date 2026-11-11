package controllers

import (
	"context"
	"encoding/json"
	"maps"
	"strings"
	"testing"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func configAssetsReconciliation(t *testing.T) *FrontendReconciliation {
	t.Helper()
	for key, value := range map[string]string{
		"PUSHCACHE_AWS_ACCESS_KEY_ID": "test-access", "PUSHCACHE_AWS_SECRET_ACCESS_KEY": "test-secret",
		"PUSHCACHE_AWS_BUCKET_NAME": "frontend", "PUSHCACHE_AWS_REGION": "us-east-1",
		"PUSHCACHE_AWS_ENDPOINT": "s3.example.com", "PUSHCACHE_AWS_PORT": "443",
		"PUSHCACHE_AWS_SECRET_NAME": "",
	} {
		t.Setenv(key, value)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&batchv1.Job{}).Build()
	return &FrontendReconciliation{
		Ctx: context.Background(), Client: c, Log: logr.Discard(),
		Frontend: &crd.Frontend{ObjectMeta: metav1.ObjectMeta{Name: "chrome", Namespace: "test-ns"}, Spec: crd.FrontendSpec{EnvName: "test-env"}},
		FrontendEnvironment: &crd.FrontendEnvironment{
			TypeMeta:   metav1.TypeMeta{APIVersion: "cloud.redhat.com/v1alpha1", Kind: "FrontendEnvironment"},
			ObjectMeta: metav1.ObjectMeta{Name: "test-env", UID: "test-env-uid"},
			Spec:       crd.FrontendEnvironmentSpec{EnableConfigAssets: true, ValpopImage: "quay.io/valpop:test", DisableContainerDeployments: true},
		},
		configAssets: map[string]string{"fed-modules.json": `{"chrome":{"manifestLocation":"/apps/chrome/fed-mods.json"}}`, "sso-config.json": `{}`},
	}
}

func configAssetsJob(t *testing.T, r *FrontendReconciliation) *batchv1.Job {
	t.Helper()
	nn := types.NamespacedName{Name: r.generateConfigAssetsJobName(), Namespace: r.Frontend.Namespace}
	for range 4 {
		if _, err := r.reconcileConfigAssetsJob(); err != nil {
			t.Fatal(err)
		}
		job := &batchv1.Job{}
		if err := r.Client.Get(r.Ctx, nn, job); err == nil {
			return job
		} else if !k8serr.IsNotFound(err) {
			t.Fatal(err)
		}
	}
	t.Fatal("config upload Job was not created")
	return nil
}

func setConfigAssetsJobStatus(t *testing.T, r *FrontendReconciliation, job *batchv1.Job, condition batchv1.JobConditionType) {
	t.Helper()
	job.Status.Conditions = []batchv1.JobCondition{{Type: condition, Status: v1.ConditionTrue, Reason: "test"}}
	if err := r.Client.Status().Update(r.Ctx, job); err != nil {
		t.Fatal(err)
	}
}

func TestSelectConfigAssets(t *testing.T) {
	input := map[string]string{"fed-modules.json": "{\n}\n", "exports.json": "[ ]\n", "Caddyfile": "not JSON", "locales.json": `[]`}
	files, err := selectConfigAssets(input)
	if err != nil || !maps.Equal(files, map[string]string{"fed-modules.json": "{\n}\n", "exports.json": "[ ]\n"}) {
		t.Fatalf("selected files: %v, error: %v", files, err)
	}
	for _, bad := range []map[string]string{
		nil, {}, {"fed-modules.json": "null"}, {"fed-modules.json": "[]"},
		{"fed-modules.json": "broken"}, {"fed-modules.json": `{}`, "sso-config.json": "broken"},
	} {
		if _, err := selectConfigAssets(bad); err == nil {
			t.Errorf("accepted missing/invalid input: %v", bad)
		}
	}
}

func TestConfigAssetsContentRefreshAndNoOp(t *testing.T) {
	r := configAssetsReconciliation(t)
	first := configAssetsJob(t, r)
	setConfigAssetsJobStatus(t, r, first, batchv1.JobComplete)
	// Changes to files outside the publication inventory must not cause an upload.
	r.configAssets["Caddyfile"] = "new proxy settings"
	r.configAssets["locales.json"] = `[{"id":"fr"}]`
	result, err := r.reconcileConfigAssetsJob()
	if err != nil || result.RequeueAfter != 0 {
		t.Fatalf("completed unchanged Job should be a no-op: %+v, %v", result, err)
	}
	unchanged := configAssetsJob(t, r)
	if unchanged.ResourceVersion != first.ResourceVersion {
		t.Fatal("unchanged publication rewrote the Job")
	}
	r.configAssets["fed-modules.json"] = `{"inventory":{"manifestLocation":"/apps/inventory/fed-mods.json"}}`
	updated := configAssetsJob(t, r)
	oldContainer, newContainer := first.Spec.Template.Spec.Containers[0], updated.Spec.Template.Spec.Containers[0]
	if newContainer.Image != oldContainer.Image || updated.Spec.Template.Annotations["config-hash"] == first.Spec.Template.Annotations["config-hash"] {
		t.Fatal("config-only change must refresh without an image change")
	}
	if strings.Join(newContainer.Args, " ") == strings.Join(oldContainer.Args, " ") {
		t.Fatal("config-only change did not update valpop upload identity")
	}
	cm := &v1.ConfigMap{}
	if err := r.Client.Get(r.Ctx, types.NamespacedName{Name: updated.Name, Namespace: updated.Namespace}, cm); err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"fed-modules.json": `{"inventory":{"manifestLocation":"/apps/inventory/fed-mods.json"}}`,
		"sso-config.json":  `{}`,
	}
	if cm.Immutable == nil || !*cm.Immutable || !maps.Equal(cm.Data, expected) {
		t.Fatalf("snapshot does not match publication: %+v", cm)
	}
}

func TestConfigAssetsWaitForOldPodsBeforeReplacingSnapshot(t *testing.T) {
	r := configAssetsReconciliation(t)
	job := configAssetsJob(t, r)
	deletes := 0
	r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if _, ok := obj.(*batchv1.Job); ok {
				deletes++
				options := (&client.DeleteOptions{}).ApplyOptions(opts)
				if options.PropagationPolicy == nil || *options.PropagationPolicy != metav1.DeletePropagationForeground {
					t.Fatal("old upload pods must be deleted before replacing the snapshot")
				}
			}
			return c.Delete(ctx, obj, opts...)
		},
	})
	// The fake client has no garbage collector; hold the Job while checking
	// both the requested propagation policy and the snapshot's lifetime.
	job.Finalizers = []string{metav1.FinalizerDeleteDependents}
	if err := r.Client.Update(r.Ctx, job); err != nil {
		t.Fatal(err)
	}
	oldData := maps.Clone(r.configAssets)
	r.configAssets["fed-modules.json"] = `{"next":{}}`
	for range 2 {
		result, err := r.reconcileConfigAssetsJob()
		if err != nil || result.RequeueAfter == 0 {
			t.Fatalf("must wait for foreground deletion: %+v, %v", result, err)
		}
		cm := &v1.ConfigMap{}
		if err := r.Client.Get(r.Ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, cm); err != nil {
			t.Fatal(err)
		}
		if !maps.Equal(cm.Data, oldData) {
			t.Fatal("changed the snapshot while the old upload could still be running")
		}
	}
	if deletes != 1 {
		t.Fatalf("expected one foreground delete request, got %d", deletes)
	}
}

func TestConfigAssetsFailureRetriesAndDisable(t *testing.T) {
	r := configAssetsReconciliation(t)
	job := configAssetsJob(t, r)
	setConfigAssetsJobStatus(t, r, job, batchv1.JobFailed)
	if _, err := r.reconcileConfigAssetsJob(); err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("failed upload must be surfaced, got %v", err)
	}
	retry := configAssetsJob(t, r)
	if retry.Spec.Template.Annotations["config-publication"] != job.Spec.Template.Annotations["config-publication"] {
		t.Fatal("retry changed the content identity")
	}
	r.FrontendEnvironment.Spec.EnableConfigAssets = false
	for range 2 {
		if _, err := r.reconcileConfigAssetsJob(); err != nil {
			t.Fatal(err)
		}
	}
	nn := types.NamespacedName{Name: job.Name, Namespace: job.Namespace}
	if err := r.Client.Get(r.Ctx, nn, &batchv1.Job{}); !k8serr.IsNotFound(err) {
		t.Fatalf("disabled publisher retained its Job: %v", err)
	}
	if err := r.Client.Get(r.Ctx, nn, &v1.ConfigMap{}); !k8serr.IsNotFound(err) {
		t.Fatalf("disabled publisher retained its snapshot: %v", err)
	}
}

func TestConfigAssetsCutoffForcesUpload(t *testing.T) {
	r := configAssetsReconciliation(t)
	first := configAssetsJob(t, r)
	setConfigAssetsJobStatus(t, r, first, batchv1.JobComplete)
	r.FrontendEnvironment.Spec.DeployCutoffTimestampConfigAssets = "2026-11-11T12:00:00Z"
	forced := configAssetsJob(t, r)
	if forced.Spec.Template.Annotations["config-hash"] != first.Spec.Template.Annotations["config-hash"] ||
		forced.Spec.Template.Annotations["config-publication"] == first.Spec.Template.Annotations["config-publication"] {
		t.Fatal("manual refresh must bypass upload deduplication without changing the content hash")
	}
}

func TestConfigAssetsCredentialsAndCache(t *testing.T) {
	r := configAssetsReconciliation(t)
	job := configAssetsJob(t, r)
	container := job.Spec.Template.Spec.Containers[0]
	encoded, _ := json.Marshal(job)
	if strings.Contains(string(encoded), "test-access") || strings.Contains(string(encoded), "test-secret") {
		t.Fatal("Job exposed literal credentials")
	}
	if !strings.Contains(strings.Join(container.Args, " "), "--cache-max-age 60") {
		t.Fatal("configuration must use a short cache lifetime")
	}
	expected := map[string]string{
		"PUSHCACHE_AWS_ACCESS_KEY_ID":     "aws-access-key-id",
		"PUSHCACHE_AWS_SECRET_ACCESS_KEY": "aws-secret-access-key",
	}
	if len(container.Env) != len(expected) {
		t.Fatalf("both credential variables must be wired, got %v", container.Env)
	}
	for _, env := range container.Env {
		key, ok := expected[env.Name]
		if !ok || env.Value != "" || env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
			t.Fatalf("invalid credential environment variable: %v", env)
		}
		ref := env.ValueFrom.SecretKeyRef
		if ref.Name != "pushcache-s3-credentials" || ref.Key != key {
			t.Fatalf("incorrect credential Secret reference: %v", ref)
		}
		delete(expected, env.Name)
	}
}

func TestConfigAssetsDestinationChangeForcesUpload(t *testing.T) {
	r := configAssetsReconciliation(t)
	t.Setenv("PUSHCACHE_AWS_BUCKET_NAME", "stage-frontend")
	first := configAssetsJob(t, r)
	setConfigAssetsJobStatus(t, r, first, batchv1.JobComplete)
	t.Setenv("PUSHCACHE_AWS_BUCKET_NAME", "prod-frontend")
	updated := configAssetsJob(t, r)
	if updated.Spec.Template.Annotations["config-hash"] != first.Spec.Template.Annotations["config-hash"] ||
		updated.Spec.Template.Annotations["config-publication"] == first.Spec.Template.Annotations["config-publication"] {
		t.Fatal("a different destination must bypass deduplication even with identical content")
	}
	for _, item := range []struct {
		job    *batchv1.Job
		bucket string
	}{{first, "stage-frontend"}, {updated, "prod-frontend"}} {
		args := strings.Join(item.job.Spec.Template.Spec.Containers[0].Args, " ")
		if !strings.Contains(args, "--bucket "+item.bucket+" ") {
			t.Fatalf("upload does not target %s: %s", item.bucket, args)
		}
	}
}

func TestConfigAssetsDisabledIgnoresUnownedObjectsAndUsesCache(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "foreign objects"}[collision], func(t *testing.T) {
			r := configAssetsReconciliation(t)
			r.FrontendEnvironment.Spec.EnableConfigAssets = false
			nn := types.NamespacedName{Name: r.generateConfigAssetsJobName(), Namespace: r.Frontend.Namespace}
			if collision {
				for _, obj := range []client.Object{&batchv1.Job{}, &v1.ConfigMap{}} {
					obj.SetName(nn.Name)
					obj.SetNamespace(nn.Namespace)
					if err := r.Client.Create(r.Ctx, obj); err != nil {
						t.Fatal(err)
					}
				}
			}
			r.APIReader = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
				Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
					t.Fatal("disabled publisher must not bypass the cache")
					return nil
				},
			})
			result, err := r.reconcileConfigAssetsJob()
			if err != nil || result.RequeueAfter != 0 {
				t.Fatalf("disabled publisher must be a no-op: %+v, %v", result, err)
			}
			if collision {
				for _, obj := range []client.Object{&batchv1.Job{}, &v1.ConfigMap{}} {
					if err := r.Client.Get(r.Ctx, nn, obj); err != nil {
						t.Fatalf("modified an unowned resource: %v", err)
					}
				}
			}
		})
	}
}

func TestConfigAssetsCleanupAfterLastFrontendLeaves(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(map[bool]string{false: "envName changed", true: "deleted"}[deleted], func(t *testing.T) {
			r := configAssetsReconciliation(t)
			job := configAssetsJob(t, r)
			job.Finalizers = []string{metav1.FinalizerDeleteDependents}
			if err := r.Client.Update(r.Ctx, job); err != nil {
				t.Fatal(err)
			}
			first := r.Frontend.DeepCopy()
			second := r.Frontend.DeepCopy()
			second.Name = "another-frontend"
			for _, frontend := range []*crd.Frontend{first, second} {
				if err := r.Client.Create(r.Ctx, frontend); err != nil {
					t.Fatal(err)
				}
			}
			reconciler := &FrontendReconciler{Client: r.Client, APIReader: r.Client, Scheme: scheme}
			leave := func(frontend *crd.Frontend) {
				t.Helper()
				var err error
				if deleted {
					err = r.Client.Delete(r.Ctx, frontend)
				} else {
					frontend.Spec.EnvName = "other-env"
					err = r.Client.Update(r.Ctx, frontend)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			leave(first)
			result, err := reconciler.pruneOrphanedConfigAssets(r.Ctx, r.Frontend.Namespace)
			if err != nil || result.RequeueAfter != 0 {
				t.Fatalf("another Frontend still uses this upload: %+v, %v", result, err)
			}
			if err := r.Client.Get(r.Ctx, client.ObjectKeyFromObject(job), job); err != nil || !job.DeletionTimestamp.IsZero() {
				t.Fatalf("deleted a shared upload: %v", err)
			}
			leave(second)
			for range 2 {
				result, err = reconciler.pruneOrphanedConfigAssets(r.Ctx, r.Frontend.Namespace)
				if err != nil || result.RequeueAfter == 0 {
					t.Fatalf("must wait for the orphaned upload's pods: %+v, %v", result, err)
				}
				if err := r.Client.Get(r.Ctx, client.ObjectKeyFromObject(job), &v1.ConfigMap{}); err != nil {
					t.Fatalf("removed snapshot before the uploader: %v", err)
				}
			}
			if err := r.Client.Get(r.Ctx, client.ObjectKeyFromObject(job), job); err != nil {
				t.Fatal(err)
			}
			job.Finalizers = nil
			if err := r.Client.Update(r.Ctx, job); err != nil {
				t.Fatal(err)
			}
			result, err = reconciler.pruneOrphanedConfigAssets(r.Ctx, r.Frontend.Namespace)
			if err != nil || result.RequeueAfter != 0 {
				t.Fatalf("orphan cleanup failed: %+v, %v", result, err)
			}
			if err := r.Client.Get(r.Ctx, client.ObjectKeyFromObject(job), &v1.ConfigMap{}); !k8serr.IsNotFound(err) {
				t.Fatalf("orphaned snapshot remains: %v", err)
			}
		})
	}
}
