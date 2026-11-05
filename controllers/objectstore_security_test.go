package controllers

import (
	"context"
	"os"
	"strings"
	"testing"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	apps "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func setObjectStoreTestEnv(t *testing.T, values map[string]string) {
	t.Helper()
	for key, value := range values {
		previous, existed := os.LookupEnv(key)
		if err := os.Setenv(key, value); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if existed {
				_ = os.Setenv(key, previous)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
}

func TestGetObjectStoreConfigReadsRotatedSecret(t *testing.T) {
	setObjectStoreTestEnv(t, map[string]string{
		"PUSHCACHE_AWS_ACCESS_KEY_ID":     "stale-access",
		"PUSHCACHE_AWS_SECRET_ACCESS_KEY": "stale-secret",
		"PUSHCACHE_AWS_BUCKET_NAME":       "frontend",
		"PUSHCACHE_AWS_REGION":            "us-east-1",
		"PUSHCACHE_AWS_ENDPOINT":          "minio.example.test",
		"PUSHCACHE_AWS_PORT":              "9000",
		"PUSHCACHE_AWS_SECRET_NAME":       "pushcache-aws-credentials",
		"POD_NAMESPACE":                   "operator-system",
	})

	scheme := runtime.NewScheme()
	if err := v1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "pushcache-aws-credentials", Namespace: "operator-system"},
		Data: map[string][]byte{
			"aws-access-key-id":     []byte("first-access"),
			"aws-secret-access-key": []byte("first-secret"),
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	config, err := getObjectStoreConfig(context.Background(), c, "frontend-env")
	if err != nil {
		t.Fatalf("get config from Secret: %v", err)
	}
	if *config.AccessKey != "first-access" || *config.SecretKey != "first-secret" {
		t.Fatalf("initial credentials = (%q, %q), want live Secret values", *config.AccessKey, *config.SecretKey)
	}
	proxyReconciliation := &ReverseProxyReconciliation{
		Client: c, Ctx: context.Background(), Namespace: "frontend-env",
		FrontendEnvironment: &crd.FrontendEnvironment{
			ObjectMeta: metav1.ObjectMeta{Name: "frontend-env"},
			Spec:       crd.FrontendEnvironmentSpec{EnablePushCache: true, ReverseProxyImage: "proxy:v1"},
		},
	}
	firstDeployment, err := proxyReconciliation.buildReverseProxyDeployment()
	if err != nil {
		t.Fatalf("build proxy deployment with initial Secret: %v", err)
	}
	firstChecksum := firstDeployment.Spec.Template.ObjectMeta.Annotations["pushcache-credentials-checksum"]

	secret.Data["aws-access-key-id"] = []byte("rotated-access")
	secret.Data["aws-secret-access-key"] = []byte("rotated-secret")
	if err := c.Update(context.Background(), secret); err != nil {
		t.Fatalf("update credentials Secret: %v", err)
	}
	config, err = getObjectStoreConfig(context.Background(), c, "frontend-env")
	if err != nil {
		t.Fatalf("get config after Secret rotation: %v", err)
	}
	if *config.AccessKey != "rotated-access" || *config.SecretKey != "rotated-secret" {
		t.Fatalf("rotated credentials = (%q, %q), want latest Secret values", *config.AccessKey, *config.SecretKey)
	}
	rotatedDeployment, err := proxyReconciliation.buildReverseProxyDeployment()
	if err != nil {
		t.Fatalf("build proxy deployment after Secret rotation: %v", err)
	}
	rotatedChecksum := rotatedDeployment.Spec.Template.ObjectMeta.Annotations["pushcache-credentials-checksum"]
	if firstChecksum == rotatedChecksum {
		t.Fatal("proxy Pod-template checksum did not change after input Secret rotation")
	}
}

func TestCommandOnlyLegacyPushcacheJobMigratesToManagedSecret(t *testing.T) {
	setObjectStoreTestEnv(t, map[string]string{
		"PUSHCACHE_AWS_ACCESS_KEY_ID":     "current-access",
		"PUSHCACHE_AWS_SECRET_ACCESS_KEY": "current-secret",
		"PUSHCACHE_AWS_BUCKET_NAME":       "frontend",
		"PUSHCACHE_AWS_REGION":            "us-east-1",
		"PUSHCACHE_AWS_ENDPOINT":          "minio.example.test",
		"PUSHCACHE_AWS_PORT":              "9000",
		"PUSHCACHE_AWS_SECRET_NAME":       "",
	})

	scheme := runtime.NewScheme()
	if err := v1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	legacyJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend-frontend-pushcache", Namespace: "frontend-ns"},
		Spec: batchv1.JobSpec{Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"frontend-image": "frontend:v1", "valpop-image": "valpop:v1"}},
			Spec: v1.PodSpec{Containers: []v1.Container{{
				Name:    "valpop-pushcache",
				Command: []string{"/bin/bash", "-c", "valpop populate --username legacy-access --password legacy-secret"},
			}}},
		}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(legacyJob).Build()
	frontend := &crd.Frontend{ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "frontend-ns"}, Spec: crd.FrontendSpec{Image: "frontend:v1"}}
	environment := &crd.FrontendEnvironment{Spec: crd.FrontendEnvironmentSpec{ValpopImage: "valpop:v1"}}
	r := &FrontendReconciliation{Client: c, Ctx: context.Background(), Frontend: frontend, FrontendEnvironment: environment}

	managed, err := r.manageExistingJob(legacyJob.Name)
	if err != nil {
		t.Fatalf("manage legacy Job: %v", err)
	}
	if managed {
		t.Fatal("command-only legacy Job was reported as managed")
	}
	if err := c.Get(context.Background(), types.NamespacedName{Name: legacyJob.Name, Namespace: legacyJob.Namespace}, &batchv1.Job{}); err == nil {
		t.Fatal("legacy Job still exists after migration was requested")
	}

	replacement := &batchv1.Job{}
	if err := r.populatePushCacheContainer(replacement); err != nil {
		t.Fatalf("populate replacement Job: %v", err)
	}
	managedSecret := &v1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: PushCacheCredentialsSecretName, Namespace: frontend.Namespace}, managedSecret); err != nil {
		t.Fatalf("managed credentials Secret was not created: %v", err)
	}
	accessKey := managedSecret.Data["aws-access-key-id"]
	secretKey := managedSecret.Data["aws-secret-access-key"]
	if len(accessKey) == 0 {
		accessKey = []byte(managedSecret.StringData["aws-access-key-id"])
	}
	if len(secretKey) == 0 {
		secretKey = []byte(managedSecret.StringData["aws-secret-access-key"])
	}
	if string(accessKey) != "current-access" || string(secretKey) != "current-secret" {
		t.Fatalf("managed Secret has unexpected credentials: %#v", managedSecret.Data)
	}

	container := replacement.Spec.Template.Spec.Containers[0]
	if !isJobUsingSecretKeyRef(&batchv1.Job{Spec: batchv1.JobSpec{Template: replacement.Spec.Template}}) {
		t.Fatal("replacement Job does not reference both managed Secret keys")
	}
	if strings.Contains(strings.Join(container.Command, " "), "current-access") || strings.Contains(strings.Join(container.Command, " "), "current-secret") {
		t.Fatal("replacement Job command contains literal credentials")
	}
}

func TestObjectStoreSecretUpdatesEnqueueAffectedResources(t *testing.T) {
	setObjectStoreTestEnv(t, map[string]string{
		"PUSHCACHE_AWS_SECRET_NAME": "",
		"POD_NAMESPACE":             "operator-system",
	})
	scheme := runtime.NewScheme()
	if err := v1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := crd.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	frontendA := &crd.Frontend{ObjectMeta: metav1.ObjectMeta{Name: "app-a", Namespace: "apps-a"}, Spec: crd.FrontendSpec{EnvName: "team-a"}}
	frontendB := &crd.Frontend{ObjectMeta: metav1.ObjectMeta{Name: "app-b", Namespace: "apps-b"}, Spec: crd.FrontendSpec{EnvName: "team-b"}}
	environmentA := &crd.FrontendEnvironment{ObjectMeta: metav1.ObjectMeta{Name: "team-a"}, Spec: crd.FrontendEnvironmentSpec{EnablePushCache: true, ReverseProxyImage: "proxy:v1"}}
	environmentB := &crd.FrontendEnvironment{ObjectMeta: metav1.ObjectMeta{Name: "team-b"}, Spec: crd.FrontendEnvironmentSpec{EnablePushCache: true, ReverseProxyImage: "proxy:v1"}}
	environmentC := &crd.FrontendEnvironment{ObjectMeta: metav1.ObjectMeta{Name: "team-c"}, Spec: crd.FrontendEnvironmentSpec{EnablePushCache: false, ReverseProxyImage: "proxy:v1"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(frontendA, frontendB, environmentA, environmentB, environmentC).Build()

	frontendReconciler := &FrontendReconciler{Client: c, Log: logr.Discard()}
	proxyReconciler := &ReverseProxyController{Client: c, Log: logr.Discard()}
	clowderSecret := &v1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "env-team-a-minio", Namespace: "team-a"}}
	frontendRequests := frontendReconciler.frontendsToEnqueueUponObjectStoreSecretUpdate()(context.Background(), clowderSecret)
	if len(frontendRequests) != 1 || frontendRequests[0].Name != "app-a" || frontendRequests[0].Namespace != "apps-a" {
		t.Fatalf("Clowder Secret enqueued Frontends = %#v, want only app-a", frontendRequests)
	}
	proxyRequests := proxyReconciler.environmentsToEnqueueUponObjectStoreSecretUpdate()(context.Background(), clowderSecret)
	if len(proxyRequests) != 1 || proxyRequests[0].Name != "team-a" {
		t.Fatalf("Clowder Secret enqueued environments = %#v, want only team-a", proxyRequests)
	}

	if err := os.Setenv("PUSHCACHE_AWS_SECRET_NAME", "pushcache-aws-credentials"); err != nil {
		t.Fatal(err)
	}
	inputSecret := &v1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pushcache-aws-credentials", Namespace: "operator-system"}}
	frontendRequests = frontendReconciler.frontendsToEnqueueUponObjectStoreSecretUpdate()(context.Background(), inputSecret)
	if len(frontendRequests) != 2 {
		t.Fatalf("configured credentials Secret enqueued %d Frontends, want 2", len(frontendRequests))
	}
	proxyRequests = proxyReconciler.environmentsToEnqueueUponObjectStoreSecretUpdate()(context.Background(), inputSecret)
	if len(proxyRequests) != 2 {
		t.Fatalf("configured credentials Secret enqueued %d enabled reverse proxies, want 2", len(proxyRequests))
	}
}

func TestRemoveObsoleteCredentialReplicaSetsWaitsForSafeRollout(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := apps.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := v1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	deployment := &apps.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "reverse-proxy", Namespace: "env-a", UID: types.UID("deployment-uid"), Generation: 2},
		Spec:       apps.DeploymentSpec{Replicas: int32Ptr(3)},
		Status: apps.DeploymentStatus{
			ObservedGeneration: 1,
			Replicas:           3,
			UpdatedReplicas:    2,
			AvailableReplicas:  2,
		},
	}
	owner := metav1.OwnerReference{APIVersion: "apps/v1", Kind: "Deployment", Name: deployment.Name, UID: deployment.UID, Controller: boolPtr(true)}
	legacyReplicaSet := &apps.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Name: "reverse-proxy-old", Namespace: deployment.Namespace, OwnerReferences: []metav1.OwnerReference{owner}},
		Spec: apps.ReplicaSetSpec{
			Replicas: int32Ptr(0),
			Template: v1.PodTemplateSpec{Spec: v1.PodSpec{Containers: []v1.Container{{Name: "reverse-proxy", Env: []v1.EnvVar{
				{Name: "PUSHCACHE_AWS_ACCESS_KEY_ID", Value: "old-access"},
				{Name: "PUSHCACHE_AWS_SECRET_ACCESS_KEY", Value: "old-secret"},
			}}}}},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(legacyReplicaSet).Build()
	r := &ReverseProxyReconciliation{Client: c, Ctx: context.Background(), Namespace: deployment.Namespace}

	pending, err := r.removeObsoleteCredentialReplicaSets(deployment)
	if err != nil {
		t.Fatalf("check incomplete rollout: %v", err)
	}
	if !pending {
		t.Fatal("expected credential-bearing history to remain pending before rollout completes")
	}
	if err := c.Get(context.Background(), types.NamespacedName{Name: legacyReplicaSet.Name, Namespace: legacyReplicaSet.Namespace}, &apps.ReplicaSet{}); err != nil {
		t.Fatalf("ReplicaSet was deleted before rollout completed: %v", err)
	}

	deployment.Status.ObservedGeneration = deployment.Generation
	deployment.Status.UpdatedReplicas = 3
	deployment.Status.AvailableReplicas = 3
	pending, err = r.removeObsoleteCredentialReplicaSets(deployment)
	if err != nil {
		t.Fatalf("remove old zero-replica history after rollout: %v", err)
	}
	if pending {
		t.Fatal("credential-bearing zero-replica history should be removed after safe rollout")
	}
	if err := c.Get(context.Background(), types.NamespacedName{Name: legacyReplicaSet.Name, Namespace: legacyReplicaSet.Namespace}, &apps.ReplicaSet{}); err == nil {
		t.Fatal("obsolete credential-bearing ReplicaSet remains after safe rollout")
	}
}

func int32Ptr(value int32) *int32 { return &value }

func boolPtr(value bool) *bool { return &value }
