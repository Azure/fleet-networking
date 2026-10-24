#!/bin/bash

export ENABLE_TRAFFIC_MANAGER="${ENABLE_TRAFFIC_MANAGER:-false}"
export ENABLE_AZURE_FRONT_DOOR="${ENABLE_AZURE_FRONT_DOOR:-false}"
export AZURE_NETWORK_SETTING="${AZURE_NETWORK_SETTING:-shared-vnet}"

if [[ "$ENABLE_AZURE_FRONT_DOOR" != true && "$ENABLE_AZURE_FRONT_DOOR" != false ]]; then
  echo "error: ENABLE_AZURE_FRONT_DOOR must be true or false" >&2
  return 1
fi

if [[ "$ENABLE_AZURE_FRONT_DOOR" == true ]]; then
  if [[ "$AZURE_NETWORK_SETTING" != shared-vnet ]]; then
    echo "error: ENABLE_AZURE_FRONT_DOOR requires shared-vnet" >&2
    return 1
  fi
  if [[ -z "${AZURE_SUBSCRIPTION_ID:-}" || -z "${AZURE_RESOURCE_GROUP:-}" ]]; then
    echo "error: AFD requires AZURE_SUBSCRIPTION_ID and AZURE_RESOURCE_GROUP" >&2
    return 1
  fi
  if [[ "${HUB_CLUSTER:-hub}" != hub || "${MEMBER_CLUSTER_1:-member-1}" != member-1 ||
        "${MEMBER_CLUSTER_2:-member-2}" != member-2 ]]; then
    echo "error: AFD proof requires default hub/member cluster names" >&2
    return 1
  fi
  export ENABLE_GATEWAY_API=true
  export ENABLE_AFD_PRIVATE_HTTP_PROOF=true
  # Derive the same candidate in separate setup/test shells; do not rely on child exports.
  export REGISTRY="${AZURE_RESOURCE_GROUP//-}.azurecr.io"
  if [[ -z "${TAG:-}" ]]; then
    TAG="$(git rev-parse --short=7 HEAD)" || return 1
    export TAG
  fi
  export AFD_PROOF_CONTROLLER_NAMESPACE="${AFD_PROOF_CONTROLLER_NAMESPACE:-fleet-system}"
  export AFD_PROOF_CONTROLLER_DEPLOYMENT="${AFD_PROOF_CONTROLLER_DEPLOYMENT:-hub-gateway-controller-manager}"
  export AFD_PROOF_CONTROLLER_IMAGE="${REGISTRY}/${HUB_GATEWAY_CONTROLLER_MANAGER_IMAGE_NAME:-hub-gateway-controller-manager}:${HUB_GATEWAY_CONTROLLER_MANAGER_IMAGE_VERSION:-$TAG}"
fi
