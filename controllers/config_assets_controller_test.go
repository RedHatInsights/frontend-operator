package controllers

import (
	"context"
	"time"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	prom "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	v1 "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = ginkgo.Describe("Config assets admission", func() {
	ginkgo.It("requires a timezone on the refresh timestamp", func() {
		ctx := context.Background()
		for _, value := range []string{"2026-11-11T12:00:00Z", "2026-11-11T12:00:00.123+01:00"} {
			environment := &crd.FrontendEnvironment{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "config-assets-admission-"},
				Spec:       crd.FrontendEnvironmentSpec{SSO: "https://sso.example.com", DeployCutoffTimestampConfigAssets: value},
			}
			gomega.Expect(k8sClient.Create(ctx, environment)).To(gomega.Succeed())
			gomega.Expect(k8sClient.Delete(ctx, environment)).To(gomega.Succeed())
		}
		for _, value := range []string{"2026-11-11T12:00:00", "not-a-timestamp"} {
			environment := &crd.FrontendEnvironment{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "config-assets-admission-"},
				Spec:       crd.FrontendEnvironmentSpec{SSO: "https://sso.example.com", DeployCutoffTimestampConfigAssets: value},
			}
			gomega.Expect(k8serr.IsInvalid(k8sClient.Create(ctx, environment))).To(gomega.BeTrue(), "accepted %q", value)
		}
	})
})

var _ = ginkgo.Describe("Config assets reconciliation", func() {
	ginkgo.It("prunes obsolete Frontend resources even when publication fails", func() {
		ctx := context.Background()
		namespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "config-assets-pruning-"}}
		gomega.Expect(k8sClient.Create(ctx, namespace)).To(gomega.Succeed())
		environment := &crd.FrontendEnvironment{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "config-assets-pruning-"},
			Spec:       crd.FrontendEnvironmentSpec{SSO: "https://sso.example.com", EnableConfigAssets: true, DisableContainerDeployments: true},
		}
		gomega.Expect(k8sClient.Create(ctx, environment)).To(gomega.Succeed())
		frontend := &crd.Frontend{
			ObjectMeta: metav1.ObjectMeta{Name: "chrome", Namespace: namespace.Name},
			Spec:       crd.FrontendSpec{EnvName: environment.Name, Service: "external-service", Frontend: crd.FrontendInfo{Paths: []string{"/chrome"}}},
		}
		gomega.Expect(k8sClient.Create(ctx, frontend)).To(gomega.Succeed())
		ginkgo.DeferCleanup(func() {
			gomega.Expect(k8sClient.Delete(ctx, frontend)).To(gomega.Succeed())
			gomega.Eventually(func() bool {
				return k8serr.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(frontend), &crd.Frontend{}))
			}, 10*time.Second, 100*time.Millisecond).To(gomega.BeTrue())
			for _, obj := range []client.Object{&prom.ServiceMonitor{}, &v1.ConfigMap{}, &networking.Ingress{}} {
				gomega.Expect(k8sClient.DeleteAllOf(ctx, obj, client.InNamespace(namespace.Name))).To(gomega.Succeed())
			}
			gomega.Expect(k8sClient.Delete(ctx, environment)).To(gomega.Succeed())
			gomega.Expect(k8sClient.Delete(ctx, namespace)).To(gomega.Succeed())
		})
		stale := &prom.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{
			Name: "stale-monitor", Namespace: namespace.Name,
			Labels:          map[string]string{"frontend": frontend.Name},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: crd.GroupVersion.String(), Kind: "Frontend", Name: frontend.Name, UID: frontend.UID}},
		}, Spec: prom.ServiceMonitorSpec{Selector: metav1.LabelSelector{}, Endpoints: []prom.Endpoint{{Port: "web"}}}}
		gomega.Expect(k8sClient.Create(ctx, stale)).To(gomega.Succeed())
		reconciler := &FrontendReconciler{Client: k8sManagerClient, APIReader: k8sClient, Scheme: scheme}
		gomega.Eventually(func(g gomega.Gomega) {
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(frontend)})
			g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("ValpopImage is required")))
			g.Expect(k8serr.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(stale), &prom.ServiceMonitor{}))).To(gomega.BeTrue())
		}, 10*time.Second, 100*time.Millisecond).To(gomega.Succeed())
	})
})
