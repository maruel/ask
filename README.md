# ask

Extremely lightweight Go application to query a LLM API. Currently supports
Anthropic, DeepSeek, Google's Gemini, Groq and OpenAI. Features are in flux and
will break regularly.

Supports using an input file for content analysis, e.g. a picture.

As of March 2025, the following services offer a free tier (other limits
apply):
- [Cohere](https://docs.cohere.com/docs/rate-limits) (1000 RPCs/month)
- [Google's Gemini](https://ai.google.dev/gemini-api/docs/rate-limits) 0.25qps, 1m tokens/month
- [Groq](https://console.groq.com/docs/rate-limits) 0.5qps, 500k tokens/day
- [Mistral](https://help.mistral.ai/en/articles/225174-what-are-the-limits-of-the-free-tier) 1qps, 1B tokens/month

Running [llama.cpp](https://github.com/ggml-org/llama.cpp) locally is free. :)

## Installation

Install [Go](https://go.dev/dl) and run:

```bash
go install github.com/maruel/ask/cmd/ask@latest
```
