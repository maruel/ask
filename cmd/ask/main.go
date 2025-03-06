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
	"github.com/maruel/genai/gemini"
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
	provider := flag.String("backend", "gemini", "backend to use")
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
	case "gemini":
		if *model == "" {
			*model = "gemini-2.0-flash-lite"
		}
		slog.Info("main", "model", *model)
		rawKey, err2 := os.ReadFile(path.Join(home, "bin", "gemini_api.txt"))
		if err2 != nil {
			return fmt.Errorf("need API key from ttps://aistudio.google.com/apikey: %w", err2)
		}
		apiKey := strings.TrimSpace(string(rawKey))
		b = &gemini.Client{ApiKey: apiKey, Model: *model}
	default:
		return fmt.Errorf("unsupported backend %q", *provider)
	}
	query := flag.Arg(0)

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
		resp, err = b.QueryContent(ctx, *systemPrompt, query, mimeType, rawContent)
	} else {
		// https://ai.google.dev/gemini-api/docs/file-prompting-strategies?hl=en is pretty good.
		resp, err = b.Query(ctx, *systemPrompt, query)
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
