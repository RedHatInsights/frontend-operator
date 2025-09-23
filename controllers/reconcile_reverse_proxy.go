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
	"fmt"
	"strconv"

	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"github.com/RedHatInsights/rhc-osdk-utils/utils"
	"github.com/go-logr/logr"
)

const (
	ReverseProxyPort = 8080
)

// ReverseProxyReconciler reconciles reverse proxy resources for FrontendEnvironments
type ReverseProxyReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// ReverseProxyReconciliation handles the reconciliation logic for reverse proxy resources
type ReverseProxyReconciliation struct {
	Log                 logr.Logger
	Recorder            record.EventRecorder
	Client              client.Client
	Ctx                 context.Context
	Frontend            *crd.Frontend
	FrontendEnvironment *crd.FrontendEnvironment
}

//+kubebuilder:rbac:groups=cloud.redhat.com,resources=frontends,verbs=get;list;watch
//+kubebuilder:rbac:groups=cloud.redhat.com,resources=frontendenvironments,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// ReconcileReverseProxy handles the reverse proxy reconciliation for a frontend
func (r *ReverseProxyReconciler) ReconcileReverseProxy(ctx context.Context, frontend *crd.Frontend, fe *crd.FrontendEnvironment) error {
	log := r.Log.WithValues("reverse-proxy reconcile: triggered by", fmt.Sprintf("%s:%s", frontend.Namespace, frontend.Name))

	// Only deploy reverse proxy if push cache is enabled and reverse proxy image is configured
	if !fe.Spec.EnablePushCache || fe.Spec.ReverseProxyImage == "" {
		return nil
	}

	reconciliation := ReverseProxyReconciliation{
		Log:                 log,
		Recorder:            r.Recorder,
		Client:              r.Client,
		Ctx:                 ctx,
		Frontend:            frontend,
		FrontendEnvironment: fe,
	}

	return reconciliation.run()
}

// run executes the reverse proxy reconciliation logic
func (r *ReverseProxyReconciliation) run() error {
	// Check if reverse proxy deployment already exists
	existingDeployment := &apps.Deployment{}
	deploymentKey := types.NamespacedName{
		Name:      "reverse-proxy",
		Namespace: r.Frontend.Namespace,
	}

	err := r.Client.Get(r.Ctx, deploymentKey, existingDeployment)
	if err != nil && k8serr.IsNotFound(err) {
		if err := r.createReverseProxyDeployment(); err != nil {
			r.Log.Error(err, "Failed to create reverse proxy deployment")
			return err
		}
	} else if err != nil {
		return err
	}

	// Check if reverse proxy service already exists
	existingService := &v1.Service{}
	serviceKey := types.NamespacedName{
		Name:      "reverse-proxy",
		Namespace: r.Frontend.Namespace,
	}

	err = r.Client.Get(r.Ctx, serviceKey, existingService)
	if err != nil && k8serr.IsNotFound(err) {
		if err := r.createReverseProxyService(); err != nil {
			r.Log.Error(err, "Failed to create reverse proxy service")
			return err
		}
	} else if err != nil {
		return err
	}

	return nil
}

// createReverseProxyDeployment creates a reverse proxy deployment for frontend assets
func (r *ReverseProxyReconciliation) createReverseProxyDeployment() error {
	deploymentName := "reverse-proxy"

	// Define name of resource
	nn := types.NamespacedName{
		Name:      deploymentName,
		Namespace: r.Frontend.Namespace,
	}

	// Get consistent labels that won't conflict between frontends
	labels := r.getReverseProxyLabels()

	// Configure the reverse proxy container
	container, err := r.createReverseProxyContainer()
	if err != nil {
		return err
	}

	// Create the deployment
	deployment := &apps.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
			Labels:    labels,
			Annotations: map[string]string{
				"kube-linter.io/ignore-all": "we don't need no any checking",
			},
		},
		Spec: apps.DeploymentSpec{
			Replicas: utils.Int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{container},
				},
			},
		},
	}

	// Set owner reference to the environment instead of the frontend
	deployment.SetOwnerReferences([]metav1.OwnerReference{r.getReverseProxyOwnerRef()})

	return r.Client.Create(r.Ctx, deployment)
}

// createReverseProxyService creates a service for the reverse proxy deployment
func (r *ReverseProxyReconciliation) createReverseProxyService() error {
	serviceName := "reverse-proxy"

	// Define name of resource
	nn := types.NamespacedName{
		Name:      serviceName,
		Namespace: r.Frontend.Namespace,
	}

	// Get consistent labels that won't conflict between frontends
	labels := r.getReverseProxyLabels()

	// Create ports for the reverse proxy service
	appProtocol := "http"
	ports := []v1.ServicePort{
		{
			Name:        "http",
			Port:        ReverseProxyPort,
			TargetPort:  intstr.FromInt(ReverseProxyPort),
			Protocol:    "TCP",
			AppProtocol: &appProtocol,
		},
	}

	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
			Labels:    labels,
		},
		Spec: v1.ServiceSpec{
			Selector: labels,
			Ports:    ports,
		},
	}

	// Set owner reference to the environment instead of the frontend
	service.SetOwnerReferences([]metav1.OwnerReference{r.getReverseProxyOwnerRef()})

	return r.Client.Create(r.Ctx, service)
}

// createReverseProxyContainer configures the reverse proxy container
func (r *ReverseProxyReconciliation) createReverseProxyContainer() (v1.Container, error) {
	// Get object store configuration from environment variables (same as push cache)
	objectStoreInfo, err := ExtractBucketConfigFromEnv()
	if err != nil {
		return v1.Container{}, err
	}

	// Get default values
	minioPort := *objectStoreInfo.Port
	minioEndpoint := *objectStoreInfo.Endpoint // PUSHCACHE_AWS_ENDPOINT
	bucketPathPrefix := *objectStoreInfo.Name  // PUSHCACHE_AWS_BUCKET_NAME
	var minioUpstreamURL string
	var protocol string
	// Construct upstream URL with appropriate scheme based on port
	switch minioPort {
	case "443":
		protocol = "https://"
	default:
		// For non-standard ports, use http by default (local development)
		protocol = "http://"
	}
	minioUpstreamURL = protocol + minioEndpoint + ":" + minioPort

	logLevel := r.FrontendEnvironment.Spec.ReverseProxyLogLevel
	if logLevel == "" {
		logLevel = "DEBUG"
	}

	spaEntrypointPath := r.FrontendEnvironment.Spec.ReverseProxySPAEntrypointPath
	if spaEntrypointPath == "" {
		spaEntrypointPath = "/index.html"
	}

	// Environment variables for the reverse proxy using object store config
	envVars := []v1.EnvVar{
		{
			Name:  "SERVER_PORT",
			Value: strconv.Itoa(ReverseProxyPort),
		},
		{
			Name:  "MINIO_UPSTREAM_URL",
			Value: minioUpstreamURL,
		},
		{
			Name:  "BUCKET_PATH_PREFIX",
			Value: bucketPathPrefix,
		},
		{
			Name:  "SPA_ENTRYPOINT_PATH",
			Value: spaEntrypointPath,
		},
		{
			Name:  "LOG_LEVEL",
			Value: logLevel,
		},
	}

	// Configure the container
	container := v1.Container{
		Name:  "reverse-proxy",
		Image: r.FrontendEnvironment.Spec.ReverseProxyImage,
		Ports: []v1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: int32(ReverseProxyPort),
				Protocol:      "TCP",
			},
		},
		Env: envVars,
		Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("30m"),
				v1.ResourceMemory: resource.MustParse("50Mi"),
			},
			Limits: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("100m"),
				v1.ResourceMemory: resource.MustParse("128Mi"),
			},
		},
		LivenessProbe: &v1.Probe{
			ProbeHandler: v1.ProbeHandler{
				HTTPGet: &v1.HTTPGetAction{
					Path:   "/healthz",
					Port:   intstr.FromInt(ReverseProxyPort),
					Scheme: v1.URISchemeHTTP,
				},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       30,
			FailureThreshold:    3,
		},
		ReadinessProbe: &v1.Probe{
			ProbeHandler: v1.ProbeHandler{
				HTTPGet: &v1.HTTPGetAction{
					Path:   "/healthz",
					Port:   intstr.FromInt(ReverseProxyPort),
					Scheme: v1.URISchemeHTTP,
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
		},
	}

	return container, nil
}

// getReverseProxyOwnerRef returns an owner reference to the FrontendEnvironment
// instead of the individual Frontend, which allows multiple Frontends sharing
// the same environment to co-exist without conflicts.
func (r *ReverseProxyReconciliation) getReverseProxyOwnerRef() metav1.OwnerReference {
	// If we have a FrontendEnvironment, use it as the owner
	if r.FrontendEnvironment != nil {
		// Create a ClusterRole-referencing OwnerReference since FrontendEnvironment is cluster-scoped
		return metav1.OwnerReference{
			APIVersion: r.FrontendEnvironment.APIVersion,
			Kind:       r.FrontendEnvironment.Kind,
			Name:       r.FrontendEnvironment.Name,
			UID:        r.FrontendEnvironment.UID,
			// We don't want to set controller=true since FrontendEnvironment is cluster-scoped
			// and the reverse proxy is namespace-scoped
		}
	}

	// Fall back to the Frontend if no environment is available
	return r.Frontend.MakeOwnerReference()
}

// getReverseProxyLabels returns consistent labels for the reverse proxy
// that don't depend on the specific Frontend being reconciled
func (r *ReverseProxyReconciliation) getReverseProxyLabels() map[string]string {
	labels := make(map[string]string)

	// Use environment-specific labels
	if r.FrontendEnvironment != nil {
		labels["environment"] = r.FrontendEnvironment.Name
	}

	// Add common reverse proxy labels
	labels["app"] = "reverse-proxy"
	labels["component"] = "reverse-proxy"

	return labels
}
