package controllers

import (
	"context"
	"time"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	apps "k8s.io/api/apps/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// This file tests RHCLOUD-46492: adding GenerationChangedPredicate to Owns
// watches in SetupWithManager so that Deployment status-only updates (pod
// readiness, replica counts) do not trigger unnecessary reconciliations.
//
// Without the predicate: every Deployment status update bumps resourceVersion,
// which triggers the Owns watch and a full reconciliation.
//
// With the predicate: only spec changes (which increment metadata.generation)
// trigger reconciliation. Status-only updates are filtered out.

// getReconcileCount reads the reconciliation request counter for a given app.
func getReconcileCount(appName string) float64 {
	var metric io_prometheus_client.Metric
	counter, err := reconciliationRequestMetric.GetMetricWith(prometheus.Labels{"app": appName})
	if err != nil {
		return 0
	}
	if err := counter.(prometheus.Metric).Write(&metric); err != nil {
		return 0
	}
	return metric.GetCounter().GetValue()
}

var _ = ginkgo.Describe("GenerationChangedPredicate on Owns watches (RHCLOUD-46492)", func() {
	ctx := context.Background()
	ns := "default"

	ginkgo.It("should not re-reconcile when a Deployment status-only update occurs", func() {
		envName := "gen-pred-env-1"
		frontendName := "gen-pred-fe-1"

		// Create FrontendEnvironment
		fe := &crd.FrontendEnvironment{
			ObjectMeta: metav1.ObjectMeta{Name: envName},
			Spec: crd.FrontendEnvironmentSpec{
				SSO:      "https://sso.gen-pred.example.com",
				Hostname: "gen-pred.example.com",
			},
		}
		err := k8sClient.Create(ctx, fe)
		if err != nil && !k8serr.IsAlreadyExists(err) {
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}

		// Create Frontend
		frontend := &crd.Frontend{
			ObjectMeta: metav1.ObjectMeta{Name: frontendName, Namespace: ns},
			Spec: crd.FrontendSpec{
				EnvName: envName,
				Image:   "quay.io/gen-pred-test:v1",
				Frontend: crd.FrontendInfo{
					Paths: []string{"/apps/" + frontendName},
				},
				Module: &crd.FedModule{
					ManifestLocation: "/apps/" + frontendName + "/fed-mods.json",
					Modules: []crd.Module{{
						ID:     frontendName,
						Module: "./RootApp",
						Routes: []crd.Route{{Pathname: "/apps/" + frontendName}},
					}},
				},
				FeoConfigEnabled: true,
			},
		}
		gomega.Expect(k8sClient.Create(ctx, frontend)).To(gomega.Succeed())

		// Wait for initial reconciliation to complete (Deployment exists).
		deployNN := types.NamespacedName{Name: frontendName + "-frontend", Namespace: ns}
		gomega.Eventually(func() error {
			return k8sClient.Get(ctx, deployNN, &apps.Deployment{})
		}, 30*time.Second, 100*time.Millisecond).Should(gomega.Succeed(),
			"Deployment should be created by initial reconciliation")

		// Wait for the reconciliation storm to settle. Initial creation
		// triggers multiple reconciliations (Frontend create, Deployment
		// create via Owns watch, etc.). Wait until the counter stabilizes.
		var settledCount float64
		gomega.Eventually(func() bool {
			count1 := getReconcileCount(frontendName)
			time.Sleep(2 * time.Second)
			count2 := getReconcileCount(frontendName)
			settledCount = count2
			return count1 == count2
		}, 30*time.Second, 100*time.Millisecond).Should(gomega.BeTrue(),
			"Reconciliation count should stabilize")

		// Now update the Deployment's status only (simulating what the k8s
		// deployment controller does: updating readyReplicas, availableReplicas).
		// This bumps the Deployment's resourceVersion but NOT metadata.generation.
		deploy := &apps.Deployment{}
		gomega.Expect(k8sClient.Get(ctx, deployNN, deploy)).To(gomega.Succeed())
		deploy.Status.ReadyReplicas = 1
		deploy.Status.AvailableReplicas = 1
		deploy.Status.Replicas = 1
		gomega.Expect(k8sClient.Status().Update(ctx, deploy)).To(gomega.Succeed())

		// With GenerationChangedPredicate: the status-only update should NOT
		// trigger reconciliation, so the counter should remain unchanged.
		//
		// Without the predicate: the Owns watch fires, the reconciler runs,
		// and the counter increments → this assertion FAILS.
		gomega.Consistently(func() float64 {
			return getReconcileCount(frontendName)
		}, 5*time.Second, 200*time.Millisecond).Should(gomega.Equal(settledCount),
			"Reconciliation count should not increase after a Deployment status-only update")
	})

	ginkgo.It("should re-reconcile when a Frontend spec change occurs", func() {
		envName := "gen-pred-env-2"
		frontendName := "gen-pred-fe-2"

		// Create FrontendEnvironment
		fe := &crd.FrontendEnvironment{
			ObjectMeta: metav1.ObjectMeta{Name: envName},
			Spec: crd.FrontendEnvironmentSpec{
				SSO:      "https://sso.gen-pred-2.example.com",
				Hostname: "gen-pred-2.example.com",
			},
		}
		err := k8sClient.Create(ctx, fe)
		if err != nil && !k8serr.IsAlreadyExists(err) {
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}

		// Create Frontend
		frontend := &crd.Frontend{
			ObjectMeta: metav1.ObjectMeta{Name: frontendName, Namespace: ns},
			Spec: crd.FrontendSpec{
				EnvName: envName,
				Image:   "quay.io/gen-pred-test:v1",
				Frontend: crd.FrontendInfo{
					Paths: []string{"/apps/" + frontendName},
				},
				Module: &crd.FedModule{
					ManifestLocation: "/apps/" + frontendName + "/fed-mods.json",
					Modules: []crd.Module{{
						ID:     frontendName,
						Module: "./RootApp",
						Routes: []crd.Route{{Pathname: "/apps/" + frontendName}},
					}},
				},
				FeoConfigEnabled: true,
			},
		}
		gomega.Expect(k8sClient.Create(ctx, frontend)).To(gomega.Succeed())

		// Wait for initial reconciliation to complete.
		deployNN := types.NamespacedName{Name: frontendName + "-frontend", Namespace: ns}
		frontendNN := types.NamespacedName{Name: frontendName, Namespace: ns}

		gomega.Eventually(func() error {
			return k8sClient.Get(ctx, deployNN, &apps.Deployment{})
		}, 30*time.Second, 100*time.Millisecond).Should(gomega.Succeed(),
			"Deployment should be created by initial reconciliation")

		// Wait for reconciliation to settle.
		gomega.Eventually(func() bool {
			count1 := getReconcileCount(frontendName)
			time.Sleep(2 * time.Second)
			count2 := getReconcileCount(frontendName)
			return count1 == count2
		}, 30*time.Second, 100*time.Millisecond).Should(gomega.BeTrue(),
			"Reconciliation count should stabilize")
		countBeforeSpecChange := getReconcileCount(frontendName)

		// Update the Frontend spec (image change). This triggers reconciliation
		// via the For(&crd.Frontend{}) watch, which updates the Deployment spec,
		// which increments its metadata.generation.
		f := &crd.Frontend{}
		gomega.Expect(k8sClient.Get(ctx, frontendNN, f)).To(gomega.Succeed())
		f.Spec.Image = "quay.io/gen-pred-test:v2"
		gomega.Expect(k8sClient.Update(ctx, f)).To(gomega.Succeed())

		// The reconciler should run and update the Deployment's container image.
		gomega.Eventually(func() string {
			d := &apps.Deployment{}
			if err := k8sClient.Get(ctx, deployNN, d); err != nil {
				return ""
			}
			if len(d.Spec.Template.Spec.Containers) == 0 {
				return ""
			}
			return d.Spec.Template.Spec.Containers[0].Image
		}, 30*time.Second, 200*time.Millisecond).Should(gomega.Equal("quay.io/gen-pred-test:v2"),
			"Deployment image should be updated after Frontend spec change")

		// Verify reconciliation actually ran.
		countAfterSpecChange := getReconcileCount(frontendName)
		gomega.Expect(countAfterSpecChange).To(gomega.BeNumerically(">", countBeforeSpecChange),
			"Reconciliation count should increase after a Frontend spec change")
	})
})
