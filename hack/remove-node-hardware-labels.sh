#!/bin/bash
# This script removes hardware labels from nodes that were added by annotate-node-hardware.sh
# This is useful for testing the runtime hardware detection capabilities of compute-gardener-scheduler

set -e

LABEL_PREFIX="feature.node.kubernetes.io"

# Function to get nodes
get_nodes() {
  kubectl get nodes -o jsonpath='{.items[*].metadata.name}'
}

# Function to remove labels from a node
remove_labels() {
  local node=$1
  
  # Check if the node has the labels before removing - using grep to be more reliable
  cpu_label=$(kubectl get node $node -o yaml | grep -q "${LABEL_PREFIX}/cpu-model.name:" && echo "found" || echo "")
  gpu_label=$(kubectl get node $node -o yaml | grep -q "${LABEL_PREFIX}/gpu.product:" && echo "found" || echo "")
  
  # Remove CPU model label if it exists
  if [ ! -z "$cpu_label" ]; then
    # Use kubectl label with the dash suffix to remove
    kubectl label node $node "${LABEL_PREFIX}/cpu-model.name-"
    echo "Removed CPU model label from node $node"
  else
    echo "No CPU model label found on node $node"
  fi
  
  # Remove GPU model label if it exists
  if [ ! -z "$gpu_label" ]; then
    # Use kubectl label with the dash suffix to remove
    kubectl label node $node "${LABEL_PREFIX}/gpu.product-"
    echo "Removed GPU model label from node $node"
  else
    echo "No GPU model label found on node $node"
  fi
}

# Function to process all nodes
process_all_nodes() {
  for node in $(get_nodes); do
    echo "Processing node: $node"
    remove_labels $node
    echo "Completed removing labels from node: $node"
    echo "-----------------------------------"
  done
}

# Main function
main() {
  echo "Starting removal of hardware labels from all nodes..."
  echo "This will remove '${LABEL_PREFIX}/cpu-model.name' and '${LABEL_PREFIX}/gpu.product' labels"
  echo "Removing these labels will force compute-gardener-scheduler to use runtime hardware detection"
  
  # Check user confirmation
  read -p "Do you want to continue? (y/n) " -n 1 -r
  echo    # New line
  if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Operation cancelled."
    exit 1
  fi
  
  process_all_nodes
  echo "Hardware label removal completed successfully!"
  echo "The compute-gardener-scheduler will now use runtime hardware detection on next pod scheduling."
}

# Run with the first argument or default behavior
if [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
  echo "Usage: $0 [node-name]"
  echo ""
  echo "If node-name is provided, only labels from that node will be removed."
  echo "Otherwise, labels from all nodes in the cluster will be removed."
  exit 0
elif [ ! -z "$1" ]; then
  echo "Processing single node: $1"
  node="$1"
  remove_labels $node
  echo "Completed removing labels from node: $node"
else
  main
fi
