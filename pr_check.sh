#!/bin/bash
set -xv

echo "### Checking Docker Info ###"
docker info | grep -E 'Architecture|Experimental'

echo
echo "### Checking Docker Version ###"
docker --version

echo
echo "### Checking for Registered Interpreters ###"
if [ -d "/proc/sys/fs/binfmt_misc/" ]; then
    for f in /proc/sys/fs/binfmt_misc/*; do
        if [[ $(basename $f) != "register" && $(basename $f) != "status" ]]; then
            echo "------"
            cat $f || echo "Cannot read $f"
        fi
    done
else
    echo "binfmt_misc is not accessible or not present."
fi

echo
echo "### Checking for QEMU binary ###"
if command -v qemu-system-arm > /dev/null; then
    echo "qemu-system-arm is installed"
else
    echo "qemu-system-arm is not installed"
fi

echo
echo "### Checking for Docker QEMU Image ###"
if docker images | grep -q 'multiarch/qemu-user-static'; then
    echo "Docker image multiarch/qemu-user-static exists."
else
    echo "Docker image multiarch/qemu-user-static does not exist."
fi

echo
echo "### Checking for other common emulation tools ###"
# Check for some other tools (This list can be expanded)
tools=("box86" "exagear")
for tool in "${tools[@]}"; do
    if command -v "$tool" > /dev/null; then
        echo "$tool is installed"
    else
        echo "$tool is not installed"
    fi
done

echo
echo "### Checking current user's groups ###"
groups

echo
echo "### Build Test (ARM Hello World) ###"
echo "Trying to build ARM64 'hello-world' image..."
docker build --platform=linux/arm64 -t arm-build-test -f build/Dockerfile.pr . || echo "Failed to build ARM64 hello-world image."
