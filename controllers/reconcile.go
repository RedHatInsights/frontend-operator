package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	localUtil "github.com/RedHatInsights/frontend-operator/controllers/utils"
	resCache "github.com/RedHatInsights/rhc-osdk-utils/resource_cache"
	"github.com/RedHatInsights/rhc-osdk-utils/utils"
	prom "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func runReconciliation(context context.Context, pClient client.Client, frontend *crd.Frontend, frontendEnvironment *crd.FrontendEnvironment, cache *resCache.ObjectCache) error {
	hash, err := createConfigConfigMap(context, pClient, frontend, frontendEnvironment, cache)
	if err != nil {
		return err
	}

	ssoHash, err := createSSOConfigMap(context, pClient, frontend, frontendEnvironment, cache)
	if err != nil {
		return err
	}

	if frontend.Spec.Image != "" {
		if err := createFrontendDeployment(context, pClient, frontend, frontendEnvironment, hash, ssoHash, cache); err != nil {
			return err
		}
		if err := createFrontendService(frontend, cache); err != nil {
			return err
		}
	}

	if err := createFrontendIngress(frontend, frontendEnvironment, cache); err != nil {
		return err
	}

	if err := createServiceMonitorObjects(cache, frontend, "env-boot", "boot"); err != nil {
		return err
	}

	return nil
}

func createFrontendDeployment(context context.Context, pClient client.Client, frontend *crd.Frontend, frontendEnvironment *crd.FrontendEnvironment, hash string, ssoHash string, cache *resCache.ObjectCache) error {
	sso := frontendEnvironment.Spec.SSO

	// Create new empty struct
	d := &apps.Deployment{}

	// Define name of resource
	nn := types.NamespacedName{
		Name:      frontend.Name,
		Namespace: frontend.Namespace,
	}

	// Create object in cache (will populate cache if exists)
	if err := cache.Create(CoreDeployment, nn, d); err != nil {
		return err
	}

	// Label with the right labels
	labels := frontend.GetLabels()

	labeler := utils.GetCustomLabeler(labels, nn, frontend)
	labeler(d)

	// Modify the obejct to set the things we care about
	d.Spec.Template.Spec.Containers = []v1.Container{{
		Name:  "fe-image",
		Image: frontend.Spec.Image,
		Ports: []v1.ContainerPort{{
			Name:          "web",
			ContainerPort: 80,
			Protocol:      "TCP",
		}},
		// }, {
		// 	Name:          "metrics",
		// 	ContainerPort: 9113,
		// 	Protocol:      "TCP",
		// }},
		VolumeMounts: []v1.VolumeMount{
			{
				Name:      "config",
				MountPath: "/opt/app-root/src/build/chrome",
			},
			{
				Name:      "sso",
				MountPath: "/opt/app-root/src/build/js/sso-url.js",
				SubPath:   "sso-url.js",
			},
		},
		Env: []v1.EnvVar{{
			Name:  "SSO_URL",
			Value: sso,
		}},
	}, {
		// TODO: Refactor after spike
		Name:  "metrics",
		Image: "nginx/nginx-prometheus-exporter:0.10.0",
		Ports: []v1.ContainerPort{{
			Name:          "metrics",
			ContainerPort: 9000,
			Protocol:      "TCP",
		}},
		Args: []string{
			"-nginx.scrape-uri=http://localhost:9113/metrics",
			"-web.listen-address=:9000",
		},
	}}

	d.Spec.Template.Spec.Volumes = []v1.Volume{
		{
			Name: "config",
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{
						Name: frontend.Spec.EnvName,
					},
				},
			},
		},
		{
			Name: "sso",
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{
						Name: fmt.Sprintf("%s-sso", frontend.Spec.EnvName),
					},
				},
			},
		},
	}

	d.Spec.Template.ObjectMeta.Labels = labels

	d.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels}

	annotations := d.Spec.Template.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations["configHash"] = hash
	annotations["ssoHash"] = ssoHash

	d.Spec.Template.SetAnnotations(annotations)

	// Inform the cache that our updates are complete
	if err := cache.Update(CoreDeployment, d); err != nil {
		return err
	}

	return nil
}

//Will need to create a service resource ident in provider like CoreDeployment
func createFrontendService(frontend *crd.Frontend, cache *resCache.ObjectCache) error {
	// Create empty service
	s := &v1.Service{}

	// Define name of resource
	nn := types.NamespacedName{
		Name:      frontend.Name,
		Namespace: frontend.Namespace,
	}

	// Create object in cache (will populate cache if exists)
	if err := cache.Create(CoreService, nn, s); err != nil {
		return err
	}

	appProtocol := "http"

	labels := make(map[string]string)
	labels["frontend"] = frontend.Name
	labeler := utils.GetCustomLabeler(labels, nn, frontend)
	labeler(s)
	// We should also set owner reference to the pod

	servicePorts := []v1.ServicePort{{
		Name:        "public",
		Port:        8000,
		TargetPort:  intstr.FromInt(8000),
		Protocol:    "TCP",
		AppProtocol: &appProtocol,
	}, {
		Name:        "metrics",
		Port:        9000,
		TargetPort:  intstr.FromInt(9000),
		Protocol:    "TCP",
		AppProtocol: &appProtocol,
	}}

	s.Spec.Selector = labels

	utils.MakeService(s, nn, labels, servicePorts, frontend, false)

	// Inform the cache that our updates are complete
	if err := cache.Update(CoreService, s); err != nil {
		return err
	}
	return nil
}

func createFrontendIngress(frontend *crd.Frontend, frontendEnvironment *crd.FrontendEnvironment, cache *resCache.ObjectCache) error {
	netobj := &networking.Ingress{}

	nn := types.NamespacedName{
		Name:      frontend.Name,
		Namespace: frontend.Namespace,
	}

	if err := cache.Create(WebIngress, nn, netobj); err != nil {
		return err
	}

	labels := frontend.GetLabels()
	labler := utils.GetCustomLabeler(labels, nn, frontend)
	labler(netobj)

	ingressClass := frontendEnvironment.Spec.IngressClass
	if ingressClass == "" {
		ingressClass = "nginx"
	}

	if frontend.Spec.Image != "" {
		populateConesoleDotIngress(nn, frontend, frontendEnvironment, netobj, ingressClass)
	} else {
		populateHACIngress(nn, frontend, frontendEnvironment, netobj, ingressClass)
	}

	if err := cache.Update(WebIngress, netobj); err != nil {
		return err
	}

	return nil
}

func populateConesoleDotIngress(nn types.NamespacedName, frontend *crd.Frontend, frontendEnvironment *crd.FrontendEnvironment, netobj *networking.Ingress, ingressClass string) {
	frontendPath := frontend.Spec.Frontend.Paths
	defaultPath := fmt.Sprintf("/apps/%s", frontend.Name)
	defaultBetaPath := fmt.Sprintf("/beta/apps/%s", frontend.Name)

	if !frontend.Spec.Frontend.HasPath(defaultPath) {
		frontendPath = append(frontendPath, defaultPath)
	}

	if !frontend.Spec.Frontend.HasPath(defaultBetaPath) {
		frontendPath = append(frontendPath, defaultBetaPath)
	}

	prefixType := "Prefix"

	var ingressPaths []networking.HTTPIngressPath
	for _, a := range frontendPath {
		newPath := networking.HTTPIngressPath{
			Path:     a,
			PathType: (*networking.PathType)(&prefixType),
			Backend: networking.IngressBackend{
				Service: &networking.IngressServiceBackend{
					Name: nn.Name,
					Port: networking.ServiceBackendPort{
						Number: 8000,
					},
				},
			},
		}
		ingressPaths = append(ingressPaths, newPath)
	}

	host := frontendEnvironment.Spec.Hostname
	if host == "" {
		host = frontend.Spec.EnvName
	}

	// we need to add /api fallback here as well
	netobj.Spec = networking.IngressSpec{
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

func populateHACIngress(nn types.NamespacedName, frontend *crd.Frontend, frontendEnvironment *crd.FrontendEnvironment, netobj *networking.Ingress, ingressClass string) {
	frontendPath := frontend.Spec.Frontend.Paths
	defaultPath := fmt.Sprintf("/apps/%s", frontend.Name)
	defaultBetaPath := fmt.Sprintf("/beta/apps/%s", frontend.Name)

	if !frontend.Spec.Frontend.HasPath(defaultPath) {
		frontendPath = append(frontendPath, defaultPath)
	}

	if !frontend.Spec.Frontend.HasPath(defaultBetaPath) {
		frontendPath = append(frontendPath, defaultBetaPath)
	}

	prefixType := "Prefix"

	var ingressPaths []networking.HTTPIngressPath
	for _, a := range frontendPath {
		newPath := networking.HTTPIngressPath{
			Path:     a,
			PathType: (*networking.PathType)(&prefixType),
			Backend: networking.IngressBackend{
				Service: &networking.IngressServiceBackend{
					Name: frontend.Spec.Service,
					Port: networking.ServiceBackendPort{
						Number: 8000,
					},
				},
			},
		}
		ingressPaths = append(ingressPaths, newPath)
	}

	host := frontendEnvironment.Spec.Hostname
	if host == "" {
		host = frontend.Spec.EnvName
	}

	// we need to add /api fallback here as well
	netobj.Spec = networking.IngressSpec{
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

func createConfigConfigMap(ctx context.Context, pClient client.Client, frontend *crd.Frontend, frontendEnvironment *crd.FrontendEnvironment, cache *resCache.ObjectCache) (string, error) {
	// Will need to interact directly with the client here, and not the cache because
	// we need to read ALL the Frontend CRDs in the Env that we care about

	frontendList := &crd.FrontendList{}

	if err := pClient.List(ctx, frontendList, client.MatchingFields{"spec.envName": frontend.Spec.EnvName}); err != nil {
		return "", err
	}

	cacheMap := make(map[string]crd.Frontend)
	for _, frontend := range frontendList.Items {
		cacheMap[frontend.Name] = frontend
	}

	bundleList := &crd.BundleList{}

	if err := pClient.List(ctx, bundleList, client.MatchingFields{"spec.envName": frontend.Spec.EnvName}); err != nil {
		return "", err
	}

	cfgMap := &v1.ConfigMap{}

	nn := types.NamespacedName{
		Name:      frontend.Spec.EnvName,
		Namespace: frontend.Namespace,
	}

	if err := cache.Create(CoreConfig, nn, cfgMap); err != nil {
		return "", err
	}

	labels := frontendEnvironment.GetLabels()
	labler := utils.GetCustomLabeler(labels, nn, frontend)
	labler(cfgMap)

	hashString := ""

	cfgMap.Data = map[string]string{}

	for _, bundle := range bundleList.Items {
		if bundle.Spec.CustomNav != nil {
			newBundleObject := bundle.Spec.CustomNav

			jsonData, err := json.Marshal(newBundleObject)
			if err != nil {
				return "", err
			}

			cfgMap.Data[fmt.Sprintf("%s.json", bundle.Name)] = string(jsonData)
		} else {
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
				return "", err
			}

			cfgMap.Data[fmt.Sprintf("%s.json", bundle.Name)] = string(jsonData)

			h := sha256.New()
			h.Write([]byte(jsonData))
			hashString += fmt.Sprintf("%x", h.Sum(nil))
		}
	}

	fedModules := make(map[string]crd.FedModule)

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
		}
	}

	jsonData, err := json.Marshal(fedModules)
	if err != nil {
		return "", err
	}

	cfgMap.Data["fed-modules.json"] = string(jsonData)

	if err := cache.Update(CoreConfig, cfgMap); err != nil {
		return "", err
	}

	h := sha256.New()
	h.Write([]byte(jsonData))
	hashString += fmt.Sprintf("%x", h.Sum(nil))

	h = sha256.New()
	h.Write([]byte(hashString))
	hash := fmt.Sprintf("%x", h.Sum(nil))

	return hash, nil
}

func createSSOConfigMap(ctx context.Context, pClient client.Client, frontend *crd.Frontend, frontendEnvironment *crd.FrontendEnvironment, cache *resCache.ObjectCache) (string, error) {
	// Will need to interact directly with the client here, and not the cache because
	// we need to read ALL the Frontend CRDs in the Env that we care about

	cfgMap := &v1.ConfigMap{}

	nn := types.NamespacedName{
		Name:      fmt.Sprintf("%s-sso", frontend.Spec.EnvName),
		Namespace: frontend.Namespace,
	}

	if err := cache.Create(SSOConfig, nn, cfgMap); err != nil {
		return "", err
	}

	hashString := ""

	labels := frontendEnvironment.GetLabels()
	labler := utils.GetCustomLabeler(labels, nn, frontend)
	labler(cfgMap)

	ssoData := fmt.Sprintf(`"use strict";(self.webpackChunkinsights_chrome=self.webpackChunkinsights_chrome||[]).push([[172],{30701:(s,e,h)=>{h.r(e),h.d(e,{default:()=>c});const c="%s"}}]);`, frontendEnvironment.Spec.SSO)

	cfgMap.Data = map[string]string{
		"sso-url.js": ssoData,
	}

	h := sha256.New()
	h.Write([]byte(ssoData))
	hashString += fmt.Sprintf("%x", h.Sum(nil))

	h = sha256.New()
	h.Write([]byte(hashString))
	hash := fmt.Sprintf("%x", h.Sum(nil))

	if err := cache.Update(SSOConfig, cfgMap); err != nil {
		return "", err
	}

	return hash, nil
}

func createServiceMonitorObjects(cache *resCache.ObjectCache, fe *crd.Frontend, promLabel string, namespace string) error {
	sm := &prom.ServiceMonitor{}
	name := fe.Name

	nn := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	if err := cache.Create(MetricsServiceMonitor, nn, sm); err != nil {
		return err
	}
	sm.Spec.Endpoints = []prom.Endpoint{{
		Interval: "15s",
		Path:     "/metrics",
		Port:     "metrics",
	}}

	sm.Spec.NamespaceSelector = prom.NamespaceSelector{
		MatchNames: []string{fe.Namespace},
	}

	sm.Spec.Selector = metav1.LabelSelector{
		MatchLabels: map[string]string{
			"frontend": nn.Name,
		},
	}
	labels := map[string]string{
		"prometheus": promLabel,
		"app":        "env-boot",
	}
	labeler := utils.GetCustomLabeler(labels, nn, fe)
	labeler(sm)

	sm.SetNamespace(namespace)

	if err := cache.Update(MetricsServiceMonitor, sm); err != nil {
		return err
	}
	return nil
}
