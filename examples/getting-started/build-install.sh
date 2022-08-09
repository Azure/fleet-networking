if [ -z ${CLIENT_ID_FOR_MEMBER_1+x} ]; then
    echo "Client ID for the first member cluster is not set; to use the script, set the CLIENT_ID_FOR_MEMBER_1 variable."
    exit 1
fi

if [ -z ${PRINCIPAL_FOR_MEMBER_1+x} ]; then
    echo "Principal ID for the first member cluster is not set; to use the script, set the PRINCIPAL_FOR_MEMBER_1 variable."
    exit 1
fi

if [ -z ${CLIENT_ID_FOR_MEMBER_2+x} ]; then
    echo "Client ID for the first member cluster is not set; to use the script, set the CLIENT_ID_FOR_MEMBER_2 variable."
    exit 1
fi

if [ -z ${PRINCIPAL_FOR_MEMBER_2+x} ]; then
    echo "Principal ID for the first member cluster is not set; to use the script, set the PRINCIPAL_FOR_MEMBER_2 variable."
    exit 1
fi

if [ -z ${HUB_URL+x} ]; then
    echo "Hub cluster API server address is not set; to use the script, set the HUB_URL variable."
    exit 1
fi

echo "Building the Fleet networking controller images..."
export TAG=v0.1.0.test
export REGISTRY=$REGISTRY.azurecr.io

echo "Building the image for Fleet networking hub cluster controllers..."
make docker-build-hub-net-controller-manager

echo "Building the image for Fleet networking member cluster controllers..."
make docker-build-member-net-controller-manager

echo "Building the image for Fleet networking MCS controllers..."
make docker-build-mcs-controller-manager

echo "Building the Hello World web application..."
docker build -f ./examples/getting-started/app/Dockerfile ./examples/getting-started/app --tag $REGISTRY/app
docker push $REGISTRY/app

echo "Install Fleet networking CRDs and controllers to the hub cluster..."
kubectl config use-context $HUB_CLUSTER-admin
kubectl apply -f config/crd/*
kubectl apply -f examples/getting-started/artifacts/crd.yaml
helm install hub-net-controller-manager \
    ./charts/hub-net-controller-manager/ \
    --set image.repository=$REGISTRY/hub-net-controller-manager \
    --set image.tag=$TAG

echo "Install Fleet networking CRDs and controllers to the first member cluster..."
kubectl config use-context $MEMBER_CLUSTER_1-admin
kubectl apply -f config/crd/*
helm install mcs-controller-manager \
    ./charts/mcs-controller-manager \
    --set image.repository=$REGISTRY/mcs-controller-manager \
    --set image.tag=$TAG \
    --set config.hubURL=$HUB_URL \
    --set config.provider=azure \
    --set config.memberClusterName=$MEMBER_CLUSTER_1 \
    --set azure.clientid=$CLIENT_ID_FOR_MEMBER_1
helm install member-net-controller-manager ./charts/member-net-controller-manager/ \
    --set image.repository=$REGISTRY/member-net-controller-manager \
    --set image.tag=$TAG \
    --set config.hubURL=$HUB_URL \
    --set config.provider=azure \
    --set config.memberClusterName=$MEMBER_CLUSTER_1 \
    --set azure.clientid=$CLIENT_ID_FOR_MEMBER_1

echo "Install Fleet networking CRDs and controllers to the second member cluster..."
kubectl config use-context $MEMBER_CLUSTER_2-admin
kubectl apply -f config/crd/*
helm install mcs-controller-manager \
    ./charts/mcs-controller-manager \
    --set image.repository=$REGISTRY/mcs-controller-manager \
    --set image.tag=$TAG \
    --set config.hubURL=$HUB_URL \
    --set config.provider=azure \
    --set config.memberClusterName=$MEMBER_CLUSTER_2 \
    --set azure.clientid=$CLIENT_ID_FOR_MEMBER_2
helm install member-net-controller-manager ./charts/member-net-controller-manager/ \
    --set image.repository=$REGISTRY/member-net-controller-manager \
    --set image.tag=$TAG \
    --set config.hubURL=$HUB_URL \
    --set config.provider=azure \
    --set config.memberClusterName=$MEMBER_CLUSTER_2 \
    --set azure.clientid=$CLIENT_ID_FOR_MEMBER_2
