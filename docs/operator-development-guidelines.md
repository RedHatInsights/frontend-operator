# Operator Development Guidelines

## CRD Types

Three custom resources in `api/v1alpha1/`:

| Resource | Scope | Short Name | File |
|----------|-------|------------|------|
| Frontend | Namespaced | `fe` | `frontend_types.go` |
| FrontendEnvironment | Cluster | `feenv` | `frontendenvironment_types.go` |
| Bundle | Namespaced | — | `bundle_types.go` |

### Relationships

- **FrontendEnvironment** is cluster-scoped and defines shared config (SSO, hostname, ingress, bundles, service categories, pushcache settings)
- **Frontend** is namespaced and references a FrontendEnvironment by `spec.envName`
- **Bundle** defines navigation structure; Frontends inject nav items into Bundles via `navItems` or `bundleSegments`

## Reconciliation Flow

Two controllers in `controllers/`:

### FrontendReconciler (`frontend_controller.go`)
Watches Frontend resources. On reconcile:
1. Looks up the referenced FrontendEnvironment
2. Lists all Frontends in the same environment
3. Calls `FrontendReconciliation.run()` in `reconcile.go`

The `run()` method:
1. `setupConfigMaps()` — generates fed-modules.json, navigation JSON, search index, service tiles, widget registry
2. Creates/updates Deployments, Services, Ingresses for each Frontend
3. Manages pushcache (valpop) jobs when `enablePushCache: true`
4. Manages Akamai cache-bust jobs when configured
5. Propagates ConfigMaps to `targetNamespaces`

### ReverseProxyController (`reverse_proxy_controller.go`)
Watches FrontendEnvironment resources. Manages Caddy-based reverse proxy deployments for S3 asset serving.

## Modifying CRD Types

After changing any `*_types.go` file:
```sh
make generate    # regenerates DeepCopy methods (zz_generated.deepcopy.go)
make manifests   # regenerates CRD YAML in config/crd/bases/
```

Always run both. The generated files must be committed.

## Resource Cache Pattern

The operator uses `rhc-osdk-utils/resourceCache` to batch Kubernetes API calls:
```go
cache := resCache.NewObjectCache(ctx, r.Client, scheme)
// ... populate resources ...
cache.Update(Frontend)   // applies all cached changes
```

Prefer the cache pattern over direct `r.Client.Create/Update` calls for consistency.

## ConfigMap Data Flow

The operator generates several ConfigMap keys from aggregated Frontend data:

| Key | Content | Source |
|-----|---------|--------|
| `fed-modules.json` | Module federation manifests | `Frontend.Spec.Module` |
| `<bundle-id>-navigation.json` | Navigation trees | `Frontend.Spec.NavItems` + `BundleSegments` |
| `search-index.json` | Search entries | `Frontend.Spec.SearchEntries` |
| `service-tiles.json` | Service dropdown tiles | `Frontend.Spec.ServiceTiles` + `FrontendEnvironment.Spec.ServiceCategories` |
| `widget-registry.json` | Widget metadata | `Frontend.Spec.WidgetRegistry` |

ConfigMaps are created in the FrontendEnvironment's target namespace and optionally propagated to `targetNamespaces`.

## Pushcache (valpop) Jobs

When `enablePushCache: true`, the operator creates a Kubernetes Job per Frontend:
- Job runs the valpop image to copy frontend assets to S3
- Tracked via pod template annotations: `frontend-image`, `valpop-image`
- Jobs are recreated when the Frontend image, valpop image, or deploy cutoff timestamp changes
- `manageExistingJob()` handles stale job detection and deletion

## Annotations Convention

- `frontend-image` — tracks which Frontend container image the pushcache job was built for
- `valpop-image` — tracks the valpop image version used
- `qontract.recycle: "true"` — signals app-interface to restart pods when ConfigMap changes

## Error Handling

- Use `errors.Wrap()` from `github.com/RedHatInsights/clowder/controllers/cloud.redhat.com/errors` for error wrapping
- Return errors up the reconciliation chain — the controller framework handles requeueing
- Status conditions are set via `controllers/status.go` using `SetFrontendConditions()`

## Logging

- Use structured logging via `logr.Logger` (passed as `r.Log`)
- Log levels: `-1` Debug, `0` Info, `1` Warn, `2` Error
- Prefer `r.Log.Info("message", "key", value)` over formatted strings
