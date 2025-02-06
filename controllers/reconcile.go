package controllers

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	localUtil "github.com/RedHatInsights/frontend-operator/controllers/utils"
	resCache "github.com/RedHatInsights/rhc-osdk-utils/resourceCache"
	"github.com/RedHatInsights/rhc-osdk-utils/utils"
	"github.com/go-logr/logr"

	prom "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"

	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	RoutePrefixDefault      = "apps"
	AkamaiSecretNameDefault = "akamai"
)

type FrontendReconciliation struct {
	Log                 logr.Logger
	Recorder            record.EventRecorder
	Cache               resCache.ObjectCache
	FRE                 *FrontendReconciler
	FrontendEnvironment *crd.FrontendEnvironment
	Frontend            *crd.Frontend
	Ctx                 context.Context
	Client              client.Client
}

//go:embed templates/Caddyfile
var caddyFileTemplate string

func (r *FrontendReconciliation) run() error {

	configMap, err := r.setupConfigMaps()
	if err != nil {
		return err
	}

	configHash, err := createConfigmapHash(configMap)
	if err != nil {
		return err
	}

	var annotationHashes []map[string]string
	annotationHashes = append(annotationHashes, map[string]string{"configHash": configHash})

	if r.Frontend.Spec.Image != "" {
		if err := r.createFrontendDeployment(annotationHashes); err != nil {
			return err
		}
		if err := r.createFrontendService(); err != nil {
			return err
		}
		// If cache busting is enabled for the environment, add the akamai cache bust container
		if r.FrontendEnvironment.Spec.EnableAkamaiCacheBust && r.FrontendEnvironment.Spec.AkamaiCacheBustImage != "" {
			if err := r.createOrUpdateCacheBustJob(); err != nil {
				return err
			}
		}
	}

	if err := r.createFrontendIngress(); err != nil {
		return err
	}

	if r.FrontendEnvironment.Spec.Monitoring != nil && !r.Frontend.Spec.ServiceMonitor.Disabled && !r.FrontendEnvironment.Spec.Monitoring.Disabled {
		if err := r.createServiceMonitor(); err != nil {
			return err
		}
	}
	return nil
}

func populateContainerVolumeMounts(frontendEnvironment *crd.FrontendEnvironment, frontend *crd.Frontend) []v1.VolumeMount {

	volumeMounts := []v1.VolumeMount{}

	if frontendEnvironment.Spec.GenerateNavJSON {
		// If we are generating all of the JSON config (nav and fed-modules)
		// then we just need to mount the while configmap over the whole chrome directory
		volumeMounts = append(volumeMounts, v1.VolumeMount{
			Name:      "config",
			MountPath: "/opt/app-root/src/build/chrome",
		})
	}

	// We always want to mount the config map under the operator-generated directory
	// This will allow chrome to incorperate the generated nav and fed-modules.json
	// at run time. This means chrome can merge the config in mixed environments

	volumeMounts = append(volumeMounts, v1.VolumeMount{
		Name:      "config",
		MountPath: "/opt/app-root/src/build/stable/operator-generated",
	})

	if frontend.Name != "chrome" {
		volumeMounts = append(volumeMounts, v1.VolumeMount{
			Name:      "caddy",
			MountPath: "/opt/app-root/src/Caddyfile",
			SubPath:   "Caddyfile",
		})
	}

	// We generate SSL cert mounts conditionally
	if frontendEnvironment.Spec.SSL {
		volumeMounts = append(volumeMounts, v1.VolumeMount{
			Name:      "certs",
			MountPath: "/opt/certs",
		})
	}

	return volumeMounts
}

func populateContainer(d *apps.Deployment, frontend *crd.Frontend, frontendEnvironment *crd.FrontendEnvironment) {

	// set the URI Scheme for the probe
	probeScheme := v1.URISchemeHTTP
	if frontendEnvironment.Spec.SSL {
		probeScheme = v1.URISchemeHTTPS
	}

	d.SetOwnerReferences([]metav1.OwnerReference{frontend.MakeOwnerReference()})

	// Modify the object to set the things we care about
	d.Spec.Template.Spec.Containers = []v1.Container{{
		Name:  "fe-image",
		Image: frontend.Spec.Image,
		Ports: []v1.ContainerPort{
			{
				Name:          "web",
				ContainerPort: 80,
				Protocol:      "TCP",
			},
			{
				Name:          "metrics",
				ContainerPort: 9000,
				Protocol:      "TCP",
			},
		},
		VolumeMounts: populateContainerVolumeMounts(frontendEnvironment, frontend),
		Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("30m"),
				v1.ResourceMemory: resource.MustParse("50Mi"),
			},
			Limits: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("40m"),
				v1.ResourceMemory: resource.MustParse("150Mi"),
			},
		},
		LivenessProbe: &v1.Probe{
			ProbeHandler: v1.ProbeHandler{
				HTTPGet: &v1.HTTPGetAction{
					Path:   "/",
					Port:   intstr.FromInt(8000),
					Scheme: probeScheme,
				},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       60,
			FailureThreshold:    3,
		},
		ReadinessProbe: &v1.Probe{
			ProbeHandler: v1.ProbeHandler{
				HTTPGet: &v1.HTTPGetAction{
					Path:   "/",
					Port:   intstr.FromInt(8000),
					Scheme: probeScheme,
				},
			},
			InitialDelaySeconds: 10,
		},
	}}
}

func getAkamaiSecretName(frontendEnvironment *crd.FrontendEnvironment) string {
	if frontendEnvironment.Spec.AkamaiSecretName == "" {
		return AkamaiSecretNameDefault
	}
	return frontendEnvironment.Spec.AkamaiSecretName
}

// getAkamaiSecret gets the akamai secret from the cluster
func getAkamaiSecret(ctx context.Context, client client.Client, frontend *crd.Frontend, secretName string) (*v1.Secret, error) {
	secret := &v1.Secret{}
	if err := client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: frontend.Namespace}, secret); err != nil {
		return nil, err
	}
	return secret, nil
}

// constructAkamaiEdgercFileFromSecret constructs the akamai edgerc file from the secret
func makeAkamaiEdgercFileFromSecret(secret *v1.Secret) string {
	edgercFile := "[default]\n"
	edgercFile += fmt.Sprintf("host = %s\n", secret.Data["host"])
	edgercFile += fmt.Sprintf("access_token = %s\n", secret.Data["access_token"])
	edgercFile += fmt.Sprintf("client_token = %s\n", secret.Data["client_token"])
	edgercFile += fmt.Sprintf("client_secret = %s\n", secret.Data["client_secret"])
	edgercFile += "[ccu]\n"
	edgercFile += fmt.Sprintf("host = %s\n", secret.Data["host"])
	edgercFile += fmt.Sprintf("access_token = %s\n", secret.Data["access_token"])
	edgercFile += fmt.Sprintf("client_token = %s\n", secret.Data["client_token"])
	edgercFile += fmt.Sprintf("client_secret = %s\n", secret.Data["client_secret"])
	return edgercFile
}

func createCachePurgePathList(frontend *crd.Frontend, frontendEnvironment *crd.FrontendEnvironment) []string {
	var purgePaths []string

	// Helper function to check if a path is already in the list
	contains := func(slice []string, item string) bool {
		for _, existing := range slice {
			if existing == item {
				return true
			}
		}
		return false
	}

	cacheBustUrls := frontendEnvironment.Spec.AkamaiCacheBustURLs

	if frontendEnvironment.Spec.AkamaiCacheBustURL != "" {
		cacheBustUrls = append(cacheBustUrls, frontendEnvironment.Spec.AkamaiCacheBustURL)
	}

	// Return early if we have no cache bust URLs of any kind to process
	if len(cacheBustUrls) == 0 {
		return purgePaths
	}

	for _, cacheBustURL := range cacheBustUrls {
		// Ensure the URL begins with https:// and has no trailing /
		purgeHost := strings.TrimSuffix(fmt.Sprintf("https://%s", strings.TrimPrefix(cacheBustURL, "https://")), "/")

		// Add default path if AkamaiCacheBustPaths is nil
		if frontend.Spec.AkamaiCacheBustPaths == nil {
			defaultPath := fmt.Sprintf("%s/apps/%s/fed-mods.json", purgeHost, frontend.Name)
			if !contains(purgePaths, defaultPath) {
				purgePaths = append(purgePaths, defaultPath)
			}
			continue
		}

		// Append paths based on AkamaiCacheBustPaths
		for _, path := range frontend.Spec.AkamaiCacheBustPaths {
			var fullPath string

			if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
				// Add full URL path directly
				fullPath = path
			} else {
				// Ensure each path has a leading slash but no double slashes
				if !strings.HasPrefix(path, "/") {
					path = "/" + path
				}
				fullPath = purgeHost + path
			}

			// Append the fullPath only if it doesn't already exist in purgePaths
			if !contains(purgePaths, fullPath) {
				purgePaths = append(purgePaths, fullPath)
			}
		}
	}

	return purgePaths
}

// populateCacheBustContainer adds the akamai cache bust container to the deployment
func (r *FrontendReconciliation) populateCacheBustContainer(j *batchv1.Job) error {

	// Get the akamai secret
	akamaiSecretName := getAkamaiSecretName(r.FrontendEnvironment)
	secret, err := getAkamaiSecret(r.Ctx, r.Client, r.Frontend, akamaiSecretName)
	if err != nil {
		return err
	}
	// Make the akamai file from the secret
	edgercFile := makeAkamaiEdgercFileFromSecret(secret)

	configMap := &v1.ConfigMap{}
	configMap.SetName("akamai-edgerc")
	configMap.SetNamespace(r.Frontend.Namespace)

	nn := types.NamespacedName{
		Name:      "akamai-edgerc",
		Namespace: r.Frontend.Namespace,
	}
	labels := r.FrontendEnvironment.GetLabels()
	labler := utils.GetCustomLabeler(labels, nn, r.FrontendEnvironment)
	labler(configMap)

	configMap.SetOwnerReferences([]metav1.OwnerReference{r.Frontend.MakeOwnerReference()})

	// Add the akamai edgerc file to the configmap
	configMap.Data = map[string]string{
		"edgerc": edgercFile,
	}

	// Create the configmap with the Client if it doesn't already exist
	if err := r.Client.Create(r.Ctx, configMap); err != nil {
		if !k8serr.IsAlreadyExists(err) {
			return err
		}
	}

	akamaiVolume := v1.Volume{
		Name: "akamai-edgerc",
		VolumeSource: v1.VolumeSource{
			ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: v1.LocalObjectReference{
					Name: "akamai-edgerc",
				},
			},
		},
	}

	j.Spec.Template.Spec.Volumes = []v1.Volume{akamaiVolume}

	// Get the paths to cache bust
	pathsToCacheBust := createCachePurgePathList(r.Frontend, r.FrontendEnvironment)

	// Construct the akamai cache bust command
	command := fmt.Sprintf("sleep 120; /cli/.akamai-cli/src/cli-purge/bin/akamai-purge --edgerc /opt/app-root/edgerc delete %s", strings.Join(pathsToCacheBust, " "))

	// Modify the obejct to set the things we care about
	cacheBustContainer := v1.Container{
		Name:  "akamai-cache-bust",
		Image: r.FrontendEnvironment.Spec.AkamaiCacheBustImage,
		// Mount the akamai edgerc file from the configmap
		VolumeMounts: []v1.VolumeMount{
			{
				Name:      "akamai-edgerc",
				MountPath: "/opt/app-root/edgerc",
				SubPath:   "edgerc",
			},
		},
		// Run the akamai cache bust script
		Command: []string{"/bin/bash", "-c", command},
	}
	// add the container to the spec containers
	j.Spec.Template.Spec.Containers = []v1.Container{cacheBustContainer}

	// Add the restart policy
	j.Spec.Template.Spec.RestartPolicy = v1.RestartPolicyNever

	annotations := j.Spec.Template.ObjectMeta.Annotations
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations["kube-linter.io/ignore-all"] = "we don't need no any checking"

	j.Spec.Template.ObjectMeta.SetAnnotations(annotations)

	// Add the akamai edgerc configmap to the deployment

	return nil
}

func populateVolumes(d *apps.Deployment, frontend *crd.Frontend, frontendEnvironment *crd.FrontendEnvironment) {
	// By default we just want the config and caddy volume
	volumes := []v1.Volume{}
	volumes = append(volumes, v1.Volume{
		Name: "config",
		VolumeSource: v1.VolumeSource{
			ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: v1.LocalObjectReference{
					Name: frontend.Spec.EnvName,
				},
			},
		},
	}, v1.Volume{
		Name: "caddy",
		VolumeSource: v1.VolumeSource{
			ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: v1.LocalObjectReference{
					Name: frontend.Spec.EnvName,
				},
				Items: []v1.KeyToPath{{
					Key:  "Caddyfile",
					Path: "Caddyfile",
				}},
			},
		},
	})

	if frontendEnvironment.Spec.SSL {
		volumes = append(volumes, v1.Volume{
			Name: "certs",
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: fmt.Sprintf("%s-cert", frontend.Name),
				},
			},
		})
	}

	// Set the volumes on the deployment
	d.Spec.Template.Spec.Volumes = volumes
}

// Add the env vars if eny are set
func (r *FrontendReconciliation) populateEnvVars(d *apps.Deployment, frontendEnvironment *crd.FrontendEnvironment) {
	envVars := []v1.EnvVar{}

	if frontendEnvironment.Spec.SSL {
		envVars = append(envVars, v1.EnvVar{
			Name:  "CADDY_TLS_MODE",
			Value: "https_port 8000",
		})
		envVars = append(envVars, v1.EnvVar{
			Name:  "CADDY_TLS_CERT",
			Value: "tls /opt/certs/tls.crt /opt/certs/tls.key",
		})
	}

	// Add the HTTP Headers to the env vars if the frontend environment has them
	if frontendEnvironment.Spec.HTTPHeaders != nil {
		envVars = append(envVars, v1.EnvVar{
			Name:  "CADDY_HTTP_HEADERS",
			Value: createCaddyfileHeaderBlock(frontendEnvironment.Spec.HTTPHeaders),
		})
	}

	d.Spec.Template.Spec.Containers[0].Env = envVars
}

// Create a Caddyfile header block from the FrontenbdEnvironment httpHeaders
func createCaddyfileHeaderBlock(httpHeaders map[string]string) string {
	if len(httpHeaders) == 0 {
		return ""
	}
	var keys []string
	for key := range httpHeaders {
		keys = append(keys, key)
	}
	// Sort keys alphabetically
	// We do this to ensure the order of the headers is consistent
	// without this tests will be flaky
	sort.Strings(keys)

	headerBlock := "header {\n"
	for _, key := range keys {
		headerBlock += fmt.Sprintf("%s %s\n", key, httpHeaders[key])
	}
	headerBlock += "}"
	return headerBlock
}

func (r *FrontendReconciliation) createFrontendDeployment(annotationHashes []map[string]string) error {

	// Create new empty struct
	d := &apps.Deployment{}

	deploymentName := r.Frontend.Name + "-frontend"

	// Define name of resource
	nn := types.NamespacedName{
		Name:      deploymentName,
		Namespace: r.Frontend.Namespace,
	}

	// Create object in cache (will populate cache if exists)
	if err := r.Cache.Create(CoreDeployment, nn, d); err != nil {
		return err
	}

	// Label with the right labels
	labels := r.Frontend.GetLabels()

	labeler := utils.GetCustomLabeler(labels, nn, r.Frontend)
	labeler(d)

	// Set the replicas if specified, otherwise default to 1
	if r.Frontend.Spec.Replicas != nil {
		d.Spec.Replicas = r.Frontend.Spec.Replicas
	} else {
		if r.FrontendEnvironment.Spec.DefaultReplicas != nil {
			d.Spec.Replicas = r.FrontendEnvironment.Spec.DefaultReplicas
		} else {
			d.Spec.Replicas = utils.Int32Ptr(1)
		}
	}

	populateVolumes(d, r.Frontend, r.FrontendEnvironment)
	populateContainer(d, r.Frontend, r.FrontendEnvironment)
	r.populateEnvVars(d, r.FrontendEnvironment)

	d.Spec.Template.ObjectMeta.Labels = labels

	d.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels}

	utils.UpdateAnnotations(&d.Spec.Template, annotationHashes...)

	// This is a temporary measure to silence DVO from opening 600 million tickets for each frontend - Issue fix ETA is TBD
	deploymentAnnotation := d.ObjectMeta.GetAnnotations()
	if deploymentAnnotation == nil {
		deploymentAnnotation = make(map[string]string)
	}

	// Gabor wrote the string "we don't need no any checking" and we will never change it
	deploymentAnnotation["kube-linter.io/ignore-all"] = "we don't need no any checking"
	d.ObjectMeta.SetAnnotations(deploymentAnnotation)

	// Inform the cache that our updates are complete
	err := r.Cache.Update(CoreDeployment, d)
	return err
}

func createPorts() []v1.ServicePort {
	appProtocol := "http"
	return []v1.ServicePort{
		{
			Name:        "public",
			Port:        8000,
			TargetPort:  intstr.FromInt(8000),
			Protocol:    "TCP",
			AppProtocol: &appProtocol,
		},
		{
			Name:        "metrics",
			Port:        9000,
			TargetPort:  intstr.FromInt(9000),
			Protocol:    "TCP",
			AppProtocol: &appProtocol,
		},
	}
}

func (r *FrontendReconciliation) generateJobName() string {
	return r.Frontend.Name + "-frontend-cachebust"
}

// getExistingJob returns the existing job if it exists
// and a bool indicating if it exists or not
func (r *FrontendReconciliation) getExistingJob() (*batchv1.Job, bool, error) {
	// Job we'll fill up
	j := &batchv1.Job{}

	jobName := r.generateJobName()

	// Define name of resource
	nn := types.NamespacedName{
		Name:      jobName,
		Namespace: r.Frontend.Namespace,
	}

	// Try and get the job
	err := r.Client.Get(r.Ctx, nn, j)
	if err != nil {
		if k8serr.IsNotFound(err) {
			// It doesn't exist so we can return false and no error
			return j, false, nil
		}
		// Something is wrong so we return the error
		return j, false, err
	}

	// It exists so we return true and no error
	return j, true, nil

}

func (r *FrontendReconciliation) isJobFromCurrentFrontendImage(j *batchv1.Job) bool {
	return j.Spec.Template.ObjectMeta.Annotations["frontend-image"] == r.Frontend.Spec.Image
}

// manageExistingJob will delete the existing job if it exists and is not from the current frontend image
// It will return true if the job exists and is from the current frontend image
func (r *FrontendReconciliation) manageExistingJob() (bool, error) {
	j, exists, err := r.getExistingJob()
	if err != nil {
		return false, err
	}

	// If it doesn't exist we can return false and no error
	if !exists {
		return false, nil
	}

	// If it exists but is not from the current frontend image we delete it
	if !r.isJobFromCurrentFrontendImage(j) {
		backgroundDeletion := metav1.DeletePropagationBackground
		return false, r.Client.Delete(r.Ctx, j, &client.DeleteOptions{
			PropagationPolicy: &backgroundDeletion,
		})
	}

	// If it exists and is from the current frontend image we return true and no error
	return true, nil
}

// createOrUpdateCacheBustJob will create a new job if it doesn't exist
// If it does exist and is from the current frontend image it will return
// If it does exist and is not from the current frontend image it will delete it and create a new one
func (r *FrontendReconciliation) createOrUpdateCacheBustJob() error {
	// Guard on frontend opting out of cache busting
	if r.Frontend.Spec.AkamaiCacheBustDisable {
		return nil
	}

	// If the job exists and is from the current frontend image we can return
	// If the job exists and is not from the current frontend image we delete it
	// If the job doesn't exist we create it
	existsAndMatchesCurrentFrontendImage, err := r.manageExistingJob()
	if err != nil {
		return err
	}
	if existsAndMatchesCurrentFrontendImage {
		return nil
	}

	// Create job
	j := &batchv1.Job{}

	jobName := r.generateJobName()

	// Set name
	j.SetName(jobName)
	// Set namespace
	j.SetNamespace(r.Frontend.Namespace)

	// Label with the right labels
	labels := r.Frontend.GetLabels()

	// Define name of resource
	nn := types.NamespacedName{
		Name:      jobName,
		Namespace: r.Frontend.Namespace,
	}

	labeler := utils.GetCustomLabeler(labels, nn, r.Frontend)
	labeler(j)

	j.SetOwnerReferences([]metav1.OwnerReference{r.Frontend.MakeOwnerReference()})

	j.Spec.Template.Spec.RestartPolicy = v1.RestartPolicyNever

	j.Spec.Completions = utils.Int32Ptr(1)

	// Set the image frontend image annotation
	annotations := j.Spec.Template.ObjectMeta.Annotations
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations["frontend-image"] = r.Frontend.Spec.Image
	annotations["kube-linter.io/ignore-all"] = "we don't need no any checking"

	j.Spec.Template.ObjectMeta.SetAnnotations(annotations)

	errr := r.populateCacheBustContainer(j)
	if errr != nil {
		return errr
	}

	return r.Client.Create(r.Ctx, j)
}

// Will need to create a service resource ident in provider like CoreDeployment
func (r *FrontendReconciliation) createFrontendService() error {
	// Create empty service
	s := &v1.Service{}

	// Define name of resource
	nn := types.NamespacedName{
		Name:      r.Frontend.Name,
		Namespace: r.Frontend.Namespace,
	}

	// Create object in cache (will populate cache if exists)
	if err := r.Cache.Create(CoreService, nn, s); err != nil {
		return err
	}

	if r.FrontendEnvironment.Spec.SSL {
		annotations := s.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations["service.beta.openshift.io/serving-cert-secret-name"] = fmt.Sprintf("%s-%s", r.Frontend.Name, "cert")
		s.SetAnnotations(annotations)
	}

	labels := make(map[string]string)
	labels["frontend"] = r.Frontend.Name
	labeler := utils.GetCustomLabeler(labels, nn, r.Frontend)
	labeler(s)
	// We should also set owner reference to the pod
	s.SetOwnerReferences([]metav1.OwnerReference{r.Frontend.MakeOwnerReference()})

	ports := createPorts()

	s.Spec.Selector = labels
	utils.MakeService(s, nn, labels, ports, r.Frontend, false)

	// Inform the cache that our updates are complete
	err := r.Cache.Update(CoreService, s)
	return err

}

func (r *FrontendReconciliation) createFrontendIngress() error {
	netobj := &networking.Ingress{}

	nn := types.NamespacedName{
		Name:      r.Frontend.Name,
		Namespace: r.Frontend.Namespace,
	}

	if err := r.Cache.Create(WebIngress, nn, netobj); err != nil {
		return err
	}

	labels := r.Frontend.GetLabels()
	labler := utils.GetCustomLabeler(labels, nn, r.Frontend)
	labler(netobj)

	netobj.SetOwnerReferences([]metav1.OwnerReference{r.Frontend.MakeOwnerReference()})

	r.createAnnotationsAndPopulate(nn, netobj)

	err := r.Cache.Update(WebIngress, netobj)
	return err
}

func (r *FrontendReconciliation) createAnnotationsAndPopulate(nn types.NamespacedName, netobj *networking.Ingress) {
	ingressClass := r.FrontendEnvironment.Spec.IngressClass
	if ingressClass == "" {
		ingressClass = "nginx"
	}

	if r.FrontendEnvironment.Spec.IngressAnnotations != nil {
		annotations := netobj.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		for k, v := range r.FrontendEnvironment.Spec.IngressAnnotations {
			annotations[k] = v
		}
		netobj.SetAnnotations(annotations)
	}

	if len(r.FrontendEnvironment.Spec.Whitelist) != 0 {
		annotations := netobj.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations["haproxy.router.openshift.io/ip_whitelist"] = strings.Join(r.FrontendEnvironment.Spec.Whitelist, " ")
		annotations["nginx.ingress.kubernetes.io/whitelist-source-range"] = strings.Join(r.FrontendEnvironment.Spec.Whitelist, ",")
		netobj.SetAnnotations(annotations)
	}

	if r.FrontendEnvironment.Spec.SSL {
		annotations := netobj.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}

		annotations["route.openshift.io/termination"] = "reencrypt"
		netobj.SetAnnotations(annotations)
	}

	if r.Frontend.Spec.Image != "" {
		r.populateConsoleDotIngress(netobj, ingressClass, nn.Name)
	} else {
		r.populateConsoleDotIngress(netobj, ingressClass, r.Frontend.Spec.Service)
	}
}

func (r *FrontendReconciliation) getFrontendPaths() []string {
	frontendPaths := r.Frontend.Spec.Frontend.Paths
	defaultPath := fmt.Sprintf("/apps/%s", r.Frontend.Name)

	if r.Frontend.Spec.AssetsPrefix != "" {
		defaultPath = fmt.Sprintf("/%s/%s", r.Frontend.Spec.AssetsPrefix, r.Frontend.Name)
	}

	if !r.Frontend.Spec.Frontend.HasPath(defaultPath) {
		frontendPaths = append(frontendPaths, defaultPath)
	}

	return frontendPaths
}

func (r *FrontendReconciliation) populateConsoleDotIngress(netobj *networking.Ingress, ingressClass, serviceName string) {
	frontendPaths := r.getFrontendPaths()

	var ingressPaths []networking.HTTPIngressPath
	for _, a := range frontendPaths {
		newPath := createNewIngressPath(a, serviceName)
		ingressPaths = append(ingressPaths, newPath)
	}

	host := r.FrontendEnvironment.Spec.Hostname
	if host == "" {
		host = r.Frontend.Spec.EnvName
	}

	// we need to add /api fallback here as well
	netobj.Spec = defaultNetSpec(ingressClass, host, ingressPaths)
}

func createNewIngressPath(a, serviceName string) networking.HTTPIngressPath {
	prefixType := "Prefix"
	return networking.HTTPIngressPath{
		Path:     a,
		PathType: (*networking.PathType)(&prefixType),
		Backend: networking.IngressBackend{
			Service: &networking.IngressServiceBackend{
				Name: serviceName,
				Port: networking.ServiceBackendPort{
					Number: 8000,
				},
			},
		},
	}
}

func defaultNetSpec(ingressClass, host string, ingressPaths []networking.HTTPIngressPath) networking.IngressSpec {
	return networking.IngressSpec{
		TLS: []networking.IngressTLS{{
			Hosts: []string{},
		}},
		IngressClassName: &ingressClass,
		Rules: []networking.IngressRule{
			{
				Host: host,
				IngressRuleValue: networking.IngressRuleValue{
					HTTP: &networking.HTTPIngressRuleValue{
						Paths: ingressPaths,
					},
				},
			},
		},
	}
}

func setupFedModules(feEnv *crd.FrontendEnvironment, frontendList *crd.FrontendList, fedModules map[string]crd.FedModule) error {
	for _, frontend := range frontendList.Items {
		if (frontend.Name == "chrome" || frontend.Spec.FeoConfigEnabled) && frontend.Spec.Module != nil {
			// module names in fed-modules.json must be camelCase
			// K8s does not allow camelCase names, only
			// whatever-this-case-is, so we convert.
			modName := localUtil.ToCamelCase(frontend.GetName())
			if frontend.Spec.Module.ModuleID != "" {
				modName = frontend.Spec.Module.ModuleID
			}
			fedModules[modName] = *frontend.Spec.Module

			module := fedModules[modName]

			if frontend.Spec.Module.FullProfile == nil || !*frontend.Spec.Module.FullProfile {
				module.FullProfile = crd.FalsePtr()
			} else {
				module.FullProfile = crd.TruePtr()
			}

			if frontend.Name == "chrome" {

				var configSource apiextensions.JSON
				err := configSource.UnmarshalJSON([]byte(`{}`))
				if err != nil {
					return fmt.Errorf("error unmarshaling base config: %w", err)
				}

				if module.Config == nil {
					module.Config = &configSource
				} else {
					configSource = *module.Config
				}

				innerConfig := make(map[string]interface{})
				if err := json.Unmarshal(configSource.Raw, &innerConfig); err != nil {
					fmt.Printf("error unpacking custom config")
				}
				innerConfig["ssoUrl"] = feEnv.Spec.SSO

				bytes, err := json.Marshal(innerConfig)
				if err != nil {
					fmt.Print(err)
				}

				err = module.Config.UnmarshalJSON(bytes)
				if err != nil {
					return fmt.Errorf("error unmarshaling config: %w", err)
				}

			}

			fedModules[modName] = module
		}
	}
	return nil
}

func adjustSearchEntry(searchEntry *crd.SearchEntry, frontend crd.Frontend) crd.SearchEntry {
	altTitleCopy := make([]string, len(searchEntry.AltTitle))
	copy(altTitleCopy, searchEntry.AltTitle)
	newSearchEntry := crd.SearchEntry{
		// make the id environment and frontend specific to reduce duplicate ids across Frontend resources
		ID:          fmt.Sprintf("%s-%s-%s", frontend.Name, frontend.Spec.EnvName, searchEntry.ID),
		Title:       searchEntry.Title,
		Description: searchEntry.Description,
		Href:        searchEntry.Href,
		AltTitle:    altTitleCopy,
		IsExternal:  searchEntry.IsExternal,
		FrontendRef: frontend.Name,
	}
	return newSearchEntry
}

func setupSearchIndex(feList *crd.FrontendList) []crd.SearchEntry {
	searchIndex := []crd.SearchEntry{}
	for _, frontend := range feList.Items {
		if frontend.Spec.FeoConfigEnabled && frontend.Spec.SearchEntries != nil {
			for _, searchEntry := range frontend.Spec.SearchEntries {
				if searchEntry != nil {
					searchIndex = append(searchIndex, adjustSearchEntry(searchEntry, frontend))
				}
			}
		}
	}
	return searchIndex
}

func setupWidgetRegistry(feList *crd.FrontendList) []crd.WidgetEntry {
	widgetRegistry := []crd.WidgetEntry{}
	for _, frontend := range feList.Items {
		if frontend.Spec.FeoConfigEnabled {
			for _, widget := range frontend.Spec.WidgetRegistry {
				widget.FrontendRef = frontend.Name
				widgetRegistry = append(widgetRegistry, *widget)
			}
		}
	}

	return widgetRegistry
}

func getServiceTilePath(section string, group string) string {
	return fmt.Sprintf("%s-%s", section, group)
}

func setupServiceTilesData(feList *crd.FrontendList, feEnvironment crd.FrontendEnvironment) ([]crd.FrontendServiceCategoryGenerated, []string) {
	categories := []crd.FrontendServiceCategoryGenerated{}
	if feEnvironment.Spec.ServiceCategories == nil {
		// skip if we do not have service categories
		return categories, []string{}
	}

	// just a quick cache to make it easier and faster to assign tiles to their destination
	tileGroupAccessMap := make(map[string]*[]crd.ServiceTile)

	for _, category := range *feEnvironment.Spec.ServiceCategories {
		groups := []crd.FrontendServiceCategoryGroupGenerated{}
		for _, gr := range category.Groups {
			tiles := []crd.ServiceTile{}
			group := crd.FrontendServiceCategoryGroupGenerated{
				ID:    gr.ID,
				Title: gr.Title,
				Tiles: &tiles,
			}
			groups = append(groups, group)
			groupKey := getServiceTilePath(category.ID, gr.ID)
			tileGroupAccessMap[groupKey] = &tiles
		}
		newCategory := crd.FrontendServiceCategoryGenerated{
			ID:     category.ID,
			Title:  category.Title,
			Groups: groups,
		}

		categories = append(categories, newCategory)
	}

	skippedTiles := []string{}
	for _, frontend := range feList.Items {
		if frontend.Spec.FeoConfigEnabled && frontend.Spec.ServiceTiles != nil {
			for _, tile := range frontend.Spec.ServiceTiles {
				groupKey := getServiceTilePath(tile.Section, tile.Group)
				if groupTiles, ok := tileGroupAccessMap[groupKey]; ok {
					// assign the tile to the service category and group
					tile.FrontendRef = frontend.Name
					*groupTiles = append(*groupTiles, *tile)
				} else {
					// ignore the tile if destination does not exist
					skippedTiles = append(skippedTiles, tile.ID)
				}
			}
		}
	}

	return categories, skippedTiles
}

func getNavItemPath(feName string, bundleID string, segmentID string) string {
	return fmt.Sprintf("%s-%s-%s", feName, bundleID, segmentID)
}

type NavSegmentCacheEntry struct {
	NavItems []crd.ChromeNavItem
	IsFilled bool
}

func fillNavRefsTree(navItems []crd.ChromeNavItem, navSegmentsCache map[string]map[string]*NavSegmentCacheEntry, depth uint) ([]crd.ChromeNavItem, error) {
	parsedNavItems := []crd.ChromeNavItem{}
	// prevent infinite recursion and navigation nesting
	// currently max known depth is 2, so 10 should be more than enough for ever
	// event the depth 2 is challenging when it comes to UX, this should prevent infinite reference loops
	if depth > 10 {
		return parsedNavItems, fmt.Errorf("maximum navigation depth reached")
	}
	parsedNavItems = navItems
	var err error
	for index := 0; index < len(parsedNavItems); index++ {
		navItem := parsedNavItems[index]
		// if navItem is a segment ref, replace it with the actual segment
		if navItem.HasSegmentRef() && navItem.Href == "" && navItem.Title == "" {
			segmentRef := navItem.SegmentRef
			segmentRefCacheEntry, ok := navSegmentsCache[segmentRef.FrontendName][segmentRef.SegmentID]
			if !ok {
				// skip if segment ref does not exist
				continue
			}
			segmentRefItems := segmentRefCacheEntry.NavItems
			// pre-fill the cache for the segment to make the next pass quicker
			if !segmentRefCacheEntry.IsFilled {
				segmentRefItems, err = fillNavRefsTree(segmentRefItems, navSegmentsCache, depth+1)
				if err != nil {
					return parsedNavItems, err
				}
				// add attributes required for the frontend local dev environment

				// don't forget to mark the segment as filled and fill it
				navSegmentsCache[segmentRef.FrontendName][segmentRef.SegmentID].IsFilled = true
				navSegmentsCache[segmentRef.FrontendName][segmentRef.SegmentID].NavItems = segmentRefItems
			}

			// copy segmentRefItems and add segment ref for Frontend local dev environment
			frontendSegmentRefItems := []crd.ChromeNavItem{}
			for _, item := range segmentRefItems {
				newItem := item.DeepCopy()
				newItem.BundleSegmentRef = navItem.BundleSegmentRef
				newItem.FrontendRef = segmentRef.FrontendName
				newItem.SegmentRef = segmentRef
				frontendSegmentRefItems = append(frontendSegmentRefItems, *newItem)
			}

			// delete the original ref and replace it with the filled segments
			parsedNavItems = append(parsedNavItems[:index], parsedNavItems[index+1:]...)
			parsedNavItems = slices.Insert(parsedNavItems, index, frontendSegmentRefItems...)
		}

		// Make sure nested nav items have their refs filled as well
		if navItem.IsExpandable() {
			parsedRoutes, err := fillNavRefsTree(navItem.Routes, navSegmentsCache, depth+1)
			if err != nil {
				return parsedNavItems, err
			}
			parsedNavItems[index].Routes = parsedRoutes
		}

		if navItem.IsGroup() {
			parsedGroupItems, err := fillNavRefsTree(navItem.NavItems, navSegmentsCache, depth+1)
			if err != nil {
				return parsedNavItems, err
			}
			parsedNavItems[index].NavItems = parsedGroupItems
		}
	}

	return parsedNavItems, nil
}

// Remove segment refs from nav items before they are emitted to bundles
func filterUnknownNavRefs(navItems []crd.ChromeNavItem) []crd.ChromeNavItem {
	res := []crd.ChromeNavItem{}
	for _, navItem := range navItems {
		if navItem.HasSegmentRef() && navItem.Href == "" && navItem.Title == "" {
			// skip if segment is a ref
			continue
		}

		if navItem.IsExpandable() {
			navItem.Routes = filterUnknownNavRefs(navItem.Routes)
		}

		if navItem.IsGroup() {
			navItem.NavItems = filterUnknownNavRefs(navItem.NavItems)
		}

		res = append(res, navItem)
	}
	return res
}

func addRefsToNavItems(navItems []crd.ChromeNavItem, bundleID string, frontendID string, parentBundleSegmentRef string) []crd.ChromeNavItem {
	res := []crd.ChromeNavItem{}
	for _, navItem := range navItems {
		newNavItem := navItem
		newNavItem.BundleSegmentRef = parentBundleSegmentRef
		newNavItem.FrontendRef = frontendID

		if newNavItem.IsExpandable() {
			newNavItem.Routes = addRefsToNavItems(newNavItem.Routes, bundleID, frontendID, parentBundleSegmentRef)
		}

		if newNavItem.IsGroup() {
			newNavItem.NavItems = addRefsToNavItems(newNavItem.NavItems, bundleID, frontendID, parentBundleSegmentRef)
		}

		res = append(res, newNavItem)
	}
	return res
}

func setupBundlesData(feList *crd.FrontendList, feEnvironment crd.FrontendEnvironment) ([]crd.FrontendBundlesGenerated, []string, error) {
	bundles := []crd.FrontendBundlesGenerated{}
	if feEnvironment.Spec.Bundles == nil {
		// skip if we do not have bundles in fe environment
		return bundles, []string{}, nil
	}

	// fill the nav segment cache
	navSegmentsCache := make(map[string]map[string]*NavSegmentCacheEntry)
	for _, frontend := range feList.Items {
		if frontend.Spec.NavigationSegments != nil {
			// Create empty map for a frontend if not yet in cache
			if _, ok := navSegmentsCache[frontend.Name]; !ok {
				navSegmentsCache[frontend.Name] = make(map[string]*NavSegmentCacheEntry)
			}
			for _, navSegment := range frontend.Spec.NavigationSegments {
				navSegmentsCache[frontend.Name][navSegment.SegmentID] = &NavSegmentCacheEntry{
					IsFilled: false,
					NavItems: *navSegment.NavItems,
				}
			}
		}
	}

	skippedNavItemsMap := make(map[string][]string)
	bundleNavSegmentMap := make(map[string][]crd.BundleSegment)
	for _, frontend := range feList.Items {
		if frontend.Spec.FeoConfigEnabled && frontend.Spec.BundleSegments != nil {
			for _, bundleNavSegment := range frontend.Spec.BundleSegments {
				navItemsWithRefs := addRefsToNavItems(*bundleNavSegment.NavItems, bundleNavSegment.BundleID, frontend.Name, bundleNavSegment.SegmentID)
				bundleNavSegment.NavItems = &navItemsWithRefs
				bundleNavSegmentMap[bundleNavSegment.BundleID] = append(bundleNavSegmentMap[bundleNavSegment.BundleID], *bundleNavSegment)
				skippedNavItemsMap[bundleNavSegment.BundleID] = append(skippedNavItemsMap[bundleNavSegment.BundleID], getNavItemPath(frontend.Name, bundleNavSegment.BundleID, bundleNavSegment.SegmentID))
			}
		}
	}

	for _, bundle := range *feEnvironment.Spec.Bundles {
		delete(skippedNavItemsMap, bundle.ID)
		sort.Slice(bundleNavSegmentMap[bundle.ID], func(i, j int) bool {
			if (bundleNavSegmentMap[bundle.ID])[i].Position == (bundleNavSegmentMap[bundle.ID])[j].Position {
				return (bundleNavSegmentMap[bundle.ID])[i].SegmentID[0] < (bundleNavSegmentMap[bundle.ID])[j].SegmentID[0]
			}
			return (bundleNavSegmentMap[bundle.ID])[i].Position < (bundleNavSegmentMap[bundle.ID])[j].Position
		})
		navItems := []crd.ChromeNavItem{}
		for _, navSegment := range bundleNavSegmentMap[bundle.ID] {
			for _, navItem := range *navSegment.NavItems {
				// duplicate position for further consumption on the frontend
				// need a new variable for position to avoid pointer reference issues
				pos := navSegment.Position
				navItem.Position = &pos
				navItems = append(navItems, navItem)
			}
		}
		// fill the nav refs before adding the bundle
		navItems, err := fillNavRefsTree(navItems, navSegmentsCache, 0)
		if err != nil {
			return bundles, []string{}, err
		}

		navItems = filterUnknownNavRefs(navItems)
		newBundle := crd.FrontendBundlesGenerated{
			ID:       bundle.ID,
			Title:    bundle.Title,
			NavItems: navItems,
		}
		bundles = append(bundles, newBundle)
	}

	skippedNavItems := []string{}
	for _, skipped := range skippedNavItemsMap {
		skippedNavItems = append(skippedNavItems, skipped...)
	}

	return bundles, skippedNavItems, nil
}

func (r *FrontendReconciliation) setupBundleData(_ *v1.ConfigMap, _ map[string]crd.Frontend) error {
	bundleList := &crd.BundleList{}

	if err := r.FRE.Client.List(r.Ctx, bundleList, client.MatchingFields{"spec.envName": r.Frontend.Spec.EnvName}); err != nil {
		return err
	}

	keys := []string{}
	nBundleMap := map[string]crd.Bundle{}
	for _, bundle := range bundleList.Items {
		keys = append(keys, bundle.Name)
		nBundleMap[bundle.Name] = bundle
	}

	sort.Strings(keys)

	// TODO: Update reconcile to use the position based navigation for bundles
	return nil
}

func createConfigmapHash(cfgMap *v1.ConfigMap) (string, error) {
	hashData, err := json.Marshal(cfgMap.Data)
	if err != nil {
		return "", err
	}

	h := sha256.New()
	h.Write([]byte(hashData))
	hash := fmt.Sprintf("%x", h.Sum(nil))
	return hash, nil
}

// setupConfigMaps will create configmaps for the various config json
// files, including fed-modules.json and the various bundle json files
func (r *FrontendReconciliation) setupConfigMaps() (*v1.ConfigMap, error) {
	// Will need to interact directly with the client here, and not the cache because
	// we need to read ALL the Frontend CRDs in the Env that we care about

	// Create a frontend list
	frontendList := &crd.FrontendList{}

	// Populate the frontendlist by looking for all frontends in our env
	if err := r.FRE.Client.List(r.Ctx, frontendList, client.MatchingFields{"spec.envName": r.Frontend.Spec.EnvName}); err != nil {
		return &v1.ConfigMap{}, err
	}

	// default config map, should be always created
	defaultNN := types.NamespacedName{
		Name:      r.Frontend.Spec.EnvName,
		Namespace: r.Frontend.Namespace,
	}

	// TODO: The conntext map should be configured via env variable from app interface
	frontendCFGContextName := "feo-context-cfg"
	if strings.Contains(r.Frontend.Namespace, "beta") {
		// separate stable and beta config map names
		// quick patch to see if we can separate the configurations
		frontendCFGContextName += "-beta"
	}
	additionalNN := []types.NamespacedName{}
	for _, n := range r.FrontendEnvironment.Spec.TargetNamespaces {
		additionalNN = append(additionalNN, types.NamespacedName{
			Name:      frontendCFGContextName,
			Namespace: n,
		})
	}

	defaultCfgMap, err := r.createConfigMap(defaultNN, frontendList, false)

	for _, nn := range additionalNN {
		_, err = r.createConfigMap(nn, frontendList, true)
		if err != nil {
			return defaultCfgMap, err
		}
	}

	return defaultCfgMap, err
}

func (r *FrontendReconciliation) createConfigMap(nn types.NamespacedName, frontendList *crd.FrontendList, markForRestart bool) (*v1.ConfigMap, error) {
	cfgMap := &v1.ConfigMap{}
	if markForRestart {
		if cfgMap.Annotations == nil {
			cfgMap.Annotations = map[string]string{}
		}
		// Flag to trigger pod restart if config map changes
		cfgMap.Annotations["qontract.recycle"] = "true"
		/**
			* to use config map in a pod as an env variable use following config:
		* podSpec:
		*   env:
		*    - name: FEO_SEARCH_INDEX
		*      valueFrom:
		*        configMapKeyRef:
		*          key: search-index.json # or other key from config map
		*          name: feo-context-cfg # based on the constant in the setupConfigMaps function
		  *					 optional: true # because keys in configmap can be empty
		*/
	}

	// Create a map of frontend names to frontend objects
	cacheMap := make(map[string]crd.Frontend)
	for _, frontend := range frontendList.Items {
		cacheMap[frontend.Name] = frontend
	}

	if err := r.Cache.Create(CoreConfig, nn, cfgMap); err != nil {
		return cfgMap, err
	}

	labels := r.FrontendEnvironment.GetLabels()
	labler := utils.GetCustomLabeler(labels, nn, r.FrontendEnvironment)
	labler(cfgMap)

	if err := r.populateConfigMap(cfgMap, cacheMap, frontendList); err != nil {
		return cfgMap, err
	}

	if err := r.Cache.Update(CoreConfig, cfgMap); err != nil {
		return cfgMap, err
	}
	return cfgMap, nil
}

func (r *FrontendReconciliation) populateConfigMap(cfgMap *v1.ConfigMap, cacheMap map[string]crd.Frontend, feList *crd.FrontendList) error {
	cfgMap.SetOwnerReferences([]metav1.OwnerReference{r.FrontendEnvironment.MakeOwnerReference()})
	cfgMap.Data = map[string]string{}

	if r.FrontendEnvironment.Spec.GenerateNavJSON {
		if err := r.setupBundleData(cfgMap, cacheMap); err != nil {
			return err
		}
	}

	fedModules := make(map[string]crd.FedModule)
	if err := setupFedModules(r.FrontendEnvironment, feList, fedModules); err != nil {
		return fmt.Errorf("error setting up fedModules: %w", err)
	}

	searchIndex := setupSearchIndex(feList)

	widgetRegistry := setupWidgetRegistry(feList)

	serviceCategories, skippedTiles := setupServiceTilesData(feList, *r.FrontendEnvironment)

	bundles, skippedBundles, err := setupBundlesData(feList, *r.FrontendEnvironment)
	if err != nil {
		return err
	}

	fedModulesJSONData, err := json.Marshal(fedModules)
	if err != nil {
		return err
	}

	searchIndexJSONData, err := json.Marshal(searchIndex)

	if err != nil {
		return err
	}

	widgetRegistryJSONData, err := json.Marshal(widgetRegistry)
	if err != nil {
		return err
	}

	serviceCategoriesJSONData, err := json.Marshal(serviceCategories)

	if err != nil {
		return err
	}

	bundlesJSONData, err := json.Marshal(bundles)
	if err != nil {
		return err
	}

	if len(skippedTiles) > 0 {
		r.Log.Info("Unable to find service categories for tiles:", strings.Join(skippedTiles, ","))
	}

	if len(skippedBundles) > 0 {
		r.Log.Info(fmt.Sprintf("Unable to find bundle for nav items: %s", strings.Join(skippedBundles, ",")))
	}

	cfgMap.Data["fed-modules.json"] = string(fedModulesJSONData)
	cfgMap.Data["Caddyfile"] = caddyFileTemplate
	if len(searchIndex) > 0 {
		cfgMap.Data["search-index.json"] = string(searchIndexJSONData)
	}

	if len(widgetRegistry) > 0 {
		cfgMap.Data["widget-registry.json"] = string(widgetRegistryJSONData)
	}

	if len(serviceCategories) > 0 {
		cfgMap.Data["service-tiles.json"] = string(serviceCategoriesJSONData)
	}

	if len(bundles) > 0 {
		cfgMap.Data["bundles.json"] = string(bundlesJSONData)
	}

	return nil
}

func (r *FrontendReconciliation) createServiceMonitor() error {

	// the monitor mode will default to "app-interface"
	ns := "openshift-customer-monitoring"

	if r.FrontendEnvironment.Spec.Monitoring.Mode == "local" {
		ns = r.Frontend.Namespace
	}

	nn := types.NamespacedName{
		Name:      r.Frontend.Name,
		Namespace: ns,
	}

	svcMonitor := &prom.ServiceMonitor{}
	if err := r.Cache.Create(MetricsServiceMonitor, nn, svcMonitor); err != nil {
		return err
	}

	labler := utils.GetCustomLabeler(map[string]string{"prometheus": r.FrontendEnvironment.Name}, nn, r.Frontend)
	labler(svcMonitor)
	svcMonitor.SetOwnerReferences([]metav1.OwnerReference{r.Frontend.MakeOwnerReference()})

	svcMonitor.Spec.Endpoints = []prom.Endpoint{{
		Path:     "/metrics",
		Port:     "metrics",
		Interval: prom.Duration("15s"),
	}}
	svcMonitor.Spec.NamespaceSelector = prom.NamespaceSelector{
		MatchNames: []string{r.Frontend.Namespace},
	}
	svcMonitor.Spec.Selector = metav1.LabelSelector{
		MatchLabels: map[string]string{
			"frontend": r.Frontend.Name,
		},
	}

	err := r.Cache.Update(MetricsServiceMonitor, svcMonitor)
	return err
}
