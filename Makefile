GO ?= go

.PHONY: all build test vet fmt-check buf-lint buf-gen check interop clean

all: build

build:
	$(GO) build -o bin/okf-proto ./cmd/okf-proto
	$(GO) build -o bin/protoc-gen-okf ./cmd/protoc-gen-okf

test:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt-check:
	@unformatted=$$(gofmt -l cmd pkg gen . 2>/dev/null || true); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed on:"; echo "$$unformatted"; exit 1; \
	else echo "gofmt: clean"; fi

buf-lint:
	buf lint

buf-gen:
	PATH="$$HOME/go/bin:$$PATH" buf generate

check: fmt-check vet buf-lint test

interop: build
	bash scripts/interop.sh

clean:
	rm -rf bin
