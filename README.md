# ask

Extremely lightweight yet powerful AI tool.

- Input file analysis: images, PDF, audio, videos, etc.
- Generation: image, videos.
- Tools: websearch, on linux with bubblewrap (`bwrap`) installed, a `bash` tool mounting the file system as
  read-only is provided.
- Works on Windows, macOS and Linux.
- No need to fight with Python or Node.


## TL;DR:

Read a local file and summarizes its content. 💡 Set
[`ANTHROPIC_API_KEY`](https://console.anthropic.com/settings/keys))

```bash
ask -p anthropic -bash \
    "Can you make a summary of the file named README.md?"
```


Generate a picture. 💡 Set [`TOGETHER_API_KEY`](https://api.together.ai/settings/api-keys)

```bash
ask -p togetherai -m black-forest-labs/FLUX.1-schnell-Free \
    "Picture of a dog"
```


Generate a video starting from the image generated above. 💡 Set
[`GEMINI_API_KEY`](https://aistudio.google.com/apikey))

```bash
ask -p gemini -m veo-3.0-fast-generate-preview \
    -f content.jpg \
    "Carton dog playing with a ball on the beach"
```


Have Go install the tool while running it. 💡 Set [`GROQ_API_KEY`](https://console.groq.com/keys)

```bash
go run github.com/maruel/ask@latest \
    -p groq \
    "Give an advice that sounds good but is bad in practice"
```


## Installation

Install [Go](https://go.dev/dl) and run:

```bash
go install github.com/maruel/ask/cmd/...@latest
```

If you'd like to have binary releases, please open an issue.


## Usage


### Simple

➡ Simple usage. Defaults to a good model. 💡 Set [`GROQ_API_KEY`](https://console.groq.com/keys)

```bash
ask -provider groq "Which is the best Canadian city? Be decisive."
```


### Best model

➡ Use the provider's best model with the predefined value `SOTA` and use a system prompt. 💡 Set
[`CEREBRAS_API_KEY`](https://cloud.cerebras.ai/platform/))

```bash
ask -p cerebras -model SOTA \
    -sys "You have an holistic knowledge of the world. You reply with the style of William Zinsser and the wit of Dorothy Parker." \
    "Why is the sky blue?"
```


### Vision

➡ Analyse a picture using vision. 💡 Set [`MISTRAL_API_KEY`](https://console.mistral.ai/api-keys)

```bash
ask -p mistral -m mistral-small-latest \
    -sys "You are an expert at analysing pictures." \
    -f banana.jpg \
    "What is this? Is it ripe?"
```


### Image generation

➡ Generate an image for free. 💡 Set [`TOGETHER_API_KEY`](https://api.together.ai/settings/api-keys)

```bash
ask -p togetherai -m black-forest-labs/FLUX.1-schnell-Free \
    "Picture of a dog"
```


### File by URL

➡ Analyse a file from an URL using vision. 💡 Set
[`OPENAI_API_KEY`](https://platform.openai.com/settings/organization/api-keys)

```bash
ask -p openai \
    -sys "You are an expert at analysing pictures." \
    -f https://upload.wikimedia.org/wikipedia/commons/thumb/8/8a/Banana-Single.jpg/330px-Banana-Single.jpg \
    "What is this? Is it ripe?"
```


### Bash

➡ Leverage `bash` tool to enable the model to read local files and enable verbose logging. Only available on
Linux. 💡 Set [`ANTHROPIC_API_KEY`](https://console.anthropic.com/settings/keys)

```bash
ask -p anthropic -bash -v \
    "Can you make a summary of the file named README.md?"
```

⚠ This only works on Linux. This enables the model to read *anything* on your computer. This is dangerous. A
better solution will be added later.


## Environment variables

➡ Set `ASK_PROVIDER`, `ASK_MODEL` to set default values.

```bash
export ASK_PROVIDER=gemini
export ASK_MODEL=gemini-2.5-flash
ask "Is open source software a good idea?"
```


### Local 🏠️

➡ Use a local model using llama.cpp. [llama-serve](https://github.com/maruel/genai/tree/main/cmd/llama-serve)
takes cares of downloading the binary and the model. Jan is a tool fine tuned version of Qwen 3 4B.

```bash
# Run on your faster computer with at least 16GB of RAM:
go install github.com/maruel/genai/cmd/llama-serve@latest
llama-serve -http 0.0.0.0:8080 -model Menlo/Jan-nano-gguf/jan-nano-4b-Q8_0.gguf -- \
	--temp 0.7 --top-p 0.8 --top-k 20 --min-p 0 --jinja -fa \
    -c 0 --no-warmup --cache-type-k q8_0 --cache-type-v q8_0

# Access this model from your local network:
export ASK_PROVIDER=llamacpp
export ASK_REMOTE=http://my-server.local:8080
ask "Can you make a summary of the file named README.md?"
```


### Local Vision

➡ Use a vision enabled local model using llama.cpp.
[llama-serve](https://github.com/maruel/genai/tree/main/cmd/llama-serve) takes cares of downloading the binary
and the model files. It is critical to pass the mmproj file to enable vision. Gemma 3 4B is a Google created
model with vision.

```bash
# Run on your faster computer with at least 16GB of RAM:
go install github.com/maruel/genai/cmd/llama-serve@latest
llama-serve -http 0.0.0.0:8080 \
    -model ggml-org/gemma-3-4b-it-GGUF/gemma-3-4b-it-Q8_0.gguf#mmproj-model-f16.gguf -- \
    --temp 1.0 --top-p 0.95 --top-k 64 --jinja -fa -c 0 --no-warmup

# Access this model from your local network:
export ASK_PROVIDER=llamacpp
export ASK_REMOTE=http://my-server.local:8080
ask -f https://upload.wikimedia.org/wikipedia/commons/thumb/c/ce/Flag_of_Iceland.svg/330px-Flag_of_Iceland.svg.png \
    "What is this?"
```


## Providers

Supports all providers supported by [github.com/maruel/genai](https://github.com/maruel/genai):

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
