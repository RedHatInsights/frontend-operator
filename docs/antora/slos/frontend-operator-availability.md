# Frontend Operator Availability SLO

## Description

The frontend operator availability SLO determines if the operator is functioning normally.
This SLO tracks the deployment number of the frontend operator. There should always be at least
1 deployment running for the operator.

## SLI Rationale
Availability is the most important metric we can gather for this operator. If there are no running
pods, no operations can be conducted. Ensuring that we monitor the availability of the operator is
critical to running ConsoleDot Frontends.

## Implmentation

The SLI for availability is enabled through kubernetes metrics. We can use the `kube_deployment_status_replicas_available`
and filter on the `frontend-operator-system` namespace to determine if we have a running pod. Since
the only thing running in that namespace is the controller operator, we can match our desired pod numbers to our alerts.

## SLO Rationale

The operator's uptime should be at least 99%. Availability is the basis of OpenShift deployments. We cannot reconcile
Frontend resources without a running operator and it is a critical part of our deployment strategy for ConsoleDot.

## Alerting

Alerts for availability are high for now, but could become paged alerts in the future. When the operator becomes
unavailable, it will not delete or remove any resources. Instead, no changes can be made to CRs on the cluster.
While no destructive processes will be invoked, no changes can be made to frontend resources.
