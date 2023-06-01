#!/bin/sh

set -e

apt -qq update \
    && apt -qq install curl wget -y \
    && /usr/warpdl/cli/tests/scripts/universal-script.sh
