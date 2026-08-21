.PHONY: build build-check test clean install install-check version web-build \
        wyrd-generate wyrd-baseline wyrd-check wyrd-smoke

# VERSION is stamped into the binary at build time via -ldflags. By
# default it is derived from `git describe` so unreleased builds show
# commit info; override with `make build VERSION=v1.2.3` for release
# builds.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.Version=$(VERSION)
WYRD ?= bin/wyrd
WYRD_CHECK ?= bin/wyrd-check
WYRD_PACKAGES ?= ./...
WYRD_TARGET ?= self-hosted
WEB_DIR := web

web-build:
	npm --prefix $(WEB_DIR) run build

build: web-build
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/wyrd ./cmd/wyrd

# The validation-only binary: architecture gates, nothing else. It links
# neither the review UI nor the diagram renderer, so it needs no web build
# and is ~6x smaller than wyrd. This is the one to ship to CI.
build-check:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o $(WYRD_CHECK) ./cmd/wyrd-check

install: web-build
	go install -ldflags "$(LDFLAGS)" ./cmd/wyrd

install-check:
	go install -ldflags "$(LDFLAGS)" ./cmd/wyrd-check

test: web-build
	go test ./...

version:
	@echo $(VERSION)

wyrd-generate: build
	$(WYRD) diagram generate $(WYRD_PACKAGES)
	$(WYRD) diagram generate $(WYRD_PACKAGES) --format yaml
	$(WYRD) diagram generate $(WYRD_PACKAGES) -o docs/architecture.d2
	$(WYRD) diagram compose $(WYRD_PACKAGES) --output docs/arch-composed.d2

wyrd-baseline: wyrd-generate
	$(WYRD) target lock $(WYRD_TARGET) --description "Self-hosted wyrd architecture baseline" --skip-generate
	$(WYRD) target use $(WYRD_TARGET)

# The gate CI runs, run locally: overlay layer rules, dependency policy,
# and drift against the locked target. Uses the slim binary, so it does not
# drag in a web build.
wyrd-check: build-check
	$(WYRD_CHECK) all
	$(WYRD_CHECK) target --target $(WYRD_TARGET)

wyrd-smoke: build
	$(WYRD) version
	$(WYRD) list-daemons
	$(WYRD) extract . --out /tmp/wyrd-self-yaml
	$(WYRD) extract . --format json --out /tmp/wyrd-self-json
	$(WYRD) sequence internal/service.Service.Generate --depth 3

clean:
	rm -rf bin/
