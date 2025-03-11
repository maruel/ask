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
	"path/filepath"
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
	b, err := internal.GetBackend(*provider, *model, *content != "")
	if err != nil {
		return err
	}
	query := flag.Arg(0)

	msgs := []genaiapi.Message{}
	if *systemPrompt != "" {
		msgs = append(msgs, genaiapi.Message{
			Role:    genaiapi.System,
			Type:    genaiapi.Text,
			Content: *systemPrompt,
		})
	}
	msgs = append(msgs, genaiapi.Message{
		Role:    genaiapi.User,
		Type:    genaiapi.Text,
		Content: query,
	})
	resp := ""
	opts := genaiapi.CompletionOptions{}
	if *content != "" {
		rawContent, err2 := os.ReadFile(*content)
		if err2 != nil {
			return err2
		}
		mimeType := mime.TypeByExtension(filepath.Ext(*content))
		if mimeType == "" {
			mimeType = "text/plain"
		}
		resp, err = b.CompletionContent(ctx, msgs, &opts, mimeType, rawContent)
		if resp != "" {
			fmt.Println(resp)
		}
		return err
	}
	// https://ai.google.dev/gemini-api/docs/file-prompting-strategies?hl=en is pretty good.
	words := make(chan string, 10)
	end := make(chan struct{})
	go func() {
		start := true
		hasLF := false
		for {
			select {
			case <-ctx.Done():
				goto end
			case w, ok := <-words:
				if !ok {
					goto end
				}
				if start {
					w = strings.TrimLeftFunc(w, unicode.IsSpace)
					start = false
				}
				if w != "" {
					hasLF = strings.ContainsRune(w, '\n')
				}
				_, _ = os.Stdout.WriteString(w)
			}
		}
	end:
		if !hasLF {
			_, _ = os.Stdout.WriteString("\n")
		}
		close(end)
	}()
	err = b.CompletionStream(ctx, msgs, &opts, words)
	close(words)
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
