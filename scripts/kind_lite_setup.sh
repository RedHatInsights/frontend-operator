#!/bin/bash
# Configures a local kind cluster for testing FEO e2e.
# Creates the cluster, installs required dependencies, installs Clowder, installs FEO

set -e

CERT_MGR_VERSION='v1.18.2'

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

echo "Installing cert manager with kubectl"
# TODO: Make the version configurable?
${KUBECTL_CMD} apply -f "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MGR_VERSION}/cert-manager.yaml"

# Wait for the cert manager api to be available before proceeding
until cmctl check api; do
  echo "Waiting for cert manager..."
  sleep 5
done

echo "Creating the boot namespace"
${KUBECTL_CMD} create namespace boot

echo "Podman build and save the image, then load into cluster (local testing only)"
# See https://github.com/kubernetes-sigs/kind/issues/2027 for more info on why this is needed
# podman save
# kind load image-archive


echo "Applying FEO manifest"
${KUBECTL_CMD} apply -f manifest.yaml

echo "If the FEO is ready, this should show 1/1"
${KUBECTL_CMD}  get pods -n frontend-operator-system
