#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -x

[[ -z "${AZURE_RESOURCE_GROUP}" ]] && echo "AZURE_RESOURCE_GROUP is not set" && exit 1

az group show --name $AZURE_RESOURCE_GROUP && az group delete --name $AZURE_RESOURCE_GROUP --no-wait --yes
