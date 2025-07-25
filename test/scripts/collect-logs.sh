#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

# Script to collect logs from fleet networking agents across all clusters

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")
FLEET_NAMESPACE="${FLEET_NAMESPACE:-fleet-system}"

# Use a timestamp in the log directory name
TIMESTAMP=$(date '+%Y%m%d-%H%M%S')
LOG_DIR="${LOG_DIR:-logs}"
LOG_DIR="${LOG_DIR}/${TIMESTAMP}"

# Cluster names
HUB_CLUSTER="${HUB_CLUSTER:-hub}"
MEMBER_CLUSTER_1="${MEMBER_CLUSTER_1:-member-1}"
MEMBER_CLUSTER_2="${MEMBER_CLUSTER_2:-member-2}"

echo "Starting log collection for fleet networking components..."

# Create log directory
mkdir -p "${LOG_DIR}"

# Function to collect logs from pods with a specific label selector
collect_logs() {
    local cluster_name=$1
    local label_selector=$2
    local context="${cluster_name}-admin"
    local cluster_dir="${LOG_DIR}/${cluster_name}"
    
    echo "Collecting logs from cluster ${cluster_name} with selector ${label_selector}..."
    
    # Create cluster directory
    mkdir -p "${cluster_dir}"
    
    # Get pods matching the label selector
    local pods
    if ! pods=$(kubectl --context="${context}" get pods -n "${FLEET_NAMESPACE}" -l "${label_selector}" -o name 2>/dev/null); then
        echo "Warning: Failed to get pods with selector ${label_selector} from cluster ${cluster_name}"
        return
    fi
    
    if [[ -z "${pods}" ]]; then
        echo "No pods found with selector ${label_selector} in cluster ${cluster_name}"
        return
    fi
    
    # Collect logs from each pod
    while IFS= read -r pod; do
        local pod_name=${pod#pod/}
        echo "  Collecting logs from pod ${pod_name}..."
        
        # Get containers in the pod
        local containers
        if ! containers=$(kubectl --context="${context}" get pod "${pod_name}" -n "${FLEET_NAMESPACE}" -o jsonpath='{.spec.containers[*].name}' 2>/dev/null); then
            echo "    Warning: Failed to get containers for pod ${pod_name}"
            continue
        fi
        
        # Collect logs from each container
        for container in ${containers}; do
            local log_file="${cluster_dir}/${pod_name}-${container}.log"
            
            # Current logs
            if kubectl --context="${context}" logs "${pod_name}" -c "${container}" -n "${FLEET_NAMESPACE}" > "${log_file}" 2>&1; then
                echo "    Collected current logs for container ${container}"
            else
                echo "    Warning: Failed to collect current logs for container ${container}"
                echo "Failed to collect logs for ${pod_name}/${container} at $(date)" > "${log_file}"
            fi
            
            # Previous logs (if available)
            local prev_log_file="${cluster_dir}/${pod_name}-${container}-previous.log"
            if kubectl --context="${context}" logs "${pod_name}" -c "${container}" -n "${FLEET_NAMESPACE}" --previous > "${prev_log_file}" 2>&1; then
                echo "    Collected previous logs for container ${container}"
            else
                # Remove empty previous log file
                rm -f "${prev_log_file}"
            fi
        done
    done <<< "${pods}"
}

# Function to describe resources
describe_resources() {
    local cluster_name=$1
    local context="${cluster_name}-admin"
    local cluster_dir="${LOG_DIR}/${cluster_name}"
    local describe_file="${cluster_dir}/describe.txt"
    
    echo "Describing resources in cluster ${cluster_name}..."
    
    # Create cluster directory
    mkdir -p "${cluster_dir}"
    
    {
        echo "=== Describe Resources for ${cluster_name} ==="
        echo "Timestamp: $(date)"
        echo ""
        
        echo "=== Pods in ${FLEET_NAMESPACE} namespace ==="
        kubectl --context="${context}" describe pods -n "${FLEET_NAMESPACE}" 2>/dev/null || echo "Failed to describe pods"
        echo ""
        
        echo "=== Deployments in ${FLEET_NAMESPACE} namespace ==="
        kubectl --context="${context}" describe deployments -n "${FLEET_NAMESPACE}" 2>/dev/null || echo "Failed to describe deployments"
        echo ""
        
        echo "=== Events in ${FLEET_NAMESPACE} namespace ==="
        kubectl --context="${context}" get events -n "${FLEET_NAMESPACE}" --sort-by='.lastTimestamp' 2>/dev/null || echo "Failed to get events"
        echo ""
                
    } > "${describe_file}"
}

# Function to check if cluster context exists
check_cluster_context() {
    local cluster_name=$1
    local context="${cluster_name}-admin"
    
    if kubectl config get-contexts "${context}" &>/dev/null; then
        return 0
    else
        echo "Warning: Context ${context} not found, skipping cluster ${cluster_name}"
        return 1
    fi
}

echo "=== Starting log collection at $(date) ==="

# Collect from hub cluster
if check_cluster_context "${HUB_CLUSTER}"; then
    echo "Collecting logs from hub cluster: ${HUB_CLUSTER}"
    collect_logs "${HUB_CLUSTER}" "app.kubernetes.io/name=hub-net-controller-manager"
    describe_resources "${HUB_CLUSTER}"
fi

# Collect from member clusters
MEMBER_CLUSTERS=("${MEMBER_CLUSTER_1}" "${MEMBER_CLUSTER_2}")

for member_cluster in "${MEMBER_CLUSTERS[@]}"; do
    if check_cluster_context "${member_cluster}"; then
        echo "Collecting logs from member cluster: ${member_cluster}"
        collect_logs "${member_cluster}" "app.kubernetes.io/name=member-net-controller-manager"
        collect_logs "${member_cluster}" "app.kubernetes.io/name=mcs-controller-manager"
        describe_resources "${member_cluster}"
    fi
done

# Create summary
summary_file="${LOG_DIR}/summary.txt"
{
    echo "Log Collection Summary"
    echo "====================="
    echo "Generated: $(date)"
    echo "Script: ${BASH_SOURCE[0]}"
    echo "Fleet Namespace: ${FLEET_NAMESPACE}"
    echo ""
    echo "Clusters:"
    echo "  Hub: ${HUB_CLUSTER}"
    for member in "${MEMBER_CLUSTERS[@]}"; do
        echo "  Member: ${member}"
    done
    echo ""
    echo "Log Files:"
    find "${LOG_DIR}" -name "*.log" | sort
    echo ""
    echo "Resource Files:"
    find "${LOG_DIR}" -name "describe.txt" | sort
    echo ""
    echo "Cluster Directories:"
    find "${LOG_DIR}" -maxdepth 1 -type d ! -path "${LOG_DIR}" | sort
    echo ""
    echo "Statistics:"
    echo "  Total files: $(find "${LOG_DIR}" -type f | wc -l)"
    echo "  Log files: $(find "${LOG_DIR}" -name "*.log" | wc -l)"
    echo "  Resource files: $(find "${LOG_DIR}" -name "describe.txt" | wc -l)"
    echo "  Total size: $(du -sh "${LOG_DIR}" | cut -f1)"
    
} > "${summary_file}"

echo "=== Log collection completed at $(date) ==="
echo "Logs directory: ${LOG_DIR}"
echo "Summary file: ${summary_file}"
echo ""
echo "Log collection summary:"
cat "${summary_file}"
