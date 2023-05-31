#!/bin/bash

set -e

# deb
echo "Testing ubuntu"
time docker run --rm -it -v "$(pwd)/tests/distros":/usr/warpdl/cli:ro ubuntu /usr/warpdl/cli/ubuntu-debian.sh
echo "Testing debian"
time docker run --rm -it -v "$(pwd)/tests/distros":/usr/warpdl/cli:ro debian /usr/warpdl/cli/ubuntu-debian.sh

# rpm
echo "Testing centos"
time docker run --rm -it -v "$(pwd)/tests/distros":/usr/warpdl/cli:ro centos /usr/warpdl/cli/fedora-centos.sh
echo "Testing fedora"
time docker run --rm -it -v "$(pwd)/tests/distros":/usr/warpdl/cli:ro fedora /usr/warpdl/cli/fedora-centos.sh

# apk
echo "Testing alpine"
time docker run --rm -it -v "$(pwd)/tests/distros":/usr/warpdl/cli:ro alpine /usr/warpdl/cli/alpine.sh
