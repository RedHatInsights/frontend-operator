#!/bin/bash

set -exv

# copy the workspace from the Jenkins job off the ro volume into this container
mkdir -p /container_workspace
cp -r /workspace/. /container_workspace
cd /container_workspace

mkdir -p /container_workspace/bin
cp /root/go/* /container_workspace/bin

mkdir -p artifacts

source build/template_check.sh
make junit
