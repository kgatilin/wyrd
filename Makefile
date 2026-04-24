.PHONY: build test clean install version

# VERSION is stamped into the binary at build time via -ldflags. By
# default it is derived from `git describe` so unreleased builds show
# commit info; override with `make build VERSION=v1.2.3` for release
# builds.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.Version=$(VERSION)

build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/archai ./cmd/archai

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/archai

test:
	go test ./...

version:
	@echo $(VERSION)

clean:
	rm -rf bin/
