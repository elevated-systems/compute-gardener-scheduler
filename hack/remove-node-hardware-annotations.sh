#!/bin/bash
# This script removes hardware annotations from nodes that were added by annotate-node-hardware.sh
# This is useful for testing the runtime hardware detection capabilities of compute-gardener-scheduler

set -e

ANNOTATION_PREFIX="compute-gardener-scheduler.kubernetes.io"

# Function to get nodes
get_nodes() {
  kubectl get nodes -o jsonpath='{.items[*].metadata.name}'
}

# Function to remove annotations from a node
remove_annotations() {
  local node=$1
  
  # Check if the node has the annotations before removing - using grep to be more reliable
  cpu_annotation=$(kubectl get node $node -o yaml | grep -q "${ANNOTATION_PREFIX}/cpu-model:" && echo "found" || echo "")
  gpu_annotation=$(kubectl get node $node -o yaml | grep -q "${ANNOTATION_PREFIX}/gpu-model:" && echo "found" || echo "")
  
  # Remove CPU model annotation if it exists
  if [ ! -z "$cpu_annotation" ]; then
    # Use kubectl annotate with the dash suffix to remove
    kubectl annotate node $node "${ANNOTATION_PREFIX}/cpu-model-"
    echo "Removed CPU model annotation from node $node"
  else
    echo "No CPU model annotation found on node $node"
  fi
  
  # Remove GPU model annotation if it exists
  if [ ! -z "$gpu_annotation" ]; then
    # Use kubectl annotate with the dash suffix to remove
    kubectl annotate node $node "${ANNOTATION_PREFIX}/gpu-model-"
    echo "Removed GPU model annotation from node $node"
  else
    echo "No GPU model annotation found on node $node"
  fi
}

# Function to process all nodes
process_all_nodes() {
  for node in $(get_nodes); do
    echo "Processing node: $node"
    remove_annotations $node
    echo "Completed removing annotations from node: $node"
    echo "-----------------------------------"
  done
}

# Main function
main() {
  echo "Starting removal of hardware annotations from all nodes..."
  echo "This will remove '${ANNOTATION_PREFIX}/cpu-model' and '${ANNOTATION_PREFIX}/gpu-model' annotations"
  echo "Removing these annotations will force compute-gardener-scheduler to use runtime hardware detection"
  
  # Check user confirmation
  read -p "Do you want to continue? (y/n) " -n 1 -r
  echo    # New line
  if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Operation cancelled."
    exit 1
  fi
  
  process_all_nodes
  echo "Hardware annotation removal completed successfully!"
  echo "The compute-gardener-scheduler will now use runtime hardware detection on next pod scheduling."
}

# Run with the first argument or default behavior
if [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
  echo "Usage: $0 [node-name]"
  echo ""
  echo "If node-name is provided, only annotations from that node will be removed."
  echo "Otherwise, annotations from all nodes in the cluster will be removed."
  exit 0
elif [ ! -z "$1" ]; then
  echo "Processing single node: $1"
  node="$1"
  remove_annotations $node
  echo "Completed removing annotations from node: $node"
else
  main
fi