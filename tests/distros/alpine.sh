#!/bin/sh

set -e

apk add -q --no-cache curl wget \
    && /app/scripts/universal-script.sh
