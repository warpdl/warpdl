#!/bin/sh

set -e

apt -qq update \
    && apt -qq install curl wget -y \
    && ./../scripts/universal-script.sh
