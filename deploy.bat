:: Pseudo variables
go build -ldflags="-s -w -X main.BuildType=alpha -X main.version=v0.0.48 -X main.commit=b2f0696cad918fb61420a6aff173eb36662b406e -X main.date=2023-08-07T12:49:48Z" -o="bin/warp-dev.exe" .