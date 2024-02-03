# Pseudo variables
BUILD_TYPE="alpha"
VERSION="v1.0.0"
COMMIT="b2f0696cad918fb61420a6aff173eb36662b406e"
OUTPUT="bin/warp-dev"

go build -ldflags="-s -w -X main.BuildType=$BUILD_TYPE -X main.version=$VERSION -X main.commit=$COMMIT -X main.date=2023-08-07T12:49:48Z" -o=$OUTPUT .