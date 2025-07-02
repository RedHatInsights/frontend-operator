#!/bin/bash
# Configures a local kind cluster for testing FEO e2e.
# Creates the cluster, installs required dependencies, installs Clowder, installs FEO

set -e

# TODO: check for kind command
# TODO: check for cmctl command

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

python3 -m venv "build/.build_venv"
source build/.build_venv/bin/activate
pip install --upgrade pip setuptools wheel
pip install pyyaml

echo "Setting up the kind cluster"
kind delete cluster
kind create cluster
kubectl config set-context kind-kind

echo "Installing cert manager with kubectl"
# TODO: Make the version configurable?
${KUBECTL_CMD} apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.18.2/cert-manager.yaml

# Wait for the cert manager api to be available before proceeding
until cmctl check api; do
  echo "Waiting for cert manager..."
  sleep 5
done

echo "Installing Clowder to kind cluster"
${KUBECTL_CMD} apply -f $(curl https://api.github.com/repos/RedHatInsights/clowder/releases/latest | jq '.assets[0].browser_download_url' -r) --validate=false

until ${KUBECTL_CMD} get pod -n clowder-system | grep clowder-controller; do
  echo "Waiting for clowder-controller availability ... "
  sleep 5
done 

echo "Creating the boot namespace"
${KUBECTL_CMD} create namespace boot

echo "Applying FEO manifest"
${KUBECTL_CMD} apply -f manifest.yaml



