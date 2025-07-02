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

until ${KUBECTL_CMD} get pod -n clowder-system | grep clowder-system; do
  echo "Waiting for clowder-system availability ... "
  sleep 5
done 

echo "Creating the boot namespace"
${KUBECTL_CMD} create namespace boot

echo "Installing FEO resources"
${KUBECTL_CMD} apply -f config/crd/bases/cloud.redhat.com_frontends.yaml
${KUBECTL_CMD} apply -f config/crd/bases/cloud.redhat.com_frontendenvironments.yaml
${KUBECTL_CMD} apply -f config/crd/bases/cloud.redhat.com_bundles.yaml
${KUBECTL_CMD} apply -f examples/clowdenvironment.yaml
${KUBECTL_CMD} apply -f examples/feenvironment.yaml -n boot
${KUBECTL_CMD} apply -f examples/inventory.yaml -n boot
${KUBECTL_CMD} apply -f examples/bundle.yaml -n boot 

# TODO: Figure out what is required next to install the FEO
# TODO: Figure out the command to confirm the success of FEO install



