# LoRA Fine-tuning on Ray with Persistent Storage

This directory contains the configuration for running LoRA fine-tuning of Qwen2.5-Coder-3B-Instruct on a Ray cluster with persistent model storage.

## Components

### 1. Docker Image (`Dockerfile`)
- Base: `rayproject/ray-ml:2.30.0-py310-gpu`
- Upgraded PyTorch 2.2.0 with CUDA 11.8 support
- Upgraded Transformers, PEFT, and Accelerate for modern LLM training

### 2. Training Script (`train_lora.py`)
- Fine-tunes Qwen2.5-Coder-3B-Instruct using LoRA
- Uses cosine learning rate schedule (2e-4 peak LR)
- Saves models to persistent storage with timestamps
- Configured for GPUs with 24GB VRAM (e.g., RTX 3090/4090, A5000, A6000)

### 3. Persistent Storage (`model-storage-pvc.yaml`)
- 20Gi PVC backed by Longhorn
- Stores fine-tuned models at `/mnt/models/`
- Models are timestamped: `qwen2.5-coder-3b-lora_YYYYMMDD_HHMMSS`

### 4. RayJob Manifest (`rayjob-lora.yaml`)
- 1 GPU worker node with PVC mounted
- Compute Gardener scheduler integration
- Auto-cleanup after completion

## Deployment

### Step 1: Create the PVC
```bash
kubectl apply -f model-storage-pvc.yaml
```

### Step 2: Build and push the Docker image
```bash
docker buildx build \
  --platform linux/amd64 \
  --tag dmasselink/ray-ml-modern:2.30.0 --push .
```

### Step 3: Create the training script ConfigMap
```bash
kubectl create configmap llm-finetuning-script \
  --from-file=train_lora.py \
  --namespace=ray-jobs \
  --dry-run=client -o yaml | kubectl apply -f -
```

### Step 4: Submit the RayJob
```bash
kubectl apply -f rayjob-lora.yaml
```

## Accessing Fine-tuned Models

Models are saved to the PVC at `/mnt/models/` with timestamps. To access them:

### Option 1: Mount PVC in a pod
```bash
kubectl run -it --rm model-browser \
  --image=ubuntu:22.04 \
  --overrides='
{
  "spec": {
    "containers": [{
      "name": "model-browser",
      "image": "ubuntu:22.04",
      "command": ["/bin/bash"],
      "stdin": true,
      "tty": true,
      "volumeMounts": [{
        "name": "models",
        "mountPath": "/mnt/models"
      }]
    }],
    "volumes": [{
      "name": "models",
      "persistentVolumeClaim": {
        "claimName": "llm-finetuned-models"
      }
    }]
  }
}' \
  --namespace=ray-jobs
```

### Option 2: Copy model out of PVC
Create a pod that copies the model to a local directory or S3.

## Training Configuration

- **Learning Rate**: 2e-4 (higher for LoRA)
- **Scheduler**: Cosine with warmup (maintains ~10% LR at end)
- **Batch Size**: 2 per GPU, gradient accumulation 4 (effective batch size: 8)
- **Epochs**: 3
- **LoRA Rank**: 16
- **Target Modules**: All attention and MLP projections

## Deploying Fine-tuned Model with vLLM

vLLM provides an OpenAI-compatible API that works seamlessly with Cline and other tools.

### Step 1: Update the LoRA path in vllm-deployment.yaml
```bash
# Find your trained model timestamp
kubectl exec -it deployment/vllm-lora -n ray-jobs -- ls /mnt/models/

# Edit vllm-deployment.yaml and update the --lora-modules line:
# qwen-lora=/mnt/models/qwen2.5-coder-3b-lora_[YOUR_TIMESTAMP]
```

### Step 2: Deploy vLLM
```bash
kubectl apply -f vllm-deployment.yaml
```

### Step 3: Port-forward to access locally
```bash
kubectl port-forward -n ray-jobs svc/vllm-lora 8000:8000
```

### Step 4: Configure Cline to use your fine-tuned model

In Cline settings, configure a custom OpenAI-compatible endpoint:

```json
{
  "apiProvider": "openai-compatible",
  "openAiCompatible": {
    "baseUrl": "http://localhost:8000/v1",
    "apiKey": "dummy-key",
    "modelId": "Qwen/Qwen2.5-Coder-3B-Instruct:qwen-lora"
  }
}
```

**Note**: The model ID format is `base-model:lora-adapter-name`. The `qwen-lora` part matches what you specified in `--lora-modules`.

### Testing the API

```bash
# Test basic completion
curl http://localhost:8000/v1/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "Qwen/Qwen2.5-Coder-3B-Instruct:qwen-lora",
    "prompt": "def fibonacci(n):",
    "max_tokens": 100
  }'

# Test chat completion (for Cline)
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "Qwen/Qwen2.5-Coder-3B-Instruct:qwen-lora",
    "messages": [
      {"role": "user", "content": "Write a Python function to compute fibonacci numbers"}
    ]
  }'
```

## vLLM vs Ollama

**vLLM Advantages:**
- Native LoRA support (no merging needed)
- OpenAI-compatible API out of the box
- Better GPU utilization
- Can serve multiple LoRA adapters simultaneously

**Ollama Advantages:**
- Simpler setup for non-technical users
- Better for CPU-only inference
- Easier model management

For your use case with LoRA fine-tuning on Kubernetes, **vLLM is the better choice**.

## Automatic Cluster Cleanup

The RayJob is configured to automatically clean up the cluster after job completion:

```yaml
shutdownAfterJobFinishes: true
enableInTreeAutoscaling: true
ttlSecondsAfterFinished: 14400  # 4 hours
```

**This means:**
- ✅ Worker and head pods shut down when training completes
- ✅ Compute Gardener properly tracks pod lifecycle
- ✅ Submitter pod remains for log inspection (until TTL expires)
- ✅ No manual cleanup needed

**Verify cleanup:**
```bash
# Watch the cluster shutdown after job completes
kubectl get pods -n ray-jobs -w

# Check RayJob status
kubectl get rayjob -n ray-jobs
```

## Scheduled/Repeated Training Runs

For scheduled training, the cleanest approach is **manual triggering** with unique names:

### Option 1: Manual Trigger Script (Recommended)
```bash
#!/bin/bash
# trigger-training.sh

TIMESTAMP=$(date +%Y%m%d-%H%M%S)

# Create unique RayJob
kubectl apply -f - <<EOF
apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: llm-lora-finetune-${TIMESTAMP}
  namespace: ray-jobs
$(tail -n +7 rayjob-lora.yaml)  # Rest of the RayJob spec
EOF

echo "Started training job: llm-lora-finetune-${TIMESTAMP}"
```

### Option 2: GitOps (Argo CD / Flux)
Use your GitOps tool to create new RayJobs on demand:
- Update the RayJob manifest in git with a new name
- GitOps tool automatically applies it
- Job runs and cleans up automatically

### Option 3: Simple Kubernetes CronJob
If you need scheduled runs, use a CronJob that creates RayJob resources directly:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: schedule-lora-training
  namespace: ray-jobs
spec:
  schedule: "0 2 * * 0"  # Weekly Sunday 2 AM
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: rayjob-creator
          restartPolicy: Never
          containers:
            - name: create-rayjob
              image: bitnami/kubectl:latest
              command: ["/bin/sh", "-c"]
              args:
                - |
                  TIMESTAMP=$(date +%Y%m%d-%H%M%S)
                  kubectl create -f /config/rayjob-template.yaml \
                    --dry-run=client -o yaml | \
                    sed "s/llm-lora-finetune/llm-lora-finetune-${TIMESTAMP}/" | \
                    kubectl apply -f -
              volumeMounts:
                - name: rayjob-template
                  mountPath: /config
          volumes:
            - name: rayjob-template
              configMap:
                name: rayjob-lora-template
```

**Why this is better than inline YAML:**
- RayJob template stored in ConfigMap (version controlled)
- CronJob just modifies the name and applies it
- Uses K8s API properly (not kubectl with heredocs)
- Cleaner separation of concerns

## Notes

- The PVC uses Longhorn storage class by default - adjust `storageClassName` if needed
- Models include both LoRA adapters and full tokenizer
- Training takes ~1 hour on 24GB VRAM GPUs for 5000 examples
- Each fine-tuned model uses ~5-10GB of storage
- vLLM automatically downloads the base model on first startup (~6GB)
- RayJob cleanup happens automatically - no need to manually delete
