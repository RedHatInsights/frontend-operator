# Testing Guidelines

## Test Framework

- **Unit tests**: Ginkgo v2 + Gomega assertions with controller-runtime's `envtest` (provides a real API server and etcd, no mocking)
- **E2E tests**: [kuttl](https://kuttl.dev/) — declarative YAML-based test suites in `tests/e2e/`
- **Test binary**: `make test` runs unit tests; `make kuttl` runs e2e (requires `make run-local` in a separate terminal)

## Unit Test Patterns

### Test Environment Setup

Tests use a shared `testEnv` initialized in `controllers/suite_test.go`. It starts a real API server with the operator's CRDs installed. Do not create separate environments per test file.

```go
// suite_test.go pattern — already configured, do not duplicate
testEnv = &envtest.Environment{
    CRDs: [...],
}
cfg, _ := testEnv.Start()
```

### Writing Controller Tests

- Place controller tests in `controllers/` alongside the source (e.g., `reconcile_reverse_proxy_test.go`)
- Use `BeforeEach` to create fresh Frontend/FrontendEnvironment resources per test
- Use `Eventually` with timeouts for reconciliation assertions — the controller runs asynchronously:
  ```go
  Eventually(func() bool {
      err := k8sClient.Get(ctx, key, &deployment)
      return err == nil
  }, timeout, interval).Should(BeTrue())
  ```
- Clean up resources in `AfterEach` — envtest does not automatically delete between tests

### Test Naming

- Use `Describe`/`Context`/`It` blocks that read as sentences
- Test file names: `<feature>_test.go` in the `controllers` package

## E2E Tests (kuttl)

### Directory Structure

Each test lives in `tests/e2e/<test-name>/` with numbered step files:
```
tests/e2e/pushcache/
  00-install.yaml        # CRD resources to apply
  01-assert.yaml         # Expected state assertions
  02-assert.yaml         # Additional assertions after changes
```

### Step File Conventions

- `NN-install.yaml` — applies resources to the cluster
- `NN-assert.yaml` / `NN-assert-*.yaml` — kuttl asserts these resources exist with matching fields
- `NN-errors.yaml` — asserts resources do NOT exist
- `NN-delete.yaml` — deletes resources

### Key Rules

- Assert files must include ALL annotations and labels set by the operator on generated resources (e.g., pod template annotations for pushcache jobs)
- When adding new annotations to operator-generated resources, update ALL assert files that reference those resources
- kuttl runs against a live cluster — `make run-local` must be running in a separate terminal
- Test namespaces are `boot` and `env-boot`; these must exist before running tests

## Running Tests

```sh
# Unit tests (starts envtest API server internally)
make test

# E2E tests (requires operator running separately)
make run-local          # terminal 1
make kuttl              # terminal 2

# Single e2e test
kubectl kuttl test --config kuttl-config.yml ./tests/e2e --test pushcache
```

## CI Checks

- **GitHub Actions**: `lint.yml` runs `golangci-lint`, unit tests run via `Run Unit Tests` workflow
- **Konflux**: builds container image and runs enterprise contract checks
- Grype vulnerability scans may fail due to infrastructure issues — these are not code-related
