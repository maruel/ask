# ask

Extremely lightweight Go application to query a LLM API.

- Supports using input files for content analysis, e.g. images, PDF, audio, videos, etc.
- Supports all providers supported by [github.com/maruel/genai](https://github.com/maruel/genai)
    - Anthropic
    - Cerebras
    - Cloudflare Workers AI
    - Cohere
    - DeepSeek
    - Google's Gemini
    - Groq
    - HuggingFace
    - llama.cpp
    - Mistral
    - Ollama
    - OpenAI
    - Perplexity
    - Pollinations
    - TogetherAI

On linux with bubblewrap (`bwrap`) installed, a `bash` tool mounting the file system as read-only is provided.

## Installation

Install [Go](https://go.dev/dl) and run:

```bash
go install github.com/maruel/ask/cmd/ask@latest
```

## Usage

### Simple

➡ Simple usage. Defaults to a good model.

```bash
ask -provider groq "Which is the best Canadian city? Be decisive."
```

💡 Set `GROQ_API_KEY` (get it at [console.groq.com/keys](https://console.groq.com/keys)) for Groq.

### Best model

➡ Use the provider's best model with the predefined value `PREFERRED_SOTA` and use a system prompt.

```bash
ask -provider cerebras -model PREFERRED_SOTA \
    -sys "You have an holistic knowledge of the world. You reply with the style of William Zinsser and the wit of Dorothy Parker." \
    "Why is the sky blue?"
```

💡 Set `CEREBRAS_API_KEY` (get it at [cloud.cerebras.ai/platform/](https://cloud.cerebras.ai/platform/)) for
Cerebras.

### Vision

➡ Analyse a file using vision. Use `ASK_PROVIDER` and `ASK_MODEL` environment variables to set default provider
and models.

```bash
export ASK_PROVIDER=gemini
export ASK_MODEL=gemini-2.5-flash
ask -sys "You are an expert at analysing pictures." -f banana.jpg "What is this? Is it ripe?"
```

💡 Set `GEMINI_API_KEY` (get it at [aistudio.google.com/apikey](https://aistudio.google.com/apikey)) for
Google's Gemini.

### File by URL

➡ Analyse a file from an URL using vision.

```bash
ask -provider openai \
    -sys "You are an expert at analysing pictures." \
    -f https://upload.wikimedia.org/wikipedia/commons/thumb/8/8a/Banana-Single.jpg/330px-Banana-Single.jpg \
    "What is this? Is it ripe?"
```

💡 Set `OPENAI_API_KEY` (get it at
[platform.openai.com/settings/organization/api-keys](https://platform.openai.com/settings/organization/api-keys))
for OpenAI.

### Bash

➡ Leverage `bash` tool to enable the model to read local files and enable verbose logging. Only available on Linux.

```bash
ask -provider anthropic -v "Can you make a summary of the file named README.md?"
```

💡 Set `ANTHROPIC_API_KEY` (get it at
[console.anthropic.com/settings/keys](https://console.anthropic.com/settings/keys)) for Anthropic.

⚠ This only works on Linux. This enables the model to read *anything* on your computer. This is dangerous. A
better solution will be added later.

### Local

➡ Use a local model using llama.cpp. llama-serve takes cares of downloading the binary and the model. Jan is a
tool fine tuned version of Qwen 3 4B.

```bash
# Run on your faster computer with at least 16GB of RAM:
go install github.com/maruel/genai/cmd/llama-serve@latest
llama-serve -http 0.0.0.0:8080 -model Menlo/Jan-nano-gguf/jan-nano-4b-Q8_0.gguf -- \
	--temp 0.7 --top-p 0.8 --top-k 20 --min-p 0 --jinja -fa -c 0 --no-warmup --cache-type-k q8_0 --cache-type-v q8_0

# Access this model from your local network:
export ASK_PROVIDER=llamacpp
export ASK_REMOTE=http://my-server.local:8080
ask "Can you make a summary of the file named README.md?"
```

### Local Vision

➡ Use a vision enabled local model using llama.cpp. llama-serve takes cares of downloading the binary and the
model. It is critical to pass the mmproj file to enable vision. Gemma 3 4B is a Google created model with vision.

```bash
# Run on your faster computer with at least 16GB of RAM:
go install github.com/maruel/genai/cmd/llama-serve@latest
llama-serve -http 0.0.0.0:8080 -model ggml-org/gemma-3-4b-it-GGUF/gemma-3-4b-it-Q8_0.gguf#mmproj-model-f16.gguf -- \
    --temp 1.0 --top-p 0.95 --top-k 64 --jinja -fa -c 0 --no-warmup

# Access this model from your local network:
export ASK_PROVIDER=llamacpp
export ASK_REMOTE=http://my-server.local:8080
ask -f https://upload.wikimedia.org/wikipedia/commons/thumb/c/ce/Flag_of_Iceland.svg/330px-Flag_of_Iceland.svg.png "What is this?"
```
