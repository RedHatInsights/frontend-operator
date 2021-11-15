package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	resCache "github.com/RedHatInsights/rhc-osdk-utils/resource_cache"
	"github.com/RedHatInsights/rhc-osdk-utils/utils"

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

	if frontend.Spec.Image != "" {
		if err := createFrontendDeployment(context, pClient, frontend, frontendEnvironment, hash, cache); err != nil {
			return err
		}
		if err := createFrontendService(frontend, cache); err != nil {
			return err
		}
	}

	if err := createFrontendIngress(frontend, frontendEnvironment, cache); err != nil {
		return err
	}

	return nil
}

func createFrontendDeployment(context context.Context, pClient client.Client, frontend *crd.Frontend, frontendEnvironment *crd.FrontendEnvironment, hash string, cache *resCache.ObjectCache) error {
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
		VolumeMounts: []v1.VolumeMount{{
			Name:      "config",
			MountPath: "/opt/app-root/src/chrome",
		}},
		Env: []v1.EnvVar{{
			Name:  "SSO_URL",
			Value: sso,
		}},
	}}

	d.Spec.Template.Spec.Volumes = []v1.Volume{}
	d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, v1.Volume{
		Name: "config",
		VolumeSource: v1.VolumeSource{
			ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: v1.LocalObjectReference{
					Name: frontend.Spec.EnvName,
				},
			},
		},
	})

	d.Spec.Template.ObjectMeta.Labels = labels

	d.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels}

	annotations := d.Spec.Template.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations["configHash"] = hash

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

	servicePorts := []v1.ServicePort{}

	appProtocol := "http"

	labels := make(map[string]string)
	labels["frontend"] = frontend.Name
	labeler := utils.GetCustomLabeler(labels, nn, frontend)
	labeler(s)
	// We should also set owner reference to the pod

	port := v1.ServicePort{
		Name:        "public",
		Port:        8000,
		TargetPort:  intstr.FromInt(8000),
		Protocol:    "TCP",
		AppProtocol: &appProtocol,
	}

	servicePorts = append(servicePorts, port)
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

	var ingressPapths []networking.HTTPIngressPath
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
		ingressPapths = append(ingressPapths, newPath)
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
						Paths: ingressPapths,
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

	var ingressPapths []networking.HTTPIngressPath
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
		ingressPapths = append(ingressPapths, newPath)
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
						Paths: ingressPapths,
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
		if bundle.CustomNav != nil {
			newBundleObject := bundle.CustomNav

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
					navitem := getNavItem(&retrievedFrontend)
					if navitem != nil {
						newBundleObject.NavItems = append(newBundleObject.NavItems, *navitem)
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
		module := getModule(&frontend)
		if frontend.Spec.Extensions != nil {
			modName := frontend.GetName()
			if module.ModuleID != "" {
				modName = module.ModuleID
			}
			fedModules[modName] = *module
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

func getModule(frontend *crd.Frontend) *crd.FedModule {
	if ec := getExtensionContent(frontend); ec != nil {
		return &ec.Module
	}
	return nil
}

func getNavItem(frontend *crd.Frontend) *crd.BundleNavItem {
	if ec := getExtensionContent(frontend); ec != nil {
		return ec.NavItem
	}
	return nil
}

func getExtensionContent(frontend *crd.Frontend) *crd.ExtensionContent {
	for _, extension := range frontend.Spec.Extensions {
		if extension.Type == "cloud.redhat.com/frontend" {
			extensionContent := &crd.ExtensionContent{}
			json.Unmarshal(extension.Properties.Raw, extensionContent)
			return extensionContent
		}
	}
	return nil
}
