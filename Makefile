SHELL := /bin/zsh

PROJECT_NAME := tcloud-public-csi-driver
BINARY_NAME := tcloud-public-csi-driver
CMD_PATH := ./cmd/tcloud-public-csi-driver
BIN_DIR := ./bin
DIST_DIR := ./dist
GOCACHE_DIR := $(CURDIR)/.cache/go-build
IMAGE ?= ghcr.io/example/$(PROJECT_NAME):dev
KUSTOMIZE_DIR := ./deploy/kubernetes

export GOCACHE := $(GOCACHE_DIR)
export CGO_ENABLED ?= 0

.PHONY: help
help:
	@echo "Targets:"
	@echo "  make fmt              Format Go sources"
	@echo "  make test             Run unit tests"
	@echo "  make build            Build local binary"
	@echo "  make run              Run the CSI driver locally"
	@echo "  make tidy             Run go mod tidy"
	@echo "  make vet              Run go vet"
	@echo "  make lint             Run golangci-lint"
	@echo "  make check            Run fmt-check, vet, and test"
	@echo "  make image            Build container image"
	@echo "  make manifests        Print rendered Kubernetes manifests"
	@echo "  make install          Apply Kubernetes manifests"
	@echo "  make uninstall        Delete Kubernetes manifests"
	@echo "  make clean            Remove build artifacts and local caches"

.PHONY: dirs
dirs:
	@mkdir -p $(BIN_DIR) $(DIST_DIR) $(GOCACHE_DIR)

.PHONY: fmt
fmt:
	@go fmt ./...

.PHONY: fmt-check
fmt-check:
	@unformatted="$$(gofmt -l $$(find . -type f -name '*.go' -not -path './.cache/*'))"; \
	if [[ -n "$$unformatted" ]]; then \
		echo "Unformatted files:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

.PHONY: test
test: dirs
	@go test ./...

.PHONY: vet
vet: dirs
	@go vet ./...

.PHONY: lint
lint:
	@golangci-lint run ./...

.PHONY: build
build: dirs
	@go build -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PATH)

.PHONY: run
run: dirs
	@go run $(CMD_PATH)

.PHONY: tidy
tidy: dirs
	@go mod tidy

.PHONY: check
check: fmt-check vet test

.PHONY: image
image:
	@docker build -t $(IMAGE) .

.PHONY: manifests
manifests:
	@kubectl kustomize $(KUSTOMIZE_DIR)

.PHONY: install
install:
	@kubectl apply -k $(KUSTOMIZE_DIR)

.PHONY: uninstall
uninstall:
	@kubectl delete -k $(KUSTOMIZE_DIR)

.PHONY: clean
clean:
	@rm -rf $(BIN_DIR) $(DIST_DIR) ./.cache
