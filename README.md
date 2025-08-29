# ask

Extremely lightweight yet powerful AI tool.

- Input file analysis: images, PDF, audio, videos, etc.
- Generation: [images](#image-generation), [videos](#video-generation).
- Tools: websearch, on linux with bubblewrap (`bwrap`) installed, a `bash` tool mounting the file system as
  read-only is provided.
- Works on Windows, macOS and Linux.
- No need to fight with Python or Node.
- For short prompts:
   - 2x faster than `claude -p` with Claude Sonnet 4 (1.5s vs >3s)
   - 250ms~350ms operation on cerebras (qwen-3-coder-480b) and groq (openai/gpt-oss-120b)


## Installation

Get the binaries from [github.com/maruel/ask/releases](https://github.com/maruel/ask/releases)

From sources: Install [Go](https://go.dev/dl), then run:

```bash
go install github.com/maruel/ask/cmd/...@latest
```


Have Go install the tool while running it. 💡 Set [`GROQ_API_KEY`](https://console.groq.com/keys).

```bash
go run github.com/maruel/ask@latest \
    -p groq \
    "Give an advice that sounds good but is bad in practice"
```


## Usage


### Simple

➡ Simple usage. Defaults to a good model. 💡 Set [`GROQ_API_KEY`](https://console.groq.com/keys).

```bash
ask -provider groq "Which is the best Canadian city? Be decisive."
```

This may respond:

> A question that sparks debate! After careful consideration, I'm ready to make a definitive call. The best
> Canadian city is... **Vancouver**!
>
> Here's why:
>
> (...)


### Best model

➡ Use the provider's best model with the predefined value `SOTA` and use a system prompt. 💡 Set
[`DEEPSEEK_API_KEY`](https://platform.deepseek.com/api_keys).

```bash
ask -p deepseek -model SOTA \
    -sys "You have an holistic knowledge of the world. You reply with the style of William Zinsser and the wit of Dorothy Parker." \
    "Why is the sky blue?"
```

This may respond:

> Well, my dear, if you must know, the sky is blue because the universe is something of a show-off. (...)


## Environment variables

➡ Set `ASK_PROVIDER`, `ASK_MODEL`, `ASK_SYSTEM_PROMPT` (and a few more) to set default values.
💡 Set [`GEMINI_API_KEY`](https://aistudio.google.com/apikey).

```bash
export ASK_PROVIDER=gemini
export ASK_MODEL=gemini-2.5-flash
export ASK_SYSTEM_PROMPT="You are an expert at software engineering."
ask "Is open source software a good idea?"
```


### Image generation

➡ Generate an image for free. 💡 Set [`TOGETHER_API_KEY`](https://api.together.ai/settings/api-keys).

```bash
ask -p togetherai -m black-forest-labs/FLUX.1-schnell-Free \
    "Cartoon of a dog on the beach"
```

This may respond:

> - Writing content.jpg

![dog.jpg](https://raw.githubusercontent.com/wiki/maruel/ask/dog.jpg)


### Video generation

➡ Generate a video starting from the image generated above. 💡 Set
[`GEMINI_API_KEY`](https://aistudio.google.com/apikey).

```bash
ask -p gemini -m veo-3.0-fast-generate-preview \
    -f content.jpg \
    "Dog playing on the beach with fishes jumping out of the water"
```

This may respond:

> - Writing content.mp4

![dog.avif](https://raw.githubusercontent.com/wiki/maruel/ask/dog.avif)

🎬️ See the video with sound 🔊: [dog.mp4](https://raw.githubusercontent.com/wiki/maruel/ask/dog.mp4)


### Vision

➡ Analyse a picture using vision. 💡 Set [`MISTRAL_API_KEY`](https://console.mistral.ai/api-keys).

```bash
ask -p mistral -m mistral-small-latest \
    -sys "You are an expert at analysing pictures." \
    -f content.jpg \
    "What is this? Where is it? Reply succinctly."
```

This may respond:

> This is a cartoon dog. It is on a beach.


### File by URL

➡ Analyse a file from an URL using vision. 💡 Set
[`OPENAI_API_KEY`](https://platform.openai.com/settings/organization/api-keys).

```bash
ask -p openai \
    -sys "You are an expert at analysing pictures." \
    -f https://upload.wikimedia.org/wikipedia/commons/thumb/8/8a/Banana-Single.jpg/330px-Banana-Single.jpg \
    "What is this? Is it ripe?"
```

![banana.jpg](https://upload.wikimedia.org/wikipedia/commons/thumb/8/8a/Banana-Single.jpg/330px-Banana-Single.jpg)

This may respond:

> That’s a banana. The peel is mostly yellow with only a few tiny brown flecks, so it’s ripe and ready to eat now.
>
> Notes:
> - If you prefer a firmer, less sweet banana, wait until it has a little green at the stem.
> - If you like it sweeter/softer, wait for brown spots to appear.
> - To speed ripening, put it in a paper bag with an apple; to slow it, refrigerate (the peel will darken but
>   the fruit stays fine).


### Bash

➡ Leverage `bash` tool to enable the model to read local files. Only available on
Linux. 💡 Set [`ANTHROPIC_API_KEY`](https://console.anthropic.com/settings/keys).

```bash
ask -p anthropic -bash \
    "Can you make a summary of the file named README.md?"
```

This may output:

> A lightweight AI tool that supports multiple providers, file analysis (images, PDF, audio, video), content
> generation, and includes tools like websearch and bash access on Linux.

⚠ This only works on Linux. This enables the model to read *anything* on your computer. This is dangerous. A
better solution will be added later.


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
ask -bash "Can you make a summary of the file named README.md?"
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
