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


## Installation

Install [Go](https://go.dev/dl) and run:

```bash
go install github.com/maruel/ask/cmd/ask@latest
```

## Usage

```bash
ask -provider groq "Which is the best Canadian city? Be decisive."

ask -provider cerebras -model PREFERRED_SOTA -sys "You have an holistic knowledge of the world. You reply with the style of William Zinsser and the wit of Dorothy Parker." "Why is the sky blue?"

ask -provider gemini -model gemini-2.5-flash -sys "You are an expert at analysing pictures." -f banana.jpg "What is this? Is it ripe?"
```
