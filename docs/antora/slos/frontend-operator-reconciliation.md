# Frontend Operator Reconciliation SLO

## Description

The frontend operator implements metrics to expose the error rate of reconcilations targeting its CRDs. When that
error rate is too high, we will alert. Reconciliation errors can indicate a wide array of issue including misconfigurations,
outages, and quota/resource constraints. In general, if the operator's cannot reconcile successfully at a nominal rate,
investigation is needed.

## SLI Rationale

Reconciliation error rates show many different issues across several environments. We can use this metric to catch
misconfigurations in production apps and find deploy time issues with the operator. 

## Implmentation

The Operator SDK exposes the `controller_runtime_reconcile_total` metric to show the nominal reconcilation rate. Using the
`sum(increase)` modifier allows us to determine if that amount is less than 100%.

## SLO Rationale
Almost all reconciler calls should be handled without issue. If we are hitting more than 10% errors on reconcile, debugging
should begin.

## Alerting
Alerts should be kept to a medium level. Because there are a myriad of issues that could cause a reconciliation error, breaking
this SLO should not result in a page. It should be addressed, but error rate alone does not indiciate an outage.
