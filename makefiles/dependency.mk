ROOT_DIR := $(abspath $(patsubst %/,%,$(dir $(abspath $(firstword $(MAKEFILE_LIST))))))
TOOLS_BIN_DIR := $(abspath $(ROOT_DIR)/hack/tools/bin)

KUSTOMIZE = $(TOOLS_BIN_DIR)/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(TOOLS_BIN_DIR),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

# go-get-tool will 'go get' any package $2 and install it to $1.
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(1) go get $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef