#!/bin/bash

set -exv

cd "$HOME"

# copy the workspace from the Jenkins job off the ro volume into this container
mkdir container_workspace
cd container_workspace

cp -r /workspace/. .

mkdir bin
cp /root/go/* bin/

mkdir -p artifacts

# Clear Go module cache to force fresh package downloads
go clean -modcache

source build/template_check.sh
make junit
