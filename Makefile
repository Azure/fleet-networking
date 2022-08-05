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

# Kind cluster
KIND_IMAGE ?= kindest/node:v1.23.3
HUB_KIND_CLUSTER_NAME = hub-testing
MEMBER_KIND_CLUSTER_NAME = member-testing
CLUSTER_CONFIG := $(abspath test/e2e/kind-config.yaml)
KUBECONFIG ?= $(HOME)/.kube/config

# Directories
ROOT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
TOOLS_DIR := hack/tools
TOOLS_BIN_DIR := $(abspath $(TOOLS_DIR)/bin)

# Binaries
# Note: Need to use abspath so we can invoke these from subdirectories

CONTROLLER_GEN_VER := v0.7.0
CONTROLLER_GEN_BIN := controller-gen
CONTROLLER_GEN := $(abspath $(TOOLS_BIN_DIR)/$(CONTROLLER_GEN_BIN)-$(CONTROLLER_GEN_VER))

STATICCHECK_VER := 2022.1
STATICCHECK_BIN := staticcheck
STATICCHECK := $(abspath $(TOOLS_BIN_DIR)/$(STATICCHECK_BIN)-$(STATICCHECK_VER))

GOIMPORTS_VER := latest
GOIMPORTS_BIN := goimports
GOIMPORTS := $(abspath $(TOOLS_BIN_DIR)/$(GOIMPORTS_BIN)-$(GOIMPORTS_VER))

GOLANGCI_LINT_VER := v1.46.2
GOLANGCI_LINT_BIN := golangci-lint
GOLANGCI_LINT := $(abspath $(TOOLS_BIN_DIR)/$(GOLANGCI_LINT_BIN)-$(GOLANGCI_LINT_VER))

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VER = v0.0.0-20211110210527-619e6b92dab9
ENVTEST_K8S_BIN := setup-envtest
ENVTEST :=  $(abspath $(TOOLS_BIN_DIR)/$(ENVTEST_K8S_BIN)-$(ENVTEST_K8S_VER))

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
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) sigs.k8s.io/controller-runtime/tools/setup-envtest $(ENVTEST_K8S_BIN) $(ENVTEST_K8S_VER)

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

.PHONY: reviewable # run reviewable to local basic test and code format.
reviewable: fmt vet lint staticcheck
	go mod tidy

## --------------------------------------
## Kind
## --------------------------------------

create-hub-kind-cluster:
	kind create cluster --name $(HUB_KIND_CLUSTER_NAME) --image=$(KIND_IMAGE) --config=$(CLUSTER_CONFIG) --kubeconfig=$(KUBECONFIG)

create-member-kind-cluster:
	kind create cluster --name $(MEMBER_KIND_CLUSTER_NAME) --image=$(KIND_IMAGE) --config=$(CLUSTER_CONFIG) --kubeconfig=$(KUBECONFIG)

load-hub-docker-image:
	kind load docker-image  --name $(HUB_KIND_CLUSTER_NAME) $(REGISTRY)/$(HUB_NET_CONTROLLER_MANAGER_IMAGE_NAME):$(HUB_NET_CONTROLLER_MANAGER_IMAGE_VERSION)

load-member-docker-image:
	kind load docker-image  --name $(MEMBER_KIND_CLUSTER_NAME) \
		$(REGISTRY)/$(MEMBER_NET_CONTROLLER_MANAGER_IMAGE_NAME):$(MEMBER_NET_CONTROLLER_MANAGER_IMAGE_VERSION) \
		$(REGISTRY)/$(MCS_CONTROLLER_MANAGER_IMAGE_NAME):$(MCS_CONTROLLER_MANAGER_IMAGE_VERSION)

## --------------------------------------
## test
## --------------------------------------

.PHONY: test
test: manifests generate fmt vet local-unit-test

.PHONY: local-unit-test
local-unit-test: $(ENVTEST) ## Run tests.
	CGO_ENABLED=1 KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test ./pkg/... -race -coverprofile=coverage.xml -covermode=atomic -v

install-hub-controller-manager-helm:
	kind export kubeconfig --name $(HUB_KIND_CLUSTER_NAME)
	kubectl apply -f ./config/crd/*
	helm install hub-net-controller-manager ./charts/hub-net-controller-manager/ \
		--set image.pullPolicy=Never \
		--set image.repository=$(REGISTRY)/$(HUB_NET_CONTROLLER_MANAGER_IMAGE_NAME) \
		--set image.tag=$(HUB_NET_CONTROLLER_MANAGER_IMAGE_VERSION) \
		--set secretTokenForMember.enabled=true \
		--namespace fleet-system --create-namespace

.PHONY: e2e-hub-kubeconfig-secret
e2e-hub-kubeconfig-secret:
	kind export kubeconfig --name $(HUB_KIND_CLUSTER_NAME); \
	TOKEN=$$(kubectl get secret hub-kubeconfig-secret -n fleet-system -o jsonpath='{.data.token}' | base64 -d); \
	kind export kubeconfig --name $(MEMBER_KIND_CLUSTER_NAME); \
	kubectl create namespace fleet-system; \
	kubectl delete secret hub-kubeconfig-secret -n fleet-system --ignore-not-found; \
	kubectl create secret generic hub-kubeconfig-secret -n fleet-system --from-literal=kubeconfig=$$TOKEN

install-member-controller-manager-helm: e2e-hub-kubeconfig-secret
	kind export kubeconfig --name $(HUB_KIND_CLUSTER_NAME); \
	# Create member cluster namesapce in hub cluster.; \
	MEMBER_CLUSTER_NAME="kind-$(MEMBER_KIND_CLUSTER_NAME)"; \
	HUB_MEMBER_NAMESAPCE="fleet-member-$$MEMBER_CLUSTER_NAME"; \
	kubectl get namespace $$HUB_MEMBER_NAMESAPCE || kubectl create namespace $$HUB_MEMBER_NAMESAPCE; \
	# Get kind cluster IP that docker uses internally so we can talk to the other cluster. the port is the default one.; \
	HUB_SERVER_URL="https://$$(docker inspect $(HUB_KIND_CLUSTER_NAME)-control-plane --format='{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'):6443"; \
	kind export kubeconfig --name $(MEMBER_KIND_CLUSTER_NAME); \
	kubectl apply -f ./config/crd/*; \
	helm install member-net-controller-manager ./charts/member-net-controller-manager/ \
		--set config.hubURL=$$HUB_SERVER_URL \
		--set image.repository=$(REGISTRY)/$(MEMBER_NET_CONTROLLER_MANAGER_IMAGE_NAME) \
		--set image.tag=$(MEMBER_NET_CONTROLLER_MANAGER_IMAGE_VERSION) \
		--set image.pullPolicy=Never \
		--set refreshtoken.repository=ghcr.io/azure/fleet/refresh-token \
		--set refreshtoken.tag=v0.1.0 \
		--set config.memberClusterName=$$MEMBER_CLUSTER_NAME \
		--set secret.name=hub-kubeconfig-secret \
		--set secret.namespace=fleet-system \
		--namespace fleet-system --create-namespace; \
	helm install mcs-controller-manager ./charts/mcs-controller-manager/ \
		--set image.repository=$(REGISTRY)/$(MCS_CONTROLLER_MANAGER_IMAGE_NAME) \
		--set image.tag=$(MCS_CONTROLLER_MANAGER_IMAGE_VERSION) \
		--set image.pullPolicy=Never \
		--namespace fleet-system --create-namespace; \
	# to make sure member-agent reads the token file.
	kubectl delete pod --all -n fleet-system

build-e2e:
	go test -c ./test/e2e

run-e2e: build-e2e
	KUBECONFIG=$(KUBECONFIG) HUB_SERVER_URL="https://$$(docker inspect $(HUB_KIND_CLUSTER_NAME)-control-plane --format='{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'):6443" ./e2e.test -test.v -ginkgo.v

.PHONY: e2e-tests
e2e-tests: create-hub-kind-cluster create-member-kind-cluster load-hub-docker-image load-member-docker-image install-hub-controller-manager-helm install-member-controller-manager-helm run-e2e

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
QEMU_VERSION ?= 5.2.0-2

.PHONY: docker-buildx-builder
docker-buildx-builder:
	@if ! docker buildx ls | grep $(BUILDX_BUILDER_NAME); then \
		docker run --rm --privileged multiarch/qemu-user-static:$(QEMU_VERSION) --reset -p yes; \
		docker buildx create --name $(BUILDX_BUILDER_NAME) --use; \
		docker buildx inspect $(BUILDX_BUILDER_NAME) --bootstrap; \
	fi

.PHONY: docker-build-hub-net-controller-manager
docker-build-hub-net-controller-manager: docker-buildx-builder
	docker buildx build \
		--file docker/$(HUB_NET_CONTROLLER_MANAGER_IMAGE_NAME).Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/amd64" \
		--pull \
		--tag $(REGISTRY)/$(HUB_NET_CONTROLLER_MANAGER_IMAGE_NAME):$(HUB_NET_CONTROLLER_MANAGER_IMAGE_VERSION) .

.PHONY: docker-build-member-net-controller-manager
docker-build-member-net-controller-manager: docker-buildx-builder
	docker buildx build \
		--file docker/$(MEMBER_NET_CONTROLLER_MANAGER_IMAGE_NAME).Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/amd64" \
		--pull \
		--tag $(REGISTRY)/$(MEMBER_NET_CONTROLLER_MANAGER_IMAGE_NAME):$(MEMBER_NET_CONTROLLER_MANAGER_IMAGE_VERSION) .

.PHONY: docker-build-mcs-controller-manager
docker-build-mcs-controller-manager: docker-buildx-builder
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

.PHONY: uninstall-helm-charts
uninstall-helm-charts:
	kind export kubeconfig --name $(HUB_KIND_CLUSTER_NAME)
	helm status hub-net-controller-manager -n fleet-system && helm uninstall hub-net-controller-manager -n fleet-system

	kind export kubeconfig --name $(MEMBER_KIND_CLUSTER_NAME)
	helm status member-net-controller-manager -n fleet-system && helm uninstall member-net-controller-manager -n fleet-system
	helm status mcs-controller-manager -n fleet-system && helm uninstall mcs-controller-manager -n fleet-system

.PHONY: clean-testing-kind-clusters-resources
clean-testing-kind-clusters-resources: uninstall-helm-charts
	kind export kubeconfig --name $(HUB_KIND_CLUSTER_NAME)
	MEMBER_CLUSTER_NAME="kind-$(MEMBER_KIND_CLUSTER_NAME)"; \
	HUB_MEMBER_NAMESAPCE="fleet-member-$$MEMBER_CLUSTER_NAME"; \
	kubectl delete ns $$HUB_MEMBER_NAMESAPCE --ignore-not-found
	kubectl delete -f ./config/crd/*
	kubectl delete namespace fleet-system

	kind export kubeconfig --name $(MEMBER_KIND_CLUSTER_NAME)
	kubectl delete -f ./config/crd/*
	kubectl delete namespace fleet-system

.PHONY: clean-e2e-tests
clean-e2e-tests: ## Remove
	kind delete cluster --name $(HUB_KIND_CLUSTER_NAME)
	kind delete cluster --name $(MEMBER_KIND_CLUSTER_NAME)
