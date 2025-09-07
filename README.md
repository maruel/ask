# ask

Extremely lightweight yet powerful AI tool.

![ask.gif](https://raw.githubusercontent.com/wiki/maruel/ask/ask.gif)

- Input file analysis: text, images, PDF, audio, videos, etc.
- Generation: [images](#image-generation), [videos](#video-generation).
- Tools:
    - `-web` Web search for anthropic, gemini, openai and perplexity! Use `-web` 🕸️
    - `-shell` Run commands via sandboxing (sandbox-exec on macOS, bubblewrap on linux), mounting the file
      system as read-only. 🧰
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
go run github.com/maruel/ask/cmd/ask@latest \
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


### Text file

➡ Analyse any text file on any provider as long as it fits in the context window. 💡 Set
[`CEREBRAS_API_KEY`](https://cloud.cerebras.ai/platform/).

```bash
ask -f README.md -p cerebras Summarize this document in one sentence
```

This may respond:

> The "ask" tool is an extremely lightweight yet powerful AI tool that supports various providers, file
> analysis, content generation, and additional tools like web search and bash access on Linux.


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


### Web search 🕸️

➡ Tell the model to search the web to answer your question. 💡 Set
[`ANTHROPIC_API_KEY`](https://console.anthropic.com/settings/keys).

```bash
ask -p anthropic -web \
    "Why is paid parental leave missing in certain advanced economies?"
```

This works with anthropic, gemini, openai and perplexity!


### Bash & zsh 🧰

➡ Leverage [shelltool](http://pkg.go.dev/github.com/maruel/genaitools/shelltool) to enable the model to run
commands locally without network access nor write access. Available on
macOS and Linux. 💡 Set [`CEREBRAS_API_KEY`](https://cloud.cerebras.ai/platform/).

```bash
time ask -shell -p cerebras "Read README.md then summarize it in two sentences"
```

This may output:

> The "ask" tool is a lightweight and versatile AI utility that supports multiple providers, file analysis
> (including images, audio, and video), content generation, and integrated tools like web search and local
> command execution via sandboxing. It works across Windows, macOS, and Linux, offers fast performance, and
> allows easy customization through environment variables and system prompts.
> 
> real    0m0,784s
>
> user    0m0,040s
>
> sys     0m0,050s

*784ms total*; that was on macOS.

⚠ Works on macOS and Linux. This enables the model to read most files on your computer. Write access is denied
and network is disallowed. So the damage is limited but this can still send secrets to the LLM.


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
ask -shell "Can you make a summary of the file named README.md?"
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


### HTTP session recording and playback

➡ Save the HTTP record as a YAML file and play it back.

For example, first record the session, then play it back the second time.

```bash
$ time ask -p cerebras -m qwen-3-235b-a22b-instruct-2507 -record file.yaml tell a good joke
Sure! Here's a clean and classic one:

Why don't skeletons fight each other?

Because they don’t have the guts! 💀😄

Want another? I've got a whole punchline drawer!

real    0m4,152s
user    0m0,040s
sys     0m0,023s

$ wc -c file.yaml
14336 file.yaml

$ time ask -p cerebras -m qwen-3-235b-a22b-instruct-2507 -record file.yaml tell a good joke
Sure! Here's a clean and classic one:

Why don't skeletons fight each other?

Because they don’t have the guts! 💀😄

Want another? I've got a whole punchline drawer!

real    0m0,018s
user    0m0,009s
sys     0m0,013s
```


### List models

➡ List all available models.

```bash
ask -p anthropic -list-models
```

This may print:

> claude-opus-4-1-20250805: Claude Opus 4.1 (2025-08-05)
>
> claude-opus-4-20250514: Claude Opus 4 (2025-05-22)
>
> claude-sonnet-4-20250514: Claude Sonnet 4 (2025-05-22)
>
> claude-3-7-sonnet-20250219: Claude Sonnet 3.7 (2025-02-24)
>
> claude-3-5-sonnet-20241022: Claude Sonnet 3.5 (New) (2024-10-22)
>
> claude-3-5-haiku-20241022: Claude Haiku 3.5 (2024-10-22)
>
> claude-3-5-sonnet-20240620: Claude Sonnet 3.5 (Old) (2024-06-20)
>
> claude-3-haiku-20240307: Claude Haiku 3 (2024-03-07)
>
> claude-3-opus-20240229: Claude Opus 3 (2024-02-29)


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
