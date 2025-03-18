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
	"strings"
	"unicode"

	"github.com/maruel/ask/internal"
	"github.com/maruel/genai/genaiapi"
)

func mainImpl() error {
	ctx, stop := internal.Init()
	defer stop()

	verbose := flag.Bool("v", false, "verbose")
	provider := flag.String("provider", "gemini", "backend to use: "+strings.Join(internal.Providers, ", "))
	model := flag.String("model", "", "model to use")
	systemPrompt := flag.String("sys", "", "system prompt to use")
	content := flag.String("content", "", "file to analyze")
	flag.Parse()
	if flag.NArg() != 1 {
		return errors.New("ask a question")
	}
	if *verbose {
		internal.Level.Set(slog.LevelDebug)
	}
	b, err := internal.GetBackend(*provider, *model)
	if err != nil {
		return err
	}
	query := flag.Arg(0)

	msgs := []genaiapi.Message{}
	if *content != "" {
		f, err2 := os.Open(*content)
		if err2 != nil {
			return err2
		}
		defer f.Close()
		msgs = append(msgs, genaiapi.Message{
			Role:     genaiapi.User,
			Contents: []genaiapi.Content{{Document: f, Filename: f.Name()}},
		})
	}
	msgs = append(msgs, genaiapi.NewTextMessage(genaiapi.User, query))
	opts := genaiapi.CompletionOptions{
		SystemPrompt: *systemPrompt,
	}
	// https://ai.google.dev/gemini-api/docs/file-prompting-strategies?hl=en is pretty good.
	chunks := make(chan genaiapi.MessageFragment)
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
	err = b.CompletionStream(ctx, msgs, &opts, chunks)
	close(chunks)
	<-end
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
