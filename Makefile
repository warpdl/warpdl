all: build

build:
	go build -ldflags="-w -s" .
