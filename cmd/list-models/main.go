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
	"sort"

	"github.com/maruel/ask/internal"
)

func mainImpl() error {
	ctx, stop := internal.Init()
	defer stop()

	verbose := flag.Bool("v", false, "verbose")
	provider := flag.String("provider", "", "backend to use: anthropic, cohere, deepseek, gemini, groq, mistral or openai")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	if *verbose {
		internal.Level.Set(slog.LevelDebug)
	}
	b, err := internal.GetBackend(*provider, "", false)
	if err != nil {
		return err
	}
	models, err := b.ListModels(ctx)
	if err != nil {
		return err
	}
	s := make([]string, 0, len(models))
	for _, m := range models {
		s = append(s, m.String())
		// fmt.Printf("  %#v\n", m)
	}
	sort.Strings(s)
	for _, m := range s {
		fmt.Printf("%s\n", m)
	}
	return nil
}

func main() {
	if err := mainImpl(); err != nil {
		if err != context.Canceled {
			fmt.Fprintf(os.Stderr, "list-models: %s\n", err)
		}
		os.Exit(1)
	}
}
