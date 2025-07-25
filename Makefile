# Source of reference: https://github.com/Azure/fleet/blob/main/Makefile
REGISTRY ?= ghcr.io/azure

ifndef TAG
	TAG ?= $(shell git rev-parse --short=7 HEAD)
endif
HUB_NET_CONTROLLER_MANAGER_IMAGE_VERSION ?= $(TAG)
MEMBER_NET_CONTROLLER_MANAGER_IMAGE_VERSION ?= $(TAG)
MCS_CONTROLLER_MANAGER_IMAGE_VERSION ?= $(TAG)

HUB_NET_CONTROLLER_MANAGER_IMAGE_NAME ?= hub-net-controller-manager
MEMBER_NET_CONTROLLER_MANAGER_IMAGE_NAME ?= member-net-controller-manager
MCS_CONTROLLER_MANAGER_IMAGE_NAME ?= mcs-controller-manager

# Directories
ROOT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
TOOLS_DIR := hack/tools
TOOLS_BIN_DIR := $(abspath $(TOOLS_DIR)/bin)

# Binaries
# Note: Need to use abspath so we can invoke these from subdirectories

CONTROLLER_GEN_VER := v0.16.0
CONTROLLER_GEN_BIN := controller-gen
CONTROLLER_GEN := $(abspath $(TOOLS_BIN_DIR)/$(CONTROLLER_GEN_BIN)-$(CONTROLLER_GEN_VER))

STATICCHECK_VER := 2023.1.7
STATICCHECK_BIN := staticcheck
STATICCHECK := $(abspath $(TOOLS_BIN_DIR)/$(STATICCHECK_BIN)-$(STATICCHECK_VER))

GOIMPORTS_VER := latest
GOIMPORTS_BIN := goimports
GOIMPORTS := $(abspath $(TOOLS_BIN_DIR)/$(GOIMPORTS_BIN)-$(GOIMPORTS_VER))

GOLANGCI_LINT_VER := v1.64.7
GOLANGCI_LINT_BIN := golangci-lint
GOLANGCI_LINT := $(abspath $(TOOLS_BIN_DIR)/$(GOLANGCI_LINT_BIN)-$(GOLANGCI_LINT_VER))

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.28.x
# ENVTEST_VER is the version of the ENVTEST binary
# Use a fixed version to avoid Go version conflicts.
ENVTEST_VER = v0.0.0-20240317073005-bd9ea79e8d18
ENVTEST_BIN := setup-envtest
ENVTEST :=  $(abspath $(TOOLS_BIN_DIR)/$(ENVTEST_BIN)-$(ENVTEST_VER))

# Scripts
GO_INSTALL := ./hack/go-install.sh

## --------------------------------------
## Tooling Binaries
## --------------------------------------

$(GOLANGCI_LINT):
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) github.com/golangci/golangci-lint/cmd/golangci-lint $(GOLANGCI_LINT_BIN) $(GOLANGCI_LINT_VER)

$(CONTROLLER_GEN):
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) sigs.k8s.io/controller-tools/cmd/controller-gen $(CONTROLLER_GEN_BIN) $(CONTROLLER_GEN_VER)

# Style checks
$(STATICCHECK):
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) honnef.co/go/tools/cmd/staticcheck $(STATICCHECK_BIN) $(STATICCHECK_VER)

# GOIMPORTS
$(GOIMPORTS):
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) golang.org/x/tools/cmd/goimports $(GOIMPORTS_BIN) $(GOIMPORTS_VER)

# ENVTEST
$(ENVTEST):
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) sigs.k8s.io/controller-runtime/tools/setup-envtest $(ENVTEST_BIN) $(ENVTEST_VER)

## --------------------------------------
## Linting
## --------------------------------------

.PHONY: lint
lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run -v

.PHONY: lint-full
lint-full: $(GOLANGCI_LINT) ## Run slower linters to detect possible issues
	$(GOLANGCI_LINT) run -v --fast=false

## --------------------------------------
## Development
## --------------------------------------

staticcheck: $(STATICCHECK)
	$(STATICCHECK) ./...

.PHONY: fmt
fmt:  $(GOIMPORTS) ## Run go fmt against code.
	go fmt ./...
	$(GOIMPORTS) -local go.goms.io/fleet-networking -w $$(go list -f {{.Dir}} ./...)

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

## --------------------------------------
## test
## --------------------------------------

.PHONY: test
test: manifests generate fmt vet local-unit-test integration-test

.PHONY: local-unit-test
local-unit-test: $(ENVTEST) ## Run tests.
	CGO_ENABLED=1 KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test ./pkg/... -race -coverprofile=coverage.xml -covermode=atomic -v

.PHONY: integration-test
integration-test: $(ENVTEST) ## Run integration tests.
	CGO_ENABLED=1 KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" \
	ginkgo -v -p --race --cover --coverpkg=./... ./test/apis/...

.PHONY: e2e-setup
e2e-setup:
	bash test/scripts/bootstrap.sh

.PHONY: e2e-tests
e2e-tests:
	go test -timeout 50m -tags=e2e -v ./test/e2e -args -ginkgo.v

.PHONY: e2e-collect-logs
e2e-collect-logs:
	bash test/scripts/collect-logs.sh
.PHONY: e2e-cleanup
e2e-cleanup:
	bash test/scripts/cleanup.sh

reviewable: fmt vet lint staticcheck
	go mod tidy

## --------------------------------------
## Code Generation
## --------------------------------------

# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:crdVersions=v1"

# Generate manifests e.g. CRD, RBAC etc.
.PHONY: manifests
manifests: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) \
		$(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Generate code
generate: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) \
		object:headerFile="hack/boilerplate.go.txt" paths="./..."

## --------------------------------------
## Build
## --------------------------------------

.PHONY: build
build: generate fmt vet ## Build binaries.
	go build -o bin/hub-net-controller-manager cmd/hub-net-controller-manager/main.go
	go build -o bin/member-net-controller-manager cmd/member-net-controller-manager/main.go
	go build -o bin/mcs-controller-manager cmd/mcs-controller-manager/main.go

.PHONY: run-hub-net-controller-manager
run-hub-net-controller-manager: manifests generate fmt vet ## Run a controllers from your host.
	go run ./cmd/hub-net-controller-manager/main.go

.PHONY: run-member-net-controller-manager
run-member-net-controller-manager: manifests generate fmt vet ## Run a controllers from your host.
	go run ./cmd/member-net-controller-manager/main.go

.PHONY: run-mcs-controller-manager
run-mcs-controller-manager: manifests generate fmt vet ## Run a controllers from your host.
	go run ./cmd/mcs-controller-manager/main.go

## --------------------------------------
## Images
## --------------------------------------

OUTPUT_TYPE ?= type=registry
BUILDX_BUILDER_NAME ?= img-builder
QEMU_VERSION ?= 7.2.0-1
BUILDKIT_VERSION ?= v0.18.1

.PHONY: vendor
vendor:
	go mod tidy && go mod vendor

.PHONY: image
image:
	$(MAKE) OUTPUT_TYPE="type=docker" docker-build-hub-net-controller-manager docker-build-member-net-controller-manager docker-build-mcs-controller-manager

.PHONY: push
push:
	$(MAKE) OUTPUT_TYPE="type=registry" docker-build-hub-net-controller-manager docker-build-member-net-controller-manager docker-build-mcs-controller-manager

# By default, docker buildx create will pull image moby/buildkit:buildx-stable-1 and hit the too many requests error.
.PHONY: docker-buildx-builder
docker-buildx-builder:
	@if ! docker buildx ls | grep $(BUILDX_BUILDER_NAME); then \
		docker run --rm --privileged mcr.microsoft.com/mirror/docker/multiarch/qemu-user-static:$(QEMU_VERSION) --reset -p yes; \
		docker buildx create --driver-opt image=mcr.microsoft.com/oss/v2/moby/buildkit:$(BUILDKIT_VERSION) --name $(BUILDX_BUILDER_NAME) --use; \
		docker buildx inspect $(BUILDX_BUILDER_NAME) --bootstrap; \
	fi

.PHONY: docker-build-hub-net-controller-manager
docker-build-hub-net-controller-manager: docker-buildx-builder vendor
	docker buildx build \
		--file docker/$(HUB_NET_CONTROLLER_MANAGER_IMAGE_NAME).Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/amd64" \
		--pull \
		--tag $(REGISTRY)/$(HUB_NET_CONTROLLER_MANAGER_IMAGE_NAME):$(HUB_NET_CONTROLLER_MANAGER_IMAGE_VERSION) .

.PHONY: docker-build-member-net-controller-manager
docker-build-member-net-controller-manager: docker-buildx-builder vendor
	docker buildx build \
		--file docker/$(MEMBER_NET_CONTROLLER_MANAGER_IMAGE_NAME).Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/amd64" \
		--pull \
		--tag $(REGISTRY)/$(MEMBER_NET_CONTROLLER_MANAGER_IMAGE_NAME):$(MEMBER_NET_CONTROLLER_MANAGER_IMAGE_VERSION) .

.PHONY: docker-build-mcs-controller-manager
docker-build-mcs-controller-manager: docker-buildx-builder vendor
	docker buildx build \
		--file docker/$(MCS_CONTROLLER_MANAGER_IMAGE_NAME).Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/amd64" \
		--pull \
		--tag $(REGISTRY)/$(MCS_CONTROLLER_MANAGER_IMAGE_NAME):$(MCS_CONTROLLER_MANAGER_IMAGE_VERSION) .

## -----------------------------------
## Cleanup
## -----------------------------------

.PHONY: clean-bin
clean-bin: ## Remove all generated binaries
	rm -rf $(TOOLS_BIN_DIR)
	rm -rf ./bin
