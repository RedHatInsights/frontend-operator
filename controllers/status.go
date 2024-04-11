package controllers

import (
	"context"

	"github.com/RedHatInsights/clowder/controllers/cloud.redhat.com/errors"
	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"github.com/RedHatInsights/rhc-osdk-utils/resources"
	apps "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func SetFrontendConditions(ctx context.Context, client client.Client, o *crd.Frontend, state string, err error) error {
	oldStatus := o.Status.DeepCopy()
	conditions := []metav1.Condition{}

	loopConditions := []string{crd.ReconciliationSuccessful, crd.ReconciliationFailed}
	for _, conditionType := range loopConditions {
		condition := &metav1.Condition{}
		condition.Type = conditionType
		condition.Status = metav1.ConditionFalse
		condition.Reason = "NoError"

		if state == conditionType {
			condition.Status = metav1.ConditionTrue
			if err != nil {
				condition.Message = err.Error()
				condition.Reason = "Error"
			}
		}

		condition.LastTransitionTime = metav1.Now()
		conditions = append(conditions, *condition)
	}

	frontendStatus, err := GetFrontendResources(ctx, client, o)
	if err != nil {
		return err
	}

	condition := &metav1.Condition{}

	condition.Reason = "NoError"
	condition.Status = metav1.ConditionFalse
	condition.Message = "Deployments are not yet ready"
	if frontendStatus {
		condition.Status = metav1.ConditionTrue
		condition.Message = "All managed deployments ready"
	}

	condition.Type = crd.FrontendsReady
	condition.LastTransitionTime = metav1.Now()
	if err != nil {
		condition.Message += err.Error()
		condition.Reason = "Error"
	}

	conditions = append(conditions, *condition)
	for _, condition := range conditions {
		innerCondition := condition
		meta.SetStatusCondition(&o.Status.Conditions, innerCondition)
	}

	o.Status.Ready = frontendStatus
	stats, _, err := GetFrontendFigures(ctx, client, o)
	if err != nil {
		return err
	}

	o.Status.Deployments.ManagedDeployments = stats.ManagedDeployments
	o.Status.Deployments.ReadyDeployments = stats.ReadyDeployments

	if !equality.Semantic.DeepEqual(*oldStatus, o.Status) {
		if err := client.Status().Update(ctx, o); err != nil {
			return err
		}
	}
	return nil
}

func GetFrontendResources(ctx context.Context, client client.Client, o *crd.Frontend) (bool, error) {
	stats, _, err := GetFrontendFigures(ctx, client, o)
	if err == nil {
		return stats.ManagedDeployments == stats.ReadyDeployments, err
	}
	return false, err
}

func GetFrontendFigures(ctx context.Context, client client.Client, o *crd.Frontend) (crd.FrontendDeployments, string, error) {
	deploymentStats := crd.FrontendDeployments{}

	namespaces, err := o.GetNamespacesInEnv(ctx, client)
	if err != nil {
		return crd.FrontendDeployments{}, "", errors.Wrap("get namespaces: ", err)
	}

	query, _ := resources.MakeQuery(&apps.Deployment{}, *scheme, namespaces, o.GetUID())

	counter := resources.ResourceCounter{
		Query: query,
		ReadyRequirements: []resources.ResourceConditionReadyRequirements{{
			Type:   "Available",
			Status: "True",
		}},
	}

	results, err := counter.Count(ctx, client)
	if err != nil {
		return crd.FrontendDeployments{}, "", errors.Wrap("count resources: ", err)
	}

	deploymentStats.ManagedDeployments = int32(results.Managed)
	deploymentStats.ReadyDeployments = int32(results.Ready)
	return deploymentStats, results.BrokenMessage, nil
}
