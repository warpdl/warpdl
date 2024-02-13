#!/bin/sh

set -e

apt -qq update \
    && apt -qq install curl wget -y \
    && /app/scripts/universal-script.sh
