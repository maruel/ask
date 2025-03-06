// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"mime"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/lmittmann/tint"
	"github.com/maruel/genai"
	"github.com/maruel/genai/anthropic"
	"github.com/maruel/genai/deepseek"
	"github.com/maruel/genai/gemini"
	"github.com/maruel/genai/groq"
	"github.com/maruel/genai/openai"
	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
)

func mainImpl() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	defer stop()
	programLevel := &slog.LevelVar{}
	programLevel.Set(slog.LevelError)
	logger := slog.New(tint.NewHandler(colorable.NewColorable(os.Stderr), &tint.Options{
		Level:      programLevel,
		TimeFormat: "15:04:05.000", // Like time.TimeOnly plus milliseconds.
		NoColor:    !isatty.IsTerminal(os.Stderr.Fd()),
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			switch t := a.Value.Any().(type) {
			case string:
				if t == "" {
					return slog.Attr{}
				}
			case bool:
				if !t {
					return slog.Attr{}
				}
			case uint64:
				if t == 0 {
					return slog.Attr{}
				}
			case int64:
				if t == 0 {
					return slog.Attr{}
				}
			case float64:
				if t == 0 {
					return slog.Attr{}
				}
			case time.Time:
				if t.IsZero() {
					return slog.Attr{}
				}
			case time.Duration:
				if t == 0 {
					return slog.Attr{}
				}
			}
			return a
		},
	}))
	slog.SetDefault(logger)
	go func() {
		<-ctx.Done()
		slog.Info("main", "message", "quitting")
	}()
	defer func() {
		if r := recover(); r != nil {
			slog.Error("main", "panic", r)
			panic(r)
		}
	}()

	verbose := flag.Bool("v", false, "verbose")
	provider := flag.String("backend", "google", "backend to use: anthropic, deepseek, google, groq or openai")
	model := flag.String("model", "", "model to use")
	//  and the wit of Dorothy Parker
	// "You are an expert at analysing pictures."
	systemPrompt := flag.String("sys", "You have an holistic knowledge of the world. You reply with the style of William Zinsser.", "system prompt to use")
	content := flag.String("content", "", "file to analyze")
	flag.Parse()
	if flag.NArg() != 1 {
		return errors.New("ask a question")
	}
	if *verbose {
		programLevel.Set(slog.LevelDebug)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	var b genai.Backend
	switch *provider {
	case "anthropic":
		if *model == "" {
			// https://docs.anthropic.com/en/docs/about-claude/models/all-models
			//*model = "claude-3-7-sonnet-20250219"
			*model = "claude-3-5-haiku-20241022"
		}
		rawKey, err2 := os.ReadFile(path.Join(home, "bin", "anthropic_api.txt"))
		if err2 != nil {
			return fmt.Errorf("need API key from https://console.anthropic.com/settings/keys: %w", err2)
		}
		apiKey := strings.TrimSpace(string(rawKey))
		b = &anthropic.Client{ApiKey: apiKey, Model: *model}
	case "deepseek":
		if *model == "" {
			// https://api-docs.deepseek.com/quick_start/pricing
			*model = "deepseek-chat"
			// But in the evening "deepseek-reasoner" is the same price.
		}
		rawKey, err2 := os.ReadFile(path.Join(home, "bin", "deepseek_api.txt"))
		if err2 != nil {
			return fmt.Errorf("need API key from https://platform.deepseek.com/api_keys: %w", err2)
		}
		apiKey := strings.TrimSpace(string(rawKey))
		b = &deepseek.Client{ApiKey: apiKey, Model: *model}
	case "google":
		if *model == "" {
			if *content != "" {
				// 2025-03-06: Until caching is enabled.
				// https://ai.google.dev/gemini-api/docs/models/gemini?hl=en
				*model = "gemini-1.5-flash-002"
			} else {
				*model = "gemini-2.0-flash-lite"
			}
		}
		rawKey, err2 := os.ReadFile(path.Join(home, "bin", "gemini_api.txt"))
		if err2 != nil {
			return fmt.Errorf("need API key from https://aistudio.google.com/apikey: %w", err2)
		}
		apiKey := strings.TrimSpace(string(rawKey))
		b = &gemini.Client{ApiKey: apiKey, Model: *model}
	case "groq":
		if *model == "" {
			*model = "qwen-2.5-coder-32b"
		}
		rawKey, err2 := os.ReadFile(path.Join(home, "bin", "groq_api.txt"))
		if err2 != nil {
			return fmt.Errorf("need API key from https://console.groq.com/keys: %w", err2)
		}
		apiKey := strings.TrimSpace(string(rawKey))
		b = &groq.Client{ApiKey: apiKey, Model: *model}
	case "openai":
		if *model == "" {
			*model = "gpt-4o-mini"
		}
		rawKey, err2 := os.ReadFile(path.Join(home, "bin", "openai_api.txt"))
		if err2 != nil {
			return fmt.Errorf("need API key from https://platform.openai.com/settings/organization/api-keys: %w", err2)
		}
		apiKey := strings.TrimSpace(string(rawKey))
		b = &openai.Client{ApiKey: apiKey, Model: *model}
	default:
		return fmt.Errorf("unsupported backend %q", *provider)
	}
	slog.Info("main", "provider", *provider, "model", *model)
	query := flag.Arg(0)

	msgs := []genai.Message{
		{Content: *systemPrompt, Role: genai.System},
		{Content: query, Role: genai.User},
	}
	resp := ""
	if *content != "" {
		rawContent, err2 := os.ReadFile(*content)
		if err2 != nil {
			return err2
		}
		mimeType := mime.TypeByExtension(filepath.Ext(*content))
		if mimeType == "" {
			mimeType = "text/plain"
		}
		resp, err = b.CompletionContent(ctx, msgs, 0, 0, 0, mimeType, rawContent)
	} else {
		// https://ai.google.dev/gemini-api/docs/file-prompting-strategies?hl=en is pretty good.
		resp, err = b.Completion(ctx, msgs, 0, 0, 0)
	}
	if err != nil {
		return err
	}
	fmt.Println(resp)
	return nil
}

func main() {
	if err := mainImpl(); err != nil {
		if err != context.Canceled {
			fmt.Fprintf(os.Stderr, "ask: %s\n", err)
		}
		os.Exit(1)
	}
}
