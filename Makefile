.PHONY: build build-check test clean install install-check version web-build \
        archai-generate archai-baseline archai-check archai-smoke

# VERSION is stamped into the binary at build time via -ldflags. By
# default it is derived from `git describe` so unreleased builds show
# commit info; override with `make build VERSION=v1.2.3` for release
# builds.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.Version=$(VERSION)
ARCHAI ?= bin/archai
ARCHAI_CHECK ?= bin/archai-check
ARCHAI_PACKAGES ?= ./...
ARCHAI_TARGET ?= self-hosted
WEB_DIR := web

web-build:
	npm --prefix $(WEB_DIR) run build

build: web-build
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/archai ./cmd/archai

# The validation-only binary: architecture gates, nothing else. It links
# neither the review UI nor the diagram renderer, so it needs no web build
# and is ~6x smaller than archai. This is the one to ship to CI.
build-check:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o $(ARCHAI_CHECK) ./cmd/archai-check

install: web-build
	go install -ldflags "$(LDFLAGS)" ./cmd/archai

install-check:
	go install -ldflags "$(LDFLAGS)" ./cmd/archai-check

test: web-build
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

# The gate CI runs, run locally: overlay layer rules, dependency policy,
# and drift against the locked target. Uses the slim binary, so it does not
# drag in a web build.
archai-check: build-check
	$(ARCHAI_CHECK) all
	$(ARCHAI_CHECK) target --target $(ARCHAI_TARGET)

archai-smoke: build
	$(ARCHAI) version
	$(ARCHAI) list-daemons
	$(ARCHAI) extract . --out /tmp/archai-self-yaml
	$(ARCHAI) extract . --format json --out /tmp/archai-self-json
	$(ARCHAI) sequence internal/service.Service.Generate --depth 3

clean:
	rm -rf bin/
