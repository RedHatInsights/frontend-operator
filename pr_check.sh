#!/bin/bash

set -exv

IMAGE="quay.io/cloudservices/frontend-operator"
IMAGE_TAG=$(git rev-parse --short=7 HEAD)

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

docker --config="$DOCKER_CONF" login -u="$QUAY_USER" -p="$QUAY_TOKEN" quay.io
docker --config="$DOCKER_CONF" login -u="$RH_REGISTRY_USER" -p="$RH_REGISTRY_TOKEN" registry.redhat.io

CONTAINER_NAME="${FEO_CONTAINER_NAME:-frontend-operator-pr-check-$ghprbPullId}"
docker rm -f $CONTAINER_NAME
docker rm -f $CONTAINER_NAME-run


# We're mounting the jenkins workspace over the root of the container
# This means that the pr_check_inner.sh script will be run in the context of the jenkins workspace
# This confused me for a while because pr_check_inner.sh is also copied into the pr check container at build time
# but the template_check.sh isn't. I couldn't figure out how it was sourcing it

function start_builder() {
    # Check if the "multiarch" builder instance exists
    BUILDER_EXISTS=$(docker buildx ls | grep "multiarch" || true)

    # If the "multiarch" builder does not exist, create it
    if [ -z "$BUILDER_EXISTS" ]; then
        echo "Creating 'multiarch' builder instance..."
        docker buildx create --name multiarch --platform linux/amd64,linux/arm64 --use --driver-opt network=host --buildkitd-flags '--allow-insecure-entitlement network .host'
    fi

    # Check if the "multiarch" builder is running
    BUILDER_RUNNING=$(docker buildx inspect multiarch | grep "running" || true)

    # If the "multiarch" builder is not running, bootstrap it
    if [ -z "$BUILDER_RUNNING" ]; then
        echo "Bootstraping 'multiarch' builder instance..."
        docker buildx inspect multiarch --bootstrap
    fi

    echo "All set!"
}

start_builder

docker --config="$DOCKER_CONF" buildx build --builder multiarch0 --platform linux/amd64  --build-arg BASE_IMAGE="$BASE_IMG" --build-arg GOARCH="amd64" -t "${IMAGE}:${IMAGE_TAG}-amd64" .
docker --config="$DOCKER_CONF" buildx build --builder multiarch0 --platform linux/arm64  --build-arg BASE_IMAGE="$BASE_IMG" --build-arg GOARCH="arm64" -t "${IMAGE}:${IMAGE_TAG}-arm64" .

# Create and push multi-arch manifest
docker --config="$DOCKER_CONF" manifest create "${IMAGE}:${IMAGE_TAG}" \
    "${IMAGE}:${IMAGE_TAG}-amd64" \
    "${IMAGE}:${IMAGE_TAG}-arm64"

docker --config="$DOCKER_CONF" manifest push "${IMAGE}:${IMAGE_TAG}-multiarch"




docker build -t $CONTAINER_NAME -f build/Dockerfile.pr .

docker run -i --name $CONTAINER_NAME-run -v $PWD:/workspace:ro $CONTAINER_NAME /workspace/build/pr_check_inner.sh

TEST_RESULT=$?

mkdir -p artifacts

docker cp $CONTAINER_NAME-run:/container_workspace/artifacts/ $PWD

docker rm -f $CONTAINER_NAME
docker rm -f $CONTAINER_NAME-run

exit $TEST_RESULT
