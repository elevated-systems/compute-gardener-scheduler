#!/usr/bin/env python3
"""
LoRA Fine-tuning for Qwen2.5-Coder-7B with Carbon-Aware Scheduling

Configured for GPUs with 24GB+ VRAM (e.g., RTX 3090/4090, A5000, A6000)
Store as ConfigMap: kubectl create configmap llm-finetuning-script --from-file=train_lora.py -n ray-jobs
"""

import argparse
import json
import os
import time
from datetime import datetime

import torch
from torch.utils.data import DataLoader
import ray
from ray import train
from ray.train import ScalingConfig, CheckpointConfig
from ray.train.torch import TorchTrainer
from transformers import AutoModelForCausalLM, AutoTokenizer, get_cosine_schedule_with_warmup
from peft import LoraConfig, get_peft_model, TaskType
from datasets import load_dataset


# ============================================================================
# Configuration Constants
# ============================================================================

MODEL_NAME = "Qwen/Qwen2.5-Coder-7B-Instruct"
CACHE_DIR = "/mnt/models/.cache"
MODEL_OUTPUT_DIR = "/mnt/models"

DATASET_NAME = "nvidia/HelpSteer2"
DATASET_SIZE = 5000
TRAIN_SPLIT = 0.8  # 80% train, 20% validation

MAX_SEQUENCE_LENGTH = 512


# ============================================================================
# Data Preparation
# ============================================================================

def format_examples(examples):
    """Format examples into instruction-response pairs"""
    texts = []
    for i in range(len(examples['prompt'])):
        prompt = examples['prompt'][i]
        response = examples['response'][i]
        text = f"### Instruction:\n{prompt}\n\n### Response:\n{response}"
        texts.append(text)
    return texts


def prepare_datasets(tokenizer, batch_size):
    """Load and prepare train/validation datasets with tokenization"""

    print(f"Loading dataset: {DATASET_NAME}...")
    dataset = load_dataset(DATASET_NAME, split=f"train[:{DATASET_SIZE}]")

    # Split into train/validation
    split_dataset = dataset.train_test_split(test_size=1 - TRAIN_SPLIT, seed=42)
    train_dataset = split_dataset["train"]
    eval_dataset = split_dataset["test"]

    print(f"Training examples: {len(train_dataset)}")
    print(f"Validation examples: {len(eval_dataset)}")

    # Tokenization function
    def tokenize_function(examples):
        texts = format_examples(examples)
        return tokenizer(
            texts,
            padding="max_length",
            truncation=True,
            max_length=MAX_SEQUENCE_LENGTH,
        )

    # Tokenize datasets
    tokenized_train = train_dataset.map(
        tokenize_function,
        batched=True,
        remove_columns=train_dataset.column_names
    )

    tokenized_eval = eval_dataset.map(
        tokenize_function,
        batched=True,
        remove_columns=eval_dataset.column_names
    )

    # Set format for PyTorch
    tokenized_train.set_format(type="torch", columns=["input_ids", "attention_mask"])
    tokenized_eval.set_format(type="torch", columns=["input_ids", "attention_mask"])

    # Create DataLoaders
    train_dataloader = DataLoader(tokenized_train, batch_size=batch_size, shuffle=True)
    eval_dataloader = DataLoader(tokenized_eval, batch_size=batch_size, shuffle=False)

    return train_dataloader, eval_dataloader, len(train_dataset), len(eval_dataset)


# ============================================================================
# Model Setup
# ============================================================================

def setup_model_and_tokenizer(lora_config):
    """Load model, apply LoRA, and prepare tokenizer"""

    print(f"\nLoading model: {MODEL_NAME}")
    print(f"Cache directory: {CACHE_DIR}")

    # Load base model in bfloat16 for numerical stability
    model = AutoModelForCausalLM.from_pretrained(
        MODEL_NAME,
        dtype=torch.bfloat16,
        device_map="auto",
        use_cache=False,  # Disable KV cache for training
        cache_dir=CACHE_DIR,
    )

    # Load tokenizer
    tokenizer = AutoTokenizer.from_pretrained(MODEL_NAME, cache_dir=CACHE_DIR)
    tokenizer.pad_token = tokenizer.eos_token
    tokenizer.padding_side = "right"

    # Apply LoRA
    print(f"\nApplying LoRA: r={lora_config['r']}, alpha={lora_config['alpha']}, dropout={lora_config['dropout']}")

    peft_config = LoraConfig(
        task_type=TaskType.CAUSAL_LM,
        inference_mode=False,
        r=lora_config['r'],
        lora_alpha=lora_config['alpha'],
        lora_dropout=lora_config['dropout'],
        target_modules=[
            "q_proj", "k_proj", "v_proj", "o_proj",  # Attention
            "gate_proj", "up_proj", "down_proj",      # MLP
        ],
    )

    model = get_peft_model(model, peft_config)
    model.print_trainable_parameters()

    return model, tokenizer


# ============================================================================
# Training Loop
# ============================================================================

def run_validation(model, eval_dataloader, device):
    """Run validation and return average loss"""
    model.eval()
    total_loss = 0
    num_batches = len(eval_dataloader)

    print(f"Running validation on {num_batches} batches...")

    with torch.no_grad():
        for step, batch in enumerate(eval_dataloader):
            batch = {k: v.to(device) for k, v in batch.items()}
            outputs = model(**batch, labels=batch["input_ids"])
            total_loss += outputs.loss.item()

            # Print progress every 25% or every 100 batches, whichever is more frequent
            progress_interval = min(100, max(1, num_batches // 4))
            if (step + 1) % progress_interval == 0 or (step + 1) == num_batches:
                avg_loss_so_far = total_loss / (step + 1)
                pct = 100 * (step + 1) / num_batches
                print(f"  Validation progress: {step+1}/{num_batches} ({pct:.0f}%) | Avg loss: {avg_loss_so_far:.4f}")

    model.train()
    return total_loss / num_batches


def train_model(model, train_dataloader, eval_dataloader, optimizer, lr_scheduler, config, device):
    """Main training loop with validation"""

    num_epochs = config['num_epochs']
    gradient_accumulation_steps = config['gradient_accumulation_steps']

    print(f"\nStarting training for {num_epochs} epochs...")
    print(f"Gradient accumulation steps: {gradient_accumulation_steps}")
    print(f"Effective batch size: {config['batch_size'] * gradient_accumulation_steps}\n")

    model.train()

    for epoch in range(num_epochs):
        epoch_start = time.time()
        total_loss = 0

        for step, batch in enumerate(train_dataloader):
            # Move batch to device
            batch = {k: v.to(device) for k, v in batch.items()}

            # Forward pass
            outputs = model(**batch, labels=batch["input_ids"])
            loss = outputs.loss / gradient_accumulation_steps

            # NaN detection
            if torch.isnan(loss):
                print(f"⚠️  WARNING: NaN loss at epoch {epoch+1}, step {step} - skipping batch")
                continue

            # Backward pass
            loss.backward()

            # Update weights every N steps
            if (step + 1) % gradient_accumulation_steps == 0:
                torch.nn.utils.clip_grad_norm_(model.parameters(), 1.0)
                optimizer.step()
                lr_scheduler.step()
                optimizer.zero_grad()

            total_loss += loss.item() * gradient_accumulation_steps

            # Log progress every 10 steps
            if step % 10 == 0:
                avg_loss = total_loss / (step + 1)
                elapsed = time.time() - epoch_start
                print(f"Epoch {epoch+1}/{num_epochs} | Step {step}/{len(train_dataloader)} | "
                      f"Loss: {avg_loss:.4f} | LR: {lr_scheduler.get_last_lr()[0]:.2e} | "
                      f"Time: {elapsed:.1f}s")

                # Report to Ray
                train.report({
                    "epoch": epoch,
                    "step": step,
                    "train_loss": avg_loss,
                    "learning_rate": lr_scheduler.get_last_lr()[0],
                })

        # Epoch summary
        avg_train_loss = total_loss / len(train_dataloader)
        avg_val_loss = run_validation(model, eval_dataloader, device)
        epoch_time = time.time() - epoch_start

        print(f"\n✅ Epoch {epoch+1} completed in {epoch_time:.1f}s")
        print(f"   Train Loss: {avg_train_loss:.4f}")
        print(f"   Val Loss:   {avg_val_loss:.4f}\n")

        # Report to Ray
        train.report({
            "epoch": epoch,
            "train_loss": avg_train_loss,
            "validation_loss": avg_val_loss,
        })

    return avg_train_loss, avg_val_loss


# ============================================================================
# Model Saving
# ============================================================================

def save_model(model, tokenizer, config, train_loss, val_loss, train_size, eval_size):
    """Save model, tokenizer, and metrics to persistent storage"""

    if train.get_context().get_world_rank() != 0:
        return  # Only rank 0 saves

    # Create timestamped directory
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    r = config['lora_r']
    alpha = config['lora_alpha']
    lr = config['learning_rate']
    dropout = config['lora_dropout']
    model_dir = f"{MODEL_OUTPUT_DIR}/qwen2.5-coder-7b-r{r}-a{alpha}-lr{lr:.0e}-d{int(dropout*100)}_{timestamp}"

    print(f"\nSaving model to {model_dir}...")
    os.makedirs(model_dir, exist_ok=True)

    # Save model and tokenizer
    model.save_pretrained(model_dir)
    tokenizer.save_pretrained(model_dir)

    # Save metrics
    metrics = {
        "final_train_loss": float(train_loss),
        "final_validation_loss": float(val_loss),
        "hyperparameters": {
            "lora_r": config['lora_r'],
            "lora_alpha": config['lora_alpha'],
            "lora_dropout": config['lora_dropout'],
            "learning_rate": config['learning_rate'],
            "batch_size": config['batch_size'],
            "gradient_accumulation_steps": config['gradient_accumulation_steps'],
            "num_epochs": config['num_epochs'],
        },
        "dataset": {
            "name": DATASET_NAME,
            "train_size": train_size,
            "eval_size": eval_size,
        },
        "timestamp": timestamp,
    }

    with open(os.path.join(model_dir, "metrics.json"), "w") as f:
        json.dump(metrics, f, indent=2)

    print(f"✅ Model saved successfully!")
    print(f"   Final validation loss: {val_loss:.4f}")


# ============================================================================
# Ray Training Function
# ============================================================================

def train_func(config):
    """Main training function executed by Ray workers"""

    # Print worker info
    print(f"\n{'='*60}")
    print(f"Worker {train.get_context().get_world_rank()} starting...")
    print(f"GPU Available: {torch.cuda.is_available()}")
    if torch.cuda.is_available():
        print(f"GPU: {torch.cuda.get_device_name(0)}")
        print(f"VRAM: {torch.cuda.get_device_properties(0).total_memory / 1e9:.2f} GB")
    print(f"{'='*60}\n")

    # Setup device
    device = torch.device("cuda" if torch.cuda.is_available() else "cpu")

    # Load model and tokenizer
    lora_config = {
        'r': config['lora_r'],
        'alpha': config['lora_alpha'],
        'dropout': config['lora_dropout'],
    }
    model, tokenizer = setup_model_and_tokenizer(lora_config)
    model = model.to(device)

    # Prepare datasets
    train_dataloader, eval_dataloader, train_size, eval_size = prepare_datasets(
        tokenizer,
        config['batch_size']
    )

    # Setup optimizer and scheduler
    optimizer = torch.optim.AdamW(
        model.parameters(),
        lr=config['learning_rate'],
        weight_decay=0.01
    )

    num_update_steps_per_epoch = len(train_dataloader) // config['gradient_accumulation_steps']
    num_training_steps = config['num_epochs'] * num_update_steps_per_epoch

    lr_scheduler = get_cosine_schedule_with_warmup(
        optimizer,
        num_warmup_steps=int(0.03 * num_training_steps),
        num_training_steps=num_training_steps,
        num_cycles=0.5  # Ends at ~10% of peak LR
    )

    # Train
    train_loss, val_loss = train_model(
        model, train_dataloader, eval_dataloader,
        optimizer, lr_scheduler, config, device
    )

    # Save
    save_model(model, tokenizer, config, train_loss, val_loss, train_size, eval_size)


# ============================================================================
# Main Entry Point
# ============================================================================

def print_carbon_info():
    """Print carbon-aware scheduling information if available"""
    print(f"\n{'='*60}")
    print(f"🌱 Carbon-Aware Training Session")
    print(f"📅 Started: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")

    if 'CG_SCHEDULED_AT' in os.environ:
        print(f"⏰ Submitted: {os.environ.get('CG_SUBMISSION_TIME', 'unknown')}")
        print(f"🔋 Scheduled: {os.environ['CG_SCHEDULED_AT']}")
        print(f"📊 Carbon Intensity: {os.environ.get('CG_CARBON_INTENSITY', 'unknown')} gCO2/kWh")
    print(f"{'='*60}\n")


def main():
    """Parse arguments and start Ray training"""

    parser = argparse.ArgumentParser(description='Carbon-Aware LoRA Fine-tuning')
    parser.add_argument('--r', type=int, default=16, help='LoRA rank (default: 16)')
    parser.add_argument('--alpha', type=int, default=32, help='LoRA alpha (default: 32)')
    parser.add_argument('--lr', type=float, default=2e-4, help='Learning rate (default: 2e-4)')
    parser.add_argument('--dropout', type=float, default=0.1, help='LoRA dropout (default: 0.1)')
    parser.add_argument('--batch-size', type=int, default=1, help='Batch size per GPU (default: 1)')
    parser.add_argument('--gradient-accumulation-steps', type=int, default=8,
                        help='Gradient accumulation steps (default: 8)')
    parser.add_argument('--num-epochs', type=int, default=3, help='Training epochs (default: 3)')
    args = parser.parse_args()

    # Initialize Ray
    ray.init(address="auto")

    print_carbon_info()
    print(f"Ray cluster resources: {ray.cluster_resources()}")

    # Training configuration
    train_config = {
        "lora_r": args.r,
        "lora_alpha": args.alpha,
        "lora_dropout": args.dropout,
        "batch_size": args.batch_size,
        "learning_rate": args.lr,
        "num_epochs": args.num_epochs,
        "gradient_accumulation_steps": args.gradient_accumulation_steps,
    }

    print(f"\n{'='*60}")
    print(f"Training Configuration:")
    print(f"  Model: {MODEL_NAME}")
    print(f"  Dataset: {DATASET_NAME} ({DATASET_SIZE} examples)")
    print(f"  LoRA: r={train_config['lora_r']}, alpha={train_config['lora_alpha']}, dropout={train_config['lora_dropout']}")
    print(f"  Learning Rate: {train_config['learning_rate']:.2e}")
    print(f"  Batch Size: {train_config['batch_size']} (effective: {train_config['batch_size'] * train_config['gradient_accumulation_steps']})")
    print(f"  Epochs: {train_config['num_epochs']}")
    print(f"{'='*60}\n")

    # Setup Ray trainer
    trainer = TorchTrainer(
        train_func,
        train_loop_config=train_config,
        scaling_config=ScalingConfig(
            num_workers=1,
            use_gpu=True,
            resources_per_worker={"GPU": 1, "CPU": 4}
        ),
        run_config=train.RunConfig(
            name="qwen-lora-finetuning",
            checkpoint_config=CheckpointConfig(num_to_keep=None),  # Disable Ray checkpoints, we save final model manually
        ),
    )

    # Train
    print("🚀 Starting training...\n")
    start_time = time.time()
    result = trainer.fit()

    # Summary
    total_time = (time.time() - start_time) / 3600
    print(f"\n{'='*60}")
    print(f"✅ Training completed in {total_time:.2f} hours!")
    print(f"Final metrics: {result.metrics}")

    if 'CG_CARBON_SAVED' in os.environ:
        print(f"🌿 Carbon saved: {os.environ['CG_CARBON_SAVED']} gCO2")
    print(f"{'='*60}\n")


if __name__ == "__main__":
    main()
