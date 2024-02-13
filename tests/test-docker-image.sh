#!/bin/bash

set -e

echo "Testing docker image"
time docker run --rm ghcr.io/warpdl/warp-cli:latest version
