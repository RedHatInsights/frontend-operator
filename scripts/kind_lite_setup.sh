#!/usr/bin/env bash

# Configures a local kind cluster for testing FEO e2e.
# Creates the cluster, installs required dependencies, installs FEO

set -e

if command -v "kind" >/dev/null 2>&1; then
   echo "Found kind!"
else 
    echo "Script requires kind command; install and try again"
    exit 1
fi

if command -v "cmctl" > /dev/null 2>&1; then 
    echo "Found cmctl!"
else
    echo "Script requires cmctl command; install and try again"
    exit 1
fi

# Default context established by kind upon cluster creation is 'kind-kind'
KUBECTL_CMD='kubectl --context kind-kind'

# There is some weird business to resolve with remote images versus
# locally-built ones with podman. For now, let's expect IMG to be the
# fully-qualified quay image path, e.g. https://quay.io/cloudservices/frontend-operator:12345
IMG=$1
if [ -z "${IMG}" ]; then
   echo "Need an image to build, try again..."
   exit 1
else
   echo "IMG is ${IMG}"
fi

# kubectl is required for interactions with the cluster.
if [ -n "${KUBECTL_CMD}" ]; then
    :  # already set via env var
elif command -v kubectl; then
    KUBECTL_CMD=kubectl
else
    echo "*** 'kubectl' not found in path. Please install it or minikube, or set KUBECTL_CMD"
    exit 1
fi

echo "Setting up the kind cluster"
kind delete cluster
kind create cluster
kubectl config set-context kind-kind

echo "Creating the boot namespace"
${KUBECTL_CMD} create namespace boot

# echo "Podman build and save the image, then load into cluster (local testing only)"
# See https://github.com/kubernetes-sigs/kind/issues/2027 for more info on why this is needed
# -- podman build -f Dockerfile -t controller:latest
# -- podman pull "${IMG}"
# -- podman image save "${IMG}" -o image.tar
echo "Loading ${IMG} from image.tar"
kind load image-archive image.tar

# hacky, but better than spending hours messing around with various kube tools
echo "Doing an in-place update of the manifest (loading ${IMG})"
MANIFEST_IMG="${IMG#https://}"
echo "MANIFEST_IMG ${MANIFEST_IMG}"
sed -i -e "1108s#controller:latest#${MANIFEST_IMG}#" manifest.yaml

echo "Applying FEO manifest"
${KUBECTL_CMD} apply -f manifest.yaml

echo "If the FEO is ready, this should show Running"
until ${KUBECTL_CMD} get pods -n frontend-operator-system | grep "Running"; do
    echo "Waiting for frontend-operator-system"
    sleep 5
done

echo "Firing up Chrome example frontend"
${KUBECTL_CMD} apply -f examples/chrome.yaml
${KUBECTL_CMD} get frontend | grep chrome
