# AI Agent Guide — frontend-operator

A Kubernetes operator that deploys and manages containerized frontend applications in the Red Hat Hybrid Cloud Console (consoledot) ecosystem. Built with controller-runtime and kubebuilder.

## Documentation Index

| Document | Description |
|----------|-------------|
| [Testing Guidelines](docs/testing-guidelines.md) | Unit test patterns (envtest/Ginkgo), e2e tests (kuttl), CI checks |
| [Operator Development Guidelines](docs/operator-development-guidelines.md) | CRD types, reconciliation flow, ConfigMap generation, pushcache jobs |

## Tech Stack

- **Language**: Go 1.25
- **Framework**: controller-runtime v0.23, kubebuilder
- **CRDs**: Frontend (namespaced), FrontendEnvironment (cluster-scoped), Bundle (namespaced)
- **Testing**: Ginkgo v2 + Gomega (unit), kuttl (e2e)
- **Linting**: golangci-lint with gocritic, gosec, revive, bodyclose
- **Dependencies**: Clowder (shared error types, SDK utils), Prometheus Operator (ServiceMonitor)

## Project Structure

```
api/v1alpha1/          # CRD type definitions (Frontend, FrontendEnvironment, Bundle)
controllers/           # Reconcilers, resource management, metrics
  templates/           # Embedded templates (Caddyfile)
  utils/               # Helper functions
config/                # Kustomize manifests, CRD bases, RBAC, test resources
tests/e2e/             # kuttl e2e test suites (numbered YAML steps)
examples/              # Sample CRD instances for local development
docs/antora/           # Antora-based user documentation (API reference, guides)
hack/                  # Boilerplate headers
```

## Key Conventions

### Code Style

- Follow `golangci-lint` rules defined in `.golangci.yml` — gocritic, gosec, revive, bodyclose are enabled
- gofmt and goimports are enforced as formatters
- Exported type comments are not required (revive `exported` rule is disabled)
- Test files are exempt from errcheck and gosec linters

### Naming

- CRD types use `FrontendSpec`, `FrontendEnvironmentSpec` pattern — `Spec` suffix for desired state, `Status` suffix for observed state
- Generated types use `Generated` suffix (e.g., `FrontendBundlesGenerated`, `FrontendServiceCategoryGenerated`)
- Helper functions on types use receiver methods (e.g., `Frontend.GetIdent()`, `FrontendEnvironment.GetFrontendsInEnv()`)

### Resource Commands

- Use `oc` (OpenShift CLI) for resource commands, not `kubectl` — this is the established convention in the Makefile and documentation
- Exception: `kubectl` is used for kuttl tests and context management

### CRD Changes Workflow

After modifying any `*_types.go` file, always run:
```sh
make generate   # regenerates DeepCopy methods
make manifests  # regenerates CRD YAML
```
Both generated outputs must be committed.

### Commit Messages

Use conventional commits: `type(scope): description`
- Types: `fix`, `feat`, `refactor`, `test`, `docs`, `chore`
- Scope: component area (e.g., `pushcache`, `reconciler`, `crd`, `proxy`)
- Keep title under 50 characters
- Include Jira ticket key in the commit body

### Pull Requests

- PRs must pass: golangci-lint, unit tests, Konflux build
- Grype vulnerability scan failures are typically infrastructure-related, not code issues
- Update e2e test assert files when changing operator-generated resource fields/annotations
- Commits should be atomic — one logical change per commit

## Common Pitfalls

- **Forgetting `make generate && make manifests`** after CRD type changes — the PR will fail lint or tests with mismatched types
- **Missing e2e assert updates** — when adding annotations or labels to operator-generated resources (Jobs, Deployments, ConfigMaps), all kuttl assert files referencing those resources must be updated
- **Wrong kubectl context** — always verify `kubectl config current-context` shows `minikube` before applying resources locally
- **Pushcache requires ValpopImage** — if `enablePushCache: true` is set in a FrontendEnvironment, `valpopImage` must also be set or the operator will error
- **Unit tests need envtest** — controller tests require `KUBEBUILDER_ASSETS` from envtest; `make test` handles this automatically
