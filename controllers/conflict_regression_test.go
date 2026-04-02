package controllers

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// This file reproduces RHCLOUD-46019: the reconciler enters a continuous retry
// loop when Kubernetes optimistic-concurrency conflicts (HTTP 409) occur during
// resource updates.
//
// In production, the Kubernetes deployment controller continuously updates
// Deployment status (pod readiness, replica counts), which bumps the
// Deployment's resourceVersion. When the frontend-operator's cache.ApplyAll()
// later tries to Update the Deployment, the resourceVersion is stale → 409.
//
// These tests use a client wrapper to deterministically inject 409 errors on
// Deployment Update calls, simulating the production race condition without
// depending on timing.
//
// Before the fix: Reconcile returns an error because cache.ApplyAll() hits a
//   409 and there is no retry logic → tests FAIL.
//
// After the fix: retry.RetryOnConflict re-fetches resources and retries,
//   succeeding after the injected conflicts clear → tests PASS.

// conflictClient wraps a client.Client and injects 409 Conflict errors on the
// first N Update calls targeting Deployments. All other operations are delegated
// to the underlying client (which retains field indexes from the manager's cache).
type conflictClient struct {
	client.Client
	conflictsRemaining atomic.Int32
}

func (c *conflictClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if _, ok := obj.(*apps.Deployment); ok {
		if c.conflictsRemaining.Add(-1) >= 0 {
			// Wrap the conflict error the same way rhc-osdk-utils Updater.Apply()
			// does, so the test proves retry.RetryOnConflict can detect the 409
			// through the fmt.Errorf %w wrapping.
			conflictErr := k8serr.NewConflict(
				schema.GroupResource{Group: "apps", Resource: "deployments"},
				obj.GetName(),
				fmt.Errorf("the object has been modified; please apply your changes to the latest version and try again"),
			)
			return fmt.Errorf("error updating resource Deployment %s: %w", obj.GetName(), conflictErr)
		}
	}
	return c.Client.Update(ctx, obj, opts...)
}

// createConflictTestResources creates a FrontendEnvironment and Frontend, then
// waits for the manager's controller to finish the initial reconciliation
// (Deployment exists).
func createConflictTestResources(ctx context.Context, cl client.Client, envName, frontendName, ns string) {
	fe := &crd.FrontendEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: envName},
		Spec: crd.FrontendEnvironmentSpec{
			SSO:                  "https://sso.conflict-test.example.com",
			Hostname:             "conflict-test.example.com",
			GenerateNavJSON:      true,
			OverwriteCaddyConfig: true,
		},
	}
	err := cl.Create(ctx, fe)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	frontend := &crd.Frontend{
		ObjectMeta: metav1.ObjectMeta{Name: frontendName, Namespace: ns},
		Spec: crd.FrontendSpec{
			EnvName: envName,
			Image:   "quay.io/conflict-test:v1",
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
	err = cl.Create(ctx, frontend)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	// Wait for the manager's controller to create the Deployment.
	deployNN := types.NamespacedName{Name: frontendName + "-frontend", Namespace: ns}
	gomega.Eventually(func() error {
		return cl.Get(ctx, deployNN, &apps.Deployment{})
	}, 30*time.Second, 100*time.Millisecond).Should(gomega.Succeed(),
		"Deployment %s was not created by the manager's controller", deployNN)
}

var _ = ginkgo.Describe("Conflict regression (RHCLOUD-46019)", func() {
	ctx := context.Background()
	ns := "default"

	ginkgo.It("should recover when the first Deployment update conflicts", func() {
		envName := "conflict-retry-env-1"
		frontendName := "conflict-retry-fe-1"

		createConflictTestResources(ctx, k8sClient, envName, frontendName, ns)

		// Wrap the manager's client (which has field indexes) to return 409
		// on the first 2 Deployment Updates.
		cc := &conflictClient{Client: k8sManagerClient}
		cc.conflictsRemaining.Store(2)

		reconciler := &FrontendReconciler{
			Client: cc,
			Scheme: scheme,
			Log:    ctrl.Log.WithName("conflict-retry-test"),
		}

		_, reconcileErr := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{Name: frontendName, Namespace: ns},
		})

		// Before the fix: reconcileErr != nil (409 from cache.ApplyAll, no retry).
		// After the fix:  reconcileErr == nil (retry.RetryOnConflict handles it).
		// Note: result.Requeue is not asserted because the manager's background
		// controller can race with SetFrontendConditions, causing a benign 409
		// on the status update (Requeue: true, err: nil). This does not affect
		// the retry logic under test.
		gomega.Expect(reconcileErr).NotTo(gomega.HaveOccurred(),
			"Reconcile should succeed after retrying past the injected 409 conflicts")
	})

	ginkgo.It("should return an error when all retries are exhausted", func() {
		envName := "conflict-retry-env-2"
		frontendName := "conflict-retry-fe-2"

		createConflictTestResources(ctx, k8sClient, envName, frontendName, ns)

		// Inject enough conflicts to exhaust all retries
		// (retry.DefaultRetry has 5 steps).
		cc := &conflictClient{Client: k8sManagerClient}
		cc.conflictsRemaining.Store(100)

		reconciler := &FrontendReconciler{
			Client: cc,
			Scheme: scheme,
			Log:    ctrl.Log.WithName("conflict-retry-test"),
		}

		_, reconcileErr := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{Name: frontendName, Namespace: ns},
		})

		// Both before and after the fix, exhausted retries must return an error
		// (not loop forever).
		gomega.Expect(reconcileErr).To(gomega.HaveOccurred(),
			"Reconcile must return an error when all retries are exhausted")
	})

	ginkgo.It("should succeed on first attempt when there are no conflicts", func() {
		envName := "conflict-retry-env-3"
		frontendName := "conflict-retry-fe-3"

		createConflictTestResources(ctx, k8sClient, envName, frontendName, ns)

		// No conflicts injected — use the manager's client directly.
		reconciler := &FrontendReconciler{
			Client: k8sManagerClient,
			Scheme: scheme,
			Log:    ctrl.Log.WithName("conflict-retry-test"),
		}

		_, reconcileErr := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{Name: frontendName, Namespace: ns},
		})

		// See test 1 comment — result.Requeue is not asserted due to manager race.
		gomega.Expect(reconcileErr).NotTo(gomega.HaveOccurred(),
			"Reconcile should succeed without conflicts")
	})
})
