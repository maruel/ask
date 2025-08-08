// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

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
	"unicode"

	"github.com/maruel/ask/internal"
	"github.com/maruel/genai"
	"github.com/maruel/genai/adapters"
	"github.com/maruel/genai/base"
	"github.com/maruel/genai/providers"
	"github.com/maruel/roundtrippers"
)

type stringsFlag []string

func (s *stringsFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func (s *stringsFlag) String() string {
	return strings.Join(([]string)(*s), ", ")
}

func listProviderGen() []string {
	var names []string
	for name, f := range providers.Available() {
		c, err := f(&genai.OptionsProvider{Model: base.NoModel}, nil)
		if err != nil {
			continue
		}
		if _, ok := c.(genai.ProviderGen); ok {
			names = append(names, name)
		} else if _, ok := c.(genai.ProviderGenDoc); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func loadProviderGen(provider, remote, model string, wrapper func(http.RoundTripper) http.RoundTripper) (genai.ProviderGen, error) {
	f := providers.All[provider]
	if f == nil {
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
	c, err := f(&genai.OptionsProvider{Model: model, Remote: remote}, wrapper)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to provider %q: %w", provider, err)
	}
	p, ok := c.(genai.ProviderGen)
	if !ok {
		c, ok := p.(genai.ProviderGenDoc)
		if !ok {
			return nil, fmt.Errorf("provider %q doesn't implement genai.ProviderGen", provider)
		}
		p = &adapters.ProviderGenDocToGen{ProviderGenDoc: c}
	}
	if s, ok := c.(genai.ProviderScoreboard); ok {
		id := c.ModelID()
		for _, sc := range s.Scoreboard().Scenarios {
			if slices.Contains(sc.Models, id) {
				if sc.ThinkingTokenStart != "" {
					p = &adapters.ProviderGenThinking{ProviderGen: p, ThinkingTokenStart: sc.ThinkingTokenStart, ThinkingTokenEnd: sc.ThinkingTokenEnd}
				}
				break
			}
		}
	}
	return p, nil
}

func mainImpl() error {
	ctx, stop := internal.Init()
	defer stop()

	names := listProviderGen()
	verbose := flag.Bool("v", false, "verbose")
	provider := flag.String("provider", "", "backend to use: "+strings.Join(names, ", "))
	remote := flag.String("remote", "", "URL to use, useful for local backend")
	model := flag.String("model", "", "model to use, defaults to a cheap model; use either the model ID or PREFERRED_GOOD and PREFERRED_SOTA to automatically select better models")
	systemPrompt := flag.String("sys", "", "system prompt to use")
	var files stringsFlag
	flag.Var(&files, "f", "file(s) to analyze; it can be a text file, a PDF or an image; can be specified multiple times")
	flag.Parse()
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
	if *model == "" {
		// Default to a cheap model instead of good.
		*model = base.PreferredCheap
	}
	c, err := loadProviderGen(*provider, *remote, *model, wrapper)
	if err != nil {
		return err
	}
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
		return errors.New("need a prompt or input files")
	}
	opts := genai.OptionsText{SystemPrompt: *systemPrompt}
	chunks := make(chan genai.ContentFragment)
	end := make(chan struct{})
	go func() {
		start := true
		hasLF := false
		for {
			select {
			case <-ctx.Done():
				goto end
			case pkt, ok := <-chunks:
				if !ok {
					goto end
				}
				if start {
					pkt.TextFragment = strings.TrimLeftFunc(pkt.TextFragment, unicode.IsSpace)
					start = false
				}
				if pkt.TextFragment != "" {
					hasLF = strings.ContainsRune(pkt.TextFragment, '\n')
				}
				_, _ = os.Stdout.WriteString(pkt.TextFragment)
			}
		}
	end:
		if !hasLF {
			_, _ = os.Stdout.WriteString("\n")
		}
		close(end)
	}()
	res, err := c.GenStream(ctx, msgs, chunks, &opts)
	close(chunks)
	<-end
	for _, c := range res.Contents {
		if c.Document != nil {
			n := c.GetFilename()
			fmt.Printf("- Writing %s\n", n)
			d, err2 := io.ReadAll(c.Document)
			if err2 != nil {
				return err2
			}
			if err = os.WriteFile(n, d, 0o644); err != nil {
				return err
			}
		}
		if c.URL != "" {
			fmt.Printf("- Result URL: %s\n", c.URL)
		}
	}
	return err
}

func main() {
	if err := mainImpl(); err != nil {
		if err != context.Canceled {
			fmt.Fprintf(os.Stderr, "ask: %s\n", err)
		}
		os.Exit(1)
	}
}
