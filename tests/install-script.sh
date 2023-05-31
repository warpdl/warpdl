#!/bin/bash

set -e

# deb
echo "Testing ubuntu"
time docker run --rm -it ubuntu (curl -Ls --tlsv1.2 --proto "=https" --retry 3 https://cli.warpdl.org/install.sh || wget -t 3 -qO- https://cli.warpdl.org/install.sh) | sh
echo "Testing debian"
time docker run --rm -it debian (curl -Ls --tlsv1.2 --proto "=https" --retry 3 https://cli.warpdl.org/install.sh || wget -t 3 -qO- https://cli.warpdl.org/install.sh) | sh

# rpm
echo "Testing centos"
time docker run --rm -it centos (curl -Ls --tlsv1.2 --proto "=https" --retry 3 https://cli.warpdl.org/install.sh || wget -t 3 -qO- https://cli.warpdl.org/install.sh) | sh
echo "Testing fedora"
time docker run --rm -it fedora (curl -Ls --tlsv1.2 --proto "=https" --retry 3 https://cli.warpdl.org/install.sh || wget -t 3 -qO- https://cli.warpdl.org/install.sh) | sh

# apk
echo "Testing alpine"
time docker run --rm -it alpine (curl -Ls --tlsv1.2 --proto "=https" --retry 3 https://cli.warpdl.org/install.sh || wget -t 3 -qO- https://cli.warpdl.org/install.sh) | sh
