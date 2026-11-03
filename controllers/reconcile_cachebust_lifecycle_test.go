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
})
