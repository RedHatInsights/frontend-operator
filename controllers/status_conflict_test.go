package controllers

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// This file reproduces RHCLOUD-46529: SetFrontendConditions calls
// client.Status().Update() using a Frontend object whose resourceVersion may
// be stale. When an external write (annotation change, concurrent reconciler)
// bumps the CR's resourceVersion between the reconciler's initial Get and the
// status update, the API server returns a 409 Conflict.
//
// Before the fix: SetFrontendConditions propagates the 409 → tests FAIL.
// After the fix: SetFrontendConditions wraps the update in
//   retry.RetryOnConflict with a re-fetch → tests PASS.

// statusConflictWriter wraps a SubResourceWriter and injects 409 Conflict
// errors on the first N Update calls targeting Frontend objects.
type statusConflictWriter struct {
	client.SubResourceWriter
	conflictsRemaining atomic.Int32
}

func (w *statusConflictWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	if _, ok := obj.(*crd.Frontend); ok {
		if w.conflictsRemaining.Add(-1) >= 0 {
			return k8serr.NewConflict(
				schema.GroupResource{Group: "cloud.redhat.com", Resource: "frontends"},
				obj.GetName(),
				fmt.Errorf("the object has been modified; please apply your changes to the latest version and try again"),
			)
		}
	}
	return w.SubResourceWriter.Update(ctx, obj, opts...)
}

// statusConflictClient wraps a client.Client and overrides Status() to return
// a writer that injects 409 conflicts on Frontend status updates.
type statusConflictClient struct {
	client.Client
	writer *statusConflictWriter
}

func (c *statusConflictClient) Status() client.SubResourceWriter {
	return c.writer
}

var _ = ginkgo.Describe("Status update conflict (RHCLOUD-46529)", func() {
	ctx := context.Background()
	ns := "default"

	ginkgo.It("should retry SetFrontendConditions when resourceVersion is stale", func() {
		envName := "status-conflict-env-1"
		frontendName := "status-conflict-fe-1"

		createConflictTestResources(ctx, k8sClient, envName, frontendName, ns)

		// Wait for the manager's controller to finish initial reconciliation
		// so that field indexes are populated and GetFrontendFigures works.
		gomega.Eventually(func() bool {
			fe := &crd.Frontend{}
			if err := k8sManagerClient.Get(ctx, types.NamespacedName{Name: frontendName, Namespace: ns}, fe); err != nil {
				return false
			}
			for _, c := range fe.Status.Conditions {
				if c.Type == crd.ReconciliationSuccessful && c.Status == metav1.ConditionTrue {
					return true
				}
			}
			return false
		}, 30*time.Second, 100*time.Millisecond).Should(gomega.BeTrue(),
			"Initial reconciliation should succeed")

		// Simulate the race: fetch the Frontend, bump resourceVersion with
		// an annotation (simulates external write), then call
		// SetFrontendConditions with the stale object.
		fe := &crd.Frontend{}
		gomega.Expect(k8sManagerClient.Get(ctx, types.NamespacedName{Name: frontendName, Namespace: ns}, fe)).To(gomega.Succeed())

		staleFrontend := fe.DeepCopy()

		// Bump resourceVersion by annotating the Frontend.
		if fe.Annotations == nil {
			fe.Annotations = map[string]string{}
		}
		fe.Annotations["test-bump"] = "force-rv-change"
		gomega.Expect(k8sManagerClient.Update(ctx, fe)).To(gomega.Succeed())

		// Clear conditions on the stale copy so SetFrontendConditions sees a
		// status diff and actually calls Status().Update() (otherwise DeepEqual
		// skips the update and the stale resourceVersion is never tested).
		staleFrontend.Status.Conditions = nil

		// staleFrontend now has a stale resourceVersion.
		// Before the fix: returns 409 because Status().Update uses stale rv.
		// After the fix: re-fetches and retries → succeeds.
		err := SetFrontendConditions(ctx, k8sManagerClient, staleFrontend, crd.ReconciliationSuccessful, nil)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(),
			"SetFrontendConditions should handle stale resourceVersion via retry")
	})

	ginkgo.It("should propagate status 409 through the reconciler error path", func() {
		envName := "status-conflict-env-2"
		frontendName := "status-conflict-fe-2"

		createConflictTestResources(ctx, k8sClient, envName, frontendName, ns)

		// Inject status conflicts AND deploy conflicts.
		// Deploy conflicts (100) exhaust all retries → retryErr != nil.
		// Then SetFrontendConditions is called on the error path (line 231)
		// to report the failure, but status 409 makes that fail too.
		deployCC := &conflictClient{Client: k8sManagerClient}
		deployCC.conflictsRemaining.Store(100)

		writer := &statusConflictWriter{
			SubResourceWriter: k8sManagerClient.Status(),
		}
		writer.conflictsRemaining.Store(2)

		cc := &statusConflictClient{
			Client: deployCC,
			writer: writer,
		}

		reconciler := &FrontendReconciler{
			Client: cc,
			Scheme: scheme,
			Log:    ctrl.Log.WithName("status-conflict-test"),
		}

		_, reconcileErr := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{Name: frontendName, Namespace: ns},
		})

		gomega.Expect(reconcileErr).To(gomega.HaveOccurred(),
			"Reconcile should return an error")

		// Before the fix: the error wraps "error setting status" because
		//   SetFrontendConditions propagates the 409 on the error path.
		// After the fix: SetFrontendConditions retries past the status 409,
		//   successfully reports the failed condition, and returns the original
		//   deploy conflict error (no "error setting status" wrapper).
		gomega.Expect(reconcileErr.Error()).NotTo(gomega.ContainSubstring("error setting status"),
			"Status update should succeed via retry; error should be the original reconciliation failure, not a status update failure")
	})
})
