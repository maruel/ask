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
	"path/filepath"
	"syscall"
	"time"

	"github.com/lmittmann/tint"
	"github.com/maruel/genai"
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
	provider := flag.String("backend", "google", "backend to use: anthropic, cohere, deepseek, google, groq, mistral or openai")
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
	b, err := getBackend(*provider, *model, *content != "")
	if err != nil {
		return err
	}
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
