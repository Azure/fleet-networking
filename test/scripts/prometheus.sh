#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -x

# This script installs Prometheus (specifically the kube-prometheus-stack helm chart) to member clusters
# set up for performance test.

# Get the kube-prometheus-stack helm chart.
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# Install the kube-prometheus-stack helm chart in each cluster.
export INSTALLATION_NAME=monitor
export MONITORING_NS=monitoring

kubectl config use-context $HUB_CLUSTER-admin
kubectl create ns $MONITORING_NS
helm install $INSTALLATION_NAME prometheus-community/kube-prometheus-stack \
    --namespace $MONITORING_NS \
    --set prometheus.prometheusSpec.enableAdminAPI=true
kubectl apply -f ./test/perftest/manifests/hubpodmonitor.yaml
kubectl apply -f ./test/perftest/manifests/metricsdashboardsvc.yaml

kubectl config use-context $MEMBER_CLUSTER_1-admin
kubectl create ns $MONITORING_NS
helm install $INSTALLATION_NAME prometheus-community/kube-prometheus-stack \
    --namespace $MONITORING_NS \
    --set prometheus.prometheusSpec.enableAdminAPI=true
kubectl apply -f ./test/perftest/manifests/memberpodmonitor.yaml
kubectl apply -f ./test/perftest/manifests/metricsdashboardsvc.yaml

kubectl config use-context $MEMBER_CLUSTER_2-admin
kubectl create ns $MONITORING_NS
helm install $INSTALLATION_NAME prometheus-community/kube-prometheus-stack \
    --namespace $MONITORING_NS \
    --set prometheus.prometheusSpec.enableAdminAPI=true
kubectl apply -f ./test/perftest/manifests/memberpodmonitor.yaml
kubectl apply -f ./test/perftest/manifests/metricsdashboardsvc.yaml

kubectl config use-context $MEMBER_CLUSTER_3-admin
kubectl create ns $MONITORING_NS
helm install $INSTALLATION_NAME prometheus-community/kube-prometheus-stack \
    --namespace $MONITORING_NS \
    --set prometheus.prometheusSpec.enableAdminAPI=true
kubectl apply -f ./test/perftest/manifests/memberpodmonitor.yaml
kubectl apply -f ./test/perftest/manifests/metricsdashboardsvc.yaml

kubectl config use-context $MEMBER_CLUSTER_4-admin
kubectl create ns $MONITORING_NS
helm install $INSTALLATION_NAME prometheus-community/kube-prometheus-stack \
    --namespace $MONITORING_NS \
    --set prometheus.prometheusSpec.enableAdminAPI=true
kubectl apply -f ./test/perftest/manifests/memberpodmonitor.yaml
kubectl apply -f ./test/perftest/manifests/metricsdashboardsvc.yaml

