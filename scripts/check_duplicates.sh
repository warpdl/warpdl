#!/bin/bash

# Run jscpd (JavaScript Copy/Paste Detector) for duplicate code detection
# This script is used locally and in CI to detect code duplication

set -e

# Check if jscpd is installed
if ! command -v jscpd &> /dev/null; then
    echo "jscpd is not installed. Installing..."
    if command -v npm &> /dev/null; then
        npm install -g jscpd
    else
        echo "Error: npm is required to install jscpd"
        exit 1
    fi
fi

# Run jscpd with the project configuration
echo "Running duplicate code detection..."
jscpd --exitCode 1 .
