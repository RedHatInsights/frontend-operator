package controllers

import (
	"fmt"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	resCache "github.com/RedHatInsights/rhc-osdk-utils/resource_cache"
	"github.com/RedHatInsights/rhc-osdk-utils/utils"
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var frontendConfigNamespace = "fon"

func runReconciliation(frontend *crd.Frontend, cache *resCache.ObjectCache) error {
	if err := createFrontendDeployment(frontend, cache); err != nil {
		return err
	}

	if err := createConfigDeployment(frontend, cache); err != nil {
		return err
	}

	if err := createConfigIngress(frontend, cache); err != nil {
		return err
	}

	return nil
}

func createFrontendDeployment(frontend *crd.Frontend, cache *resCache.ObjectCache) error {
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
	}}

	d.Spec.Template.ObjectMeta.Labels = labels

	d.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels}

	// Inform the cache that our updates are complete
	if err := cache.Update(CoreDeployment, d); err != nil {
		return err
	}

	return nil
}

func createFrontendService() {
	//s := v1.Service{}
	//Will need to create a service resource ident in provider like CoreDeployment
}

func createFrontendIngress() {
	// https://github.com/RedHatInsights/clowder/pull/393/files#diff-ac84089738397c0bc1c32c7f4375abeaec31567072384a096e3e8c972f1359f1R183 is an example
	// of a backend service ingress *hint* it should be almost identical
}

func createConfigDeployment(frontend *crd.Frontend, cache *resCache.ObjectCache) error {
	// Create new empty struct
	d := &apps.Deployment{}

	// Define name of resource
	nn := types.NamespacedName{
		Name:      frontend.Spec.EnvName,
		Namespace: frontendConfigNamespace,
	}

	// Create object in cache (will populate cache if exists)
	if err := cache.Create(ConfigDeployment, nn, d); err != nil {
		return err
	}

	// Label with the right labels
	labels := frontend.GetLabels()

	labeler := utils.GetCustomLabeler(labels, nn, frontend)
	labeler(d)

	// Modify the obejct to set the things we care about
	d.Spec.Template.Spec.Containers = []v1.Container{{
		Name:  "config",
		Image: "quay.io/redhat-cloud-services/cloud-services-config",
		Ports: []v1.ContainerPort{{
			Name:          "web",
			ContainerPort: 80,
			Protocol:      "TCP",
		}},
	}}

	d.Spec.Template.ObjectMeta.Labels = labels

	d.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels}

	// Inform the cache that our updates are complete
	if err := cache.Update(ConfigDeployment, d); err != nil {
		return err
	}

	return nil
}

func createConfigService() {
	// This will be a service like above
}

func createConfigConfigMap() {
	// Will need to interact directly with the client here, and not the cache because
	// we need to read ALL the Frontend CRDs in the Env that we care about
}

func createConfigIngress(frontend *crd.Frontend, cache *resCache.ObjectCache) error {
	netobj := &networking.Ingress{}

	nn := types.NamespacedName{
		Name:      frontend.Spec.EnvName,
		Namespace: frontendConfigNamespace,
	}

	if err := cache.Create(WebIngress, nn, netobj); err != nil {
		return err
	}

	labels := frontend.GetLabels()
	labler := utils.MakeLabeler(nn, labels, frontend)
	labler(netobj)

	frontendPath := frontend.Spec.Frontend.Paths
	defaultPath := fmt.Sprintf("/apps/%s", frontend.Spec.EnvName)

	if !frontend.Spec.Frontend.HasPath(defaultPath) {
		frontendPath = append(frontendPath, defaultPath)
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
						Number: 80,
					},
				},
			},
		}
		ingressPapths = append(ingressPapths, newPath)
	}

	netobj.Spec = networking.IngressSpec{
		Rules: []networking.IngressRule{
			{
				Host: "main",
				IngressRuleValue: networking.IngressRuleValue{
					HTTP: &networking.HTTPIngressRuleValue{
						Paths: ingressPapths,
					},
				},
			},
		},
	}

	if err := cache.Update(WebIngress, netobj); err != nil {
		return err
	}

	return nil
}
