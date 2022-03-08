package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/RedHatInsights/clowder/controllers/cloud.redhat.com/errors"
	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	cond "sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func SetFrontendConditions(ctx context.Context, client client.Client, o *crd.Frontend, state clusterv1.ConditionType, err error) error {
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

	if err := client.Status().Update(ctx, o); err != nil {
		return err
	}
	return nil
}

func GetFrontendResources(ctx context.Context, client client.Client, o *crd.Frontend) (bool, error) {
	stats, _, err := GetFrontendFigures(ctx, client, o)
	if err != nil {
		return false, err
	}
	if stats.ManagedDeployments == stats.ReadyDeployments {
		return true, nil
	}
	return false, nil
}

func GetFrontendFigures(ctx context.Context, client client.Client, o *crd.Frontend) (crd.FrontendDeployments, string, error) {

	var totalManagedDeployments int32
	var totalReadyDeployments int32
	var msgs []string

	deploymentStats := crd.FrontendDeployments{}

	namespaces, err := o.GetNamespacesInEnv(ctx, client)
	if err != nil {
		return crd.FrontendDeployments{}, "", errors.Wrap("get namespaces: ", err)
	}

	managedDeployments, readyDeployments, msg, err := countDeployments(ctx, client, o, namespaces)
	if err != nil {
		return crd.FrontendDeployments{}, "", errors.Wrap("count deploys: ", err)
	}
	totalManagedDeployments += managedDeployments
	totalReadyDeployments += readyDeployments
	if msg != "" {
		msgs = append(msgs, msg)
	}

	msg = fmt.Sprintf("dependency failure: [%s]", strings.Join(msgs, ","))
	deploymentStats.ManagedDeployments = totalManagedDeployments
	deploymentStats.ReadyDeployments = totalReadyDeployments
	return deploymentStats, msg, nil
}

func deploymentStatusChecker(deployment apps.Deployment) bool {
	if deployment.Generation > deployment.Status.ObservedGeneration {
		// The status on this resource needs to update
		return false
	}

	for _, condition := range deployment.Status.Conditions {
		if condition.Type == "Available" && condition.Status == "True" {
			return true
		}
	}

	return false
}

func countDeployments(ctx context.Context, pClient client.Client, o *crd.Frontend, namespaces []string) (int32, int32, string, error) {
	var managedDeployments int32
	var readyDeployments int32
	var brokenDeployments []string
	var msg = ""

	deployments := []apps.Deployment{}
	for _, namespace := range namespaces {
		opts := []client.ListOption{
			client.InNamespace(namespace),
		}
		tmpDeployments := apps.DeploymentList{}
		err := pClient.List(ctx, &tmpDeployments, opts...)
		if err != nil {
			return 0, 0, "", err
		}
		deployments = append(deployments, tmpDeployments.Items...)
	}

	// filter for resources owned by the ClowdObject and check their status
	for _, deployment := range deployments {
		for _, owner := range deployment.GetOwnerReferences() {
			if owner.UID == o.GetUID() {
				managedDeployments++
				if ok := deploymentStatusChecker(deployment); ok {
					readyDeployments++
				} else {
					brokenDeployments = append(brokenDeployments, fmt.Sprintf("%s/%s", deployment.Name, deployment.Namespace))
				}
				break
			}
		}
	}

	if len(brokenDeployments) > 0 {
		msg = fmt.Sprintf("broken deployments: [%s]", strings.Join(brokenDeployments, ", "))
	}

	return managedDeployments, readyDeployments, msg, nil
}
