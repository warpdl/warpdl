all: build

build:
	go build -ldflags="-w -s" .

lint:
	golangci-lint run --timeout=5m ./...

goreleaser:
	goreleaser release --snapshot --skip-publish --clean
