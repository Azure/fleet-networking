# CAN ONLY BE RUN AFTER CREATING NEEDED HUB CLUSTER, MEMBER CLUSTERS AND AFTER INSTALLING THE FLEET MEMBER AGENT ON EACH MEMBER CLUSTER.
# This can be achieved by running the script in fleet/hack/Azure/setup/joinMC.sh in the fleet repository https://github.com/Azure/fleet.
# This script installs the fleet-networking member agents on each member cluster.

if [ "$#" -lt 4 ]; then
  echo "Usage: $0 <FLEET-IMAGE-TAG> <FLEET-NETWORKING-IMAGE-TAG> <HUB-CLUSTER-NAME> <MEMBER-CLUSTER-NAME-1> [<MEMBER-CLUSTER-NAME-2> ...]"
  exit 1
fi

export FLEET_IMAGE_TAG="$1"
if [[ $(curl "https://api.github.com/repos/Azure/fleet/tags") != *$1* ]] > /dev/null 2>&1; then
  echo "fleet image tag $1 does not exist"
  exit 1
fi

export FLEET_NETWORKING_AGENT_IMAGE_TAG="$2"
if [[ $(curl "https://api.github.com/repos/Azure/fleet-networking/tags") != *$2* ]] > /dev/null 2>&1; then
  echo "fleet networking image tag $2 does not exist"
  exit 1
fi

export HUB_CLUSTER="$3"
if [[ ! $(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$HUB_CLUSTER\")]}") ]] > /dev/null 2>&1; then
  echo "The cluster named $HUB_CLUSTER does not exist."
  exit 1
fi

for MEMBER_CLUSTER in "${@:4}"; do
if [[ ! $(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$MEMBER_CLUSTER\")]}") ]] > /dev/null 2>&1; then
  echo "The cluster named $MEMBER_CLUSTER does not exist."
  exit 1
fi
done

export HUB_CLUSTER_CONTEXT=$(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$HUB_CLUSTER\")].name}")
export HUB_CLUSTER_ADDRESS=$(kubectl config view -o jsonpath="{.clusters[?(@.name==\"$HUB_CLUSTER\")].cluster.server}")

for MEMBER_CLUSTER in "${@:4}"; do
export MEMBER_CLUSTER_CONTEXT=$(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$MEMBER_CLUSTER\")].name}")

echo "Switching to member cluster context..."
kubectl config use-context $MEMBER_CLUSTER_CONTEXT

echo "Apply the Fleet networking CRDs..."
kubectl apply -f config/crd/*

# # Install the fleet-networking member agent helm charts on the member cluster.
# The variables below uses the fleet-networking images kept in the Microsoft Container Registry (MCR)
echo "Retrieving image..."
export REGISTRY="mcr.microsoft.com/aks/fleet"
export MCS_CONTROLLER_MANAGER_IMAGE="mcs-controller-manager"
export MEMBER_NET_CONTROLLER_MANAGER_IMAGE="member-net-controller-manager"
export REFRESH_TOKEN_IMAGE="${REFRESH_TOKEN_NAME:-refresh-token}"
export OUTPUT_TYPE="${OUTPUT_TYPE:-type=docker}"

echo "Uninstalling mcs-controller-manager..."
helm uninstall mcs-controller-manager --wait

echo "Installing mcs-controller-manager..."
helm install mcs-controller-manager ./charts/mcs-controller-manager/ \
--set image.repository=$REGISTRY/$MCS_CONTROLLER_MANAGER_IMAGE \
--set refreshtoken.repository=$REGISTRY/$REFRESH_TOKEN_IMAGE \
--set refreshtoken.tag=$FLEET_IMAGE_TAG \
--set image.tag=$FLEET_NETWORKING_AGENT_IMAGE_TAG \
--set image.pullPolicy=Always \
--set refreshtoken.pullPolicy=Always \
--set config.hubURL=$HUB_CLUSTER_ADDRESS \
--set config.memberClusterName=$MEMBER_CLUSTER \
--set enableV1Alpha1APIs=false \
--set enableV1Beta1APIs=true \
--set logVerbosity=8

echo "Uninstalling member-net-controller-manager..."
helm uninstall member-net-controller-manager --wait

echo "Installing member-net-controller-manager..."
helm install member-net-controller-manager ./charts/member-net-controller-manager/ \
--set image.repository=$REGISTRY/$MEMBER_NET_CONTROLLER_MANAGER_IMAGE \
--set refreshtoken.repository=$REGISTRY/$REFRESH_TOKEN_IMAGE \
--set refreshtoken.tag=$FLEET_IMAGE_TAG \
--set image.tag=$FLEET_NETWORKING_AGENT_IMAGE_TAG \
--set timage.pullPolicy=Always \
--set refreshtoken.pullPolicy=Always \
--set config.hubURL=$HUB_CLUSTER_ADDRESS \
--set config.memberClusterName=$MEMBER_CLUSTER \
--set enableV1Alpha1APIs=false \
--set enableV1Beta1APIs=true \
--set logVerbosity=8

kubectl get pods -A
kubectl config use-context $HUB_CLUSTER_CONTEXT
kubectl get membercluster $MEMBER_CLUSTER
done
