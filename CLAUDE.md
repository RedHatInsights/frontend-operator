# frontend-operator Local Development

## Prerequisites
- podman (rootless mode required)
- minikube
- oc (OpenShift CLI) — resource commands use `oc`, not `kubectl`
- kubectl — for cluster verification and context management

## Initial Setup

### 1. Configure minikube for rootless podman (once per machine)
```sh
minikube config set rootless true
```

### 2. Start the cluster
```sh
minikube start --driver=podman --container-runtime=cri-o
```

The operator creates Ingress resources (nginx ingress class) for each Frontend and the reverse proxy. Enable the ingress addon if you want frontends accessible in the browser — not required for operator development:
```sh
minikube addons enable ingress
```

### 3. Verify you're talking to minikube — do this at the start of every session
```sh
kubectl config current-context   # must show "minikube"
# If not:
kubectl config use-context minikube
```

### 4. Create namespaces and install resources
`make create-namespaces` creates both the `boot` and `env-boot` namespaces and regenerates manifests as a side effect.
Note: `env-boot` is also the FrontendEnvironment resource name — both the namespace and the resource are required.
```sh
make create-namespaces
make install-resources
```

### 5. Run the operator (terminal 1)
`make run-local` defaults to Info log level (`--log-level 0`). Use `--log-level -1` for Debug output.
Available levels: `-1` Debug, `0` Info, `1` Warn, `2` Error.
```sh
make run-local
# or for debug:
make run-local 2>&1 | grep -v "level\":\"info\""  # filter if too noisy
```

### 6. Exercise it (terminal 2)
```sh
kubectl apply -f config/crd/test-resources/
watch -n 0.1 'kubectl annotate frontend inventory -n default force-conflict=$(date +%s) --overwrite'
```

## Optional minikube addons

Enable these as needed depending on what you're working on:

```sh
# Required to access deployed frontends in the browser (operator creates nginx Ingress resources)
minikube addons enable ingress

# Useful for building and pushing custom operator or frontend images locally
minikube addons enable registry

# Enables kubectl top and HPA (horizontal pod autoscaler) support
minikube addons enable metrics-server
```

`--disable-optimizations` can be passed to `minikube start` for more production-like cluster behaviour.

**ServiceMonitor CRD warning** — the operator logs a non-fatal error on startup if the Prometheus Operator CRDs are not installed:
```
no matches for kind "ServiceMonitor" in version "monitoring.coreos.com/v1"
```
This is unrelated to `metrics-server`. To silence it, install the Prometheus Operator CRDs. Safe to ignore for most local development.

## Common Failures

**`PROVIDER_PODMAN_NOT_RUNNING: sudo -n -k podman version` exit status 1**
→ podman is not configured for rootless mode: `minikube config set rootless true`

**`volume with name minikube already exists`**
→ `podman volume rm minikube` then retry `minikube start`

**`no container with name or ID "minikube" found`**
→ Full reset: `minikube delete --purge && minikube config set rootless true && minikube start --driver=podman`

**`ValpopImage must be specified in the FrontendEnvironment when PushCache is enabled`**
→ `examples/feenvironment.yaml` has `enablePushCache: true`. Set it to `false` for local dev, or add `valpopImage: quay.io/redhat-services-prod/hcc-platex-services-tenant/valpop:latest`. Re-apply: `oc apply -f examples/feenvironment.yaml -n boot`

**Resource exhaustion / OOMKilled / cluster instability**
→ Beef up the podman machine and minikube resources:
```sh
# Stop the cluster first
minikube stop
podman machine stop

# Set podman machine resources
podman machine set --cpus 6 --disk-size 100 --memory 20000

# Set minikube resources (persisted in config)
minikube config set cpus 4
minikube config set memory 16000
minikube config set disk-size 36GB

podman machine start
minikube start --driver=podman --container-runtime=cri-o
```

## Context Safety
Always verify context before applying resources — a wrong context means you're targeting a real cluster.
```sh
kubectl config current-context   # must be "minikube"
```
