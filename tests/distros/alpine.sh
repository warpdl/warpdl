#!/bin/sh

set -e

apk add -q --no-cache curl wget \
    && /usr/warpdl/cli/tests/scripts/universal-script.sh
