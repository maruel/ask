// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"fmt"
	"log/slog"
	"os"
	"path"
	"strings"

	"github.com/maruel/genai/anthropic"
	"github.com/maruel/genai/cohere"
	"github.com/maruel/genai/deepseek"
	"github.com/maruel/genai/gemini"
	"github.com/maruel/genai/genaiapi"
	"github.com/maruel/genai/groq"
	"github.com/maruel/genai/mistral"
	"github.com/maruel/genai/openai"
)

func getBackend(provider, model string, hasContent bool) (genaiapi.ChatProvider, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	switch provider {
	case "anthropic":
		if model == "" {
			// https://docs.anthropic.com/en/docs/about-claude/models/all-models
			//*model = "claude-3-7-sonnet-20250219"
			model = "claude-3-5-haiku-20241022"
		}
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			rawKey, err2 := os.ReadFile(path.Join(home, "bin", "anthropic_api.txt"))
			if err2 != nil {
				return nil, fmt.Errorf("need API key from https://console.anthropic.com/settings/keys: %w", err2)
			}
			apiKey = strings.TrimSpace(string(rawKey))
		}
		slog.Info("main", "provider", provider, "model", model)
		return &anthropic.Client{ApiKey: apiKey, Model: model}, nil
	case "cohere":
		if model == "" {
			// https://docs.cohere.com/v2/docs/models
			model = "command-r7b-12-2024"
		}
		apiKey := os.Getenv("COHERE_API_KEY")
		if apiKey == "" {
			rawKey, err2 := os.ReadFile(path.Join(home, "bin", "cohere_api.txt"))
			if err2 != nil {
				return nil, fmt.Errorf("need API key from https://dashboard.cohere.com/api-keys: %w", err2)
			}
			apiKey = strings.TrimSpace(string(rawKey))
		}
		slog.Info("main", "provider", provider, "model", model)
		return &cohere.Client{ApiKey: apiKey, Model: model}, nil
	case "deepseek":
		if model == "" {
			// https://api-docs.deepseek.com/quick_start/pricing
			model = "deepseek-chat"
			// But in the evening "deepseek-reasoner" is the same price.
		}
		apiKey := os.Getenv("DEEPSEEK_API_KEY")
		if apiKey == "" {
			rawKey, err2 := os.ReadFile(path.Join(home, "bin", "deepseek_api.txt"))
			if err2 != nil {
				return nil, fmt.Errorf("need API key from https://platform.deepseek.com/api_keys: %w", err2)
			}
			apiKey = strings.TrimSpace(string(rawKey))
		}
		slog.Info("main", "provider", provider, "model", model)
		return &deepseek.Client{ApiKey: apiKey, Model: model}, nil
	case "gemini":
		if model == "" {
			if hasContent {
				// 2025-03-06: Until caching is enabled.
				// https://ai.google.dev/gemini-api/docs/models/gemini?hl=en
				model = "gemini-1.5-flash-002"
			} else {
				model = "gemini-2.0-flash-lite"
			}
		}
		apiKey := os.Getenv("GEMINI_API_KEY")
		if apiKey == "" {
			rawKey, err2 := os.ReadFile(path.Join(home, "bin", "gemini_api.txt"))
			if err2 != nil {
				return nil, fmt.Errorf("need API key from https://aistudio.google.com/apikey: %w", err2)
			}
			apiKey = strings.TrimSpace(string(rawKey))
		}
		slog.Info("main", "provider", provider, "model", model)
		return &gemini.Client{ApiKey: apiKey, Model: model}, nil
	case "groq":
		if model == "" {
			model = "qwen-qwq-32b"
			// model = "qwen-2.5-coder-32b"
		}
		apiKey := os.Getenv("GROQ_API_KEY")
		if apiKey == "" {
			rawKey, err2 := os.ReadFile(path.Join(home, "bin", "groq_api.txt"))
			if err2 != nil {
				return nil, fmt.Errorf("need API key from https://console.groq.com/keys: %w", err2)
			}
			apiKey = strings.TrimSpace(string(rawKey))
		}
		slog.Info("main", "provider", provider, "model", model)
		return &groq.Client{ApiKey: apiKey, Model: model}, nil
	case "mistral":
		if model == "" {
			model = "ministral-8b-latest"
		}
		apiKey := os.Getenv("MISTRAL_API_KEY")
		if apiKey == "" {
			rawKey, err2 := os.ReadFile(path.Join(home, "bin", "mistral_api.txt"))
			if err2 != nil {
				return nil, fmt.Errorf("need API key from https://console.mistral.ai/api-keys: %w", err2)
			}
			apiKey = strings.TrimSpace(string(rawKey))
		}
		slog.Info("main", "provider", provider, "model", model)
		return &mistral.Client{ApiKey: apiKey, Model: model}, nil
	case "openai":
		if model == "" {
			model = "gpt-4o-mini"
		}
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			rawKey, err2 := os.ReadFile(path.Join(home, "bin", "openai_api.txt"))
			if err2 != nil {
				return nil, fmt.Errorf("need API key from https://platform.openai.com/settings/organization/api-keys: %w", err2)
			}
			apiKey = strings.TrimSpace(string(rawKey))
		}
		slog.Info("main", "provider", provider, "model", model)
		return &openai.Client{ApiKey: apiKey, Model: model}, nil
	}
	return nil, fmt.Errorf("unsupported backend %q", provider)
}
