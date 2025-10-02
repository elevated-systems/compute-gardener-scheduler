#!/usr/bin/env python3
"""
Simple PyTorch ResNet50 training script
Demonstrates an ~1 hour GPU training job suitable for carbon-aware scheduling
"""
import torch
import torch.nn as nn
import torch.optim as optim
import torchvision
import torchvision.transforms as transforms
import torchvision.models as models
from torch.utils.data import DataLoader
import time
import os
from datetime import datetime

def print_status(message):
    """Print status with timestamp"""
    timestamp = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    print(f"[{timestamp}] {message}", flush=True)

def get_data_loaders(batch_size=64, num_workers=2):
    """Create CIFAR-100 data loaders (more complex than CIFAR-10)"""
    # Reduced num_workers from 4 to 2 to avoid shared memory issues
    transform_train = transforms.Compose([
        transforms.RandomCrop(32, padding=4),
        transforms.RandomHorizontalFlip(),
        transforms.Resize(224),  # ResNet expects 224x224
        transforms.ToTensor(),
        transforms.Normalize((0.5071, 0.4867, 0.4408), 
                           (0.2675, 0.2565, 0.2761)),
    ])
    
    transform_test = transforms.Compose([
        transforms.Resize(224),
        transforms.ToTensor(),
        transforms.Normalize((0.5071, 0.4867, 0.4408), 
                           (0.2675, 0.2565, 0.2761)),
    ])
    
    # Use CIFAR-100 for longer training time
    trainset = torchvision.datasets.CIFAR100(
        root='/tmp/data', 
        train=True,
        download=True, 
        transform=transform_train
    )
    
    testset = torchvision.datasets.CIFAR100(
        root='/tmp/data', 
        train=False,
        download=True, 
        transform=transform_test
    )
    
    trainloader = DataLoader(
        trainset, 
        batch_size=batch_size,
        shuffle=True, 
        num_workers=num_workers,
        pin_memory=True,
        persistent_workers=True  # Keep workers alive between epochs
    )
    
    testloader = DataLoader(
        testset, 
        batch_size=batch_size,
        shuffle=False, 
        num_workers=num_workers,
        pin_memory=True,
        persistent_workers=True  # Keep workers alive between epochs
    )
    
    return trainloader, testloader

def train_epoch(model, device, train_loader, optimizer, criterion, epoch):
    """Train for one epoch"""
    model.train()
    running_loss = 0.0
    correct = 0
    total = 0
    
    start_time = time.time()
    
    for batch_idx, (inputs, targets) in enumerate(train_loader):
        inputs, targets = inputs.to(device), targets.to(device)
        
        optimizer.zero_grad()
        outputs = model(inputs)
        loss = criterion(outputs, targets)
        loss.backward()
        optimizer.step()
        
        running_loss += loss.item()
        _, predicted = outputs.max(1)
        total += targets.size(0)
        correct += predicted.eq(targets).sum().item()
        
        if batch_idx % 50 == 0:
            print_status(
                f'Epoch {epoch}: [{batch_idx}/{len(train_loader)}] '
                f'Loss: {running_loss/(batch_idx+1):.3f} | '
                f'Acc: {100.*correct/total:.2f}%'
            )
    
    epoch_time = time.time() - start_time
    return running_loss/len(train_loader), 100.*correct/total, epoch_time

def test(model, device, test_loader, criterion):
    """Test the model"""
    model.eval()
    test_loss = 0.0
    correct = 0
    total = 0
    
    with torch.no_grad():
        for inputs, targets in test_loader:
            inputs, targets = inputs.to(device), targets.to(device)
            outputs = model(inputs)
            loss = criterion(outputs, targets)
            
            test_loss += loss.item()
            _, predicted = outputs.max(1)
            total += targets.size(0)
            correct += predicted.eq(targets).sum().item()
    
    return test_loss/len(test_loader), 100.*correct/total

def main():
    """Main training function"""
    print_status("Starting PyTorch ResNet50 training job")
    print_status(f"PyTorch version: {torch.__version__}")
    
    # Check GPU availability
    device = torch.device('cuda' if torch.cuda.is_available() else 'cpu')
    if torch.cuda.is_available():
        print_status(f"Using GPU: {torch.cuda.get_device_name(0)}")
        print_status(f"GPU Memory: {torch.cuda.get_device_properties(0).total_memory / 1e9:.2f} GB")
    else:
        print_status("WARNING: No GPU available, using CPU (will be slow)")
    
    # Hyperparameters
    batch_size = 32  # Smaller batch size to extend training time
    learning_rate = 0.001
    num_epochs = 30  # Adjusted for ~1 hour on RTX 3090
    
    print_status("Loading dataset...")
    train_loader, test_loader = get_data_loaders(batch_size)
    print_status(f"Dataset loaded: {len(train_loader)} training batches")
    
    # Model setup
    print_status("Initializing ResNet50 model...")
    model = models.resnet50(pretrained=False, num_classes=100)  # CIFAR-100 has 100 classes
    model = model.to(device)
    
    # Use mixed precision training for better GPU utilization
    scaler = torch.cuda.amp.GradScaler()
    
    criterion = nn.CrossEntropyLoss()
    optimizer = optim.SGD(
        model.parameters(), 
        lr=learning_rate,
        momentum=0.9, 
        weight_decay=5e-4
    )
    
    # Learning rate scheduler
    scheduler = optim.lr_scheduler.CosineAnnealingLR(
        optimizer, 
        T_max=num_epochs
    )
    
    # Training loop
    print_status(f"Starting training for {num_epochs} epochs...")
    total_start_time = time.time()
    
    best_acc = 0.0
    for epoch in range(1, num_epochs + 1):
        print_status(f"Epoch {epoch}/{num_epochs}")
        
        # Train
        train_loss, train_acc, epoch_time = train_epoch(
            model, device, train_loader, optimizer, criterion, epoch
        )
        
        # Test
        test_loss, test_acc = test(model, device, test_loader, criterion)
        
        # Update learning rate
        scheduler.step()
        
        print_status(
            f'Epoch {epoch} completed in {epoch_time:.1f}s | '
            f'Train Loss: {train_loss:.3f} | Train Acc: {train_acc:.2f}% | '
            f'Test Loss: {test_loss:.3f} | Test Acc: {test_acc:.2f}%'
        )
        
        # Save best model
        if test_acc > best_acc:
            best_acc = test_acc
            torch.save({
                'epoch': epoch,
                'model_state_dict': model.state_dict(),
                'optimizer_state_dict': optimizer.state_dict(),
                'best_acc': best_acc,
            }, '/output/best_model.pth')
            print_status(f'New best model saved with accuracy: {best_acc:.2f}%')
        
        # Save checkpoint every 10 epochs
        if epoch % 10 == 0:
            torch.save({
                'epoch': epoch,
                'model_state_dict': model.state_dict(),
                'optimizer_state_dict': optimizer.state_dict(),
                'train_acc': train_acc,
                'test_acc': test_acc,
            }, f'/output/checkpoint_epoch_{epoch}.pth')
            print_status(f'Checkpoint saved for epoch {epoch}')
    
    # Training completed
    total_time = time.time() - total_start_time
    hours = total_time / 3600
    print_status(f"Training completed in {hours:.2f} hours")
    print_status(f"Best test accuracy: {best_acc:.2f}%")
    
    # Save final model
    torch.save({
        'model_state_dict': model.state_dict(),
        'final_train_acc': train_acc,
        'final_test_acc': test_acc,
        'best_acc': best_acc,
        'total_epochs': num_epochs,
        'total_time_hours': hours,
    }, '/output/final_model.pth')
    
    print_status("Model saved to /output/final_model.pth")
    print_status("Job completed successfully!")
    
    # Print carbon-aware scheduling summary if available
    if 'CG_SCHEDULED_AT' in os.environ:
        print_status(f"This job was scheduled by Compute Gardener at: {os.environ['CG_SCHEDULED_AT']}")
    if 'CG_CARBON_INTENSITY' in os.environ:
        print_status(f"Carbon intensity at scheduling: {os.environ['CG_CARBON_INTENSITY']} gCO2/kWh")

if __name__ == "__main__":
    main()
