#!/bin/bash

set -xv

# Check if binfmt_misc is supported (required for emulation/translation)
if [[ -d /proc/sys/fs/binfmt_misc ]]; then
    echo "binfmt_misc support is available."

    # Check for known interpreters in binfmt_misc
    for f in /proc/sys/fs/binfmt_misc/*; do
        if grep -q 'qemu-' "$f"; then
            echo "QEMU support detected for $(basename "$f")."
        elif grep -q 'box86' "$f"; then
            echo "Box86 support detected for $(basename "$f")."
        # Add other known emulators/interpreters as needed
        fi
    done

else
    echo "binfmt_misc is NOT supported on this machine."
fi

# Check if docker is installed
if command -v docker &> /dev/null; then
    echo "Docker is installed."

    # Using docker info to find any known runtimes for emulation
    if docker info 2>/dev/null | grep -q 'qemu'; then
        echo "QEMU is available as a runtime for Docker."
    fi

    # Checking Docker's default platform, which can be set for cross-compilation
    default_platform=$(docker info --format '{{.DefaultRuntime}}' 2>/dev/null)
    if [[ $default_platform != "runc" ]]; then
        echo "Docker's default platform is set to $default_platform."
    fi

else
    echo "Docker is NOT installed."
fi

# Checking for the presence of other emulation tools
if command -v box86 &> /dev/null; then
    echo "Box86 emulator is installed."
fi

# Add checks for other known emulation or translation tools as you become aware of them

