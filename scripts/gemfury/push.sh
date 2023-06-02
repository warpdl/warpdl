#!/bin/bash

set -euo pipefail

# helper function to upload a package to gemfury
pushToGemfury() {
    type=$1
    pkgs_list=($(echo "$(find dist/*.$type -type f)" | tr ' ' '\n'))
    echo "Uploading $type packages"
    for package in "${pkgs_list[@]}"
    do
        echo "Uploading $package"
        curl -F "package=@$package" "https://${GEMFURY_PUSH_KEY}@push.fury.io/warpdl/"
    done
}


pushToGemfury deb
pushToGemfury rpm

echo "Uploaded all packages to Gemfury"
