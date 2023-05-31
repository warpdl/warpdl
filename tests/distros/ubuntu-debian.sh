#!/bin/sh

set -e

apt update \
&& apt install curl wget -y \
&& (curl -Ls --tlsv1.2 --proto "=https" --retry 3 https://cli.warpdl.org/install.sh \
    || wget -t 3 -qO- https://cli.warpdl.org/install.sh) | sh
