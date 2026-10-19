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
	"sort"
	"time"

	apps "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	resCache "github.com/RedHatInsights/rhc-osdk-utils/resourceCache"
	prom "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	"github.com/RedHatInsights/rhc-osdk-utils/utils"
	"github.com/go-logr/logr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	"github.com/prometheus/client_golang/prometheus"
)

const frontendFinalizer = "finalizer.frontend.cloud.redhat.com"

type FEKey string

func createNewScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(prom.AddToScheme(scheme))
	utilruntime.Must(crd.AddToScheme(scheme))

	return scheme
}

var scheme = createNewScheme()

var CoreDeployment = resCache.NewSingleResourceIdent("main", "deployment", &apps.Deployment{})
var CacheBustJob = resCache.NewSingleResourceIdent("main", "cachebust_job", &batchv1.Job{})
var PushCacheJob = resCache.NewSingleResourceIdent("main", "pushcache_job", &batchv1.Job{})
var CoreService = resCache.NewSingleResourceIdent("main", "service", &v1.Service{})
var CoreConfig = resCache.NewSingleResourceIdent("main", "config", &v1.ConfigMap{})
var SSOConfig = resCache.NewSingleResourceIdent("main", "sso_config", &v1.ConfigMap{})
var WebIngress = resCache.NewMultiResourceIdent("ingress", "web_ingress", &networking.Ingress{})
var MetricsServiceMonitor = resCache.NewMultiResourceIdent("main", "metrics-service-monitor", &prom.ServiceMonitor{})

type ReconciliationMetrics struct {
	appName            string
	reconcileStartTime time.Time
}

func (rm *ReconciliationMetrics) init(appName string) {
	rm.appName = appName
}

func (rm *ReconciliationMetrics) start() {
	rm.reconcileStartTime = time.Now()
}

func (rm *ReconciliationMetrics) stop() {
	elapsedTime := time.Since(rm.reconcileStartTime).Seconds()
	reconciliationTimeMetrics.With(prometheus.Labels{"app": rm.appName}).Observe(elapsedTime)
}

// FrontendReconciler reconciles a Frontend object
type FrontendReconciler struct {
	client.Client
	Log                   logr.Logger
	Scheme                *runtime.Scheme
	Recorder              record.EventRecorder
	reconciliationMetrics ReconciliationMetrics
}

//+kubebuilder:rbac:groups=cloud.redhat.com,resources=frontends,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cloud.redhat.com,resources=frontends/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cloud.redhat.com,resources=frontends/finalizers,verbs=update

//+kubebuilder:rbac:groups=cloud.redhat.com,resources=frontendenvironments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cloud.redhat.com,resources=frontendenvironments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cloud.redhat.com,resources=frontendenvironments/finalizers,verbs=update

//+kubebuilder:rbac:groups=cloud.redhat.com,resources=bundles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cloud.redhat.com,resources=bundles/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cloud.redhat.com,resources=bundles/finalizers,verbs=update

// +kubebuilder:rbac:groups="",resources=serviceaccounts;configmaps;services;secrets;persistentvolumeclaims;events;namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=cronjobs;jobs,verbs=get;list;create;update;watch;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=prometheuses;servicemonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=endpoints;pods,verbs=get;list;watch

// Reconcile manages the lifecycle of Frontend resources.
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

	if frontend.Spec.Disabled {
		return ctrl.Result{}, fmt.Errorf("frontend is disabled")
	}

	r.reconciliationMetrics = ReconciliationMetrics{}
	r.reconciliationMetrics.init(req.Name)
	r.reconciliationMetrics.start()

	reconciliationRequestMetric.With(prometheus.Labels{"app": req.Name}).Inc()

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

	fe := &crd.FrontendEnvironment{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: frontend.Spec.EnvName}, fe); err != nil {
		return ctrl.Result{Requeue: false}, err
	}

	ctx = context.WithValue(ctx, FEKey("obj"), &frontend)

	// RetryOnConflict handles 409 conflicts caused by the reconciler racing
	// against itself (Owns watch triggers, FrontendEnvironment/Bundle fan-out).
	// Each retry re-fetches resources to get the latest resourceVersion.
	var cache resCache.ObjectCache
	var localesCondition *metav1.Condition
	var peerLocalesConditions map[types.NamespacedName]*metav1.Condition
	var requeueAfter time.Duration
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Re-fetch Frontend and FrontendEnvironment on each retry to get the
		// latest resourceVersion, preventing stale-object 409 loops.
		if err := r.Client.Get(ctx, req.NamespacedName, &frontend); err != nil {
			return err
		}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: frontend.Spec.EnvName}, fe); err != nil {
			return err
		}

		cacheConfig := resCache.NewCacheConfig(scheme, nil, nil, resCache.Options{})
		cache = resCache.NewObjectCache(ctx, r.Client, &log, cacheConfig)
		cache.AddPossibleGVKFromIdent(
			CoreDeployment,
			CoreService,
			CoreConfig,
			SSOConfig,
			WebIngress,
			MetricsServiceMonitor,
			CacheBustJob,
			PushCacheJob,
		)

		reconciliation := FrontendReconciliation{
			Log:                 log,
			Recorder:            r.Recorder,
			Cache:               cache,
			FRE:                 r,
			FrontendEnvironment: fe,
			Ctx:                 ctx,
			Frontend:            &frontend,
			Client:              r.Client,
		}

		if err := reconciliation.run(); err != nil {
			return err
		}
		localesCondition = reconciliation.localesCondition()
		peerLocalesConditions = reconciliation.peerLocalesConditions()
		requeueAfter = reconciliation.requeueAfter

		return cache.ApplyAll()
	})

	if retryErr != nil {
		if sErr := SetFrontendConditions(ctx, r.Client, &frontend, crd.ReconciliationFailed, retryErr); sErr != nil {
			return ctrl.Result{Requeue: true}, fmt.Errorf("error setting status after reconciliation error: %w", sErr)
		}
		return ctrl.Result{Requeue: true}, retryErr
	}

	opts := []client.ListOption{
		client.MatchingLabels{"frontend": frontend.Name},
	}

	err = cache.Reconcile(frontend.GetUID(), opts...)
	if err != nil {
		log.Info("Reconcile error", "error", err)
		if sErr := SetFrontendConditions(ctx, r.Client, &frontend, crd.ReconciliationFailed, err); sErr != nil {
			return ctrl.Result{Requeue: true}, fmt.Errorf("error setting status after reconcile delete error: %w", sErr)
		}
		return ctrl.Result{Requeue: true}, err
	}

	if _, ok := managedFrontends[frontend.GetIdent()]; !ok {
		managedFrontends[frontend.GetIdent()] = true
	}
	managedFrontendsMetric.Set(float64(len(managedFrontends)))

	log.Info("Reconciliation successful", "app", fmt.Sprintf("%s:%s", frontend.Namespace, frontend.Name))
	if err = SetFrontendConditions(ctx, r.Client, &frontend, crd.ReconciliationSuccessful, nil, localesCondition); err != nil {
		return ctrl.Result{Requeue: true}, nil
	}

	if err = setPeerLocalesConditions(ctx, r.Client, peerLocalesConditions); err != nil {
		log.Info("Cannot update LocalesValid of other Frontends", "error", err.Error())
		return ctrl.Result{Requeue: true}, nil
	}

	log.Info("Finished reconcile")
	r.reconciliationMetrics.stop()
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FrontendReconciler) SetupWithManager(mgr ctrl.Manager) error {

	cache := mgr.GetCache()

	if err := cache.IndexField(
		context.TODO(), &crd.Frontend{}, "spec.envName", func(o client.Object) []string {
			return []string{o.(*crd.Frontend).Spec.EnvName}
		}); err != nil {
		return err
	}

	if err := cache.IndexField(
		context.TODO(), &crd.Bundle{}, "spec.envName", func(o client.Object) []string {
			return []string{o.(*crd.Bundle).Spec.EnvName}
		}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&crd.Frontend{}, builder.WithPredicates(defaultPredicate(r.Log, "frontend"))).
		Watches(
			&v1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.appsToEnqueueUponLocaleMessagesUpdate()),
			builder.WithPredicates(localeMessagesPredicate()),
		).
		Watches(
			&crd.Frontend{},
			handler.EnqueueRequestsFromMapFunc(r.appsToEnqueueUponFrontendDeletion()),
			builder.WithPredicates(frontendDeletionPredicate()),
		).
		Watches(
			&crd.Bundle{},
			handler.EnqueueRequestsFromMapFunc(r.appsToEnqueueUponBundleUpdate()),
		).
		Watches(
			&crd.FrontendEnvironment{},
			handler.EnqueueRequestsFromMapFunc(r.appsToEnqueueUponFrontendEnvironmentUpdate()),
		).
		// GenerationChangedPredicate filters out status-only updates (e.g. pod
		// readiness) that don't change metadata.generation, preventing unnecessary
		// reconciliations and 409 conflicts (RHCLOUD-46492).
		Owns(&apps.Deployment{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&networking.Ingress{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&prom.ServiceMonitor{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		// Watch Jobs (cachebust/pushcache) so a completed deletion re-enqueues the
		// Frontend and recreates them, e.g. after a disable->enable toggle. Delete
		// events pass GenerationChangedPredicate.
		Owns(&batchv1.Job{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

func localeMessagesPredicate() predicate.Funcs {
	isLocaleMessages := func(obj client.Object) bool {
		return obj.GetLabels()[messagesLocaleLabel] != "" && obj.GetLabels()["frontendenv"] != ""
	}
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isLocaleMessages(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isLocaleMessages(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldCM, oldOK := e.ObjectOld.(*v1.ConfigMap)
			newCM, newOK := e.ObjectNew.(*v1.ConfigMap)
			if !oldOK || !newOK || !isLocaleMessages(oldCM) && !isLocaleMessages(newCM) {
				return false
			}
			return !equality.Semantic.DeepEqual(oldCM.Data, newCM.Data) || !equality.Semantic.DeepEqual(oldCM.BinaryData, newCM.BinaryData)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return isLocaleMessages(e.Object)
		},
	}
}

func logMessage(logr logr.Logger, msg string, keysAndValues ...interface{}) {
	logr.Info(msg, keysAndValues...)
}

func defaultPredicate(logr logr.Logger, ctrlName string) predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			gvk, _ := utils.GetKindFromObj(scheme, e.Object)
			logMessage(logr, "Reconciliation trigger", "ctrl", ctrlName, "type", "create", "resType", gvk.Kind, "name", e.Object.GetName(), "namespace", e.Object.GetNamespace())
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			gvk, _ := utils.GetKindFromObj(scheme, e.Object)
			logMessage(logr, "Reconciliation trigger", "ctrl", ctrlName, "type", "delete", "resType", gvk.Kind, "name", e.Object.GetName(), "namespace", e.Object.GetNamespace())
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldFrontend, oldOK := e.ObjectOld.(*crd.Frontend)
			newFrontend, newOK := e.ObjectNew.(*crd.Frontend)
			// Frontend status is written by this controller. Spec/metadata changes
			// still pass through, and owned resources have their own watches.
			if oldOK && newOK && isFrontendStatusOnlyUpdate(oldFrontend, newFrontend) {
				return false
			}
			gvk, _ := utils.GetKindFromObj(scheme, e.ObjectOld)
			logMessage(logr, "Reconciliation trigger", "ctrl", ctrlName, "type", "update", "resType", gvk.Kind, "name", e.ObjectNew.GetName(), "namespace", e.ObjectNew.GetNamespace(), "old", e.ObjectOld, "new", e.ObjectNew)
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			gvk, _ := utils.GetKindFromObj(scheme, e.Object)
			logMessage(logr, "Reconciliation trigger", "ctrl", ctrlName, "type", "generic", "resType", gvk.Kind, "name", e.Object.GetName(), "namespace", e.Object.GetNamespace())
			return true
		},
	}
}

func isFrontendStatusOnlyUpdate(oldFrontend, newFrontend *crd.Frontend) bool {
	if equality.Semantic.DeepEqual(oldFrontend.Status, newFrontend.Status) {
		return false
	}
	oldCopy := oldFrontend.DeepCopy()
	newCopy := newFrontend.DeepCopy()
	oldCopy.Status = newCopy.Status
	oldCopy.ResourceVersion = newCopy.ResourceVersion
	oldCopy.ManagedFields = newCopy.ManagedFields
	return equality.Semantic.DeepEqual(oldCopy, newCopy)
}

func frontendDeletionPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldFrontend, oldOK := e.ObjectOld.(*crd.Frontend)
			newFrontend, newOK := e.ObjectNew.(*crd.Frontend)
			return oldOK && newOK && oldFrontend.DeletionTimestamp == nil && newFrontend.DeletionTimestamp != nil
		},
		DeleteFunc: func(event.DeleteEvent) bool {
			return true
		},
		GenericFunc: func(event.GenericEvent) bool {
			return false
		},
	}
}

func (r *FrontendReconciler) appsToEnqueueUponBundleUpdate() handler.MapFunc {
	return func(ctx context.Context, clientObject client.Object) []reconcile.Request {
		reqs := []reconcile.Request{}
		obj := types.NamespacedName{
			Name:      clientObject.GetName(),
			Namespace: clientObject.GetNamespace(),
		}

		// Get the Bundle resource

		bundle := crd.Bundle{}
		err := r.Client.Get(ctx, obj, &bundle)

		if err != nil {
			if k8serr.IsNotFound(err) {
				// Must have been deleted
				return reqs
			}
			r.Log.Error(err, "Failed to fetch Bundle")
			return nil
		}

		// Get all the ClowdApp resources

		frontendList := crd.FrontendList{}
		err = r.Client.List(ctx, &frontendList, client.MatchingFields{"spec.envName": bundle.Spec.EnvName})
		if err != nil {
			r.Log.Error(err, "Failed to List Frontends")
			return nil
		}

		// Filter based on base attribute

		for _, frontend := range frontendList.Items {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      frontend.Name,
					Namespace: frontend.Namespace,
				},
			})
		}

		return reqs
	}
}

func (r *FrontendReconciler) appsToEnqueueUponFrontendEnvironmentUpdate() handler.MapFunc {
	return func(ctx context.Context, clientObject client.Object) []reconcile.Request {
		reqs := []reconcile.Request{}
		obj := types.NamespacedName{
			Name:      clientObject.GetName(),
			Namespace: clientObject.GetNamespace(),
		}

		// Get the Bundle resource

		fe := crd.FrontendEnvironment{}
		err := r.Client.Get(ctx, obj, &fe)

		if err != nil {
			if k8serr.IsNotFound(err) {
				// Must have been deleted
				return reqs
			}
			r.Log.Error(err, "Failed to fetch Bundle")
			return nil
		}

		// Get all the ClowdApp resources

		frontendList := crd.FrontendList{}
		if err := r.Client.List(ctx, &frontendList, client.MatchingFields{"spec.envName": fe.Name}); err != nil {
			r.Log.Error(err, "Failed to List Frontends")
			return nil
		}

		// Filter based on base attribute

		for _, frontend := range frontendList.Items {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      frontend.Name,
					Namespace: frontend.Namespace,
				},
			})
		}

		return reqs
	}
}

func (r *FrontendReconciler) appsToEnqueueUponLocaleMessagesUpdate() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		envName := obj.GetLabels()["frontendenv"]
		if envName == "" {
			return nil
		}
		return r.localeRepublishRequests(ctx, envName, nil)
	}
}

func (r *FrontendReconciler) appsToEnqueueUponFrontendDeletion() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		frontend, ok := obj.(*crd.Frontend)
		if !ok || frontend.Spec.EnvName == "" {
			return nil
		}
		return r.localeRepublishRequests(ctx, frontend.Spec.EnvName, frontend)
	}
}

// localeRepublishRequests picks one live Frontend, other than skip, in each
// namespace of the environment. A reconcile republishes the target messages
// and all peer conditions, but writes the source messages only in its own
// namespace, so every namespace needs one.
func (r *FrontendReconciler) localeRepublishRequests(ctx context.Context, envName string, skip *crd.Frontend) []reconcile.Request {
	frontends := &crd.FrontendList{}
	if err := r.Client.List(ctx, frontends, client.MatchingFields{"spec.envName": envName}); err != nil {
		r.Log.Error(err, "Failed to list Frontends to republish locale messages", "environment", envName)
		return nil
	}
	sort.Slice(frontends.Items, func(i, j int) bool {
		if frontends.Items[i].Namespace != frontends.Items[j].Namespace {
			return frontends.Items[i].Namespace < frontends.Items[j].Namespace
		}
		return frontends.Items[i].Name < frontends.Items[j].Name
	})
	reqs := []reconcile.Request{}
	seen := map[string]bool{}
	for _, frontend := range frontends.Items {
		if frontend.DeletionTimestamp != nil || frontend.Spec.Disabled || seen[frontend.Namespace] {
			continue
		}
		if skip != nil && frontend.Namespace == skip.Namespace && frontend.Name == skip.Name {
			continue
		}
		seen[frontend.Namespace] = true
		reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Name: frontend.Name, Namespace: frontend.Namespace}})
	}
	return reqs
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
