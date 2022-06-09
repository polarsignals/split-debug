VERSION ?= $(shell git describe --exact-match --tags $$(git log -n1 --pretty='%h') 2>/dev/null || echo "$$(git rev-parse --abbrev-ref HEAD)-$$(git rev-parse --short HEAD)")
CONTAINER_IMAGE := ghcr.io/polarsignals/split-debug:$(VERSION)

LDFLAGS="-X main.version=$(VERSION)"

OUT_DIR ?= dist
OUT_BIN := $(OUT_DIR)/split-debug

$(OUT_DIR):
	mkdir -p $@

.PHONY: build
build: $(OUT_BIN)

.PHONY: clean
clean:
	-rm -rf dist/*
	-rm split-debug-*

$(OUT_BIN): deps go.sum main.go pkg/**/*
	CGO_ENABLED=0 go build -trimpath -ldflags=$(LDFLAGS) -o $@ main.go

.PHONY: dev/setup
dev/setup:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install mvdan.cc/gofumpt@latest
	go install github.com/campoy/embedmd@latest
	go install github.com/goreleaser/goreleaser@latest

.PHONY: deps
deps:
	go mod tidy

.PHONY: lint
lint: dev/setup
	golangci-lint run
	goreleaser check

.PHONY: format
format: dev/setup
	gofumpt -l -w .
	go fmt $(shell go list ./...)

.PHONY: test
test: build
	go test -v $(shell go list ./...)

.PHONY: container
container:
	docker build -t $(CONTAINER_IMAGE) .

.PHONY: push-container
push-container:
	docker push $(CONTAINER_IMAGE)

$(OUT_DIR)/help.txt: $(OUT_BIN)
	$(OUT_BIN) --help > $@

README.md: $(OUT_DIR)/help.txt  dev/setup
	embedmd -w README.md
