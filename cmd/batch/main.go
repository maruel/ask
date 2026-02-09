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
	"github.com/maruel/genai/providers"
	"github.com/maruel/roundtrippers"
)

func listProviderGenAsync(ctx context.Context) []string {
	var names []string
	for name, cfg := range providers.Available(ctx) {
		c, err := cfg.Factory(ctx)
		if err != nil {
			continue
		}
		if c.Capabilities().GenAsync {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func loadProviderGenAsync(ctx context.Context, provider string, opts ...genai.ProviderOption) (genai.Provider, error) {
	cfg := providers.All[provider]
	if cfg.Factory == nil {
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
	c, err := cfg.Factory(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to provider %q: %w", provider, err)
	}
	if !c.Capabilities().GenAsync {
		return nil, fmt.Errorf("provider %q doesn't support async generation", provider)
	}
	return c, nil
}

type stringsFlag []string

func (s *stringsFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func (s *stringsFlag) String() string {
	return strings.Join([]string(*s), ", ")
}

func cmdEnqueue(args []string) error {
	ctx, stop := internal.Init()
	defer stop()

	names := listProviderGenAsync(ctx)
	verbose := flag.Bool("v", false, "verbose")
	provider := flag.String("provider", "", "backend to use: "+strings.Join(names, ", "))
	model := flag.String("model", "", "model to use, defaults to a cheap model")
	systemPrompt := flag.String("sys", "", "system prompt to use")
	var files stringsFlag
	flag.Var(&files, "f", "file(s) to analyze; it can be a text file, a PDF or an image; can be specified multiple times")
	_ = flag.CommandLine.Parse(args)
	var popts []genai.ProviderOption
	if *verbose {
		internal.Level.Set(slog.LevelDebug)
		popts = append(popts, genai.ProviderOptionTransportWrapper(func(r http.RoundTripper) http.RoundTripper {
			return &roundtrippers.Log{Transport: r, Logger: slog.Default()}
		}))
	}
	if *provider == "" {
		return errors.New("-provider is required")
	}
	if *model == "" {
		*model = string(genai.ModelCheap)
	}
	c, err := loadProviderGenAsync(ctx, *provider, append(popts, genai.ProviderOptionModel(*model))...)
	if err != nil {
		return err
	}

	var msgs genai.Messages
	if query := strings.Join(flag.Args(), " "); query != "" {
		msgs = append(msgs, genai.NewTextMessage(query))
	}
	var closers []io.Closer
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()
	for _, n := range files {
		f, err2 := os.Open(n)
		if err2 != nil {
			return err2
		}
		closers = append(closers, f)
		mimeType := mime.TypeByExtension(filepath.Ext(n))
		if strings.HasPrefix(mimeType, "text/plain") {
			d, err2 := io.ReadAll(f)
			if err2 != nil {
				return err2
			}
			msgs = append(msgs, genai.NewTextMessage(string(d)))
		} else {
			msgs = append(msgs, genai.Message{Requests: []genai.Request{{Doc: genai.Doc{Src: f}}}})
		}
	}
	if len(msgs) == 0 {
		return errors.New("provide a prompt as an argument or input files")
	}
	opts := genai.GenOptionText{SystemPrompt: *systemPrompt}
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

	names := listProviderGenAsync(ctx)
	verbose := flag.Bool("v", false, "verbose")
	poll := flag.Bool("poll", false, "poll until the results become available")
	provider := flag.String("provider", "", "backend to use: "+strings.Join(names, ", "))
	_ = flag.CommandLine.Parse(args)
	if len(flag.Args()) != 1 {
		return errors.New("pass only one argument: the job id")
	}
	job := genai.Job(flag.Args()[0])
	var popts []genai.ProviderOption
	if *verbose {
		internal.Level.Set(slog.LevelDebug)
		popts = append(popts, genai.ProviderOptionTransportWrapper(func(r http.RoundTripper) http.RoundTripper {
			return &roundtrippers.Log{
				Transport: r,
				Logger:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
			}
		}))
	}
	if *provider == "" {
		return errors.New("-provider is required")
	}
	if !slices.Contains(names, *provider) {
		return errors.New("unknown provider")
	}
	c, err := providers.All[*provider].Factory(ctx, popts...)
	if err != nil {
		return err
	}
	if !c.Capabilities().GenAsync {
		return fmt.Errorf("provider %q doesn't support async generation", *provider)
	}

	for {
		res, err := c.PokeResult(ctx, job)
		if err != nil {
			return err
		}
		if *poll && res.Usage.FinishReason == genai.Pending {
			time.Sleep(time.Second)
			continue
		}
		if s := res.String(); s != "" {
			fmt.Printf("%s\n", s)
		}
		for j := range res.Replies {
			c := &res.Replies[j]
			if c.Doc.Src == nil {
				continue
			}
			n := c.Doc.GetFilename()
			fmt.Printf("- Writing %s\n", n)
			d, err := io.ReadAll(c.Doc.Src)
			if err != nil {
				return err
			}
			if err := os.WriteFile(n, d, 0o644); err != nil {
				return err
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
		if !errors.Is(err, context.Canceled) {
			fmt.Fprintf(os.Stderr, "batch: %s\n", err)
		}
		os.Exit(1)
	}
}
