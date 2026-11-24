#!/bin/bash

export ENABLE_TRAFFIC_MANAGER="${ENABLE_TRAFFIC_MANAGER:-false}"
export ENABLE_AZURE_FRONT_DOOR="${ENABLE_AZURE_FRONT_DOOR:-false}"
export RETAIN_AFD_E2E_SETUP="${RETAIN_AFD_E2E_SETUP:-false}"
export AZURE_NETWORK_SETTING="${AZURE_NETWORK_SETTING:-shared-vnet}"
export E2E_SHARED_RESOURCE_GROUP="${E2E_SHARED_RESOURCE_GROUP:-false}"

case "$E2E_SHARED_RESOURCE_GROUP" in
  false)
    unset E2E_RESOURCE_PREFIX HUB_AZURE_CLUSTER MEMBER_1_AZURE_CLUSTER MEMBER_2_AZURE_CLUSTER
    ;;
  true)
    if [[ "$ENABLE_TRAFFIC_MANAGER" != true && "$ENABLE_TRAFFIC_MANAGER" != false ]]; then
      echo "error: ENABLE_TRAFFIC_MANAGER must be true or false in shared mode" >&2
      return 1
    fi
    if [[ -z "${AZURE_SUBSCRIPTION_ID:-}" || -z "${AZURE_RESOURCE_GROUP:-}" ||
          ! "${E2E_RUN_ID:-}" =~ ^[a-z0-9][a-z0-9-]{0,62}$ ]]; then
      echo "error: shared resource group mode requires subscription, resource group and a lowercase alphanumeric E2E_RUN_ID" >&2
      return 1
    fi
    if [[ "$AZURE_NETWORK_SETTING" == perf-test ]]; then
      echo "error: shared resource group mode does not support perf-test" >&2
      return 1
    fi
    if [[ "${HUB_CLUSTER:-hub}" != hub || "${MEMBER_CLUSTER_1:-member-1}" != member-1 ||
          "${MEMBER_CLUSTER_2:-member-2}" != member-2 ]]; then
      echo "error: shared resource group mode requires default logical cluster names" >&2
      return 1
    fi
    E2E_RESOURCE_PREFIX="fne$(printf '%s' "$AZURE_SUBSCRIPTION_ID/$AZURE_RESOURCE_GROUP/$E2E_RUN_ID" | sha256sum | cut -c1-16)"
    export E2E_RESOURCE_PREFIX
    export HUB_AZURE_CLUSTER="${E2E_RESOURCE_PREFIX}-hub"
    export MEMBER_1_AZURE_CLUSTER="${E2E_RESOURCE_PREFIX}-member1"
    export MEMBER_2_AZURE_CLUSTER="${E2E_RESOURCE_PREFIX}-member2"
    export REGISTRY_NAME="${E2E_RESOURCE_PREFIX}acr"
    export REGISTRY="${REGISTRY_NAME}.azurecr.io"
    export HUB_CLUSTER_AKS_ID_NAME="${E2E_RESOURCE_PREFIX}-hub-control"
    export HUB_CLUSTER_AKS_KUBELET_ID_NAME="${E2E_RESOURCE_PREFIX}-hub-kubelet"
    export MEMBER_CLUSTER_1_AKS_ID_NAME="${E2E_RESOURCE_PREFIX}-member1-control"
    export MEMBER_CLUSTER_1_AKS_KUBELET_ID_NAME="${E2E_RESOURCE_PREFIX}-member1-kubelet"
    export MEMBER_CLUSTER_2_AKS_ID_NAME="${E2E_RESOURCE_PREFIX}-member2-control"
    export MEMBER_CLUSTER_2_AKS_KUBELET_ID_NAME="${E2E_RESOURCE_PREFIX}-member2-kubelet"
    ;;
  *)
    echo "error: E2E_SHARED_RESOURCE_GROUP must be true or false" >&2
    return 1
    ;;
esac
export HUB_CLUSTER="${HUB_CLUSTER:-hub}"
export MEMBER_CLUSTER_1="${MEMBER_CLUSTER_1:-member-1}"
export MEMBER_CLUSTER_2="${MEMBER_CLUSTER_2:-member-2}"
export HUB_AZURE_CLUSTER="${HUB_AZURE_CLUSTER:-$HUB_CLUSTER}"
export MEMBER_1_AZURE_CLUSTER="${MEMBER_1_AZURE_CLUSTER:-$MEMBER_CLUSTER_1}"
export MEMBER_2_AZURE_CLUSTER="${MEMBER_2_AZURE_CLUSTER:-$MEMBER_CLUSTER_2}"
E2E_RESOURCE_TAGS=()

if [[ "$ENABLE_AZURE_FRONT_DOOR" != true && "$ENABLE_AZURE_FRONT_DOOR" != false ]]; then
  echo "error: ENABLE_AZURE_FRONT_DOOR must be true or false" >&2
  return 1
fi
export ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL="${ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL:-$ENABLE_AZURE_FRONT_DOOR}"
if [[ "$ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL" != true && "$ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL" != false ]]; then
  echo "error: ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL must be true or false" >&2
  return 1
fi

if [[ "$RETAIN_AFD_E2E_SETUP" != true && "$RETAIN_AFD_E2E_SETUP" != false ]]; then
  echo "error: RETAIN_AFD_E2E_SETUP must be true or false" >&2
  return 1
fi
if [[ "$RETAIN_AFD_E2E_SETUP" == true && "$ENABLE_AZURE_FRONT_DOOR" != true ]]; then
  echo "error: RETAIN_AFD_E2E_SETUP requires ENABLE_AZURE_FRONT_DOOR=true" >&2
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
  if [[ "$E2E_SHARED_RESOURCE_GROUP" == true && "$ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL" == true &&
        ! "${E2E_TEST_PRINCIPAL_ID:-}" =~ ^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$ ]]; then
    echo "error: shared AFD approval requires E2E_TEST_PRINCIPAL_ID" >&2
    return 1
  fi
  if [[ "$E2E_SHARED_RESOURCE_GROUP" == true &&
        ( "${AFD_PROOF_CONTROLLER_NAMESPACE:-fleet-system}" != fleet-system ||
          "${AFD_PROOF_CONTROLLER_DEPLOYMENT:-hub-gateway-controller-manager}" != hub-gateway-controller-manager ) ]]; then
    echo "error: shared AFD mode requires the default controller namespace and deployment" >&2
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
  if [[ "$E2E_SHARED_RESOURCE_GROUP" == true ]]; then
    export REGISTRY="${REGISTRY_NAME}.azurecr.io"
  else
    export REGISTRY="${AZURE_RESOURCE_GROUP//-}.azurecr.io"
  fi
  if [[ -z "${TAG:-}" ]]; then
    TAG="$(git rev-parse --short=7 HEAD)" || return 1
    export TAG
  fi
  export AFD_PROOF_CONTROLLER_NAMESPACE="${AFD_PROOF_CONTROLLER_NAMESPACE:-fleet-system}"
  export AFD_PROOF_CONTROLLER_DEPLOYMENT="${AFD_PROOF_CONTROLLER_DEPLOYMENT:-hub-gateway-controller-manager}"
  export AFD_PROOF_CONTROLLER_IMAGE="${REGISTRY}/${HUB_GATEWAY_CONTROLLER_MANAGER_IMAGE_NAME:-hub-gateway-controller-manager}:${HUB_GATEWAY_CONTROLLER_MANAGER_IMAGE_VERSION:-$TAG}"
fi

if [[ "$E2E_SHARED_RESOURCE_GROUP" == true ]]; then
  E2E_RESOURCE_TAGS=(--tags "source=fleet-networking" "fleet-e2e-run=$E2E_RUN_ID"
    "fleet-e2e-atm=$ENABLE_TRAFFIC_MANAGER" "fleet-e2e-afd=$ENABLE_AZURE_FRONT_DOOR"
    "fleet-e2e-auto-approval=$ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL")
  if [[ "$ENABLE_AZURE_FRONT_DOOR" == true && "$ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL" == true ]]; then
    E2E_RESOURCE_TAGS+=("fleet-e2e-test-principal=$E2E_TEST_PRINCIPAL_ID")
  fi
fi
