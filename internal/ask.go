// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package internal

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
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

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

func AskMainImpl() error {
	ctx, stop := Init()
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
	names := slices.Sorted(maps.Keys(providers.Available(ctx)))
	verbose := flag.Bool("v", false, "verbose")
	provider := flag.String("p", "", "(alias for -provider)")
	flag.StringVar(provider, "provider", os.Getenv("ASK_PROVIDER"), "backend to use: "+strings.Join(names, ", "))

	remote := flag.String("r", "", "(alias for -remote)")
	flag.StringVar(remote, "remote", os.Getenv("ASK_REMOTE"), "URL to use to access the backend, useful for local model")

	modelHelp := fmt.Sprintf("model ID to use, %q or %q to automatically select worse/better models; defaults to a %q model",
		genai.ModelCheap, genai.ModelSOTA, genai.ModelGood)
	model := flag.String("m", "", "(alias for -model)")
	flag.StringVar(model, "model", os.Getenv("ASK_MODEL"), modelHelp)

	modHelp := fmt.Sprintf("comma separated output modalities: %q, %q, %q, %q", genai.ModalityText, genai.ModalityAudio, genai.ModalityImage, genai.ModalityVideo)
	mod := flag.String("modality", "", modHelp)

	useBash := flag.Bool("bash", false, "enable bash tool; requires bubblewrap to mount a read-only file system")
	systemPrompt := flag.String("sys", os.Getenv("ASK_SYSTEM_PROMPT"), "system prompt to use")
	var files stringsFlag
	flag.Var(&files, "f", "file(s) to analyze; it can be a text file, a PDF or an image; can be specified multiple times; can be an URL")
	flag.Parse()
	var wrapper func(http.RoundTripper) http.RoundTripper
	if *verbose {
		Level.Set(slog.LevelDebug)
		wrapper = func(r http.RoundTripper) http.RoundTripper {
			return &roundtrippers.Log{Transport: r, Logger: slog.Default()}
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

	provOpts := genai.ProviderOptions{Model: *model, Remote: *remote}
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
	var opts []genai.Options
	if *systemPrompt != "" {
		opts = append(opts, &genai.OptionsText{SystemPrompt: *systemPrompt})
	}

	useTools := false
	// When bubblewrap is installed, use it to run bash.
	// On Ubuntu, get it with: sudo apt install bubblewrap
	if *useBash {
		if bwrapPath, err2 := exec.LookPath("bwrap"); err2 == nil {
			useTools = true
			o := &genai.OptionsText{
				Tools: []genai.ToolDef{
					{
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
					},
				},
			}
			opts = append(opts, o)
			slog.DebugContext(ctx, "bwrap", "path", bwrapPath)
		} else {
			slog.DebugContext(ctx, "bwrap", "not found", err)
		}
	}

	var fragments iter.Seq[genai.ReplyFragment]
	var finishTools func() (genai.Messages, genai.Usage, error)
	var finishStream func() (genai.Result, error)
	if useTools {
		fragments, finishTools = adapters.GenStreamWithToolCallLoop(ctx, c, msgs, opts...)
	} else {
		fragments, finishStream = c.GenStream(ctx, msgs, opts...)
	}
	start := true
	hasLF := false
	for f := range fragments {
		if start {
			f.TextFragment = strings.TrimLeftFunc(f.TextFragment, unicode.IsSpace)
			start = false
		}
		if f.TextFragment != "" {
			hasLF = strings.ContainsRune(f.TextFragment, '\n')
			_, _ = os.Stdout.WriteString(f.TextFragment)
		}
	}
	if !hasLF {
		_, _ = os.Stdout.WriteString("\n")
	}

	msg := genai.Message{}
	var usage genai.Usage
	if finishTools != nil {
		msgs, usage, err = finishTools()
		if len(msgs) != 0 {
			msg = msgs[len(msgs)-1]
		}
	} else {
		res := genai.Result{}
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
		fmt.Printf("- Writing %s\n", n)

		// The image can be returned as an URL or inline, depending on the provider. Always save it since it won't
		// be available for long.
		var src io.Reader
		if r.Doc.URL != "" {
			req, err2 := c.HTTPClient().Get(r.Doc.URL)
			if err2 != nil {
				return err2
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
