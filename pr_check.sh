#!/bin/bash

set -exv

mkdir -p "$PWD/.docker"

CONTAINER_NAME="${FEO_CONTAINER_NAME:-frontend-operator-pr-check-$ghprbPullId}"
docker rm -f $CONTAINER_NAME
docker rm -f $CONTAINER_NAME-run


# We're mounting the jenkins workspace over the root of the container
# This means that the pr_check_inner.sh script will be run in the context of the jenkins workspace
# This confused me for a while because pr_check_inner.sh is also copied into the pr check container at build time
# but the template_check.sh isn't. I couldn't figure out how it was sourcing it
echo true
docker buildx inspect --bootstrap
docker buildx ls
#docker buildx rm feo-builder || true
#docker buildx create --name feo-builder --use --bootstrap --driver docker-container --driver-opt image=quay.io/domino/buildkit:v0.12.0
#docker buildx build --platform linux/amd64,linux/arm64 -t $CONTAINER_NAME -f build/Dockerfile.pr .
#docker buildx rm feo-builder

#docker run -i --name $CONTAINER_NAME-run -v $PWD:/workspace:ro $CONTAINER_NAME /workspace/build/pr_check_inner.sh

true
TEST_RESULT=$?

#mkdir -p artifacts

#docker cp $CONTAINER_NAME-run:/container_workspace/artifacts/ $PWD

#docker rm -f $CONTAINER_NAME
#docker rm -f $CONTAINER_NAME-run

exit $TEST_RESULT
