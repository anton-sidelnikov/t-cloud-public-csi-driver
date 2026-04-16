SHELL := /bin/bash

PROJECT_NAME := t-cloud-public-csi-driver
PROJECT_OWNER := anton-sidelnikov
BINARY_NAME := tcloud-public-csi-driver
CMD_PATH := ./cmd/tcloud-public-csi-driver
BIN_DIR := ./bin
DIST_DIR := ./dist
GOCACHE_DIR := $(CURDIR)/.cache/go-build
GOLANGCI_LINT_CACHE_DIR := $(CURDIR)/.cache/golangci-lint
IMAGE ?= ghcr.io/$(PROJECT_OWNER)/$(PROJECT_NAME):dev
FUNCTIONAL_IMAGE ?= ghcr.io/$(PROJECT_OWNER)/$(PROJECT_NAME):e2e-$(COMMIT)
#FUNCTIONAL_IMAGE ?= ghcr.io/anton-sidelnikov/t-cloud-public-csi-driver:latest
KUSTOMIZE_DIR := ./deploy/kubernetes
FUNCTIONAL_TF_DIR := ./test/functional/terraform
FUNCTIONAL_CACHE_DIR := $(CURDIR)/.cache/functional
FUNCTIONAL_KUBECONFIG := $(FUNCTIONAL_CACHE_DIR)/kubeconfig
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILDX ?= $(shell if docker buildx version >/dev/null 2>&1; then echo "docker buildx build"; elif command -v docker-buildx >/dev/null 2>&1; then echo "docker-buildx build"; else echo "docker buildx build"; fi)
VERSION_PACKAGE := t-cloud-public-csi-driver/internal/version
LDFLAGS := -X $(VERSION_PACKAGE).Version=$(VERSION) -X $(VERSION_PACKAGE).Commit=$(COMMIT) -X $(VERSION_PACKAGE).Date=$(BUILD_DATE)
FUNCTIONAL_TF_ENV := \
	TF_VAR_auth_url="$${TF_VAR_auth_url:-$${OS_AUTH_URL}}" \
	TF_VAR_region="$${TF_VAR_region:-$${OS_REGION}}" \
	TF_VAR_availability_zone="$${TF_VAR_availability_zone:-$${OS_AVAILABILITY_ZONE}}" \
	TF_VAR_domain_name="$${TF_VAR_domain_name:-$${OS_DOMAIN_NAME}}" \
	TF_VAR_user_name="$${TF_VAR_user_name:-$${OS_USERNAME}}" \
	TF_VAR_password="$${TF_VAR_password:-$${OS_PASSWORD}}" \
	TF_VAR_project_id="$${TF_VAR_project_id:-$${OS_PROJECT_ID}}" \
	TF_VAR_project_name="$${TF_VAR_project_name:-$${OS_PROJECT_NAME}}"

export GOCACHE := $(GOCACHE_DIR)
export GOLANGCI_LINT_CACHE := $(GOLANGCI_LINT_CACHE_DIR)
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
	@echo "  make functional-image       Build functional-test image from current checkout"
	@echo "  make functional-image-push  Build and push functional-test image"
	@echo "  make functional-infra-init  Initialize functional-test Terraform"
	@echo "  make functional-infra-plan  Plan functional-test infrastructure"
	@echo "  make functional-infra-up    Provision functional-test infrastructure"
	@echo "  make functional-kubeconfig  Write functional-test kubeconfig"
	@echo "  make test-functional        Run functional tests"
	@echo "  make functional-infra-down  Destroy functional-test infrastructure"
	@echo "  make manifests        Print rendered Kubernetes manifests"
	@echo "  make install          Apply Kubernetes manifests"
	@echo "  make uninstall        Delete Kubernetes manifests"
	@echo "  make clean            Remove build artifacts and local caches"

.PHONY: dirs
dirs:
	@mkdir -p $(BIN_DIR) $(DIST_DIR) $(GOCACHE_DIR) $(GOLANGCI_LINT_CACHE_DIR) $(FUNCTIONAL_CACHE_DIR)

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
lint: dirs
	@golangci-lint run ./...

.PHONY: build
build: dirs
	@go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PATH)

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
	@$(BUILDX) --load \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(IMAGE) .

.PHONY: functional-image
functional-image:
	@$(BUILDX) --load \
		--build-arg VERSION=e2e-$(COMMIT) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(FUNCTIONAL_IMAGE) .

.PHONY: functional-image-push
functional-image-push:
	@$(BUILDX) --push \
		--build-arg VERSION=e2e-$(COMMIT) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(FUNCTIONAL_IMAGE) .

.PHONY: functional-infra-init
functional-infra-init:
	@$(FUNCTIONAL_TF_ENV) terraform -chdir=$(FUNCTIONAL_TF_DIR) init

.PHONY: functional-infra-plan
functional-infra-plan:
	@$(FUNCTIONAL_TF_ENV) terraform -chdir=$(FUNCTIONAL_TF_DIR) plan

.PHONY: functional-infra-up
functional-infra-up:
	@$(FUNCTIONAL_TF_ENV) terraform -chdir=$(FUNCTIONAL_TF_DIR) apply -auto-approve

.PHONY: functional-kubeconfig
functional-kubeconfig: dirs
	@$(FUNCTIONAL_TF_ENV) terraform -chdir=$(FUNCTIONAL_TF_DIR) output -raw kubeconfig > $(FUNCTIONAL_KUBECONFIG)
	@chmod 0600 $(FUNCTIONAL_KUBECONFIG)
	@echo "$(FUNCTIONAL_KUBECONFIG)"

.PHONY: test-functional
test-functional:
	@KUBECONFIG=$${KUBECONFIG:-$(FUNCTIONAL_KUBECONFIG)} CSI_TEST_IMAGE=$${CSI_TEST_IMAGE:-$(FUNCTIONAL_IMAGE)} go test -tags=functional ./test/functional/evs -v -timeout 90m

.PHONY: functional-infra-down
functional-infra-down:
	@$(FUNCTIONAL_TF_ENV) terraform -chdir=$(FUNCTIONAL_TF_DIR) destroy -auto-approve

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
