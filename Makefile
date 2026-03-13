.PHONY: build test lint vet fmt clean docker

BINARY := gh-plane-sync
MODULE := github.com/mggarofalo/gh-plane-sync
VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

build:
	go build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/gh-plane-sync

test:
	go test ./... -v -race -count=1

lint:
	golangci-lint run

vet:
	go vet ./...

fmt:
	gofumpt -w .

clean:
	rm -f $(BINARY)

docker:
	docker build -t $(BINARY):$(VERSION) .
