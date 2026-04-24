.PHONY: build test clean install version archai-generate archai-baseline archai-check archai-smoke

# VERSION is stamped into the binary at build time via -ldflags. By
# default it is derived from `git describe` so unreleased builds show
# commit info; override with `make build VERSION=v1.2.3` for release
# builds.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.Version=$(VERSION)
ARCHAI ?= bin/archai
ARCHAI_PACKAGES ?= ./...
ARCHAI_TARGET ?= self-hosted

build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/archai ./cmd/archai

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/archai

test:
	go test ./...

version:
	@echo $(VERSION)

archai-generate: build
	$(ARCHAI) diagram generate $(ARCHAI_PACKAGES)
	$(ARCHAI) diagram generate $(ARCHAI_PACKAGES) --format yaml
	$(ARCHAI) diagram generate $(ARCHAI_PACKAGES) -o docs/architecture.d2
	$(ARCHAI) diagram compose $(ARCHAI_PACKAGES) --output docs/arch-composed.d2

archai-baseline: archai-generate
	$(ARCHAI) target lock $(ARCHAI_TARGET) --description "Self-hosted archai architecture baseline" --skip-generate
	$(ARCHAI) target use $(ARCHAI_TARGET)

archai-check: build
	$(ARCHAI) overlay check
	$(ARCHAI) diff --target $(ARCHAI_TARGET) --format json
	$(ARCHAI) validate --target $(ARCHAI_TARGET)

archai-smoke: build
	$(ARCHAI) version
	$(ARCHAI) list-daemons
	$(ARCHAI) extract . --out /tmp/archai-self-yaml
	$(ARCHAI) extract . --format json --out /tmp/archai-self-json
	$(ARCHAI) sequence internal/service.Service.Generate --depth 3

clean:
	rm -rf bin/
