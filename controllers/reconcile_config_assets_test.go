package controllers

import (
	"context"
	"strings"
	"testing"
	"time"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newConfigAssetsReconciliation(valpopImage, cutoff string) *FrontendReconciliation {
	return &FrontendReconciliation{
		Frontend: &crd.Frontend{
			ObjectMeta: metav1.ObjectMeta{Name: "chrome", Namespace: "test-ns"},
			Spec:       crd.FrontendSpec{EnvName: "test-env"},
		},
		FrontendEnvironment: &crd.FrontendEnvironment{
			TypeMeta:   metav1.TypeMeta{APIVersion: "cloud.redhat.com/v1alpha1", Kind: "FrontendEnvironment"},
			ObjectMeta: metav1.ObjectMeta{Name: "test-env", UID: "test-uid"},
			Spec: crd.FrontendEnvironmentSpec{
				ValpopImage:                       valpopImage,
				EnableConfigAssets:                true,
				DeployCutoffTimestampConfigAssets: cutoff,
			},
		},
	}
}

func setS3Env(t *testing.T) {
	t.Helper()
	t.Setenv("PUSHCACHE_AWS_ACCESS_KEY_ID", "akid")
	t.Setenv("PUSHCACHE_AWS_SECRET_ACCESS_KEY", "secretkey")
	t.Setenv("PUSHCACHE_AWS_BUCKET_NAME", "mybucket")
	t.Setenv("PUSHCACHE_AWS_REGION", "us-east-1")
	t.Setenv("PUSHCACHE_AWS_ENDPOINT", "s3.example.com")
	t.Setenv("PUSHCACHE_AWS_PORT", "9000")
}

func TestBuildConfigAssetsJob(t *testing.T) {
	setS3Env(t)
	r := newConfigAssetsReconciliation("quay.io/valpop:test", "")
	nn := types.NamespacedName{Name: r.generateConfigAssetsJobName(), Namespace: r.Frontend.Namespace}

	job, err := r.buildConfigAssetsJob(nn, "hash123", "quay.io/valpop:test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Name != "test-env-config-assets" {
		t.Errorf("job name = %q, want test-env-config-assets", job.Name)
	}
	if job.Namespace != "test-ns" {
		t.Errorf("job namespace = %q, want test-ns", job.Namespace)
	}

	// Owned by the FrontendEnvironment, not the Frontend.
	refs := job.GetOwnerReferences()
	if len(refs) != 1 || refs[0].Kind != "FrontendEnvironment" || refs[0].Name != "test-env" {
		t.Fatalf("owner references = %+v, want single FrontendEnvironment/test-env", refs)
	}

	if got := *job.Spec.Completions; got != 1 {
		t.Errorf("completions = %d, want 1", got)
	}
	if job.Spec.Template.Spec.RestartPolicy != v1.RestartPolicyNever {
		t.Errorf("restartPolicy = %q, want Never", job.Spec.Template.Spec.RestartPolicy)
	}

	ann := job.Spec.Template.ObjectMeta.Annotations
	if ann["config-hash"] != "hash123" {
		t.Errorf("config-hash annotation = %q, want hash123", ann["config-hash"])
	}
	if ann["valpop-image"] != "quay.io/valpop:test" {
		t.Errorf("valpop-image annotation = %q, want quay.io/valpop:test", ann["valpop-image"])
	}

	containers := job.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("got %d containers, want 1", len(containers))
	}
	c := containers[0]
	if c.Image != "quay.io/valpop:test" {
		t.Errorf("container image = %q, want quay.io/valpop:test", c.Image)
	}

	// Mirrors the pushcache valpop invocation, with the app (-r) set to "config" and
	// the source (-s) pointing at the mounted config ConfigMap.
	cmd := strings.Join(c.Command, " ")
	for _, want := range []string{
		"valpop populate -r config",
		"-s /config",
		"-i quay.io/valpop:test",
		"--valpop-image quay.io/valpop:test",
		"--timeout 172800",
		"--min-asset-records 3",
		"--bucket mybucket",
		"--hostname s3.example.com",
		"--port 9000",
		"--username akid",
		"--password secretkey",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command missing %q\nfull command: %s", want, cmd)
		}
	}

	// The whole env ConfigMap is mounted at the source path.
	vols := job.Spec.Template.Spec.Volumes
	if len(vols) != 1 || vols[0].ConfigMap == nil || vols[0].ConfigMap.Name != "test-env" {
		t.Fatalf("volumes = %+v, want single ConfigMap volume for test-env", vols)
	}
	if len(c.VolumeMounts) != 1 || c.VolumeMounts[0].MountPath != configAssetsMountPath {
		t.Fatalf("volume mounts = %+v, want single mount at %s", c.VolumeMounts, configAssetsMountPath)
	}
}

func TestBuildConfigAssetsJobMissingS3Config(t *testing.T) {
	// No PUSHCACHE_AWS_* env and no in-cluster MinIO secret: config lookup must error
	// (rather than silently build a job that cannot upload).
	t.Setenv("PUSHCACHE_AWS_ACCESS_KEY_ID", "")
	r := newConfigAssetsReconciliation("quay.io/valpop:test", "")
	r.Ctx = context.Background()
	r.Client = fake.NewClientBuilder().WithScheme(scheme).Build()
	nn := types.NamespacedName{Name: r.generateConfigAssetsJobName(), Namespace: r.Frontend.Namespace}

	if _, err := r.buildConfigAssetsJob(nn, "hash123", "quay.io/valpop:test"); err == nil {
		t.Fatal("expected error when S3 config is unavailable, got nil")
	}
}

func TestIsConfigAssetsJobCurrent(t *testing.T) {
	makeJob := func(hash, valpopImg, created string) *batchv1.Job {
		j := &batchv1.Job{}
		if created != "" {
			ts, err := time.Parse(time.RFC3339, created)
			if err != nil {
				t.Fatalf("bad timestamp %q: %v", created, err)
			}
			j.CreationTimestamp = metav1.NewTime(ts)
		}
		j.Spec.Template.ObjectMeta.Annotations = map[string]string{
			"config-hash":  hash,
			"valpop-image": valpopImg,
		}
		return j
	}

	tests := []struct {
		name       string
		valpop     string
		cutoff     string
		jobHash    string
		jobValpop  string
		jobCreated string
		wantHash   string
		current    bool
	}{
		{"matching hash and image", "vp:1", "", "h1", "vp:1", "", "h1", true},
		{"content changed", "vp:1", "", "h1", "vp:1", "", "h2", false},
		{"valpop image changed", "vp:2", "", "h1", "vp:1", "", "h1", false},
		{"missing config-hash", "vp:1", "", "", "vp:1", "", "h1", false},
		{"behind cutoff", "vp:1", "2026-01-01T00:00:00Z", "h1", "vp:1", "2025-01-01T00:00:00Z", "h1", false},
		{"at or after cutoff", "vp:1", "2026-01-01T00:00:00Z", "h1", "vp:1", "2026-06-01T00:00:00Z", "h1", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newConfigAssetsReconciliation(tc.valpop, tc.cutoff)
			j := makeJob(tc.jobHash, tc.jobValpop, tc.jobCreated)
			if got := r.isConfigAssetsJobCurrent(j, tc.wantHash); got != tc.current {
				t.Errorf("isConfigAssetsJobCurrent = %v, want %v", got, tc.current)
			}
		})
	}
}

func TestGenerateConfigAssetsJobName(t *testing.T) {
	r := newConfigAssetsReconciliation("vp:1", "")
	if got := r.generateConfigAssetsJobName(); got != "test-env-config-assets" {
		t.Errorf("generateConfigAssetsJobName = %q, want test-env-config-assets", got)
	}
}
