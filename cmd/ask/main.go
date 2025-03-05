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
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/lmittmann/tint"
	"github.com/maruel/ask/gemini"
	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
)

type backend interface {
	Query(ctx context.Context, query string) (string, error)
}

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
	var b backend
	switch *provider {
	case "gemini":
		if *model == "" {
			*model = "gemini-2.0-flash-lite"
		}
		rawKey, err := os.ReadFile(path.Join(home, "bin", "gemini_api.txt"))
		if err != nil {
			return fmt.Errorf("need API key from ttps://aistudio.google.com/apikey: %w", err)
		}
		apiKey := strings.TrimSpace(string(rawKey))
		b = &gemini.Client{ApiKey: apiKey, Model: *model}
	default:
		return fmt.Errorf("unsupported backend %q", *provider)
	}
	query := flag.Arg(0)
	resp, err := b.Query(ctx, query)
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
