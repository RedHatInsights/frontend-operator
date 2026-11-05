package controllers

import (
	"context"
	"time"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = ginkgo.Describe("Cachebust Job lifecycle", func() {
	ginkgo.DescribeTable("preserves the Job across unchanged reconciliations", func(valpopImage string) {
		ctx := context.Background()
		ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "cachebust-stability-"}}
		gomega.Expect(k8sClient.Create(ctx, ns)).Should(gomega.Succeed())
		ginkgo.DeferCleanup(func() {
			gomega.Expect(k8sClient.Delete(ctx, ns)).Should(gomega.Succeed())
		})
		namespace := ns.Name

		secret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "cachebust-stability-", Namespace: namespace},
			Data: map[string][]byte{
				"host":          []byte("test.purge.akamai.net"),
				"access_token":  []byte("test-access-token"),
				"client_token":  []byte("test-client-token"),
				"client_secret": []byte("test-client-secret"),
			},
		}
		gomega.Expect(k8sClient.Create(ctx, secret)).Should(gomega.Succeed())
		ginkgo.DeferCleanup(func() {
			gomega.Expect(k8sClient.Delete(ctx, secret)).Should(gomega.Succeed())
		})

		environment := &crd.FrontendEnvironment{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "cachebust-stability-"},
			Spec: crd.FrontendEnvironmentSpec{
				SSO:                   "https://sso.example.com",
				Hostname:              "console.example.com",
				EnableAkamaiCacheBust: true,
				AkamaiCacheBustImage:  "quay.io/cachebust:latest",
				AkamaiCacheBustURLs:   []string{"https://console.example.com"},
				AkamaiSecretName:      secret.Name,
				ValpopImage:           valpopImage,
			},
		}
		gomega.Expect(k8sClient.Create(ctx, environment)).Should(gomega.Succeed())
		ginkgo.DeferCleanup(func() {
			gomega.Expect(k8sClient.Delete(ctx, environment)).Should(gomega.Succeed())
		})

		frontend := &crd.Frontend{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "cachebust-stability-", Namespace: namespace},
			Spec: crd.FrontendSpec{
				EnvName: environment.Name,
				Title:   "Cachebust stability test",
				Image:   "quay.io/frontend:latest",
				Frontend: crd.FrontendInfo{
					Paths: []string{"/apps/cachebust-stability"},
				},
			},
		}
		gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())
		frontendKey := types.NamespacedName{Name: frontend.Name, Namespace: namespace}
		ginkgo.DeferCleanup(func() {
			gomega.Expect(k8sClient.Delete(ctx, frontend)).Should(gomega.Succeed())
			gomega.Eventually(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, frontendKey, &crd.Frontend{}))
			}, 10*time.Second, 100*time.Millisecond).Should(gomega.BeTrue())
			// envtest has no garbage collector; remove the generated resources explicitly.
			for _, object := range []client.Object{&batchv1.Job{}, &apps.Deployment{}, &v1.Service{}, &networking.Ingress{}} {
				gomega.Expect(k8sClient.DeleteAllOf(ctx, object, client.InNamespace(namespace), client.MatchingLabels{"frontend": frontend.Name})).Should(gomega.Succeed())
			}
			gomega.Expect(k8sClient.DeleteAllOf(ctx, &v1.ConfigMap{}, client.InNamespace(namespace))).Should(gomega.Succeed())
		})

		jobKey := types.NamespacedName{Name: frontend.Name + "-frontend-cachebust", Namespace: namespace}
		job := &batchv1.Job{}
		gomega.Eventually(func() error {
			return k8sClient.Get(ctx, jobKey, job)
		}, 10*time.Second, 100*time.Millisecond).Should(gomega.Succeed())
		originalUID := job.UID
		gomega.Expect(originalUID).ShouldNot(gomega.BeEmpty())

		reconciler := &FrontendReconciler{Client: k8sManagerClient, Scheme: scheme}
		for range 3 {
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: frontendKey})
			gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
			gomega.Expect(k8sClient.Get(ctx, jobKey, job)).Should(gomega.Succeed())
			gomega.Expect(job.UID).Should(gomega.Equal(originalUID), "unchanged reconciliation must not recreate the cachebust Job")
			gomega.Expect(job.DeletionTimestamp).Should(gomega.BeNil())
			gomega.Expect(job.Spec.Template.Annotations).Should(gomega.HaveKeyWithValue("valpop-image", valpopImage))
		}
		// Also catch delayed delete/recreate events from the controller's Owns(Job) watch.
		gomega.Consistently(func() types.UID {
			current := &batchv1.Job{}
			if err := k8sClient.Get(ctx, jobKey, current); err != nil || current.DeletionTimestamp != nil {
				return ""
			}
			return current.UID
		}, time.Second, 100*time.Millisecond).Should(gomega.Equal(originalUID))
	},
		ginkgo.Entry("with a configured valpop image", "quay.io/valpop:latest"),
		ginkgo.Entry("without a valpop image and with pushcache disabled", ""),
	)

	ginkgo.It("migrates legacy credentials before removing the ConfigMap even when another Frontend skips Job population", func() {
		ctx := context.Background()
		const (
			timeout  = 10 * time.Second
			interval = 100 * time.Millisecond
		)
		ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "cachebust-migration-"}}
		gomega.Expect(k8sClient.Create(ctx, ns)).Should(gomega.Succeed())
		ginkgo.DeferCleanup(func() {
			gomega.Expect(k8sClient.Delete(ctx, ns)).Should(gomega.Succeed())
		})

		const (
			environmentName = "cachebust-migration-env"
			firstName       = "cachebust-migration-first"
			secondName      = "cachebust-migration-second"
			legacyFinalizer = "frontend-operator.test/hold-legacy-job"
		)
		secret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "cachebust-migration-credentials", Namespace: ns.Name},
			Data: map[string][]byte{
				"host":          []byte("test.purge.akamai.net"),
				"access_token":  []byte("test-access-token"),
				"client_token":  []byte("test-client-token"),
				"client_secret": []byte("test-client-secret"),
			},
		}
		gomega.Expect(k8sClient.Create(ctx, secret)).Should(gomega.Succeed())

		environment := &crd.FrontendEnvironment{
			TypeMeta:   metav1.TypeMeta{APIVersion: "cloud.redhat.com/v1alpha1", Kind: "FrontendEnvironment"},
			ObjectMeta: metav1.ObjectMeta{Name: environmentName},
			Spec: crd.FrontendEnvironmentSpec{
				SSO:                   "https://sso.example.com",
				Hostname:              "console.example.com",
				EnableAkamaiCacheBust: true,
				AkamaiCacheBustImage:  "quay.io/cachebust:latest",
				AkamaiCacheBustURLs:   []string{"https://console.example.com"},
				AkamaiSecretName:      secret.Name,
			},
		}
		gomega.Expect(k8sClient.Create(ctx, environment)).Should(gomega.Succeed())

		frontends := []*crd.Frontend{}
		for _, name := range []string{firstName, secondName} {
			frontend := &crd.Frontend{
				TypeMeta:   metav1.TypeMeta{APIVersion: "cloud.redhat.com/v1alpha1", Kind: "Frontend"},
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name},
				Spec: crd.FrontendSpec{
					EnvName: environmentName,
					Title:   name,
					Image:   "quay.io/frontend:latest",
					Frontend: crd.FrontendInfo{
						Paths: []string{"/apps/" + name},
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend)).Should(gomega.Succeed())
			frontends = append(frontends, frontend)
		}

		firstKey := types.NamespacedName{Name: firstName + "-frontend-cachebust", Namespace: ns.Name}
		secondKey := types.NamespacedName{Name: secondName + "-frontend-cachebust", Namespace: ns.Name}
		legacyJobKey := types.NamespacedName{Name: "legacy-frontend-cachebust", Namespace: ns.Name}
		firstJob := &batchv1.Job{}
		secondJob := &batchv1.Job{}
		gomega.Eventually(func() error { return k8sClient.Get(ctx, firstKey, firstJob) }, timeout, interval).Should(gomega.Succeed())
		gomega.Eventually(func() error { return k8sClient.Get(ctx, secondKey, secondJob) }, timeout, interval).Should(gomega.Succeed())
		firstUID := firstJob.UID
		secondUID := secondJob.UID

		// Simulate an upgraded namespace: a legacy Job owned by the first
		// Frontend still mounts the old plaintext ConfigMap, and the generated
		// replacement Secret has not yet been created.
		legacyCM := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "akamai-edgerc", Namespace: ns.Name},
			Data:       map[string]string{"edgerc": "legacy credential material"},
		}
		gomega.Expect(k8sClient.Create(ctx, legacyCM)).Should(gomega.Succeed())
		legacyJob := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      legacyJobKey.Name,
				Namespace: legacyJobKey.Namespace,
				Finalizers: []string{
					legacyFinalizer,
				},
			},
			Spec: batchv1.JobSpec{Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					"frontend-image": "quay.io/frontend:latest",
					"valpop-image":   "",
				}},
				Spec: v1.PodSpec{
					RestartPolicy: v1.RestartPolicyNever,
					Volumes: []v1.Volume{{
						Name:         "akamai-edgerc",
						VolumeSource: v1.VolumeSource{ConfigMap: &v1.ConfigMapVolumeSource{LocalObjectReference: v1.LocalObjectReference{Name: legacyCM.Name}}},
					}},
					Containers: []v1.Container{{Name: "akamai-cache-bust", Image: "quay.io/cachebust:latest"}},
				},
			}},
		}
		gomega.Expect(k8sClient.Create(ctx, legacyJob)).Should(gomega.Succeed())
		edgercSecretKey := types.NamespacedName{Name: "akamai-edgerc", Namespace: ns.Name}
		gomega.Expect(k8sClient.Delete(ctx, &v1.Secret{ObjectMeta: metav1.ObjectMeta{Name: edgercSecretKey.Name, Namespace: edgercSecretKey.Namespace}})).Should(gomega.Succeed())

		reconciler := &FrontendReconciler{Client: k8sManagerClient, Scheme: scheme}
		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: secondName, Namespace: ns.Name}})
		gomega.Expect(err).Should(gomega.HaveOccurred(), "migration must wait for legacy Job deletion")
		gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: legacyCM.Name, Namespace: ns.Name}, &v1.ConfigMap{})).Should(gomega.Succeed(), "ConfigMap must remain while a legacy Job still references it")
		gomega.Eventually(func() bool {
			job := &batchv1.Job{}
			return k8sClient.Get(ctx, legacyJobKey, job) == nil && !job.GetDeletionTimestamp().IsZero()
		}, timeout, interval).Should(gomega.BeTrue())

		// Allow the foreground deletion to complete. Reconciliation then removes
		// the old ConfigMap and keeps the unchanged second Job intact.
		gomega.Eventually(func() error {
			job := &batchv1.Job{}
			if err := k8sClient.Get(ctx, legacyJobKey, job); err != nil {
				return err
			}
			job.Finalizers = nil
			return k8sClient.Update(ctx, job)
		}, timeout, interval).Should(gomega.Succeed())
		gomega.Eventually(func() bool {
			_, reconcileErr := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: secondName, Namespace: ns.Name}})
			if reconcileErr != nil {
				return false
			}
			return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: legacyCM.Name, Namespace: ns.Name}, &v1.ConfigMap{}))
		}, timeout, interval).Should(gomega.BeTrue())

		migratedSecret := &v1.Secret{}
		gomega.Expect(k8sClient.Get(ctx, edgercSecretKey, migratedSecret)).Should(gomega.Succeed())
		gomega.Expect(migratedSecret.OwnerReferences).Should(gomega.HaveLen(1))
		gomega.Expect(migratedSecret.OwnerReferences[0].APIVersion).Should(gomega.Equal("cloud.redhat.com/v1alpha1"))
		gomega.Expect(migratedSecret.OwnerReferences[0].Kind).Should(gomega.Equal("FrontendEnvironment"))
		gomega.Expect(migratedSecret.OwnerReferences[0].Name).Should(gomega.Equal(environmentName), "shared Secret must be owned by the stable FrontendEnvironment")
		gomega.Expect(migratedSecret.OwnerReferences[0].UID).Should(gomega.Equal(environment.UID))
		gomega.Expect(k8sClient.Get(ctx, secondKey, secondJob)).Should(gomega.Succeed())
		gomega.Expect(secondJob.UID).Should(gomega.Equal(secondUID), "migration must run even though this Frontend's unchanged Job skips population")
		gomega.Expect(k8sClient.Get(ctx, firstKey, firstJob)).Should(gomega.Succeed())
		gomega.Expect(firstJob.UID).Should(gomega.Equal(firstUID))
		gomega.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, legacyJobKey, &batchv1.Job{}))).Should(gomega.BeTrue(), "legacy ConfigMap-backed Job must be removed before ConfigMap cleanup")

		ginkgo.DeferCleanup(func() {
			job := &batchv1.Job{}
			if err := k8sClient.Get(ctx, legacyJobKey, job); err == nil && len(job.Finalizers) > 0 {
				job.Finalizers = nil
				_ = k8sClient.Update(ctx, job)
			}
			for _, frontend := range frontends {
				_ = k8sClient.Delete(ctx, frontend)
			}
			for _, frontend := range frontends {
				key := types.NamespacedName{Name: frontend.Name, Namespace: ns.Name}
				gomega.Eventually(func() bool { return k8serrors.IsNotFound(k8sClient.Get(ctx, key, &crd.Frontend{})) }, timeout, interval).Should(gomega.BeTrue())
			}
			for _, object := range []client.Object{&batchv1.Job{}, &apps.Deployment{}, &v1.Service{}, &networking.Ingress{}, &v1.ConfigMap{}, &v1.Secret{}} {
				gomega.Expect(k8sClient.DeleteAllOf(ctx, object, client.InNamespace(ns.Name))).Should(gomega.Succeed())
			}
			gomega.Expect(k8sClient.Delete(ctx, environment)).Should(gomega.Succeed())
		})
	})
})
