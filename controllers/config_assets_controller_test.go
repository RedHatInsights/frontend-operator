package controllers

import (
	"context"
	"encoding/json"
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

var _ = ginkgo.Describe("Config assets reconciliation", func() {
	ginkgo.It("refreshes shared configuration after a Frontend is deleted", func() {
		ctx := context.Background()
		namespaces := []*v1.Namespace{
			{ObjectMeta: metav1.ObjectMeta{GenerateName: "config-assets-delete-"}},
			{ObjectMeta: metav1.ObjectMeta{GenerateName: "config-assets-peer-"}},
		}
		for _, namespace := range namespaces {
			gomega.Expect(k8sClient.Create(ctx, namespace)).To(gomega.Succeed())
		}
		environment := &crd.FrontendEnvironment{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "config-assets-delete-"},
			// No publisher polling can mask a missing deletion watch in this test.
			Spec: crd.FrontendEnvironmentSpec{SSO: "https://sso.example.com", DisableContainerDeployments: true},
		}
		gomega.Expect(k8sClient.Create(ctx, environment)).To(gomega.Succeed())
		ginkgo.DeferCleanup(func() {
			for _, namespace := range namespaces {
				gomega.Expect(k8sClient.DeleteAllOf(ctx, &crd.Frontend{}, client.InNamespace(namespace.Name))).To(gomega.Succeed())
				gomega.Eventually(func() int {
					frontends := &crd.FrontendList{}
					gomega.Expect(k8sClient.List(ctx, frontends, client.InNamespace(namespace.Name))).To(gomega.Succeed())
					return len(frontends.Items)
				}, 10*time.Second, 100*time.Millisecond).To(gomega.BeZero())
				for _, obj := range []client.Object{&v1.ConfigMap{}, &networking.Ingress{}} {
					gomega.Expect(k8sClient.DeleteAllOf(ctx, obj, client.InNamespace(namespace.Name))).To(gomega.Succeed())
				}
				gomega.Expect(k8sClient.Delete(ctx, namespace)).To(gomega.Succeed())
			}
			gomega.Expect(k8sClient.Delete(ctx, environment)).To(gomega.Succeed())
		})
		frontends := []*crd.Frontend{
			{ObjectMeta: metav1.ObjectMeta{Name: "deleted-app", Namespace: namespaces[0].Name}},
			{ObjectMeta: metav1.ObjectMeta{Name: "survivor", Namespace: namespaces[0].Name}},
			{ObjectMeta: metav1.ObjectMeta{Name: "remote-peer", Namespace: namespaces[1].Name}},
		}
		for _, frontend := range frontends {
			frontend.Spec = crd.FrontendSpec{
				EnvName: environment.Name, Service: "external-service", FeoConfigEnabled: true,
				Frontend: crd.FrontendInfo{Paths: []string{"/" + frontend.Name}},
				Module:   &crd.FedModule{ManifestLocation: "/apps/" + frontend.Name + "/fed-mods.json"},
			}
			gomega.Expect(k8sClient.Create(ctx, frontend)).To(gomega.Succeed())
		}
		// Let the cache observe every peer, then refresh both namespace copies.
		gomega.Eventually(func() int {
			peers := &crd.FrontendList{}
			gomega.Expect(k8sManagerClient.List(ctx, peers, client.MatchingFields{"spec.envName": environment.Name})).To(gomega.Succeed())
			return len(peers.Items)
		}, 10*time.Second, 100*time.Millisecond).To(gomega.Equal(3))
		gomega.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(environment), environment)).To(gomega.Succeed())
		replicas := int32(2)
		environment.Spec.DefaultReplicas = &replicas
		gomega.Expect(k8sClient.Update(ctx, environment)).To(gomega.Succeed())
		assertRegistry := func(g gomega.Gomega, expected []string) {
			for _, namespace := range namespaces {
				cm := &v1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: environment.Name, Namespace: namespace.Name}, cm)).To(gomega.Succeed())
				registry := map[string]json.RawMessage{}
				g.Expect(json.Unmarshal([]byte(cm.Data["fed-modules.json"]), &registry)).To(gomega.Succeed())
				g.Expect(registry).To(gomega.HaveLen(len(expected)))
				for _, name := range expected {
					g.Expect(registry).To(gomega.HaveKey(name))
				}
			}
		}
		gomega.Eventually(func(g gomega.Gomega) {
			assertRegistry(g, []string{"deletedApp", "survivor", "remotePeer"})
		}, 10*time.Second, 100*time.Millisecond).To(gomega.Succeed())
		gomega.Consistently(func(g gomega.Gomega) {
			assertRegistry(g, []string{"deletedApp", "survivor", "remotePeer"})
		}, time.Second, 100*time.Millisecond).To(gomega.Succeed())
		gomega.Expect(k8sClient.Delete(ctx, frontends[0])).To(gomega.Succeed())
		gomega.Eventually(func(g gomega.Gomega) {
			g.Expect(k8serr.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(frontends[0]), &crd.Frontend{}))).To(gomega.BeTrue())
			assertRegistry(g, []string{"survivor", "remotePeer"})
		}, 10*time.Second, 100*time.Millisecond).To(gomega.Succeed())
	})

	ginkgo.It("prunes obsolete Frontend resources even when publication fails", func() {
		ctx := context.Background()
		namespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "config-assets-pruning-"}}
		gomega.Expect(k8sClient.Create(ctx, namespace)).To(gomega.Succeed())
		environment := &crd.FrontendEnvironment{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "config-assets-pruning-"},
			Spec:       crd.FrontendEnvironmentSpec{SSO: "https://sso.example.com", EnablePushCache: true, DisableContainerDeployments: true},
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
