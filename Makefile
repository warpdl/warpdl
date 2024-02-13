all: build

build:
	go build -ldflags="-w -s" .

goreleaser:
	goreleaser release --snapshot --skip-publish --clean
