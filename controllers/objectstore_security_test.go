package controllers

import (
	"context"
	"os"
	"strings"
	"testing"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/yaml"
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

	managed, err := r.manageExistingJob(legacyJob.Name, "")
	if err != nil {
		t.Fatalf("manage legacy Job: %v", err)
	}
	if managed {
		t.Fatal("command-only legacy Job was reported as managed")
	}
	if err := c.Get(context.Background(), types.NamespacedName{Name: legacyJob.Name, Namespace: legacyJob.Namespace}, &batchv1.Job{}); err == nil {
		t.Fatal("legacy Job still exists after migration was requested")
	}

	if _, err := r.ensurePushCacheCredentials(); err != nil {
		t.Fatalf("ensure credentials before populating replacement Job: %v", err)
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

func TestUnchangedPushcacheJobCredentialRotationPreservesFinishedUploads(t *testing.T) {
	setObjectStoreTestEnv(t, map[string]string{
		"PUSHCACHE_AWS_ACCESS_KEY_ID":     "stale-access",
		"PUSHCACHE_AWS_SECRET_ACCESS_KEY": "stale-secret",
		"PUSHCACHE_AWS_BUCKET_NAME":       "frontend",
		"PUSHCACHE_AWS_REGION":            "us-east-1",
		"PUSHCACHE_AWS_ENDPOINT":          "minio.example.test",
		"PUSHCACHE_AWS_PORT":              "9000",
		"PUSHCACHE_AWS_SECRET_NAME":       "source-credentials",
		"POD_NAMESPACE":                   "operator-system",
	})

	scheme := runtime.NewScheme()
	if err := v1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	inputSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "source-credentials", Namespace: "operator-system"},
		Data: map[string][]byte{
			"aws-access-key-id":     []byte("rotated-access"),
			"aws-secret-access-key": []byte("rotated-secret"),
		},
	}
	managedSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: PushCacheCredentialsSecretName, Namespace: "frontend-ns"},
		Data: map[string][]byte{
			"aws-access-key-id":     []byte("stale-access"),
			"aws-secret-access-key": []byte("stale-secret"),
		},
	}
	makeJob := func(name string, completed bool) *batchv1.Job {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "frontend-ns"},
			Spec: batchv1.JobSpec{Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					"frontend-image":                       "frontend:v1",
					"valpop-image":                         "valpop:v1",
					PushCacheCredentialsChecksumAnnotation: "old-checksum",
				}},
				Spec: v1.PodSpec{Containers: []v1.Container{{Name: "valpop-pushcache", Env: pushCacheCredentialEnvVars()}}},
			}},
		}
		if completed {
			job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: v1.ConditionTrue}}
		}
		return job
	}
	unfinishedJob := makeJob("frontend-pushcache-pending", false)
	completedJob := makeJob("frontend-pushcache-completed", true)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(inputSecret, managedSecret, unfinishedJob, completedJob).Build()
	frontend := &crd.Frontend{ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "frontend-ns"}, Spec: crd.FrontendSpec{Image: "frontend:v1"}}
	environment := &crd.FrontendEnvironment{Spec: crd.FrontendEnvironmentSpec{ValpopImage: "valpop:v1"}}
	r := &FrontendReconciliation{Client: c, Ctx: context.Background(), Frontend: frontend, FrontendEnvironment: environment}

	checksum, err := r.ensurePushCacheCredentials()
	if err != nil {
		t.Fatalf("reconcile managed credentials independently of Job population: %v", err)
	}
	updatedSecret := &v1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: PushCacheCredentialsSecretName, Namespace: frontend.Namespace}, updatedSecret); err != nil {
		t.Fatalf("get managed credential Secret after rotation: %v", err)
	}
	accessKey := string(updatedSecret.Data["aws-access-key-id"])
	if accessKey == "" {
		accessKey = updatedSecret.StringData["aws-access-key-id"]
	}
	secretKey := string(updatedSecret.Data["aws-secret-access-key"])
	if secretKey == "" {
		secretKey = updatedSecret.StringData["aws-secret-access-key"]
	}
	if accessKey != "rotated-access" || secretKey != "rotated-secret" {
		t.Fatal("unchanged Job path did not refresh the managed credential Secret")
	}

	managed, err := r.manageExistingJob(unfinishedJob.Name, checksum)
	if err != nil {
		t.Fatalf("manage unfinished Job after credential rotation: %v", err)
	}
	if managed {
		t.Fatal("unfinished Job with a stale credential checksum was treated as current")
	}
	if err := c.Get(context.Background(), types.NamespacedName{Name: unfinishedJob.Name, Namespace: unfinishedJob.Namespace}, &batchv1.Job{}); err == nil {
		t.Fatal("unfinished Job was not deleted for recreation with rotated credentials")
	}

	managed, err = r.manageExistingJob(completedJob.Name, checksum)
	if err != nil {
		t.Fatalf("manage completed Job after credential rotation: %v", err)
	}
	if !managed {
		t.Fatal("completed upload was scheduled for needless recreation after credential rotation")
	}
	if err := c.Get(context.Background(), types.NamespacedName{Name: completedJob.Name, Namespace: completedJob.Namespace}, &batchv1.Job{}); err != nil {
		t.Fatalf("completed upload Job was removed: %v", err)
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

var _ = ginkgo.Describe("ReplicaSet credential cleanup RBAC", func() {
	ginkgo.It("watches and deletes ReplicaSets using the deployed manager role", func(ctx context.Context) {
		const namespace = "replicaset-rbac-test"
		const username = "restricted-replicaset-cleanup"
		const roleName = "replicaset-rbac-test-manager"

		user, err := testEnv.AddUser(envtest.User{Name: username}, testEnv.Config)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		roleData, err := os.ReadFile("../config/rbac/role.yaml")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		role := &rbacv1.ClusterRole{}
		gomega.Expect(yaml.Unmarshal(roleData, role)).To(gomega.Succeed())
		role.Name = roleName
		gomega.Expect(k8sClient.Create(ctx, role)).To(gomega.Succeed())
		defer func() { _ = k8sClient.Delete(context.Background(), role) }()

		ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())
		defer func() { _ = k8sClient.Delete(context.Background(), ns) }()
		binding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "replicaset-rbac-test", Namespace: namespace},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: roleName},
			Subjects:   []rbacv1.Subject{{Kind: "User", APIGroup: rbacv1.GroupName, Name: username}},
		}
		gomega.Expect(k8sClient.Create(ctx, binding)).To(gomega.Succeed())

		restrictedClient, err := client.NewWithWatch(user.Config(), client.Options{Scheme: scheme})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		deployment := &apps.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "reverse-proxy", Namespace: namespace, UID: types.UID("restricted-rbac-deployment"), Generation: 1},
			Spec: apps.DeploymentSpec{
				Replicas: int32Ptr(0),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "reverse-proxy"}},
				Template: v1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "reverse-proxy"}},
					Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "reverse-proxy", Image: "proxy:v1"}}},
				},
			},
		}
		gomega.Expect(k8sClient.Create(ctx, deployment)).To(gomega.Succeed())
		deployment.Status.ObservedGeneration = deployment.Generation

		owner := metav1.OwnerReference{APIVersion: "apps/v1", Kind: "Deployment", Name: deployment.Name, UID: deployment.UID, Controller: boolPtr(true)}
		legacyReplicaSet := &apps.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{Name: "reverse-proxy-old", Namespace: namespace, OwnerReferences: []metav1.OwnerReference{owner}},
			Spec: apps.ReplicaSetSpec{
				Replicas: int32Ptr(0),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "reverse-proxy"}},
				Template: v1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "reverse-proxy"}},
					Spec: v1.PodSpec{Containers: []v1.Container{{
						Name:  "reverse-proxy",
						Image: "proxy:v0",
						Env: []v1.EnvVar{
							{Name: "PUSHCACHE_AWS_ACCESS_KEY_ID", Value: "old-access"},
							{Name: "PUSHCACHE_AWS_SECRET_ACCESS_KEY", Value: "old-secret"},
						},
					}}},
				},
			},
		}
		gomega.Expect(k8sClient.Create(ctx, legacyReplicaSet)).To(gomega.Succeed())

		listed := &apps.ReplicaSetList{}
		gomega.Expect(restrictedClient.List(ctx, listed, client.InNamespace(namespace))).To(gomega.Succeed())
		watcher, err := restrictedClient.Watch(ctx, &apps.ReplicaSetList{}, client.InNamespace(namespace))
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "manager cache needs watch permission, not just initial list permission")
		watcher.Stop()

		reconciliation := &ReverseProxyReconciliation{Client: restrictedClient, Ctx: ctx, Namespace: namespace}
		pending, err := reconciliation.removeObsoleteCredentialReplicaSets(deployment)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(pending).To(gomega.BeFalse())
		gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: legacyReplicaSet.Name, Namespace: namespace}, &apps.ReplicaSet{})).To(gomega.HaveOccurred())
	})
})
