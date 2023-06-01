#!/bin/sh

set -e

apk add -q --no-cache curl wget \
    && ./../scripts/universal-script.sh
