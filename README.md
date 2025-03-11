# ask

Extremely lightweight Go application to query a LLM API.

Implements support for:
- Anthropic
- Cerebras
- Cloudflare Workers AI
- Cohere
- DeepSeek
- Google's Gemini
- Groq
- HuggingFace
- Mistral
- OpenAI
- Perplexity

Features are in flux and will break regularly.

Supports using an input file for content analysis, e.g. a picture.

As of March 2025, the following services offer a free tier (other limits
apply):

- [Cerebras](https://cerebras.ai/inference) has unspecified "generous" free tier
- [Cloudflare Workers AI](https://developers.cloudflare.com/workers-ai/platform/pricing/) about 10k tokens/day
- [Cohere](https://docs.cohere.com/docs/rate-limits) (1000 RPCs/month)
- [Google's Gemini](https://ai.google.dev/gemini-api/docs/rate-limits) 0.25qps, 1m tokens/month
- [Groq](https://console.groq.com/docs/rate-limits) 0.5qps, 500k tokens/day
- [HuggingFace](https://huggingface.co/docs/api-inference/pricing) 10¢/month
- [Mistral](https://help.mistral.ai/en/articles/225174-what-are-the-limits-of-the-free-tier) 1qps, 1B tokens/month
- Running [llama.cpp](https://github.com/ggml-org/llama.cpp) locally is free. :)


## Installation

Install [Go](https://go.dev/dl) and run:

```bash
go install github.com/maruel/ask/cmd/ask@latest
```

## Usage

```bash
ask -provider groq -model qwen-qwq-32b "Which is the best Canadian city? Be decisive."

ask -provider groq -model qwen-qwq-32b -sys "You have an holistic knowledge of the world. You reply with the style of William Zinsser and the wit of Dorothy Parker." "Why is the sky blue?"

ask -provider gemini -model gemini-1.5-flash-002 -sys "You are an expert at analysing pictures." -content banana.jpg "What is this? Is it ripe?"
```

## Providers

`ask` is based on [maruel/genai](http://github.com/maruel/genai).

## Models

Snapshot of all the supported models: [MODELS.md](MODELS.md).
