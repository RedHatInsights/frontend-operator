#!/bin/bash
set -xv

# Check if binfmt_misc support is available
if [[ -e /proc/sys/fs/binfmt_misc ]]; then
    echo "binfmt_misc support is available."
else
    echo "binfmt_misc is not supported on this system."
    exit 1
fi

# List out all the registered interpreters in binfmt_misc
echo "Registered interpreters:"
for f in /proc/sys/fs/binfmt_misc/*; do
    if [[ $(basename $f) != "register" && $(basename $f) != "status" ]]; then
        echo "------"
        cat $f
    fi
done
