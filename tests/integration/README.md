# Config assets integration test

The `Config assets integration` GitHub Actions workflow runs this suite on pull
requests using a real minikube cluster. It uploads the KUTTL report, operator log,
and cluster diagnostics as the `config-assets-integration` artifact.

This test needs a Kubernetes cluster that runs pods, a locally running FEO, and
`oc` and `kubectl kuttl` on PATH. The normal `make kuttl` uses envtest and cannot
execute upload Jobs. Use a disposable minikube cluster and verify the context:

```sh
kubectl config current-context  # must print minikube
make generate manifests build
oc --context=minikube apply -f config/crd/bases -f config/crd/test-resources
```

Start the operator in a separate terminal. Empty access-key and Secret-name
overrides select the test namespace's `env-test-config-assets-minio` Secret,
even if a local `.env` contains production-style push cache settings:

```sh
KUBECONFIG="$HOME/.kube/config" PUSHCACHE_AWS_ACCESS_KEY_ID= PUSHCACHE_AWS_SECRET_NAME= \
  bin/manager --metrics-bind-address :9090 --health-probe-bind-address :9091 --log-level 0
```

Run from the repository root:

```sh
kubectl kuttl test --config tests/integration/kuttl-config.yaml --test config-assets
```

The test creates `test-config-assets` and the cluster-scoped
`test-config-assets-environment`. It deploys a disposable MinIO bucket and checks
actual HTTP reads of `data/config/fed-modules.json`, its 60-second cache header,
excluded files, a configuration-only refresh with both images unchanged, and
removal of the Job and snapshot when the feature is disabled. It then re-enables
publication and verifies cleanup and finalization when the last Frontend is deleted.
Frontend containers and per-application push cache are disabled throughout.

The MinIO fixture uses the versioned community image
`docker.io/pgsty/minio:RELEASE.2026-08-04T00-00-00Z`, including `mc`
([source](https://github.com/pgsty/silo/tree/RELEASE.2026-08-04T00-00-00Z)).
Upstream `quay.io/minio/minio` and `quay.io/minio/mc` no longer allow anonymous
pulls. The valpop fixture uses the published Red Hat image; the node must be able
to execute its architecture (including amd64 emulation on ARM when necessary).

For troubleshooting:

```sh
oc --context=minikube get jobs,pods -n test-config-assets
oc --context=minikube logs -n test-config-assets \
  job/test-config-assets-environment-config-assets -c valpop-config
oc --context=minikube get frontend chrome -n test-config-assets -o yaml
```

If fixtures remain after a manual run, remove them while the local operator is
still running, so it can handle Frontend finalization. Stop the operator afterward:

```sh
oc --context=minikube delete frontend chrome -n test-config-assets
oc --context=minikube delete frontendenvironment test-config-assets-environment
oc --context=minikube delete namespace test-config-assets
```
