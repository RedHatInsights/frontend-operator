#!/bin/bash
set -xv

# Check if binfmt_misc support is available
if [[ -e /proc/sys/fs/binfmt_misc ]]; then
    echo "binfmt_misc support is available."
else
    echo "binfmt_misc is not supported on this system."
    exit 1
fi

# Check for various interpreters
interpreters=("qemu-aarch64" "qemu-arm" "qemu-ppc64le" "qemu-s390x" "box86")
for interp in "${interpreters[@]}"; do
    if grep -q "$interp" /proc/sys/fs/binfmt_misc/status; then
        echo "$interp is enabled."
    else
        echo "$interp is not found or not enabled."
    fi
done
