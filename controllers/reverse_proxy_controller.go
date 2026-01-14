/*
Copyright 2025 RedHatInsights.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"github.com/go-logr/logr"
)

// ReverseProxyController reconciles reverse proxy resources based on FrontendEnvironment
type ReverseProxyController struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=cloud.redhat.com,resources=frontendenvironments,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles the reverse proxy reconciliation for a FrontendEnvironment
func (r *ReverseProxyController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log = log.FromContext(ctx)
	log := r.Log.WithValues("frontendenvironment", req.Name)

	// Fetch the FrontendEnvironment
	fe := &crd.FrontendEnvironment{}
	if err := r.Client.Get(ctx, req.NamespacedName, fe); err != nil {
		if k8serr.IsNotFound(err) {
			// Environment was deleted, nothing to do
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Only deploy reverse proxy if push cache is enabled and reverse proxy image is configured
	if !fe.Spec.EnablePushCache || fe.Spec.ReverseProxyImage == "" {
		log.Info("Skipping reverse proxy reconciliation: push cache not enabled or no image configured")
		return ctrl.Result{}, nil
	}

	log.Info("Reconciling reverse proxy", "namespace", fe.Spec.Namespace)

	// Create the reconciliation context
	reconciliation := ReverseProxyReconciliation{
		Log:                 log,
		Recorder:            r.Recorder,
		Client:              r.Client,
		Ctx:                 ctx,
		FrontendEnvironment: fe,
	}

	// Run the reconciliation
	if err := reconciliation.run(); err != nil {
		log.Error(err, "Failed to reconcile reverse proxy")
		return ctrl.Result{Requeue: true}, err
	}

	log.Info("Successfully reconciled reverse proxy", "namespace", fe.Spec.Namespace)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ReverseProxyController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crd.FrontendEnvironment{}).
		Named("reverseproxy").
		Complete(r)
}
