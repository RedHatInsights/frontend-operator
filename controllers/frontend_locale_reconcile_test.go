package controllers

import (
	"context"
	"slices"
	"testing"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestFrontendPredicateFiltersStatusOnlyUpdates(t *testing.T) {
	predicate := defaultPredicate(logr.Discard(), "frontend")
	oldFrontend := &crd.Frontend{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "boot", ResourceVersion: "1"}}
	newFrontend := oldFrontend.DeepCopy()
	newFrontend.ResourceVersion = "2"
	newFrontend.Status.Conditions = []metav1.Condition{{
		Type:   crd.LocalesValid,
		Status: metav1.ConditionTrue,
		Reason: "Valid",
	}}
	newFrontend.ManagedFields = []metav1.ManagedFieldsEntry{{Manager: "status-writer"}}

	if predicate.UpdateFunc(event.UpdateEvent{ObjectOld: oldFrontend, ObjectNew: newFrontend}) {
		t.Fatal("status-only Frontend update triggered reconciliation")
	}

	specChanged := newFrontend.DeepCopy()
	specChanged.Spec.Image = "image:v2"
	if !predicate.UpdateFunc(event.UpdateEvent{ObjectOld: oldFrontend, ObjectNew: specChanged}) {
		t.Fatal("spec change did not trigger reconciliation")
	}

	metadataChanged := newFrontend.DeepCopy()
	metadataChanged.Annotations = map[string]string{"test/touch": "true"}
	if !predicate.UpdateFunc(event.UpdateEvent{ObjectOld: oldFrontend, ObjectNew: metadataChanged}) {
		t.Fatal("metadata change did not trigger reconciliation")
	}
}

type statusUpdateCountingClient struct {
	client.Client
	updates int
}

func (c *statusUpdateCountingClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if obj, ok := list.(*unstructured.Unstructured); ok && obj.GetKind() == "Deployment" {
		obj.Object["kind"] = "DeploymentList"
		obj.Object["items"] = []interface{}{}
		return nil
	}
	return c.Client.List(ctx, list, opts...)
}

func (c *statusUpdateCountingClient) Status() client.SubResourceWriter {
	return statusUpdateCountingWriter{SubResourceWriter: c.Client.Status(), updates: &c.updates}
}

type statusUpdateCountingWriter struct {
	client.SubResourceWriter
	updates *int
}

func (w statusUpdateCountingWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	*w.updates++
	return w.SubResourceWriter.Update(ctx, obj, opts...)
}

func TestSetFrontendConditionsWritesLocalesInSameStatusUpdate(t *testing.T) {
	ctx := context.Background()
	env := &crd.FrontendEnvironment{ObjectMeta: metav1.ObjectMeta{Name: "stage"}}
	frontend := &crd.Frontend{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "boot"},
		Spec:       crd.FrontendSpec{EnvName: env.Name},
	}
	baseClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(env, frontend).
		WithStatusSubresource(&crd.Frontend{}).
		WithIndex(&crd.Frontend{}, "spec.envName", func(obj client.Object) []string {
			return []string{obj.(*crd.Frontend).Spec.EnvName}
		}).Build()
	c := &statusUpdateCountingClient{Client: baseClient}
	localeCondition := &metav1.Condition{
		Type:   crd.LocalesValid,
		Status: metav1.ConditionTrue,
		Reason: "Valid",
	}

	if err := SetFrontendConditions(ctx, c, frontend.DeepCopy(), crd.ReconciliationSuccessful, nil, localeCondition); err != nil {
		t.Fatal(err)
	}
	if c.updates != 1 {
		t.Fatalf("status updates: got %d, want 1", c.updates)
	}

	got := &crd.Frontend{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(frontend), got); err != nil {
		t.Fatal(err)
	}
	if cond := meta.FindStatusCondition(got.Status.Conditions, crd.ReconciliationSuccessful); cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("ReconciliationSuccessful condition: got %+v", cond)
	}
	if cond := meta.FindStatusCondition(got.Status.Conditions, crd.LocalesValid); cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("LocalesValid condition: got %+v", cond)
	}
}

// Each reconcile writes the source messages only in its own namespace, so a
// messages change or a Frontend deletion must reach one Frontend per namespace.
func TestLocaleRepublishRequestsOnePerNamespace(t *testing.T) {
	now := metav1.Now()
	frontend := func(ns, name string) *crd.Frontend {
		return &crd.Frontend{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Spec: crd.FrontendSpec{EnvName: "stage"}}
	}
	deleting := frontend("ns-c", "deleting")
	deleting.DeletionTimestamp = &now
	deleting.Finalizers = []string{"test/hold"}
	disabled := frontend("ns-c", "disabled")
	disabled.Spec.Disabled = true
	other := frontend("ns-a", "other-env")
	other.Spec.EnvName = "prod"
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(frontend("ns-a", "a1"), frontend("ns-a", "a2"), frontend("ns-b", "b1"), deleting, disabled, other).
		WithIndex(&crd.Frontend{}, "spec.envName", func(obj client.Object) []string {
			return []string{obj.(*crd.Frontend).Spec.EnvName}
		}).Build()
	r := &FrontendReconciler{Client: c, Log: logr.Discard()}

	names := func(reqs []reconcile.Request) []string {
		out := []string{}
		for _, req := range reqs {
			out = append(out, req.String())
		}
		return out
	}

	messages := &v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "stage-messages-fr", Namespace: "ns-b", Labels: map[string]string{"frontendenv": "stage", messagesLocaleLabel: "fr"}}}
	got := names(r.appsToEnqueueUponLocaleMessagesUpdate()(context.Background(), messages))
	if want := []string{"ns-a/a1", "ns-b/b1"}; !slices.Equal(got, want) {
		t.Errorf("messages update: got %v, want %v", got, want)
	}

	got = names(r.appsToEnqueueUponFrontendDeletion()(context.Background(), frontend("ns-a", "a1")))
	if want := []string{"ns-a/a2", "ns-b/b1"}; !slices.Equal(got, want) {
		t.Errorf("deletion: got %v, want %v", got, want)
	}
}
