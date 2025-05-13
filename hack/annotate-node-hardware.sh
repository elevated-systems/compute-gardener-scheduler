#!/bin/bash
# This script detects hardware information from nodes and adds annotations
# These annotations help the compute-gardener-scheduler optimize power profiling

set -e

# Constants and configuration
ANNOTATION_PREFIX="compute-gardener-scheduler.kubernetes.io"
KUBECTL_DEBUG_OPTS="--profile=general -it --quiet"
BUSYBOX_IMAGE="busybox"
NVIDIA_CUDA_IMAGE="nvidia/cuda:11.6.1-base-ubuntu20.04"

# Function to get nodes
get_nodes() {
  kubectl get nodes -o jsonpath='{.items[*].metadata.name}'
}

# Function to get CPU model from a node
get_cpu_model() {
  local node=$1
  local cleanup=$2
  
  # Try to detect ARM vs x86 architecture
  local arch=$(kubectl get node $node -o jsonpath='{.status.nodeInfo.architecture}' 2>/dev/null)
  
  # Get CPU model based on architecture
  local cpu_model=""
  
  # For x86/amd64 - this is our primary approach
  if [[ "$arch" == "amd64" ]]; then
    # Direct command that works well just for x86
    cpu_model=$(kubectl debug node/$node $KUBECTL_DEBUG_OPTS --image=$BUSYBOX_IMAGE -- cat /proc/cpuinfo 2>/dev/null | grep "model name" | head -1 | sed 's/model name.*: //')
  
  # For ARM processors - focus on more thorough extraction
  else
    # Tell me EXACTLY what's in the CPU info with very generous timing
    local all_cpu_info=""
    local try_count=0
    local max_tries=3
    
    # Retry logic to ensure we get complete CPU info
    while [ $try_count -lt $max_tries ] && [ -z "$all_cpu_info" ]; do
      all_cpu_info=$(kubectl debug node/$node $KUBECTL_DEBUG_OPTS --image=$BUSYBOX_IMAGE -- sh -c "cat /proc/cpuinfo; echo '---HARDWARE---'; cat /proc/device-tree/compatible 2>/dev/null || echo 'not available'" 2>/dev/null)
      try_count=$((try_count+1))
      
      # If we got data, break, otherwise wait and retry
      if [ ! -z "$all_cpu_info" ]; then
        break
      fi
      sleep 3
    done
    
    # Extract both CPU part and Hardware info for debugging
    local hardware_info=$(echo "$all_cpu_info" | grep -E "Hardware" | head -1)
    local part_info=$(echo "$all_cpu_info" | grep -E "CPU part" | head -1)
    local processor_info=$(echo "$all_cpu_info" | grep -E "Processor" | head -1)
    
    # Highly specific mapping from CPU parts to models based on hardware profiles
    if [[ "$arch" == "arm"* ]]; then
      # First check for Cortex-A72 (CPU part 0xd08) - used in Raspberry Pi 4
      if echo "$all_cpu_info" | grep -q "CPU part.*0xd08"; then
        # This is a direct match for ARM Cortex-A72 in the hardware profiles
        cpu_model="ARM Cortex-A72"
      # Check for Neoverse CPUs
      elif echo "$all_cpu_info" | grep -q "CPU part.*0xd0c"; then
        cpu_model="ARM Neoverse N1"
      elif echo "$all_cpu_info" | grep -q "CPU part.*0xd40"; then
        cpu_model="ARM Neoverse V1"
      # Use model name or processor field as fallback
      elif echo "$all_cpu_info" | grep -q "model name"; then
        cpu_model=$(echo "$all_cpu_info" | grep "model name" | head -1 | sed 's/model name.*: //')
      elif echo "$all_cpu_info" | grep -q "Processor"; then
        # This will capture fields like "Processor : ARMv7 Processor rev 3 (v7l)"
        cpu_model=$(echo "$all_cpu_info" | grep "Processor" | head -1 | sed 's/Processor.*: //')
      # Last resort for ARM
      else 
        # For Raspberry Pi without clear CPU identification, default to Cortex-A72
        # This matches the hardware profile for power calculation
        if echo "$all_cpu_info" | grep -q -E "BCM|Raspberry"; then
          cpu_model="ARM Cortex-A72"
        else
          # Use part number in the model name
          local part=$(echo "$part_info" | grep -o "0x[0-9a-f]*" || echo "")
          if [ ! -z "$part" ]; then
            cpu_model="ARM CPU (Part: $part)"
          else
            cpu_model="ARM CPU"
          fi
        fi
      fi
    else
      # For other non-amd64, non-arm architectures
      cpu_model=$(echo "$all_cpu_info" | grep -E "model name|Hardware|Processor|cpu model" | head -1 | cut -d: -f2 | xargs 2>/dev/null || echo "")
      
      if [ -z "$cpu_model" ]; then
        cpu_model="Unknown CPU ($arch architecture)"
      fi
    fi
  fi
  
  # Cleanup the CPU debug pod if requested
  if [ "$cleanup" == "true" ]; then
    # List all debugging pods for this node and delete them
    # The pattern may be "node-debugger-$node-*" or different based on kubectl version
    debug_pods=$(kubectl get pods --no-headers -o custom-columns=":metadata.name" | grep -E "debug.*$node|node-debugger.*$node" 2>/dev/null || true)
    if [ ! -z "$debug_pods" ]; then
      echo "$debug_pods" | xargs kubectl delete pod --wait=false >/dev/null 2>&1 || true
    fi
  fi
  
  echo "$cpu_model"
}

# Function to get GPU info (if exists)
get_gpu_info() {
  local node=$1
  local cleanup=$2
  
  # First check if node has NVIDIA GPU 
  has_gpu=$(kubectl get node $node -o jsonpath='{.status.allocatable.nvidia\.com/gpu}')
  
  if [ -z "$has_gpu" ] || [ "$has_gpu" = "0" ]; then
    echo "none"
    return 0
  fi
  
  # Create a temporary file to store only GPU model info
  local tmpfile=$(mktemp)
  local pod_name="gpu-debug-$node-$(date +%s)"
  
  # Create a GPU pod manifest
  cat > "$tmpfile.yaml" <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: $pod_name
spec:
  nodeName: $node
  restartPolicy: Never
  runtimeClassName: nvidia
  containers:
  - name: detect
    image: $NVIDIA_CUDA_IMAGE
    command: ["nvidia-smi", "--query-gpu=name", "--format=csv,noheader"]
    securityContext:
      privileged: true
EOF
  
  # Create the pod silently
  kubectl apply -f "$tmpfile.yaml" >/dev/null 2>&1
  
  # Wait briefly for pod to start
  sleep 5
  
  # Directly capture logs to a file - no echo statements
  kubectl logs $pod_name > "$tmpfile" 2>/dev/null || true
  
  # Clean up pod if requested (or not)
  if [ "$cleanup" == "true" ]; then
    kubectl delete pod "$pod_name" --wait=false >/dev/null 2>&1 || true
  fi
  
  # Get the result and clean up
  local result
  if [ -s "$tmpfile" ]; then
    # Convert any newlines to commas for proper annotation format
    # This is important for the scheduler which expects comma-separated values
    result=$(cat "$tmpfile" | tr '\n' ',' | sed 's/,$//')
  else
    result="unknown"
  fi
  
  # Clean up temporary files
  rm -f "$tmpfile" "$tmpfile.yaml"
  
  echo "$result"
}

# Helper function to annotate a node silently
annotate_node_silently() {
  local node=$1
  local key=$2
  local value=$3
  local overwrite=${4:-"--overwrite"}
  
  kubectl annotate node $node "${ANNOTATION_PREFIX}/$key=$value" $overwrite >/dev/null
}

# Function to annotate a node with CPU model
annotate_cpu() {
  local node=$1
  local cpu_model=$2
  
  if [ -z "$cpu_model" ]; then
    echo "No CPU model to annotate"
    return 1
  fi
  
  # Add CPU model annotation
  annotate_node_silently "$node" "cpu-model" "$cpu_model"
  echo "Annotated node $node with CPU model: $cpu_model"
}

# Function to annotate a node with GPU model(s)
annotate_gpu() {
  local node=$1
  local gpu_model=$2
  
  if [ -z "$gpu_model" ]; then
    echo "No GPU model to annotate"
    return 1
  fi
  
  if [ "$gpu_model" == "none" ] || [ "$gpu_model" == "unknown" ]; then
    # Simple annotation for nodes with no GPU
    annotate_node_silently "$node" "gpu-model" "$gpu_model"
    echo "Annotated node $node with GPU model: $gpu_model"
    return 0
  fi
  
  # For nodes with GPUs, clean up existing annotations first
  echo "Cleaning up existing GPU annotations for node $node..."
  kubectl annotate node $node "${ANNOTATION_PREFIX}/gpu-model-" 2>/dev/null >/dev/null || true
  
  # Remove all indexed GPU annotations at once
  local remove_indices=()
  for i in {0..9}; do
    remove_indices+=("${ANNOTATION_PREFIX}/gpu-model.$i-")
  done
  kubectl annotate node $node "${remove_indices[@]}" 2>/dev/null >/dev/null || true
  
  # Add main annotation with full GPU models (already comma-separated from get_gpu_info)
  annotate_node_silently "$node" "gpu-model" "$gpu_model"
  echo "Annotated node $node with GPU model(s): $gpu_model"
  
  # Only add indexed annotations if there are multiple GPUs (comma separated)
  if [[ "$gpu_model" == *","* ]]; then
    # Each entry is a separate GPU model, split by commas
    IFS=',' read -ra GPU_ARRAY <<< "$gpu_model"
    local i=0
    for gpu in "${GPU_ARRAY[@]}"; do
      if [ ! -z "$gpu" ]; then
        annotate_node_silently "$node" "gpu-model.$i" "$gpu"
        echo "Annotated node $node with GPU model $i: $gpu"
        i=$((i+1))
      fi
    done
  fi
}

# Function to process a single node
process_node() {
  local node=$1
  local cleanup_cpu=$2
  local cleanup_gpu=$3
  
  echo "Processing node: $node"
  
  # Get CPU model
  cpu_model=$(get_cpu_model $node $cleanup_cpu)
  annotate_cpu $node "$cpu_model"
  
  # Get GPU model(s)
  gpu_model=$(get_gpu_info $node $cleanup_gpu)
  annotate_gpu $node "$gpu_model"
  
  echo "Completed processing node: $node"
  echo "-----------------------------------"
}

# Main function
main() {
  local cleanup_cpu="true"
  local cleanup_gpu="true"
  
  # Parse command line options
  while [[ $# -gt 0 ]]; do
    case $1 in
      --keep-cpu-pods)
        cleanup_cpu="false"
        shift
        ;;
      --keep-gpu-pods)
        cleanup_gpu="false"
        shift
        ;;
      --keep-pods)
        cleanup_cpu="false"
        cleanup_gpu="false"
        shift
        ;;
      *)
        shift
        ;;
    esac
  done
  
  echo "Starting hardware detection and annotation for all nodes..."
  echo "This will add '${ANNOTATION_PREFIX}/cpu-model' and/or '${ANNOTATION_PREFIX}/gpu-model' annotations"
  echo "These annotations enable more efficient power profiling in the compute-gardener-scheduler"
  
  if [ "$cleanup_cpu" == "false" ]; then
    echo "CPU debug pods will be kept for inspection"
  fi
  
  if [ "$cleanup_gpu" == "false" ]; then
    echo "GPU debug pods will be kept for inspection"
  fi
  
  # Check user confirmation
  read -p "Do you want to continue? (y/n) " -n 1 -r
  echo    # New line
  if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Operation cancelled."
    exit 1
  fi
  
  # Process all nodes
  for node in $(get_nodes); do
    process_node $node $cleanup_cpu $cleanup_gpu
  done
  
  echo "Hardware annotation process completed successfully!"
  echo "The compute-gardener-scheduler will automatically use these annotations for power profiling."
}

# Run with the first argument or default behavior
if [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
  echo "Usage: $0 [node-name] [options]"
  echo ""
  echo "Options:"
  echo "  --keep-cpu-pods    Keep CPU debug pods after execution (for inspection)"
  echo "  --keep-gpu-pods    Keep GPU debug pods after execution (for inspection)"
  echo "  --keep-pods        Keep all debug pods after execution (for inspection)"
  echo ""
  echo "If node-name is provided, only that node will be processed."
  echo "Otherwise, all nodes in the cluster will be processed."
  exit 0
elif [[ "$1" != -* ]] && [ ! -z "$1" ]; then
  # First argument is a node name
  echo "Processing single node: $1"
  node="$1"
  shift
  
  # Parse command line options
  cleanup_cpu="true"
  cleanup_gpu="true"
  
  while [[ $# -gt 0 ]]; do
    case $1 in
      --keep-cpu-pods)
        cleanup_cpu="false"
        shift
        ;;
      --keep-gpu-pods)
        cleanup_gpu="false"
        shift
        ;;
      --keep-pods)
        cleanup_cpu="false"
        cleanup_gpu="false"
        shift
        ;;
      *)
        shift
        ;;
    esac
  done
  
  process_node $node $cleanup_cpu $cleanup_gpu
else
  main "$@"
fi
