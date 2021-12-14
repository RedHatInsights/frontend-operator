#!/bin/bash

set -exv

# Note, this does not currently work with podman. pr_check_inner.sh has insufficient permissions
RUNTIME="docker"
DOCKER_CONF="$PWD/.docker"
mkdir -p "$DOCKER_CONF"

export IMAGE_TAG=`git rev-parse --short HEAD`
export IMAGE_NAME=quay.io/cloudservices/frontend-operator

CONTAINER_NAME="frontend-operator-pr-check-$ghprbPullId"
# NOTE: Make sure this volume is mounted 'ro', otherwise Jenkins cannot clean up the workspace due to file permission errors
set +e
# Run the pr check container (stored in the build dir) and invoke the
# pr_check_inner as its command
$RUNTIME run -i \
--name $CONTAINER_NAME \
-v $PWD:/workspace:ro \
quay.io/bholifie/frontend-op-pr-check:v0.0.6 \
/workspace/build/pr_check_inner.sh

TEST_RESULT=$?

mkdir -p artifacts

$RUNTIME cp $CONTAINER_NAME:/container_workspace/artifacts/ $PWD

$RUNTIME rm -f $CONTAINER_NAME
set -e

exit $TEST_RESULT