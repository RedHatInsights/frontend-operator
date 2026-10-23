package controllers

import (
	"context"
	"slices"
	"testing"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

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
