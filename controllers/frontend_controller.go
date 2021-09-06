/*
Copyright 2021 RedHatInsights.

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
	"fmt"

	apps "k8s.io/api/apps/v1"
	networking "k8s.io/api/networking/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	resCache "github.com/RedHatInsights/rhc-osdk-utils/resource_cache"
	"github.com/RedHatInsights/rhc-osdk-utils/utils"
	"github.com/go-logr/logr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

const frontendFinalizer = "finalizer.frontend.cloud.redhat.com"

type FEKey string

func createNewScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(crd.AddToScheme(scheme))
	return scheme
}

var scheme = createNewScheme()

var cacheConfig = resCache.NewCacheConfig(scheme, FEKey("log"))

var CoreDeployment = cacheConfig.NewSingleResourceIdent("main", "deployment", &apps.Deployment{})
var ConfigDeployment = cacheConfig.NewSingleResourceIdent("config", "deployment", &apps.Deployment{})
var WebIngress = cacheConfig.NewMultiResourceIdent("ingress", "web_ingress", &networking.Ingress{})

// FrontendReconciler reconciles a Frontend object
type FrontendReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=cloud.redhat.com,resources=frontends,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cloud.redhat.com,resources=frontends/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cloud.redhat.com,resources=frontends/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Frontend object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *FrontendReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log = log.FromContext(ctx)
	qualifiedName := fmt.Sprintf("%s:%s", req.Namespace, req.Name)
	log := r.Log.WithValues("frontend", qualifiedName).WithValues("id", utils.RandString(5))
	ctx = context.WithValue(ctx, FEKey("log"), &log)
	ctx = context.WithValue(ctx, FEKey("recorder"), &r.Recorder)
	frontend := crd.Frontend{}
	err := r.Client.Get(ctx, req.NamespacedName, &frontend)

	if err != nil {
		if k8serr.IsNotFound(err) {
			// Must have been deleted
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	isAppMarkedForDeletion := frontend.GetDeletionTimestamp() != nil
	if isAppMarkedForDeletion {
		if utils.Contains(frontend.GetFinalizers(), frontendFinalizer) {
			if err := r.finalizeApp(log, &frontend); err != nil {
				return ctrl.Result{}, err
			}

			controllerutil.RemoveFinalizer(&frontend, frontendFinalizer)
			err := r.Update(ctx, &frontend)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer for this CR
	if !utils.Contains(frontend.GetFinalizers(), frontendFinalizer) {
		if err := r.addFinalizer(log, &frontend); err != nil {
			return ctrl.Result{}, err
		}
	}

	log.Info("Reconciliation started", "app", fmt.Sprintf("%s:%s", frontend.Namespace, frontend.Name))

	ctx = context.WithValue(ctx, FEKey("obj"), &frontend)

	cache := resCache.NewObjectCache(ctx, r.Client, cacheConfig)

	err = runReconciliation(&frontend, &cache)

	if err != nil {
		//	SetClowdAppConditions(ctx, r.Client, &frontend, crd.ReconciliationFailed, err)
		return ctrl.Result{Requeue: true}, err
	}

	// if err != nil {
	// 	SetClowdAppConditions(ctx, r.Client, &frontend, crd.ReconciliationFailed, err)
	// 	return ctrl.Result{Requeue: true}, err
	// }

	cacheErr := cache.ApplyAll()

	if cacheErr != nil {
		//	SetClowdAppConditions(ctx, r.Client, &frontend, crd.ReconciliationFailed, err)
		return ctrl.Result{Requeue: true}, cacheErr
	}

	// log.Info("Reconciliation successful", "app", fmt.Sprintf("%s:%s", app.Namespace, app.Name))
	// err := cache.Reconcile(&app)
	// if err != nil {
	// 	log.Info("Reconcile error", "error", err)
	// 	return ctrl.Result{Requeue: requeue}, nil
	// }
	// SetClowdAppConditions(ctx, r.Client, &frontend, crd.ReconciliationSuccessful, nil)

	if err == nil {
		if _, ok := managedFrontends[frontend.GetIdent()]; !ok {
			managedFrontends[frontend.GetIdent()] = true
		}
		managedFrontendsMetric.Set(float64(len(managedFrontends)))
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FrontendReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crd.Frontend{}).
		Complete(r)
}

func (r *FrontendReconciler) finalizeApp(reqLogger logr.Logger, a *crd.Frontend) error {

	delete(managedFrontends, a.GetIdent())

	managedFrontendsMetric.Set(float64(len(managedFrontends)))
	reqLogger.Info("Successfully finalized ClowdApp")
	return nil
}

func (r *FrontendReconciler) addFinalizer(reqLogger logr.Logger, a *crd.Frontend) error {
	reqLogger.Info("Adding Finalizer for the ClowdApp")
	controllerutil.AddFinalizer(a, frontendFinalizer)

	// Update CR
	err := r.Update(context.TODO(), a)
	if err != nil {
		reqLogger.Error(err, "Failed to update ClowdApp with finalizer")
		return err
	}
	return nil
}
