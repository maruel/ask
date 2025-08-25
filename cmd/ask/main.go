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
	"maps"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"unicode"

	"github.com/maruel/ask/internal"
	"github.com/maruel/genai"
	"github.com/maruel/genai/adapters"
	"github.com/maruel/genai/providers"
	"github.com/maruel/roundtrippers"
)

type bashArguments struct {
	CommandLine string `json:"command_line"`
}

type stringsFlag []string

func (s *stringsFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func (s *stringsFlag) String() string {
	return strings.Join(([]string)(*s), ", ")
}

func loadProvider(ctx context.Context, provider string, opts *genai.ProviderOptions, wrapper func(http.RoundTripper) http.RoundTripper) (genai.Provider, error) {
	f := providers.All[provider]
	if f == nil {
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
	c, err := f(ctx, opts, wrapper)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to provider %q: %w", provider, err)
	}
	return adapters.WrapThinking(c), nil
}

func mainImpl() error {
	ctx, stop := internal.Init()
	defer stop()

	flag.Usage = func() {
		w := flag.CommandLine.Output()
		fmt.Fprintf(w, "Usage: %s [options] <prompt>\n\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(w, "\nOn linux when bubblewrap (bwrap) is installed, tool calling is enabled with a read-only file system.\n")
		fmt.Fprintf(w, "\nEnvironment variables:\n")
		fmt.Fprintf(w, "  ASK_MODEL:    default value for -model\n")
		fmt.Fprintf(w, "  ASK_PROVIDER: default value for -provider\n")
		fmt.Fprintf(w, "  ASK_REMOTE:   default value for -remote\n")
		fmt.Fprintf(w, "\nUse github.com/maruel/genai/cmd/list-model@latest for a list of available models.\n")
	}
	names := slices.Sorted(maps.Keys(providers.Available(ctx)))
	verbose := flag.Bool("v", false, "verbose")
	provider := flag.String("provider", os.Getenv("ASK_PROVIDER"), "backend to use: "+strings.Join(names, ", "))
	remote := flag.String("remote", os.Getenv("ASK_REMOTE"), "URL to use to access the backend, useful for local model")
	model := flag.String("model", os.Getenv("ASK_MODEL"), "model ID to use, \"PREFERRED_CHEAP\" or \"PREFERRED_SOTA\" to automatically select better models; defaults to a 'good' model")
	noBash := flag.Bool("no-bash", false, "disable bash tool on Ubuntu even if bubblewrap is installed")
	systemPrompt := flag.String("sys", "", "system prompt to use")
	var files stringsFlag
	flag.Var(&files, "f", "file(s) to analyze; it can be a text file, a PDF or an image; can be specified multiple times; can be an URL")
	flag.Parse()
	var wrapper func(http.RoundTripper) http.RoundTripper
	if *verbose {
		internal.Level.Set(slog.LevelDebug)
		wrapper = func(r http.RoundTripper) http.RoundTripper {
			return &roundtrippers.Log{Transport: r, L: slog.Default()}
		}
	}
	if *provider == "" {
		return errors.New("-provider is required")
	}
	var msgs genai.Messages
	// Some models, like Gemma3 on llamacpp, require a strict alternance between user and assistant.
	userMsg := genai.Message{}
	if query := strings.Join(flag.Args(), " "); query != "" {
		userMsg.Requests = append(userMsg.Requests, genai.Request{Text: query})
	}
	for _, n := range files {
		if strings.HasPrefix(n, "http://") || strings.HasPrefix(n, "https://") {
			userMsg.Requests = append(userMsg.Requests, genai.Request{Doc: genai.Doc{URL: n}})
			continue
		}
		f, err2 := os.Open(n)
		if err2 != nil {
			return err2
		}
		defer f.Close()
		userMsg.Requests = append(userMsg.Requests, genai.Request{Doc: genai.Doc{Src: f}})
	}
	if len(userMsg.Requests) == 0 {
		return errors.New("provide a prompt as an argument or input files")
	}
	msgs = append(msgs, userMsg)

	c, err := loadProvider(ctx, *provider, &genai.ProviderOptions{Model: *model, Remote: *remote}, wrapper)
	if err != nil {
		return err
	}
	slog.Info("loaded", "provider", c.Name(), "model", c.ModelID())
	opts := genai.OptionsText{SystemPrompt: *systemPrompt}

	// When bubblewrap is installed, use it to run bash.
	// On Ubuntu, get it with: sudo apt install bubblewrap
	if !*noBash {
		if bwrapPath, err2 := exec.LookPath("bwrap"); err2 == nil {
			opts.Tools = append(opts.Tools, genai.ToolDef{
				Name:        "bash",
				Description: "Runs the requested command via bash on the computer and returns the output",
				Callback: func(ctx context.Context, args *bashArguments) (string, error) {
					v := []string{"--ro-bind", "/", "/", "--tmpfs", "/tmp", "--dev", "/dev", "--proc", "/proc", "--", "bash", "-c", args.CommandLine}
					cmd := exec.CommandContext(ctx, bwrapPath, v...)
					// Increases odds of success on non-English installation.
					cmd.Env = append(os.Environ(), "LANG=C")
					out, err3 := cmd.Output()
					slog.DebugContext(ctx, "bash", "command", args.CommandLine, "output", string(out), "err", err3)
					return string(out), err3
				},
			})
			slog.DebugContext(ctx, "bwrap", "path", bwrapPath)
		} else {
			slog.DebugContext(ctx, "bwrap", "not found", err)
		}
	}

	fragments, finish := adapters.GenStreamWithToolCallLoop(ctx, c, msgs, &opts)
	start := true
	hasLF := false
	for f := range fragments {
		if start {
			f.TextFragment = strings.TrimLeftFunc(f.TextFragment, unicode.IsSpace)
			start = false
		}
		if f.TextFragment != "" {
			hasLF = strings.ContainsRune(f.TextFragment, '\n')
		}
		_, _ = os.Stdout.WriteString(f.TextFragment)
	}
	if !hasLF {
		_, _ = os.Stdout.WriteString("\n")
	}

	newMsgs, usage, err := finish()
	if len(newMsgs) != 0 {
		res := newMsgs[len(newMsgs)-1]
		for _, r := range res.Replies {
			if r.Doc.Src != nil {
				n := r.Doc.GetFilename()
				fmt.Printf("- Writing %s\n", n)
				d, err2 := io.ReadAll(r.Doc.Src)
				if err2 != nil {
					return err2
				}
				if err = os.WriteFile(n, d, 0o644); err != nil {
					return err
				}
			}
			if r.Doc.URL != "" {
				fmt.Printf("- Result URL: %s\n", r.Doc.URL)
			}
		}
	}
	slog.Info("done", "usage", usage)
	return err
}

func main() {
	if err := mainImpl(); err != nil {
		if err != context.Canceled {
			fmt.Fprintf(os.Stderr, "%s: %s\n", os.Args[0], err)
		}
		os.Exit(1)
	}
}
