#!/bin/bash

set -exv

IMAGE="quay.io/cloudservices/frontend-operator"
IMAGE_TAG=$(git rev-parse --short=7 HEAD)
# Generate a unique builder name using Jenkins environment variables
BUILDER_NAME="builder-${JOB_NAME}-${BUILD_ID}"

# Function to remove Docker builder
cleanup() {
  echo "Cleaning up Docker builder..."
  # Check if the specified builder exists and remove it if it does
  docker buildx inspect "$BUILDER_NAME" &>/dev/null && docker buildx rm "$BUILDER_NAME"
}

# Create a trap for different signals
# It will call the cleanup function on EXIT, or if the script receives
# a SIGINT (Ctrl+C), or a SIGTERM (termination signal)
trap cleanup EXIT SIGINT SIGTERM

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


### Start base image build and push
BASE_TAG=`cat go.mod go.sum Dockerfile.base | sha256sum  | head -c 8`
BASE_IMG=quay.io/cloudservices/frontend-operator-build-base:$BASE_TAG
RESPONSE=$( \
        curl -Ls -H "Authorization: Bearer $QUAY_TOKEN" \
        "https://quay.io/api/v1/repository/cloudservices/frontend-operator-build-base/tag/?specificTag=$BASE_TAG" \
    )
echo "received HTTP response: $RESPONSE"
# find all non-expired tags
VALID_TAGS_LENGTH=$(echo $RESPONSE | jq '[ .tags[] | select(.end_ts == null) ] | length')

if [[ "$VALID_TAGS_LENGTH" -eq 0 ]]; then
    docker --config="$DOCKER_CONF" build -f Dockerfile.base . -t "$BASE_IMG"
	docker --config="$DOCKER_CONF" push "$BASE_IMG"
fi
#### End 



# Create a new buildx builder with the unique name
docker buildx create --name "${BUILDER_NAME}" --use --driver docker-container --driver-opt image=quay.io/domino/buildkit:v0.12.3

# Initialize the builder
docker buildx inspect "${BUILDER_NAME}" --bootstrap

# Build and push the multi-architecture image
docker --config="$DOCKER_CONF" buildx build --builder "${BUILDER_NAME}" \
  --platform linux/amd64,linux/arm64 \
  --build-arg BASE_IMAGE="$BASE_IMG" \
  -t "${IMAGE}:${IMAGE_TAG}" --push .
