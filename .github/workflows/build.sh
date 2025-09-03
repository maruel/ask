#!/bin/bash
# Copyright 2025 Marc-Antoine Ruel. All rights reserved.
# Use of this source code is governed under the Apache License, Version 2.0
# that can be found in the LICENSE file.

# This script builds all executables for the requested platform.

set -eu
cd "$(dirname "$0")/../.."

if [ ! $# -eq 2 ]; then
	echo "Usage: $0 <GOOS> <GOARCH>"
	exit 1
fi

export CGO_ENABLED=0
export GOOS=$1
export GOARCH=$2
SUFFIX=
if [ "$GOOS" == "windows" ]; then
	SUFFIX=".exe"
fi

mkdir -p build
PLATFORM_DIR="build/${GOOS}-${GOARCH}"
mkdir -p "$PLATFORM_DIR"
while IFS= read -r BINARY; do
	echo "- ${BINARY}"
	go build -trimpath -ldflags="-s -w" -o "${PLATFORM_DIR}/${BINARY}${SUFFIX}" "./cmd/$BINARY"
done <<< "$(find cmd -maxdepth 1 -mindepth 1 -type d -exec basename {} \; | sort)"
