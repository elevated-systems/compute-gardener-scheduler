# Dockerfile for Ray ML with modern transformers support
FROM rayproject/ray-ml:2.30.0-py310-gpu

# Uninstall old PyTorch from base image (2.0.1+cu118)
RUN pip uninstall -y torch torchvision torchaudio || true

# Install PyTorch 2.2.0 with CUDA 11.8 support from official PyTorch index
# transformers>=4.43.0 requires PyTorch>=2.2 for pytree API compatibility
RUN pip install --no-cache-dir \
    torch==2.2.0 torchvision==0.17.0 torchaudio==2.2.0 \
    --index-url https://download.pytorch.org/whl/cu118

# Now install/upgrade ML packages for Qwen2.5 support
RUN pip install --no-cache-dir \
    transformers>=4.43.0 \
    peft>=0.10.0 \
    accelerate>=0.28.0 \
    --upgrade

# Set working directory
WORKDIR /app
