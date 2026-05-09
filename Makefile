.PHONY: build build-all test clean install version \
        java-analyzer java-analyzer-build java-analyzer-test java-analyzer-clean \
        archai-generate archai-baseline archai-check archai-smoke

# VERSION is stamped into the binary at build time via -ldflags. By
# default it is derived from `git describe` so unreleased builds show
# commit info; override with `make build VERSION=v1.2.3` for release
# builds.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.Version=$(VERSION)
ARCHAI ?= bin/archai
ARCHAI_PACKAGES ?= ./...
ARCHAI_TARGET ?= self-hosted

# Java analyzer (issue #101): separate sub-project under tools/, only built
# explicitly via `make java-analyzer` or `make build-all`. Default `make
# build` stays Go-only — Go-only users don't need a JVM.
JAVA_ANALYZER_DIR := tools/archai-java-analyzer
JAVA_ANALYZER_JAR := $(JAVA_ANALYZER_DIR)/target/archai-java-analyzer.jar
# Sibling-binary distribution path: #102's default exec resolver looks here.
JAVA_ANALYZER_BIN_JAR := bin/archai-java-analyzer.jar
MVN ?= mvn

build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/archai ./cmd/archai

# build-all: Go binary + Java analyzer JAR copied next to bin/archai. Use this
# on releases that bundle the JAR alongside the binary; CI invokes it for
# tagged builds. The sibling-binary location matches #102's default resolver.
build-all: build java-analyzer
	@cp $(JAVA_ANALYZER_JAR) $(JAVA_ANALYZER_BIN_JAR)
	@echo "built: bin/archai + $(JAVA_ANALYZER_BIN_JAR)"

# java-analyzer: build + run the JAR's tests. Issue #101 acceptance requires
# this target builds AND tests; if you only want a fast build (skip tests),
# use `make java-analyzer-build`.
java-analyzer: java-analyzer-build java-analyzer-test
	@echo "built + tested: $(JAVA_ANALYZER_JAR)"

java-analyzer-build:
	$(MVN) -f $(JAVA_ANALYZER_DIR)/pom.xml -DskipTests package
	@echo "built: $(JAVA_ANALYZER_JAR)"

java-analyzer-test:
	$(MVN) -f $(JAVA_ANALYZER_DIR)/pom.xml test

java-analyzer-clean:
	$(MVN) -f $(JAVA_ANALYZER_DIR)/pom.xml clean

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
	rm -rf $(JAVA_ANALYZER_DIR)/target/
	rm -f $(JAVA_ANALYZER_BIN_JAR)
