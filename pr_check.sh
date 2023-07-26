#!/bin/bash

set -exv

mkdir -p "$PWD/.docker"

CONTAINER_NAME="${FEO_CONTAINER_NAME:-frontend-operator-pr-check-$ghprbPullId}"

docker run -i --name $CONTAINER_NAME -v $PWD:/workspace:ro quay.io/bholifie/frontend-op-pr-check:v0.0.8 /workspace/build/pr_check_inner.sh

TEST_RESULT=$?

mkdir -p artifacts

docker cp $CONTAINER_NAME:/container_workspace/artifacts/ $PWD

docker rm -f $CONTAINER_NAME

exit $TEST_RESULT
