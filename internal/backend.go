// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package internal

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/maruel/genai"
	"github.com/maruel/genai/anthropic"
	"github.com/maruel/genai/cerebras"
	"github.com/maruel/genai/cloudflare"
	"github.com/maruel/genai/cohere"
	"github.com/maruel/genai/deepseek"
	"github.com/maruel/genai/gemini"
	"github.com/maruel/genai/groq"
	"github.com/maruel/genai/huggingface"
	"github.com/maruel/genai/llamacpp"
	"github.com/maruel/genai/mistral"
	"github.com/maruel/genai/openai"
	"github.com/maruel/genai/perplexity"
)

var Providers = []string{
	"anthropic",
	"cerebras",
	"cloudflare",
	"cohere",
	"deepseek",
	"gemini",
	"groq",
	"huggingface",
	"mistral",
	"openai",
	"perplexity",
}

type Provider interface {
	genai.ProviderChat
	genai.ProviderModel
}

type fakeModel struct {
	genai.ProviderChat
}

func (f *fakeModel) ListModels(ctx context.Context) ([]genai.Model, error) {
	return nil, nil
}

func GetBackend(provider, model string) (Provider, error) {
	switch provider {
	case "anthropic":
		if model == "" {
			// https://docs.anthropic.com/en/docs/about-claude/models/all-models
			//*model = "claude-3-7-sonnet-20250219"
			model = "claude-3-5-haiku-20241022"
		}
		slog.Info("main", "provider", provider, "model", model)
		return anthropic.New("", model, nil)
	case "cerebras":
		if model == "" {
			model = "llama3.1-8b"
		}
		slog.Info("main", "provider", provider, "model", model)
		return cerebras.New("", model, nil)
	case "cloudflare":
		if model == "" {
			model = "@cf/qwen/qwen1.5-1.8b-chat"
		}
		return cloudflare.New("", "", model, nil)
	case "cohere":
		if model == "" {
			// https://docs.cohere.com/v2/docs/models
			model = "command-r7b-12-2024"
		}
		slog.Info("main", "provider", provider, "model", model)
		return cohere.New("", model, nil)
	case "deepseek":
		if model == "" {
			// https://api-docs.deepseek.com/quick_start/pricing
			model = "deepseek-chat"
			// But in the evening "deepseek-reasoner" is the same price.
		}
		slog.Info("main", "provider", provider, "model", model)
		return deepseek.New("", model, nil)
	case "gemini":
		if model == "" {
			model = "gemini-2.0-flash-lite"
		}
		slog.Info("main", "provider", provider, "model", model)
		return gemini.New("", model, nil)
	case "groq":
		if model == "" {
			model = "qwen-qwq-32b"
			// model = "qwen-2.5-coder-32b"
		}
		slog.Info("main", "provider", provider, "model", model)
		return groq.New("", model, nil)
	case "huggingface":
		if model == "" {
			model = "Qwen/Qwen2.5-1.5B-Instruct"
		}
		slog.Info("main", "provider", provider, "model", model)
		return huggingface.New("", model, nil)
	case "llamacpp":
		if model == "" {
			model = "127.0.0.1:8080"
		}
		slog.Info("main", "provider", provider, "model", model)
		c, err := llamacpp.New(model, nil, nil)
		if err != nil {
			return nil, err
		}
		return &fakeModel{c}, nil
	case "mistral":
		if model == "" {
			model = "ministral-8b-latest"
		}
		slog.Info("main", "provider", provider, "model", model)
		return mistral.New("", model, nil)
	case "openai":
		if model == "" {
			model = "gpt-4o-mini"
		}
		slog.Info("main", "provider", provider, "model", model)
		return openai.New("", model, nil)
	case "perplexity":
		slog.Info("main", "provider", provider, "model", model)
		c, err := perplexity.New("", "sonar", nil)
		if err != nil {
			return nil, err
		}
		return &fakeModel{c}, nil
	}
	return nil, fmt.Errorf("unsupported backend %q", provider)
}
