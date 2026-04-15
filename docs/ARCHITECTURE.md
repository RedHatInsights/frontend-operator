# Architecture

## System Context

The frontend-operator is a Kubernetes operator that deploys and manages containerized frontend applications in the Red Hat Hybrid Cloud Console (consoledot) ecosystem. It runs in OpenShift clusters alongside [Clowder](https://github.com/RedHatInsights/clowder) (which manages backend services) and generates the runtime configuration that [insights-chrome](https://github.com/RedHatInsights/insights-chrome) consumes to render navigation, module federation, search, and service tiles.

### External Systems

| System | Relationship |
|--------|-------------|
| **insights-chrome** | Consumes operator-generated ConfigMaps (fed-modules.json, navigation, search index, service tiles, widget registry) |
| **app-interface** | Defines FrontendEnvironment resources (SSO, hostname, bundles, service categories) for stage/prod via YAML templates |
| **Clowder** | Shares SDK utilities (`rhc-osdk-utils`) and error types; manages backend services in the same cluster |
| **valpop** | Standalone Go tool that copies frontend static assets to S3/Valkey; invoked via operator-managed Kubernetes Jobs |
| **frontend-asset-proxy** | Caddy-based reverse proxy for S3 asset serving; deployed by the ReverseProxyController |
| **Konflux** | Builds container images and runs enterprise contract checks on PRs |

## Controllers

Two controllers, each watching a different primary resource:

### FrontendReconciler

**Watches**: Frontend (primary), Bundle (secondary), FrontendEnvironment (secondary)

When any Frontend, Bundle, or FrontendEnvironment changes, the reconciler enqueues all Frontends in the affected environment. This fan-out design ensures ConfigMaps are regenerated from the complete set of Frontends — navigation and module federation require a global view.

The reconciliation flow (`FrontendReconciliation.run()` in `reconcile.go`):

1. **ConfigMap generation** — aggregates data from all Frontends in the environment into JSON ConfigMap keys
2. **Deployment + Service** — creates per-Frontend Deployment and Service (only if `spec.image` is set)
3. **Pushcache jobs** — creates valpop Jobs to copy assets to S3 (when `enablePushCache: true`)
4. **Akamai cache-bust jobs** — creates cache invalidation Jobs (when `enableAkamaiCacheBust: true`)
5. **Ingress** — creates per-Frontend nginx Ingress
6. **ServiceMonitor** — creates Prometheus ServiceMonitor for metrics scraping

Uses `RetryOnConflict` around the entire reconciliation to handle 409 conflicts caused by the fan-out watch pattern (multiple Frontends reconciling concurrently against the same FrontendEnvironment).

### ReverseProxyController

**Watches**: FrontendEnvironment

Manages a Caddy-based reverse proxy deployment per FrontendEnvironment when push cache is enabled and `reverseProxyImage` is configured. Simpler than FrontendReconciler — no fan-out, no resource cache.

## CRD Design Decisions

### Why FrontendEnvironment is Cluster-Scoped

FrontendEnvironment defines shared infrastructure config (SSO, hostname, bundles, service categories, pushcache settings) that spans multiple namespaces. Frontend resources in different namespaces reference the same FrontendEnvironment by name via `spec.envName`. Cluster scope avoids cross-namespace references and makes the environment a single source of truth.

### Why Frontend is Namespaced

Each team owns a namespace. Frontend resources are namespaced so teams can manage their own frontend deployments with standard RBAC, while the operator aggregates them at the environment level.

### Bundle Abstraction

Bundles define navigation structure (e.g., "Settings", "Insights"). They are separate from Frontends because navigation hierarchy is orthogonal to deployment — multiple Frontends inject nav items into the same Bundle. Frontends reference Bundles via `navItems` (direct injection) or `bundleSegments` (positioned insertion).

## Key Subsystems

### ConfigMap Generation

The operator generates a single ConfigMap per FrontendEnvironment containing aggregated data from all Frontends:

| Key | Purpose | Source |
|-----|---------|--------|
| `fed-modules.json` | Module federation manifests for runtime loading | `Frontend.Spec.Module` |
| `<bundle>-navigation.json` | Navigation trees per bundle | `Frontend.Spec.NavItems` + `BundleSegments` |
| `search-index.json` | Search entries for chrome search | `Frontend.Spec.SearchEntries` |
| `service-tiles.json` | Service dropdown tiles | `Frontend.Spec.ServiceTiles` + `FrontendEnvironment.Spec.ServiceCategories` |
| `widget-registry.json` | Widget metadata | `Frontend.Spec.WidgetRegistry` |

ConfigMaps are propagated to `targetNamespaces` listed in the FrontendEnvironment.

### Pushcache (valpop) Jobs

When `enablePushCache: true`, the operator creates a Kubernetes Job per Frontend that runs the valpop image to copy static assets to an S3-compatible object store. Jobs are tracked via pod template annotations (`frontend-image`, `valpop-image`) and recreated when images or the deploy cutoff timestamp changes. `manageExistingJob()` handles stale job detection.

### Resource Cache Pattern

The operator uses `rhc-osdk-utils/resourceCache` to batch Kubernetes API calls within a single reconciliation:

```go
cache := resCache.NewObjectCache(ctx, r.Client, &log, cacheConfig)
// ... populate resources ...
cache.ApplyAll()     // batch create/update
cache.Reconcile(uid) // delete orphaned resources
```

This reduces API server load and provides automatic garbage collection of resources no longer needed.

## Metrics

Three Prometheus metrics registered in `metrics.go`:

| Metric | Type | Description |
|--------|------|-------------|
| `frontend_managed_frontends` | Gauge | Number of Frontends currently managed |
| `frontend_app_reconciliation_requests` | Counter | Reconciliation requests per app |
| `frontend_app_reconciliation_time` | Histogram | Reconciliation duration per app |

Exposed via controller-runtime's metrics server (`:8080` by default). ServiceMonitor resources are created per Frontend when monitoring is enabled in the FrontendEnvironment.

## Status Conditions

Frontend resources report three status conditions:

| Condition | Meaning |
|-----------|---------|
| `ReconciliationSuccessful` | Last reconciliation completed without error |
| `ReconciliationFailed` | Last reconciliation encountered an error (message contains details) |
| `FrontendsReady` | All managed deployments have Available=True |

Status updates use `RetryOnConflict` to handle resourceVersion conflicts, and only write when the status has actually changed (deep equality check).

## Finalizers

The FrontendReconciler adds a `finalizer.frontend.cloud.redhat.com` finalizer to each Frontend. On deletion, it removes the Frontend from the `managedFrontends` map (updating the gauge metric). The resource cache's `Reconcile()` method handles cleanup of owned resources via owner references.
