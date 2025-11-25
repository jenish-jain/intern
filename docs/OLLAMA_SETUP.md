# Ollama Setup Guide

This guide walks you through setting up Ollama to run local LLMs for code generation with the AI Intern Agent.

## Table of Contents
- [What is Ollama?](#what-is-ollama)
- [Why Use Ollama?](#why-use-ollama)
- [Installation](#installation)
- [Starting Ollama](#starting-ollama)
- [Downloading Models](#downloading-models)
- [Recommended Models](#recommended-models)
- [Configuration](#configuration)
- [Testing Your Setup](#testing-your-setup)
- [Performance Tuning](#performance-tuning)
- [Troubleshooting](#troubleshooting)

---

## What is Ollama?

Ollama is a tool that makes it easy to run large language models locally on your machine. It provides:
- Simple installation and model management
- REST API compatible with OpenAI
- GPU acceleration support (NVIDIA, AMD, Apple Silicon)
- Model quantization for efficient inference

## Why Use Ollama?

**Advantages:**
- ✅ **Free**: No API costs, unlimited usage
- ✅ **Privacy**: Code never leaves your machine
- ✅ **Offline**: Works without internet connection
- ✅ **Fast**: No network latency, instant responses (with good hardware)
- ✅ **Customizable**: Fine-tune models for your specific needs

**Trade-offs:**
- ⚠️ **Hardware**: Requires decent CPU/GPU and RAM (8GB+ recommended)
- ⚠️ **Quality**: May not match Claude Sonnet 4 for complex tasks
- ⚠️ **Setup**: Requires initial configuration

---

## Installation

### macOS

```bash
# Using Homebrew
brew install ollama

# Or download from website
curl -fsSL https://ollama.ai/install.sh | sh
```

### Linux

```bash
# Install script (supports most distributions)
curl -fsSL https://ollama.ai/install.sh | sh

# Or use Docker
docker pull ollama/ollama
```

### Windows

1. Download the installer from [ollama.ai](https://ollama.ai)
2. Run the installer
3. Ollama will start automatically as a Windows service

### Docker

```bash
# Run Ollama in Docker
docker run -d \
  -v ollama:/root/.ollama \
  -p 11434:11434 \
  --name ollama \
  ollama/ollama

# With GPU support (NVIDIA)
docker run -d \
  --gpus=all \
  -v ollama:/root/.ollama \
  -p 11434:11434 \
  --name ollama \
  ollama/ollama
```

---

## Starting Ollama

### macOS/Linux

```bash
# Start the Ollama service
ollama serve
```

The service will run on `http://localhost:11434` by default.

### Windows

Ollama runs as a background service automatically. No manual start needed.

### Docker

```bash
# Start the container
docker start ollama

# Or run interactively
docker run -it --rm -p 11434:11434 ollama/ollama
```

### Verify Ollama is Running

```bash
# Check if Ollama is responding
curl http://localhost:11434/api/version

# Should return something like:
# {"version":"0.1.25"}
```

---

## Downloading Models

Ollama uses a simple `pull` command to download models:

```bash
# Pull a model (e.g., qwen2.5-coder:7b)
ollama pull qwen2.5-coder:7b

# List available models
ollama list

# Remove a model
ollama rm qwen2.5-coder:7b
```

**Note:** Models are large (4-20GB). Ensure you have sufficient disk space.

---

## Recommended Models

### For Code Generation

| Model | Size | RAM Required | Speed | Quality | Best For |
|-------|------|--------------|-------|---------|----------|
| **qwen2.5-coder:7b** | 4.7GB | 8GB | Fast | ⭐⭐⭐⭐ | **Recommended starter** |
| qwen2.5-coder:14b | 9GB | 16GB | Medium | ⭐⭐⭐⭐⭐ | Best balance |
| qwen2.5-coder:32b | 20GB | 32GB | Slow | ⭐⭐⭐⭐⭐ | Highest quality |
| **deepseek-coder:6.7b** | 3.8GB | 8GB | Fast | ⭐⭐⭐⭐ | Alternative option |
| deepseek-coder:33b | 19GB | 32GB | Slow | ⭐⭐⭐⭐⭐ | Professional use |
| codellama:13b | 7.4GB | 16GB | Medium | ⭐⭐⭐ | Legacy option |

### Quick Start Recommendation

**Start with `qwen2.5-coder:7b`** - it offers the best balance of:
- Quality: Great for most coding tasks
- Speed: Fast enough for interactive use
- Resource usage: Runs on most modern laptops

```bash
# Download the recommended model
ollama pull qwen2.5-coder:7b

# Test it
ollama run qwen2.5-coder:7b "Write a hello world function in Go"
```

### Model Variants

Models come in different quantization levels (trading quality for size):
- `:7b` - 7 billion parameters (default quantization)
- `:7b-q4_0` - 4-bit quantization (smaller, faster, lower quality)
- `:7b-q8_0` - 8-bit quantization (larger, better quality)

For most cases, the default quantization (`:7b`) is recommended.

---

## Configuration

Update your `.env` file to use Ollama:

```bash
# Set AI provider to ollama
AI_PROVIDER=ollama

# Ollama server URL (default: http://localhost:11434)
OLLAMA_BASE_URL=http://localhost:11434

# Model to use
OLLAMA_MODEL=qwen2.5-coder:7b

# Optional: You can still keep Anthropic config for fallback
# ANTHROPIC_API_KEY=your-api-key
```

### Using a Remote Ollama Instance

If Ollama is running on a different machine:

```bash
AI_PROVIDER=ollama
OLLAMA_BASE_URL=http://192.168.1.100:11434
OLLAMA_MODEL=qwen2.5-coder:7b
```

---

## Testing Your Setup

### 1. Test Ollama Directly

```bash
# Interactive test
ollama run qwen2.5-coder:7b

# Single prompt test
ollama run qwen2.5-coder:7b "Write a function to reverse a string in Go"
```

### 2. Test API Endpoint

```bash
# Test the API
curl http://localhost:11434/api/generate -d '{
  "model": "qwen2.5-coder:7b",
  "prompt": "Write a hello world function in Go",
  "stream": false
}'
```

### 3. Test with AI Intern Agent

```bash
# Initialize config if needed
make run-init

# Update .env with Ollama config
# Then run the agent
make run
```

The agent will log: `Initialized AI provider provider=ollama`

---

## Performance Tuning

### Hardware Recommendations

**Minimum (usable):**
- CPU: 4 cores
- RAM: 8GB
- Disk: 10GB free space
- Model: qwen2.5-coder:7b

**Recommended (good performance):**
- CPU: 8+ cores
- RAM: 16GB
- GPU: 8GB VRAM (NVIDIA/AMD/Apple M1+)
- Disk: 20GB free space (SSD preferred)
- Model: qwen2.5-coder:14b

**Optimal (best performance):**
- CPU: 16+ cores
- RAM: 32GB+
- GPU: 16GB+ VRAM
- Disk: 50GB free space (NVMe SSD)
- Model: qwen2.5-coder:32b

### GPU Acceleration

**NVIDIA (Linux):**
```bash
# Install CUDA drivers
# Ollama will automatically use GPU if available

# Verify GPU is being used
ollama run qwen2.5-coder:7b
# Watch nvidia-smi in another terminal
```

**Apple Silicon (M1/M2/M3):**
- GPU acceleration is automatic
- Models run very efficiently on Apple Silicon

**AMD (Linux):**
- Ollama supports ROCm for AMD GPUs
- Install ROCm drivers first

### Model Loading

First inference is slower (model loading). Subsequent calls are faster:
- First call: 5-30 seconds (model load + inference)
- Subsequent calls: 1-10 seconds (inference only)

Keep Ollama running to maintain models in memory.

### Quantization Trade-offs

```bash
# Faster, less accurate (4-bit)
ollama pull qwen2.5-coder:7b-q4_0

# Slower, more accurate (8-bit)
ollama pull qwen2.5-coder:7b-q8_0

# Default balance
ollama pull qwen2.5-coder:7b
```

---

## Troubleshooting

### Ollama Not Running

**Error:** `ollama request failed: connection refused`

**Solution:**
```bash
# Check if Ollama is running
curl http://localhost:11434/api/version

# If not, start it
ollama serve

# Or check Docker
docker ps | grep ollama
docker start ollama
```

### Model Not Found

**Error:** `model 'qwen2.5-coder:7b' not found`

**Solution:**
```bash
# List downloaded models
ollama list

# Pull the model
ollama pull qwen2.5-coder:7b
```

### Out of Memory

**Error:** `failed to allocate memory`

**Solution:**
1. Use a smaller model (e.g., `qwen2.5-coder:7b` instead of `:32b`)
2. Use a more quantized model (e.g., `:7b-q4_0`)
3. Close other applications to free RAM
4. Add swap space (Linux)

### Slow Inference

**Issue:** Model takes too long to respond

**Solutions:**
1. **Use GPU:** Ensure GPU drivers are installed
2. **Smaller model:** Try `qwen2.5-coder:7b` instead of larger variants
3. **Reduce context:** Lower `CONTEXT_MAX_FILES` and `CONTEXT_MAX_BYTES`
4. **Keep warm:** Keep Ollama running between requests
5. **SSD:** Install models on SSD, not HDD

### Invalid JSON Output

**Issue:** Model doesn't generate valid JSON

**Solutions:**
1. The agent requests `"format": "json"` from Ollama
2. Try a different model (qwen2.5-coder is more reliable)
3. Check Ollama version (update if old)
4. Reduce output size with `PLAN_MAX_FILES`

### Port Conflict

**Error:** `bind: address already in use`

**Solution:**
```bash
# Check what's using port 11434
lsof -i :11434

# Use a different port
ollama serve --port 11435

# Update config
OLLAMA_BASE_URL=http://localhost:11435
```

---

## Advanced Configuration

### Custom Model Parameters

You can customize model behavior in `internal/ai/agent/ollama/client.go`:

```go
Options: map[string]interface{}{
    "temperature": 0.2,  // Lower = more deterministic (0.0-1.0)
    "top_p":       0.9,  // Nucleus sampling (0.0-1.0)
    "top_k":       40,   // Top-k sampling
    "num_predict": 16000, // Max tokens to generate
    "stop":        []string{}, // Stop sequences
}
```

### Using Different Endpoints

Ollama provides multiple endpoints:
- `/api/generate` - Completion (currently used)
- `/api/chat` - Chat format (alternative)

To switch, modify `internal/ai/agent/ollama/client.go`.

### Remote Ollama Setup

To run Ollama on a separate server:

1. **Server:** Start Ollama with host binding
   ```bash
   OLLAMA_HOST=0.0.0.0:11434 ollama serve
   ```

2. **Client:** Update config
   ```bash
   OLLAMA_BASE_URL=http://your-server-ip:11434
   ```

3. **Security:** Use firewall rules or SSH tunneling for production

---

## Comparison: Ollama vs Anthropic

| Feature | Ollama (Local) | Anthropic Claude |
|---------|----------------|------------------|
| Cost | Free | $3-15 per million tokens |
| Speed | Fast (good hardware) | Very fast |
| Quality | Good (⭐⭐⭐⭐) | Excellent (⭐⭐⭐⭐⭐) |
| Privacy | Complete | API-based |
| Setup | Moderate | Simple (API key) |
| Hardware | Required (8GB+ RAM) | None |
| Offline | Yes | No |

**When to use Ollama:**
- Budget constraints
- Privacy requirements
- High volume usage
- Offline development
- Experimentation

**When to use Anthropic:**
- Need best quality
- Complex reasoning tasks
- No local hardware
- Getting started quickly

---

## Next Steps

1. ✅ Install Ollama
2. ✅ Download a model
3. ✅ Update `.env` configuration
4. ✅ Test with agent
5. 📚 Review [MULTI_PROVIDER_PLAN.md](./MULTI_PROVIDER_PLAN.md) for architecture details

---

## Resources

- **Ollama Website:** https://ollama.ai
- **Model Library:** https://ollama.ai/library
- **GitHub:** https://github.com/ollama/ollama
- **Discord:** https://discord.gg/ollama
- **Qwen2.5-Coder:** https://ollama.ai/library/qwen2.5-coder
- **DeepSeek Coder:** https://ollama.ai/library/deepseek-coder

---

*Last Updated: 2025-11-12*
