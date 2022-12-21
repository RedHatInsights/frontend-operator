package controllers

import (
	"context"

	"github.com/RedHatInsights/clowder/controllers/cloud.redhat.com/errors"
	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"github.com/RedHatInsights/rhc-osdk-utils/resources"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	cond "sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func SetFrontendConditions(ctx context.Context, client client.Client, o *crd.Frontend, state clusterv1.ConditionType, err error) error {
	oldStatus := o.Status.DeepCopy()
	conditions := []clusterv1.Condition{}

	loopConditions := []clusterv1.ConditionType{crd.ReconciliationSuccessful, crd.ReconciliationFailed}
	for _, conditionType := range loopConditions {
		condition := &clusterv1.Condition{}
		condition.Type = conditionType
		condition.Status = core.ConditionFalse

		if state == conditionType {
			condition.Status = core.ConditionTrue
			if err != nil {
				condition.Reason = err.Error()
			}
		}

		condition.LastTransitionTime = v1.Now()
		conditions = append(conditions, *condition)
	}

	frontendStatus, err := GetFrontendResources(ctx, client, o)
	if err != nil {
		return err
	}

	condition := &clusterv1.Condition{}

	condition.Status = core.ConditionFalse
	condition.Message = "Deployments are not yet ready"
	if frontendStatus {
		condition.Status = core.ConditionTrue
		condition.Message = "All managed deployments ready"
	}

	condition.Type = crd.FrontendsReady
	condition.LastTransitionTime = v1.Now()
	if err != nil {
		condition.Reason = err.Error()
	}

	conditions = append(conditions, *condition)
	for _, condition := range conditions {
		cond.Set(o, &condition)
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

	results := counter.Count(ctx, client)

	deploymentStats.ManagedDeployments = int32(results.Managed)
	deploymentStats.ReadyDeployments = int32(results.Ready)
	return deploymentStats, results.BrokenMessage, nil
}
