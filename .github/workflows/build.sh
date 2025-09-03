#!/bin/bash
# Copyright 2025 Marc-Antoine Ruel. All rights reserved.
# Use of this source code is governed under the Apache License, Version 2.0
# that can be found in the LICENSE file.

# This script builds all executables for the requested platform.

set -eu
cd "$(dirname "$0")/../.."
pwd

if [ ! $# -eq 3 ]; then
	echo "Usage: $0 <GOOS> <GOARCH>"
	exit 1
fi

EXECUTABLES=$(find cmd -maxdepth 1 -mindepth 1 -type d -exec basename {} \; | sort)
mkdir -p build
export CGO_ENABLED=0

export GOOS=$1
export GOARCH=$2
SUFFIX=
if [ "$GOOS" == "windows" ]; then
	SUFFIX=".exe"
fi

PLATFORM_DIR="build/${GOOS}-${GOARCH}"
echo "- $GOOS/$GOARCH"
mkdir -p "$PLATFORM_DIR"
while IFS= read -r BINARY; do
	go build -trimpath -ldflags="-s -w" -o "${PLATFORM_DIR}/${BINARY}${SUFFIX}" "./cmd/$BINARY"
done <<< "$EXECUTABLES"
