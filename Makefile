.PHONY: build test clean install

build:
	@mkdir -p bin
	go build -o bin/archai ./cmd/archai

install:
	go install ./cmd/archai

test:
	go test ./...

clean:
	rm -rf bin/
