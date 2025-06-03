// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/maruel/ask/internal"
	"github.com/maruel/genai"
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

func mainImpl() error {
	ctx, stop := internal.Init()
	defer stop()

	verbose := flag.Bool("v", false, "verbose")
	provider := flag.String("provider", "gemini", "backend to use: "+strings.Join(internal.Providers, ", "))
	model := flag.String("model", "", "model to use")
	systemPrompt := flag.String("sys", "", "system prompt to use")
	var files stringsFlag
	flag.Var(&files, "f", "file(s) to analyze; it can be a text file, a PDF or an image; can be specified multiple times")
	flag.Parse()
	r := http.DefaultTransport
	if *verbose {
		internal.Level.Set(slog.LevelDebug)
		r = &roundtrippers.Log{
			Transport: r,
			L:         slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
		}
	}
	b, err := internal.GetBackend(*provider, *model, r)
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
			d, err := io.ReadAll(f)
			if err != nil {
				return err
			}
			msgs = append(msgs, genai.NewTextMessage(genai.User, string(d)))
		} else {
			msgs = append(msgs, genai.Message{
				Role:     genai.User,
				Contents: []genai.Content{{Document: f, Filename: f.Name()}},
			})
		}
	}

	opts := genai.TextOptions{SystemPrompt: *systemPrompt}
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
	res, err := b.GenStream(ctx, msgs, chunks, &opts)
	close(chunks)
	<-end
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
