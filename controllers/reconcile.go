package controllers

import (
	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	resCache "github.com/RedHatInsights/rhc-osdk-utils/resource_cache"
	"github.com/RedHatInsights/rhc-osdk-utils/utils"
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func runReconciliation(frontend *crd.Frontend, cache *resCache.ObjectCache) error {
	d := &apps.Deployment{}
	nn := types.NamespacedName{
		Name:      frontend.Name,
		Namespace: frontend.Namespace,
	}

	if err := cache.Create(CoreDeployment, nn, d); err != nil {
		return err
	}

	labels := frontend.GetLabels()

	labeler := utils.GetCustomLabeler(labels, nn, frontend)
	labeler(d)

	d.Spec.Template.Spec.Containers = []v1.Container{{
		Name:  "fe-image",
		Image: frontend.Spec.Image,
	}}

	d.Spec.Template.ObjectMeta.Labels = labels

	d.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels}

	if err := cache.Update(CoreDeployment, d); err != nil {
		return err
	}

	return nil
}
