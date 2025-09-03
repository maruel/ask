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
	"iter"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/maruel/ask/internal"
	"github.com/maruel/ask/internal/shelltool"
	"github.com/maruel/genai"
	"github.com/maruel/genai/adapters"
	"github.com/maruel/genai/httprecord"
	"github.com/maruel/genai/providers"
	"github.com/maruel/roundtrippers"
	"github.com/mattn/go-colorable"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

type stringsFlag []string

func (s *stringsFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func (s *stringsFlag) String() string {
	return strings.Join(([]string)(*s), ", ")
}

func loadProvider(ctx context.Context, provider string, opts *genai.ProviderOptions, wrapper func(http.RoundTripper) http.RoundTripper) (genai.Provider, error) {
	if provider == "" {
		// If there's only one available, use it!
		provs := providers.Available(ctx)
		if len(provs) == 1 {
			for name, cfg := range provs {
				c, err := cfg.Factory(ctx, opts, wrapper)
				if err != nil {
					return nil, fmt.Errorf("failed to connect to provider %q: %w", name, err)
				}
				return adapters.WrapReasoning(c), nil
			}
		}
		if len(provs) == 0 {
			return nil, errors.New("no providers available, make sure to set an FOO_API_KEY env var")
		}
		names := slices.Sorted(maps.Keys(provs))
		return nil, fmt.Errorf("multiple providers available: %s", strings.Join(names, ", "))
	}
	cfg := providers.All[provider]
	if cfg.Factory == nil {
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
	c, err := cfg.Factory(ctx, opts, wrapper)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to provider %q: %w", provider, err)
	}
	return adapters.WrapReasoning(c), nil
}

const (
	reset   = "\x1b[0m"
	hiblack = "\x1b[90m"
)

func Main() error {
	flag.CommandLine.SetOutput(colorable.NewColorableStderr())
	ctx, stop := internal.Init()
	defer stop()

	flag.Usage = func() {
		w := flag.CommandLine.Output()
		fmt.Fprintf(w, "Usage: %s [options] <prompt>\n\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(w, "\nOn linux when bubblewrap (bwrap) is installed, tool calling is enabled with a read-only file system.\n")
		fmt.Fprintf(w, "\nEnvironment variables:\n")
		fmt.Fprintf(w, "  ASK_MODEL:         default value for -model\n")
		fmt.Fprintf(w, "  ASK_PROVIDER:      default value for -provider\n")
		fmt.Fprintf(w, "  ASK_REMOTE:        default value for -remote\n")
		fmt.Fprintf(w, "  ASK_SYSTEM_PROMPT: default value for -sys\n")
		fmt.Fprintf(w, "\nUse github.com/maruel/genai/cmd/list-model@latest for a list of available models.\n")
	}
	// General.
	verbose := flag.Bool("v", false, "verbose logs about metadata and usage")
	quiet := flag.Bool("q", false, "silence the thinking and citations")
	record := flag.String("record", "", "record the HTTP requests in yaml files for inspection in the specified file.")

	// Provider.
	provider := flag.String("p", "", "(alias for -provider)")
	names := slices.Sorted(maps.Keys(providers.Available(ctx)))
	flag.StringVar(provider, "provider", os.Getenv("ASK_PROVIDER"), "backend to use: "+strings.Join(names, ", "))
	remote := flag.String("r", "", "(alias for -remote)")
	flag.StringVar(remote, "remote", os.Getenv("ASK_REMOTE"), "URL to use to access the backend, useful for local model")

	// Commands.
	listModels := flag.Bool("list-models", false, "list available models and exit")

	// Model and modalities.
	modelHelp := fmt.Sprintf("model ID to use, %q or %q to automatically select worse/better models; defaults to a %q model",
		genai.ModelCheap, genai.ModelSOTA, genai.ModelGood)
	model := flag.String("m", "", "(alias for -model)")
	flag.StringVar(model, "model", os.Getenv("ASK_MODEL"), modelHelp)
	modHelp := fmt.Sprintf("comma separated output modalities: %q, %q, %q, %q", genai.ModalityText, genai.ModalityAudio, genai.ModalityImage, genai.ModalityVideo)
	mod := flag.String("modality", "", modHelp)

	// Tools.
	useShell := flag.Bool("shell", false, "enable shell tool")
	useWeb := flag.Bool("web", false, "enable web search tool; may be costly")

	// Inputs.
	systemPrompt := flag.String("sys", os.Getenv("ASK_SYSTEM_PROMPT"), "system prompt to use")
	var files stringsFlag
	flag.Var(&files, "f", "file(s) to analyze; it can be a text file, a PDF or an image; can be specified multiple times; can be an URL")

	flag.Parse()
	if *verbose {
		internal.Level.Set(slog.LevelDebug)
	}
	if *record != "" {
		if !strings.HasSuffix(*record, ".yaml") {
			return errors.New("record must end with .yaml")
		}
		*record = (*record)[:len(*record)-len(".yaml")]
	}
	var rr *recorder.Recorder
	var errRR error
	wrapper := func(h http.RoundTripper) http.RoundTripper {
		if *verbose {
			h = &roundtrippers.Log{Transport: h, Logger: slog.Default()}
		}
		if *record != "" {
			slog.Info("recording", "dir", *record+".yaml")
			rr, errRR = httprecord.New(*record, h)
			h = rr
		}
		return h
	}

	// Load provider.
	provOpts := genai.ProviderOptions{Model: *model, Remote: *remote}
	if *listModels {
		provOpts.Model = genai.ModelNone
	}
	if *mod != "" {
		parts := strings.Split(*mod, ",")
		provOpts.OutputModalities = make(genai.Modalities, len(parts))
		for i, p := range parts {
			provOpts.OutputModalities[i] = genai.Modality(strings.TrimSpace(p))
		}
	}
	c, err := loadProvider(ctx, *provider, &provOpts, wrapper)
	if err != nil {
		return err
	}
	slog.Info("loaded", "provider", c.Name(), "model", c.ModelID())
	if rr != nil {
		defer func() {
			// TODO: Return error instead of logging.
			if err2 := rr.Stop(); err2 != nil {
				slog.Error("failed to stop recorder", "error", err2)
			}
		}()
	}

	if *listModels {
		if len(flag.Args()) != 0 {
			return fmt.Errorf("cannot use -models with arguments")
		}
		if len(files) != 0 {
			return fmt.Errorf("cannot use -models with files")
		}
		if *systemPrompt != "" {
			return fmt.Errorf("cannot use -models with system prompt")
		}
		if *useShell {
			return fmt.Errorf("cannot use -models with -bash")
		}
		if *useWeb {
			return fmt.Errorf("cannot use -models with -web")
		}
		err = printModels(ctx, c)
	} else {
		err = sendRequest(ctx, c, flag.Args(), files, *systemPrompt, *useShell, *useWeb, *quiet)
	}
	if errRR != nil {
		return errRR
	}
	return err
}

func printModels(ctx context.Context, c genai.Provider) error {
	w := colorable.NewColorableStdout()
	mdls, err := c.ListModels(ctx)
	if err != nil {
		return err
	}
	for _, m := range mdls {
		// This is barebone, we'll want a cleaner output. In particular highlight which are CHEAP, GOOD and SOTA.
		fmt.Fprintln(w, m)
	}
	return err
}

func sendRequest(ctx context.Context, c genai.Provider, args []string, files stringsFlag, systemPrompt string, useShell, useWeb, quiet bool) error {
	// Process inputs
	var msgs genai.Messages
	userMsg := genai.Message{}
	if query := strings.Join(args, " "); query != "" {
		userMsg.Requests = append(userMsg.Requests, genai.Request{Text: query})
	}
	for _, n := range files {
		if strings.HasPrefix(n, "http://") || strings.HasPrefix(n, "https://") {
			userMsg.Requests = append(userMsg.Requests, genai.Request{Doc: genai.Doc{URL: n}})
			continue
		}
		f, err := os.Open(n)
		if err != nil {
			return err
		}
		defer f.Close()
		userMsg.Requests = append(userMsg.Requests, genai.Request{Doc: genai.Doc{Src: f}})
	}
	if len(userMsg.Requests) == 0 {
		return errors.New("provide a prompt as an argument or input files")
	}
	msgs = append(msgs, userMsg)
	var opts []genai.Options
	if systemPrompt != "" {
		opts = append(opts, &genai.OptionsText{SystemPrompt: systemPrompt})
	}

	useTools := false
	if useShell {
		if o, err := shelltool.New(false); o != nil {
			useTools = true
			o.WebSearch = useWeb
			opts = append(opts, o)
		} else {
			fmt.Fprintf(os.Stderr, "warning: could not find sandbox: %v\n", err)
		}
	}
	if !useTools && useWeb {
		opts = append(opts, &genai.OptionsTools{WebSearch: true})
	}
	return execRequest(ctx, c, msgs, opts, useTools, quiet)
}

func execRequest(ctx context.Context, c genai.Provider, msgs genai.Messages, opts []genai.Options, useTools, quiet bool) error {
	w := colorable.NewColorableStdout()
	// Send request.
	var fragments iter.Seq[genai.ReplyFragment]
	var finishTools func() (genai.Messages, genai.Usage, error)
	var finishStream func() (genai.Result, error)
	if useTools {
		fragments, finishTools = adapters.GenStreamWithToolCallLoop(ctx, c, msgs, opts...)
	} else {
		fragments, finishStream = c.GenStream(ctx, msgs, opts...)
	}
	mode := "text"
	last := ""
	// TODO: Another better form would be to keep track of the citations and print them at the bottom. That's
	// what most web uis do. Please send a PR to do that.
	for f := range fragments {
		if f.TextFragment != "" {
			if mode != "text" {
				mode = "text"
				if !strings.HasSuffix(last, "\n\n") {
					if !strings.HasSuffix(last, "\n") {
						_, _ = io.WriteString(w, "\n")
					}
					_, _ = io.WriteString(w, "\n")
				}
				_, _ = io.WriteString(w, hiblack+"Answer: "+reset)
			}
			_, _ = io.WriteString(w, f.TextFragment)
			last = f.TextFragment
			continue
		}
		if quiet {
			continue
		}
		if f.ReasoningFragment != "" {
			if mode != "thinking" {
				mode = "thinking"
				if last != "" && !strings.HasSuffix(last, "\n\n") {
					if !strings.HasSuffix(last, "\n") {
						_, _ = io.WriteString(w, "\n")
					}
					_, _ = io.WriteString(w, "\n")
				}
				_, _ = io.WriteString(w, hiblack+"Reasoning: "+reset)
			}
			_, _ = io.WriteString(w, f.ReasoningFragment)
			last = f.ReasoningFragment
			continue
		}
		if !f.Citation.IsZero() {
			if mode != "citation" {
				mode = "citation"
				if last != "" && !strings.HasSuffix(last, "\n\n") {
					if !strings.HasSuffix(last, "\n") {
						_, _ = io.WriteString(w, "\n")
					}
					_, _ = io.WriteString(w, "\n")
				}
				_, _ = io.WriteString(w, hiblack+"Citation:\n"+reset)
			}
			for _, src := range f.Citation.Sources {
				switch src.Type {
				case genai.CitationWeb:
					fmt.Fprintf(w, "  - %s / %s\n", src.Title, src.URL)
				case genai.CitationWebImage:
					fmt.Fprintf(w, "  - Image: %s\n", src.URL)
				case genai.CitationWebQuery, genai.CitationDocument, genai.CitationTool:
				default:
				}
			}
			last = "\n"
			continue
		}
	}
	if !strings.HasSuffix(last, "\n") {
		_, _ = io.WriteString(w, "\n")
	}

	var err error
	msg := genai.Message{}
	var usage genai.Usage
	if finishTools != nil {
		msgs, usage, err = finishTools()
		if len(msgs) != 0 {
			msg = msgs[len(msgs)-1]
		}
	} else {
		var res genai.Result
		res, err = finishStream()
		msg = res.Message
		usage = res.Usage
	}
	// Still process the files even if there was an error.
	for _, r := range msg.Replies {
		if r.Doc.IsZero() {
			continue
		}
		n, err2 := findAvailable(r.Doc.GetFilename())
		if err2 != nil {
			return err2
		}
		fmt.Fprintf(w, "- Writing %s\n", n)

		// The image can be returned as an URL or inline, depending on the provider. Always save it since it won't
		// be available for long.
		var src io.Reader
		if r.Doc.URL != "" {
			req, err3 := c.HTTPClient().Get(r.Doc.URL)
			if err3 != nil {
				return err3
			} else if req.StatusCode != http.StatusOK {
				return fmt.Errorf("got status code %d while retrieving %s", req.StatusCode, r.Doc.URL)
			}
			src = req.Body
			defer req.Body.Close()
		} else {
			src = r.Doc.Src
		}
		b, err2 := io.ReadAll(src)
		if err2 != nil {
			return err2
		}
		if err2 = os.WriteFile(n, b, 0o644); err2 != nil {
			return err2
		}
	}
	slog.Info("done", "usage", usage)
	return err
}

// findAvailable checks if a file with the given name exists, and if so, append an index number.
//
// TODO: O(n²); I'd fail the interview.
func findAvailable(filename string) (string, error) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return filename, nil
	}
	dir := filepath.Dir(filename)
	base := filepath.Base(filename)
	ext := filepath.Ext(base)
	name := base[:len(base)-len(ext)]
	for i := 1; ; i++ {
		newName := fmt.Sprintf("%s_%d%s", name, i, ext)
		newPath := filepath.Join(dir, newName)
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			return newPath, nil
		}
	}
}
