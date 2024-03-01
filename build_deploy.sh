#!/bin/bash

set -exv

IMAGE="quay.io/cloudservices/frontend-operator"
IMAGE_TAG=$(git rev-parse --short=7 HEAD)
export BUILDER_NAME="builder-${JOB_NAME}-${BUILD_ID}"

if [[ -z "$QUAY_USER" || -z "$QUAY_TOKEN" ]]; then
    echo "QUAY_USER and QUAY_TOKEN must be set"
    exit 1
fi

if [[ -z "$RH_REGISTRY_USER" || -z "$RH_REGISTRY_TOKEN" ]]; then
    echo "RH_REGISTRY_USER and RH_REGISTRY_TOKEN  must be set"
    exit 1
fi

DOCKER_CONF="$PWD/.docker"
mkdir -p "$DOCKER_CONF"




docker login -u="$QUAY_USER" -p="$QUAY_TOKEN" quay.io
docker login -u="$RH_REGISTRY_USER" -p="$RH_REGISTRY_TOKEN" registry.redhat.io

# Check if the multiarchbuilder exists
if docker buildx ls | grep -q "multiarchbuilder"; then
    docker buildx use multiarchbuilder
    echo "Using multiarchbuilder for buildx"
    # Multi-architecture build
    docker buildx build --platform linux/amd64,linux/arm64 -t "${IMAGE}:${IMAGE_TAG}" --push .
else
    echo "Falling back to standard build and push"
    # Standard build and push
    docker build -t "${IMAGE}:${IMAGE_TAG}" .
    docker push "${IMAGE}:${IMAGE_TAG}"
fi
