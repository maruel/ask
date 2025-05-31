# ask

Extremely lightweight Go application to query a LLM API.

- Supports using an input file for content analysis, e.g. a picture.
- Supports all providers supported by http://github.com/maruel/genai
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
ask -provider groq -model qwen-qwq-32b "Which is the best Canadian city? Be decisive."

ask -provider groq -model qwen-qwq-32b -sys "You have an holistic knowledge of the world. You reply with the style of William Zinsser and the wit of Dorothy Parker." "Why is the sky blue?"

ask -provider gemini -model gemini-1.5-flash-002 -sys "You are an expert at analysing pictures." -f banana.jpg "What is this? Is it ripe?"
```
