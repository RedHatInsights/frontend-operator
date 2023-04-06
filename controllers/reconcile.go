package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const RoutePrefixDefault = "apps"

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

func populateContainerVolumeMounts(frontendEnvironment *crd.FrontendEnvironment) []v1.VolumeMount {

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
		MountPath: "/opt/app-root/src/build/operator-generated",
	})

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
	d.SetOwnerReferences([]metav1.OwnerReference{frontend.MakeOwnerReference()})

	// Modify the obejct to set the things we care about
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
		VolumeMounts: populateContainerVolumeMounts(frontendEnvironment),
	},
	}
}

func populateVolumes(d *apps.Deployment, frontend *crd.Frontend, frontendEnvironment *crd.FrontendEnvironment) {
	// By default we just want the config volume
	volumes := []v1.Volume{}
	if frontendEnvironment.Spec.GenerateNavJSON {
		volumes = append(volumes, v1.Volume{
			Name: "config",
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{
						Name: frontend.Spec.EnvName,
					},
				},
			},
		})
	}

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

// Add the SSL env vars if we SSL mode is set in the frontend environment
func (r *FrontendReconciliation) populateEnvVars(d *apps.Deployment, frontendEnvironment *crd.FrontendEnvironment) {
	if !frontendEnvironment.Spec.SSL {
		return
	}
	envVars := []v1.EnvVar{
		{
			Name:  "CADDY_TLS_MODE",
			Value: "https_port 8000",
		},
		{
			Name:  "CADDY_TLS_CERT",
			Value: "tls /opt/certs/tls.crt /opt/certs/tls.key",
		}}
	d.Spec.Template.Spec.Containers[0].Env = envVars
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

	populateContainer(d, r.Frontend, r.FrontendEnvironment)
	populateVolumes(d, r.Frontend, r.FrontendEnvironment)
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
	defaultBetaPath := fmt.Sprintf("/beta/apps/%s", r.Frontend.Name)
	defaultPreviewPath := fmt.Sprintf("/preview/apps/%s", r.Frontend.Name)
	if r.Frontend.Spec.AssetsPrefix != "" {
		defaultPath = fmt.Sprintf("/%s/%s", r.Frontend.Spec.AssetsPrefix, r.Frontend.Name)
		defaultBetaPath = fmt.Sprintf("/beta/%s/%s", r.Frontend.Spec.AssetsPrefix, r.Frontend.Name)
		defaultPreviewPath = fmt.Sprintf("/preview/%s/%s", r.Frontend.Spec.AssetsPrefix, r.Frontend.Name)
	}

	if !r.Frontend.Spec.Frontend.HasPath(defaultPath) {
		frontendPaths = append(frontendPaths, defaultPath)
	}

	if !r.Frontend.Spec.Frontend.HasPath(defaultBetaPath) {
		frontendPaths = append(frontendPaths, defaultBetaPath)
	}

	if !r.Frontend.Spec.Frontend.HasPath(defaultPreviewPath) {
		frontendPaths = append(frontendPaths, defaultPreviewPath)
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

func setupCustomNav(bundle *crd.Bundle, cfgMap *v1.ConfigMap) error {
	newBundleObject := bundle.Spec.CustomNav

	jsonData, err := json.Marshal(newBundleObject)
	if err != nil {
		return err
	}

	cfgMap.Data[fmt.Sprintf("%s.json", bundle.Name)] = string(jsonData)
	return nil
}

func setupNormalNav(bundle *crd.Bundle, cacheMap map[string]crd.Frontend, cfgMap *v1.ConfigMap) error {
	newBundleObject := crd.ComputedBundle{
		ID:       bundle.Spec.ID,
		Title:    bundle.Spec.Title,
		NavItems: []crd.BundleNavItem{},
	}

	bundleCacheMap := make(map[string]crd.BundleNavItem)
	for _, extraItem := range bundle.Spec.ExtraNavItems {
		bundleCacheMap[extraItem.Name] = extraItem.NavItem
	}

	for _, app := range bundle.Spec.AppList {
		if retrievedFrontend, ok := cacheMap[app]; ok {
			if retrievedFrontend.Spec.NavItems != nil {
				for _, navItem := range retrievedFrontend.Spec.NavItems {
					newBundleObject.NavItems = append(newBundleObject.NavItems, *navItem)
				}
			}
		}
		if bundleNavItem, ok := bundleCacheMap[app]; ok {
			newBundleObject.NavItems = append(newBundleObject.NavItems, bundleNavItem)
		}
	}

	jsonData, err := json.Marshal(newBundleObject)
	if err != nil {
		return err
	}

	cfgMap.Data[fmt.Sprintf("%s.json", bundle.Name)] = string(jsonData)
	return nil
}

func setupFedModules(feEnv *crd.FrontendEnvironment, frontendList *crd.FrontendList, fedModules map[string]crd.FedModule) error {
	for _, frontend := range frontendList.Items {
		if frontend.Spec.Module != nil {
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

func (r *FrontendReconciliation) setupBundleData(cfgMap *v1.ConfigMap, cacheMap map[string]crd.Frontend) error {
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

	for _, key := range keys {
		bundle := nBundleMap[key]
		if bundle.Spec.CustomNav != nil {
			if err := setupCustomNav(&bundle, cfgMap); err != nil {
				return err
			}
		} else {
			if err := setupNormalNav(&bundle, cacheMap, cfgMap); err != nil {
				return err
			}
		}
	}
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

	cfgMap, err := r.createConfigMap(frontendList)

	return cfgMap, err
}

func (r *FrontendReconciliation) createConfigMap(frontendList *crd.FrontendList) (*v1.ConfigMap, error) {
	cfgMap := &v1.ConfigMap{}

	// Create a map of frontend names to frontends objects
	cacheMap := make(map[string]crd.Frontend)
	for _, frontend := range frontendList.Items {
		cacheMap[frontend.Name] = frontend
	}

	nn := types.NamespacedName{
		Name:      r.Frontend.Spec.EnvName,
		Namespace: r.Frontend.Namespace,
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

	jsonData, err := json.Marshal(fedModules)
	if err != nil {
		return err
	}

	cfgMap.Data["fed-modules.json"] = string(jsonData)
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
