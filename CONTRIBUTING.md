# Contributing

## Prerequisites

- Go 1.25+ (matching `go.mod`)
- [podman](https://podman.io/) in rootless mode
- [minikube](https://minikube.sigs.k8s.io/)
- [oc](https://docs.openshift.com/container-platform/latest/cli_reference/openshift_cli/getting-started-cli.html) (OpenShift CLI)
- [golangci-lint](https://golangci-lint.run/)
- [kuttl](https://kuttl.dev/) (for e2e tests)

See [CLAUDE.md](CLAUDE.md) for detailed local development setup instructions.

## Development Workflow

1. Fork the repository and clone your fork
2. Create a feature branch from `main`
3. Make your changes
4. Run checks locally:
   ```sh
   make lint       # golangci-lint
   make test       # unit tests (envtest + Ginkgo)
   ```
5. If you changed CRD types (`api/v1alpha1/*_types.go`):
   ```sh
   make generate   # regenerate DeepCopy methods
   make manifests  # regenerate CRD YAML
   ```
   Commit the generated files alongside your type changes.
6. Push your branch and open a pull request

## Commit Conventions

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
type(scope): short description
```

- **Types**: `fix`, `feat`, `refactor`, `test`, `docs`, `chore`
- **Scope**: component area (e.g., `pushcache`, `reconciler`, `crd`, `proxy`, `ingress`)
- **Title**: under 50 characters
- **Body**: include the Jira ticket key if applicable

Example:
```
fix(pushcache): recreate jobs on valpop image change

RHCLOUD-46128
Track valpop image via pod template annotation.
```

## Pull Request Guidelines

### Required CI Checks

All PRs must pass these GitHub Actions workflows:

| Workflow | What it checks |
|----------|---------------|
| **Go** (`lint.yml`) | golangci-lint with gocritic, gosec, revive, bodyclose |
| **Run Unit Tests** (`package.yml`) | `make test` — Ginkgo unit tests with envtest |
| **ConsoleDot Platform Security Scan** (`platsec.yml`) | Anchore Grype vulnerability scan + Syft SBOM |

Additionally, Konflux builds a container image and runs enterprise contract checks.

> **Note**: Grype scan failures are often infrastructure-related (resource limits in CI runners), not code issues. If all other checks pass, this is likely the case.

### E2E Test Updates

When your change modifies operator-generated resources (Deployments, Jobs, ConfigMaps, Ingresses):

- Update all kuttl assert files in `tests/e2e/` that reference the affected resources
- Assert files must include ALL annotations and labels set by the operator

### Code Review

PRs require review from a repository maintainer before merging. Reviewers will check:

- Code correctness and adherence to Go conventions
- Test coverage for new functionality
- CRD generated files are up to date
- E2e test assertions are updated

## Dependency Management

- Go dependencies are managed via `go.mod` / `go.sum`
- [Renovate](https://docs.renovatebot.com/) is configured for automated dependency updates (`renovate.json`)
- Base images (ubi9-minimal, go-toolset) are auto-updated daily via the `imageupdate.yml` workflow

## Project Documentation

| Document | Description |
|----------|-------------|
| [AGENTS.md](AGENTS.md) | AI agent onboarding: conventions, structure, docs index |
| [CLAUDE.md](CLAUDE.md) | Claude Code-specific setup and commands |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | System design, controller flow, architectural decisions |
| [docs/testing-guidelines.md](docs/testing-guidelines.md) | Unit test patterns, e2e conventions, CI checks |
| [docs/operator-development-guidelines.md](docs/operator-development-guidelines.md) | CRD types, reconciliation flow, resource patterns |
| [docs/antora/](docs/antora/) | User-facing documentation (API reference, configuration guides) |
