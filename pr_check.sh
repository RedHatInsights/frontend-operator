#!/bin/bash

set -exv

mkdir -p "$PWD/.docker"

CONTAINER_NAME="${FEO_CONTAINER_NAME:-frontend-operator-pr-check-$ghprbPullId}"

# We're mounting the jenkins workspace over the root of the container
# This means that the pr_check_inner.sh script will be run in the context of the jenkins workspace
# This confused me for a while because pr_check_inner.sh is also copied into the pr check container at build time
# but the template_check.sh isn't. I couldn't figure out how it was sourcing it
#docker run -i --name $CONTAINER_NAME -v $PWD:/workspace:ro quay.io/bholifie/frontend-op-pr-check:v0.0.8 /workspace/build/pr_check_inner.sh

docker build -t $CONTAINER_NAME -f build/Dockerfile.pr 

TEST_RESULT=$?

mkdir -p artifacts

docker cp $CONTAINER_NAME:/container_workspace/artifacts/ $PWD

docker rm -f $CONTAINER_NAME

exit $TEST_RESULT
