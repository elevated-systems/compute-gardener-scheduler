#!/bin/bash
# This script detects hardware information from nodes and adds annotations
# These annotations help the compute-gardener-scheduler optimize power profiling

set -e

ANNOTATION_PREFIX="compute-gardener-scheduler.kubernetes.io"

# Function to get nodes
get_nodes() {
  kubectl get nodes -o jsonpath='{.items[*].metadata.name}'
}

# Function to get CPU model from a node
get_cpu_model() {
  local node=$1
  
  # Run a privileged pod to extract CPU info from /proc/cpuinfo
  cpu_model=$(kubectl debug node/$node -it --image=busybox -- cat /proc/cpuinfo | grep "model name" | head -n 1 | sed 's/model name.*: //')
  
  if [ -z "$cpu_model" ]; then
    echo "Warning: Could not determine CPU model for node $node"
    return 1
  fi
  
  echo $cpu_model
}

# Function to get GPU info (if exists)
get_gpu_model() {
  local node=$1
  
  # First check if node has NVIDIA GPU 
  has_gpu=$(kubectl get node $node -o jsonpath='{.status.allocatable.nvidia\.com/gpu}')
  
  if [ -z "$has_gpu" ] || [ "$has_gpu" = "0" ]; then
    echo "none"
    return 0
  fi
  
  # Run a privileged pod with nvidia drivers to get GPU model
  gpu_model=$(kubectl debug node/$node -it --image=nvidia/cuda:11.4.0-base-ubuntu20.04 -- nvidia-smi --query-gpu=name --format=csv,noheader | head -n 1)
  
  if [ -z "$gpu_model" ]; then
    echo "Warning: Could not determine GPU model"
    return 1
  fi
  
  echo $gpu_model
}


# Function to annotate a node with hardware information
annotate_node() {
  local node=$1
  local cpu_model=$2
  local gpu_model=$3
  
  # Annotate with CPU model
  if [ ! -z "$cpu_model" ]; then
    kubectl annotate node $node "${ANNOTATION_PREFIX}/cpu-model=$cpu_model" --overwrite
    echo "Annotated node $node with CPU model: $cpu_model"
  fi
  
  # Always annotate GPU model - either actual model or "none"
  if [ ! -z "$gpu_model" ]; then
    kubectl annotate node $node "${ANNOTATION_PREFIX}/gpu-model=$gpu_model" --overwrite
    
    if [ "$gpu_model" == "none" ]; then
      echo "Annotated node $node with GPU model: none (no GPU detected)"
    else
      echo "Annotated node $node with GPU model: $gpu_model"
    fi
  fi
}

# Function to process all nodes
process_all_nodes() {
  for node in $(get_nodes); do
    echo "Processing node: $node"
    
    # Get CPU model
    cpu_model=$(get_cpu_model $node) || true
    
    # Get GPU model if exists
    gpu_model=$(get_gpu_model $node) || true
    
    # Annotate node with hardware info
    annotate_node $node "$cpu_model" "$gpu_model"
    
    echo "Completed processing node: $node"
    echo "-----------------------------------"
  done
}

# Main function
main() {
  echo "Starting hardware detection and annotation for all nodes..."
  echo "This will add '${ANNOTATION_PREFIX}/cpu-model' and/or '${ANNOTATION_PREFIX}/gpu-model' annotations"
  echo "These annotations enable more efficient power profiling in the compute-gardener-scheduler"
  
  # Check user confirmation
  read -p "Do you want to continue? (y/n) " -n 1 -r
  echo    # New line
  if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Operation cancelled."
    exit 1
  fi
  
  process_all_nodes
  echo "Hardware annotation process completed successfully!"
  echo "The compute-gardener-scheduler will automatically use these annotations for power profiling."
}

# Run with the first argument or default behavior
if [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
  echo "Usage: $0 [node-name]"
  echo ""
  echo "If node-name is provided, only that node will be processed."
  echo "Otherwise, all nodes in the cluster will be processed."
  exit 0
elif [ ! -z "$1" ]; then
  echo "Processing single node: $1"
  node="$1"
  
  # Get CPU model
  cpu_model=$(get_cpu_model $node) || true
  
  # Get GPU model if exists
  gpu_model=$(get_gpu_model $node) || true
  
  # Annotate node
  annotate_node $node "$cpu_model" "$gpu_model"
  
  echo "Completed processing node: $node"
else
  main
fi