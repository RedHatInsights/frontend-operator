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

### 3. Verify you're talking to minikube — do this at the start of every session
```sh
kubectl config current-context   # must show "minikube"
# If not:
kubectl config use-context minikube
```

### 4. Create the boot namespace and install resources
`make create-boot-namespace` creates the `boot` namespace (note: `env-boot` is the FrontendEnvironment resource name, not a namespace).
It also regenerates manifests as a side effect.
```sh
make create-boot-namespace
make install-resources
```

### 5. Run the operator (terminal 1)
```sh
make run-local
```

### 6. Exercise it (terminal 2)
```sh
kubectl apply -f config/crd/test-resources/
watch -n 0.1 'kubectl annotate frontend inventory -n default force-conflict=$(date +%s) --overwrite'
```

## Common Failures

**`PROVIDER_PODMAN_NOT_RUNNING: sudo -n -k podman version` exit status 1**
→ podman is not configured for rootless mode: `minikube config set rootless true`

**`volume with name minikube already exists`**
→ `podman volume rm minikube` then retry `minikube start`

**`no container with name or ID "minikube" found`**
→ Full reset: `minikube delete --purge && minikube config set rootless true && minikube start --driver=podman`

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
