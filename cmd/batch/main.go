// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Command batch enqueues or retrieve batched job.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/maruel/ask/internal"
	"github.com/maruel/genai"
	"github.com/maruel/genai/base"
	"github.com/maruel/genai/providers"
	"github.com/maruel/roundtrippers"
)

func getProviders() []string {
	var names []string
	for name, f := range providers.All {
		c, err := f("", nil)
		if err != nil {
			continue
		}
		if _, ok := c.(genai.ProviderGenAsync); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

type stringsFlag []string

func (s *stringsFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func (s *stringsFlag) String() string {
	return strings.Join(([]string)(*s), ", ")
}

func cmdEnqueue(args []string) error {
	ctx, stop := internal.Init()
	defer stop()

	names := getProviders()
	verbose := flag.Bool("v", false, "verbose")
	provider := flag.String("provider", "", "backend to use: "+strings.Join(names, ", "))
	model := flag.String("model", "", "model to use, defaults to a cheap model")
	systemPrompt := flag.String("sys", "", "system prompt to use")
	var files stringsFlag
	flag.Var(&files, "f", "file(s) to analyze; it can be a text file, a PDF or an image; can be specified multiple times")
	_ = flag.CommandLine.Parse(args)
	var wrapper func(http.RoundTripper) http.RoundTripper
	if *verbose {
		internal.Level.Set(slog.LevelDebug)
		wrapper = func(r http.RoundTripper) http.RoundTripper {
			return &roundtrippers.Log{
				Transport: r,
				L:         slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
			}
		}
	}
	if *provider == "" {
		return errors.New("-provider is required")
	}
	if !slices.Contains(names, *provider) {
		return errors.New("unknown provider")
	}
	if *model == "" {
		*model = base.PreferredCheap
	}
	b, err := providers.All[*provider](*model, wrapper)
	if err != nil {
		return err
	}
	c := b.(genai.ProviderGenAsync)

	var msgs genai.Messages
	for _, query := range flag.Args() {
		msgs = append(msgs, genai.NewTextMessage(genai.User, query))
	}
	for _, n := range files {
		f, err2 := os.Open(n)
		if err2 != nil {
			return err2
		}
		defer f.Close()
		mimeType := mime.TypeByExtension(filepath.Ext(n))
		if strings.HasPrefix(mimeType, "text/plain") {
			d, err2 := io.ReadAll(f)
			if err2 != nil {
				return err2
			}
			msgs = append(msgs, genai.NewTextMessage(genai.User, string(d)))
		} else {
			msgs = append(msgs, genai.Message{
				Role:     genai.User,
				Contents: []genai.Content{{Document: f, Filename: f.Name()}},
			})
		}
	}
	if len(msgs) == 0 {
		return errors.New("messages are required")
	}
	opts := genai.OptionsText{SystemPrompt: *systemPrompt}
	job, err := c.GenAsync(ctx, msgs, &opts)
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", job)
	return nil
}

func cmdGet(args []string) error {
	ctx, stop := internal.Init()
	defer stop()

	names := getProviders()
	verbose := flag.Bool("v", false, "verbose")
	poll := flag.Bool("poll", false, "poll until the results become available")
	provider := flag.String("provider", "", "backend to use: "+strings.Join(names, ", "))
	_ = flag.CommandLine.Parse(args)
	if len(flag.Args()) != 1 {
		return errors.New("pass only one argument: the job id")
	}
	job := genai.Job(flag.Args()[0])
	var wrapper func(http.RoundTripper) http.RoundTripper
	if *verbose {
		internal.Level.Set(slog.LevelDebug)
		wrapper = func(r http.RoundTripper) http.RoundTripper {
			return &roundtrippers.Log{
				Transport: r,
				L:         slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
			}
		}
	}
	if *provider == "" {
		return errors.New("-provider is required")
	}
	if !slices.Contains(names, *provider) {
		return errors.New("unknown provider")
	}
	b, err := providers.All[*provider]("", wrapper)
	if err != nil {
		return err
	}
	c := b.(genai.ProviderGenAsync)

	for {
		res, err := c.PokeResult(ctx, job)
		if err != nil {
			return err
		}
		if *poll && res.FinishReason == genai.Pending {
			time.Sleep(time.Second)
			continue
		}
		if s := res.AsText(); len(s) != 0 {
			fmt.Printf("%s\n", s)
		}
		for _, c := range res.Contents {
			if c.Document != nil {
				n := c.GetFilename()
				fmt.Printf("- Writing %s\n", n)
				d, err := io.ReadAll(c.Document)
				if err != nil {
					return err
				}
				if err = os.WriteFile(n, d, 0o644); err != nil {
					return err
				}
			}
		}
		return nil
	}
}

func mainImpl() error {
	if len(os.Args) == 1 {
		return errors.New("expected at least one argument; 'enqueue' or 'get'")
	}
	switch os.Args[1] {
	case "enqueue":
		return cmdEnqueue(os.Args[2:])
	case "get":
		return cmdGet(os.Args[2:])
	default:
		return fmt.Errorf("expected 'enqueue' or 'get'; not %q", os.Args[1])
	}
}

func main() {
	if err := mainImpl(); err != nil {
		if err != context.Canceled {
			fmt.Fprintf(os.Stderr, "batch: %s\n", err)
		}
		os.Exit(1)
	}
}
