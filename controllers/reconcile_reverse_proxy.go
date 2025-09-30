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
	"strings"

	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
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
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

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
	// Reconcile deployment
	if err := r.reconcileDeployment(); err != nil {
		return err
	}

	// Reconcile service
	if err := r.reconcileService(); err != nil {
		return err
	}

	// Reconcile ingress
	if err := r.reconcileIngress(); err != nil {
		return err
	}

	return nil
}

// reconcileDeployment ensures the reverse proxy deployment exists and is up to date
func (r *ReverseProxyReconciliation) reconcileDeployment() error {
	deployment := &apps.Deployment{}
	deploymentKey := types.NamespacedName{
		Name:      "reverse-proxy",
		Namespace: r.Frontend.Namespace,
	}

	err := r.Client.Get(r.Ctx, deploymentKey, deployment)
	if err != nil && k8serr.IsNotFound(err) {
		// Deployment doesn't exist, create it
		return r.createReverseProxyDeployment()
	} else if err != nil {
		return err
	}

	// Deployment exists, ensure it's up to date
	return r.updateReverseProxyDeployment(deployment)
}

// reconcileService ensures the reverse proxy service exists and is up to date
func (r *ReverseProxyReconciliation) reconcileService() error {
	service := &v1.Service{}
	serviceKey := types.NamespacedName{
		Name:      "reverse-proxy",
		Namespace: r.Frontend.Namespace,
	}

	err := r.Client.Get(r.Ctx, serviceKey, service)
	if err != nil && k8serr.IsNotFound(err) {
		// Service doesn't exist, create it
		return r.createReverseProxyService()
	} else if err != nil {
		return err
	}

	// Service exists, ensure it's up to date
	return r.updateReverseProxyService(service)
}

// updateReverseProxyDeployment ensures the deployment matches the desired state
func (r *ReverseProxyReconciliation) updateReverseProxyDeployment(deployment *apps.Deployment) error {
	// Get the desired container configuration
	desiredContainer, err := r.createReverseProxyContainer()
	if err != nil {
		return err
	}

	// Get the desired volumes configuration
	desiredVolumes := r.createReverseProxyVolumes()

	// Get current container
	currentContainer := &deployment.Spec.Template.Spec.Containers[0]

	// Check if container needs update
	containerNeedsUpdate, updateReason := r.compareContainer(currentContainer, &desiredContainer)

	// Check if volumes need update
	volumesNeedsUpdate := r.compareVolumes(deployment.Spec.Template.Spec.Volumes, desiredVolumes)

	if containerNeedsUpdate || volumesNeedsUpdate {
		if containerNeedsUpdate {
			r.Log.Info("Updating reverse proxy deployment", "reason", updateReason)
		}
		if volumesNeedsUpdate {
			r.Log.Info("Updating reverse proxy deployment volumes", "reason", "volumes configuration changed")
		}

		// Update the entire container specification
		deployment.Spec.Template.Spec.Containers[0] = desiredContainer

		// Update the volumes specification
		deployment.Spec.Template.Spec.Volumes = desiredVolumes

		// Add restart annotation to force pod restart
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = make(map[string]string)
		}
		deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = metav1.Now().Format("2006-01-02T15:04:05Z")

		// Update the deployment
		return r.Client.Update(r.Ctx, deployment)
	}

	return nil
}

// compareEnvVars compares two environment variable slices for equality
func (r *ReverseProxyReconciliation) compareEnvVars(existing, desired []v1.EnvVar) bool {
	if len(existing) != len(desired) {
		return false
	}

	existingMap := make(map[string]string)
	for _, env := range existing {
		existingMap[env.Name] = env.Value
	}

	for _, env := range desired {
		if value, exists := existingMap[env.Name]; !exists || value != env.Value {
			return false
		}
	}

	return true
}

// compareContainer compares current and desired container specifications
func (r *ReverseProxyReconciliation) compareContainer(current, desired *v1.Container) (bool, string) {
	// Check image
	if current.Image != desired.Image {
		return true, fmt.Sprintf("image changed from %s to %s", current.Image, desired.Image)
	}

	// Check environment variables
	if !r.compareEnvVars(current.Env, desired.Env) {
		return true, "environment variables changed"
	}

	// Check ports
	if !r.compareContainerPorts(current.Ports, desired.Ports) {
		return true, "container ports changed"
	}

	// Check resource requirements
	if !r.compareResourceRequirements(current.Resources, desired.Resources) {
		return true, "resource requirements changed"
	}

	// Check probes
	if !r.compareProbes(current.LivenessProbe, desired.LivenessProbe) {
		return true, "liveness probe changed"
	}

	if !r.compareProbes(current.ReadinessProbe, desired.ReadinessProbe) {
		return true, "readiness probe changed"
	}

	return false, ""
}

// compareContainerPorts compares two container port slices for equality
func (r *ReverseProxyReconciliation) compareContainerPorts(current, desired []v1.ContainerPort) bool {
	if len(current) != len(desired) {
		return false
	}

	currentMap := make(map[string]v1.ContainerPort)
	for _, port := range current {
		currentMap[port.Name] = port
	}

	for _, port := range desired {
		if currentPort, exists := currentMap[port.Name]; !exists ||
			currentPort.ContainerPort != port.ContainerPort ||
			currentPort.Protocol != port.Protocol {
			return false
		}
	}

	return true
}

// compareResourceRequirements compares resource requirements for equality
func (r *ReverseProxyReconciliation) compareResourceRequirements(current, desired v1.ResourceRequirements) bool {
	// Compare requests
	if !r.compareResourceList(current.Requests, desired.Requests) {
		return false
	}

	// Compare limits
	if !r.compareResourceList(current.Limits, desired.Limits) {
		return false
	}

	return true
}

// compareResourceList compares resource lists for equality
func (r *ReverseProxyReconciliation) compareResourceList(current, desired v1.ResourceList) bool {
	if len(current) != len(desired) {
		return false
	}

	for resource, desiredQuantity := range desired {
		if currentQuantity, exists := current[resource]; !exists || !currentQuantity.Equal(desiredQuantity) {
			return false
		}
	}

	return true
}

// compareProbes compares two probes for equality
func (r *ReverseProxyReconciliation) compareProbes(current, desired *v1.Probe) bool {
	// Both nil
	if current == nil && desired == nil {
		return true
	}

	// One nil, one not
	if current == nil || desired == nil {
		return false
	}

	// Compare basic settings
	if current.InitialDelaySeconds != desired.InitialDelaySeconds ||
		current.PeriodSeconds != desired.PeriodSeconds ||
		current.FailureThreshold != desired.FailureThreshold {
		return false
	}

	// Compare HTTP probe handlers
	if current.HTTPGet != nil && desired.HTTPGet != nil {
		return current.HTTPGet.Path == desired.HTTPGet.Path &&
			current.HTTPGet.Port == desired.HTTPGet.Port &&
			current.HTTPGet.Scheme == desired.HTTPGet.Scheme
	}

	// If one has HTTPGet and the other doesn't, they're different
	if (current.HTTPGet == nil) != (desired.HTTPGet == nil) {
		return false
	}

	// For other probe types, we'd need additional comparisons
	// but for now we only use HTTPGet probes
	return true
}

// createReverseProxyVolumes creates the volumes needed for the reverse proxy deployment
func (r *ReverseProxyReconciliation) createReverseProxyVolumes() []v1.Volume {
	volumes := []v1.Volume{}

	// Add SSL certificate volume if SSL is enabled
	if r.FrontendEnvironment.Spec.SSL {
		volumes = append(volumes, v1.Volume{
			Name: "certs",
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: "reverse-proxy-cert",
				},
			},
		})
	}

	return volumes
}

// compareVolumes compares current and desired volume configurations
func (r *ReverseProxyReconciliation) compareVolumes(current, desired []v1.Volume) bool {
	if len(current) != len(desired) {
		return true
	}

	// Create maps for easier comparison
	currentMap := make(map[string]v1.Volume)
	for _, vol := range current {
		currentMap[vol.Name] = vol
	}

	for _, desiredVol := range desired {
		currentVol, exists := currentMap[desiredVol.Name]
		if !exists {
			return true
		}

		// Compare secret volumes (the main case we care about)
		if desiredVol.Secret != nil {
			if currentVol.Secret == nil {
				return true
			}
			if currentVol.Secret.SecretName != desiredVol.Secret.SecretName {
				return true
			}
		} else if currentVol.Secret != nil {
			return true
		}
	}

	return false
}

// updateReverseProxyService ensures the service matches the desired state
func (r *ReverseProxyReconciliation) updateReverseProxyService(service *v1.Service) error {
	// Get the desired service configuration
	desiredService, err := r.createReverseProxyServiceConfig()
	if err != nil {
		return err
	}

	// Compare and update if needed
	serviceChanged := r.compareService(service, desiredService)

	if serviceChanged {
		r.Log.Info("Updating reverse proxy service with new configuration")

		// Update the service spec
		service.Spec.Ports = desiredService.Spec.Ports
		service.Spec.Selector = desiredService.Spec.Selector
		service.Labels = desiredService.Labels

		// Update the service
		return r.Client.Update(r.Ctx, service)
	}

	return nil
}

// compareService compares current vs desired service configuration
func (r *ReverseProxyReconciliation) compareService(current, desired *v1.Service) bool {
	// Compare ports
	if len(current.Spec.Ports) != len(desired.Spec.Ports) {
		return true
	}

	for i, currentPort := range current.Spec.Ports {
		if i >= len(desired.Spec.Ports) {
			return true
		}
		desiredPort := desired.Spec.Ports[i]

		if currentPort.Name != desiredPort.Name ||
			currentPort.Port != desiredPort.Port ||
			currentPort.TargetPort != desiredPort.TargetPort ||
			currentPort.Protocol != desiredPort.Protocol {
			return true
		}

		// Compare AppProtocol (handle nil pointers)
		if (currentPort.AppProtocol == nil) != (desiredPort.AppProtocol == nil) {
			return true
		}
		if currentPort.AppProtocol != nil && desiredPort.AppProtocol != nil &&
			*currentPort.AppProtocol != *desiredPort.AppProtocol {
			return true
		}
	}

	// Compare selectors
	if len(current.Spec.Selector) != len(desired.Spec.Selector) {
		return true
	}

	for key, currentValue := range current.Spec.Selector {
		if desiredValue, exists := desired.Spec.Selector[key]; !exists || currentValue != desiredValue {
			return true
		}
	}

	// Compare important labels
	importantLabels := []string{"app", "component", "environment"}
	for _, label := range importantLabels {
		currentValue := current.Labels[label]
		desiredValue := desired.Labels[label]
		if currentValue != desiredValue {
			return true
		}
	}

	return false
}

// createReverseProxyServiceConfig creates the desired service configuration
func (r *ReverseProxyReconciliation) createReverseProxyServiceConfig() (*v1.Service, error) {
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

	return service, nil
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

	// Create volumes for the deployment
	volumes := r.createReverseProxyVolumes()

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
					Volumes:    volumes,
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
	service, err := r.createReverseProxyServiceConfig()
	if err != nil {
		return err
	}

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

	// Add SSL environment variables if SSL is enabled (similar to main reconciler)
	if r.FrontendEnvironment.Spec.SSL {
		envVars = append(envVars, v1.EnvVar{
			Name:  "CADDY_TLS_MODE",
			Value: "https_port 8080",
		})
		envVars = append(envVars, v1.EnvVar{
			Name:  "CADDY_TLS_CERT",
			Value: "tls /opt/certs/tls.crt /opt/certs/tls.key",
		})
	}

	// Volume mounts for the reverse proxy
	volumeMounts := []v1.VolumeMount{}

	// Add SSL certificate volume mount if SSL is enabled
	if r.FrontendEnvironment.Spec.SSL {
		volumeMounts = append(volumeMounts, v1.VolumeMount{
			Name:      "certs",
			MountPath: "/opt/certs",
		})
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
		Env:          envVars,
		VolumeMounts: volumeMounts,
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
	}

	// Set the URI Scheme for the probes (HTTP or HTTPS based on SSL config)
	probeScheme := v1.URISchemeHTTP
	if r.FrontendEnvironment.Spec.SSL {
		probeScheme = v1.URISchemeHTTPS
	}

	container.LivenessProbe = &v1.Probe{
		ProbeHandler: v1.ProbeHandler{
			HTTPGet: &v1.HTTPGetAction{
				Path:   "/healthz",
				Port:   intstr.FromInt(ReverseProxyPort),
				Scheme: probeScheme,
			},
		},
		InitialDelaySeconds: 10,
		PeriodSeconds:       30,
		FailureThreshold:    3,
	}

	container.ReadinessProbe = &v1.Probe{
		ProbeHandler: v1.ProbeHandler{
			HTTPGet: &v1.HTTPGetAction{
				Path:   "/healthz",
				Port:   intstr.FromInt(ReverseProxyPort),
				Scheme: probeScheme,
			},
		},
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
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

// reconcileIngress ensures the reverse proxy ingress exists and is up to date
func (r *ReverseProxyReconciliation) reconcileIngress() error {
	ingress := &networkingv1.Ingress{}
	ingressKey := types.NamespacedName{
		Name:      "reverse-proxy",
		Namespace: r.Frontend.Namespace,
	}

	err := r.Client.Get(r.Ctx, ingressKey, ingress)
	if err != nil && k8serr.IsNotFound(err) {
		// Ingress doesn't exist, create it
		return r.createReverseProxyIngress()
	} else if err != nil {
		return err
	}

	// Ingress exists, check if it needs update
	host := r.FrontendEnvironment.Spec.ReverseProxyHostname
	if host == "" {
		return fmt.Errorf("reverseProxyHostname must be specified in FrontendEnvironment spec when reverse proxy is enabled")
	}

	// Get consistent labels
	labels := r.getReverseProxyLabels()

	// Check if ingress needs update
	needsUpdate, updateReason := r.compareIngressFields(ingress, host, labels)

	if needsUpdate {
		r.Log.Info("Updating reverse proxy ingress", "reason", updateReason)

		// Build the desired configuration
		desiredIngress, err := r.buildReverseProxyIngress()
		if err != nil {
			return err
		}

		// Update only the mutable fields, preserving metadata that Kubernetes manages
		ingress.Labels = desiredIngress.Labels
		ingress.Annotations = desiredIngress.Annotations
		ingress.Spec = desiredIngress.Spec

		return r.Client.Update(r.Ctx, ingress)
	}

	return nil
}

// buildReverseProxyIngress builds the desired ingress configuration
func (r *ReverseProxyReconciliation) buildReverseProxyIngress() (*networkingv1.Ingress, error) {
	ingressName := "reverse-proxy"

	// Define name of resource
	nn := types.NamespacedName{
		Name:      ingressName,
		Namespace: r.Frontend.Namespace,
	}

	// Get consistent labels that won't conflict between frontends
	labels := r.getReverseProxyLabels()

	// Set up annotations
	annotations := map[string]string{
		"nginx.ingress.kubernetes.io/rewrite-target": "/",
		"nginx.ingress.kubernetes.io/ssl-redirect":   "false",
	}

	// Get hostname for reverse proxy
	host := r.FrontendEnvironment.Spec.ReverseProxyHostname
	if host == "" {
		return nil, fmt.Errorf("reverseProxyHostname must be specified in FrontendEnvironment spec when reverse proxy is enabled")
	}

	// Create ingress path
	pathType := networkingv1.PathTypePrefix
	paths := []networkingv1.HTTPIngressPath{
		{
			Path:     "/",
			PathType: &pathType,
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: "reverse-proxy",
					Port: networkingv1.ServiceBackendPort{
						Number: ReverseProxyPort,
					},
				},
			},
		},
	}

	// Create ingress rules
	rules := []networkingv1.IngressRule{
		{
			Host: host,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: paths,
				},
			},
		},
	}

	// Create the ingress
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nn.Name,
			Namespace:   nn.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: networkingv1.IngressSpec{
			Rules: rules,
		},
	}

	// Add whitelist annotations if configured (using ingress pattern from reconcile.go)
	if len(r.FrontendEnvironment.Spec.Whitelist) != 0 {
		annotations := ingress.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations["haproxy.router.openshift.io/ip_whitelist"] = strings.Join(r.FrontendEnvironment.Spec.Whitelist, " ")
		annotations["nginx.ingress.kubernetes.io/whitelist-source-range"] = strings.Join(r.FrontendEnvironment.Spec.Whitelist, ",")
		ingress.SetAnnotations(annotations)
	}

	// Add TLS configuration if SSL is enabled
	if r.FrontendEnvironment.Spec.SSL {
		ingress.Spec.TLS = []networkingv1.IngressTLS{
			{
				Hosts:      []string{host},
				SecretName: "reverse-proxy-cert",
			},
		}
	}

	// Set owner reference to the environment instead of the frontend
	ownerRef := r.getReverseProxyOwnerRef()
	ingress.SetOwnerReferences([]metav1.OwnerReference{ownerRef})

	return ingress, nil
}

// createReverseProxyIngress creates an ingress for the reverse proxy
func (r *ReverseProxyReconciliation) createReverseProxyIngress() error {
	ingress, err := r.buildReverseProxyIngress()
	if err != nil {
		return err
	}

	r.Log.Info("Creating Ingress for reverse proxy", "name", ingress.Name, "namespace", ingress.Namespace, "host", ingress.Spec.Rules[0].Host)

	return r.Client.Create(r.Ctx, ingress)
}

// compareIngressFields compares the important fields of the current ingress against desired values
func (r *ReverseProxyReconciliation) compareIngressFields(current *networkingv1.Ingress, desiredHost string, desiredLabels map[string]string) (bool, string) {
	if len(current.Spec.Rules) != 1 {
		return true, fmt.Sprintf("expected 1 rule, found %d", len(current.Spec.Rules))
	}
	rule := current.Spec.Rules[0] // defines the / path above

	// Check hostname
	if rule.Host != desiredHost {
		return true, fmt.Sprintf("hostname changed from %s to %s", rule.Host, desiredHost)
	}

	// Check HTTP configuration
	if rule.HTTP == nil {
		return true, "missing HTTP configuration in rule"
	}

	// Check paths
	if len(rule.HTTP.Paths) != 1 {
		return true, fmt.Sprintf("expected 1 path, found %d", len(rule.HTTP.Paths))
	}

	path := rule.HTTP.Paths[0]

	// Check path value
	if path.Path != "/" {
		return true, fmt.Sprintf("path changed from %s to /", path.Path)
	}

	// Check path type
	expectedPathType := networkingv1.PathTypePrefix
	if path.PathType == nil || *path.PathType != expectedPathType {
		currentPathType := "<none>"
		if path.PathType != nil {
			currentPathType = string(*path.PathType)
		}
		return true, fmt.Sprintf("path type changed from %s to %s", currentPathType, expectedPathType)
	}

	// Check service backend
	if path.Backend.Service == nil {
		return true, "missing service backend"
	}
	backend := path.Backend.Service

	// Check service name
	if backend.Name != "reverse-proxy" {
		return true, fmt.Sprintf("service name changed from %s to reverse-proxy", backend.Name)
	}

	// Check service port
	if backend.Port.Number != ReverseProxyPort {
		return true, fmt.Sprintf("service port changed from %d to %d", backend.Port.Number, ReverseProxyPort)
	}

	// Check important annotations
	requiredAnnotations := map[string]string{
		"nginx.ingress.kubernetes.io/rewrite-target": "/",
		"nginx.ingress.kubernetes.io/ssl-redirect":   "false",
	}

	// Add whitelist annotations if configured
	if len(r.FrontendEnvironment.Spec.Whitelist) > 0 {
		whitelist := strings.Join(r.FrontendEnvironment.Spec.Whitelist, ",")
		requiredAnnotations["haproxy.router.openshift.io/ip_whitelist"] = whitelist
		requiredAnnotations["nginx.ingress.kubernetes.io/whitelist-source-range"] = whitelist
	}

	for key, expectedValue := range requiredAnnotations {
		if current.Annotations == nil || current.Annotations[key] != expectedValue {
			return true, fmt.Sprintf("annotation %s changed from %s to %s", key,
				func() string {
					if current.Annotations == nil {
						return "<none>"
					}
					return current.Annotations[key]
				}(), expectedValue)
		}
	}

	// Check if whitelist annotations should be removed when not configured
	if len(r.FrontendEnvironment.Spec.Whitelist) == 0 {
		whitelistAnnotations := []string{
			"haproxy.router.openshift.io/ip_whitelist",
			"nginx.ingress.kubernetes.io/whitelist-source-range",
		}
		for _, annotation := range whitelistAnnotations {
			if current.Annotations != nil && current.Annotations[annotation] != "" {
				return true, fmt.Sprintf("whitelist annotation %s should be removed", annotation)
			}
		}
	}

	// Check labels
	importantLabels := []string{"app", "component", "environment"}
	for _, label := range importantLabels {
		currentValue := ""
		if current.Labels != nil {
			currentValue = current.Labels[label]
		}
		desiredValue := ""
		if desiredLabels != nil {
			desiredValue = desiredLabels[label]
		}
		if currentValue != desiredValue {
			return true, fmt.Sprintf("label %s changed from %s to %s", label, currentValue, desiredValue)
		}
	}

	// Check TLS configuration
	sslEnabled := r.FrontendEnvironment.Spec.SSL
	hasTLS := len(current.Spec.TLS) > 0

	if sslEnabled && !hasTLS {
		return true, "SSL enabled but TLS configuration missing"
	}

	if !sslEnabled && hasTLS {
		return true, "SSL disabled but TLS configuration present"
	}

	if sslEnabled && hasTLS {
		if len(current.Spec.TLS[0].Hosts) == 0 || current.Spec.TLS[0].Hosts[0] != desiredHost {
			return true, "TLS hostname mismatch"
		}
		expectedSecretName := "reverse-proxy-cert"
		if current.Spec.TLS[0].SecretName != expectedSecretName {
			return true, fmt.Sprintf("TLS secret name changed from %s to %s", current.Spec.TLS[0].SecretName, expectedSecretName)
		}
	}

	return false, ""
}
