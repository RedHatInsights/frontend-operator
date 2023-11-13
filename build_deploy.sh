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


docker buildx use multiarchbuilder

docker login -u="$QUAY_USER" -p="$QUAY_TOKEN" quay.io
docker login -u="$RH_REGISTRY_USER" -p="$RH_REGISTRY_TOKEN" registry.redhat.io

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
    docker buildx build  --platform linux/amd64,linux/arm64 -f Dockerfile.base -t "${BASE_IMG}" --push . 
fi
#### End 


docker buildx  build  --platform linux/amd64,linux/arm64  --build-arg BASE_IMAGE="${BASE_IMG}" --build-arg GOARCH="amd64" -t "${IMAGE}:${IMAGE_TAG}" --push .
