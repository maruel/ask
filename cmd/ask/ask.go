// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// CLI flag parsing, provider selection, and streaming output.

package main

import (
	"context"
	"encoding/json"
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
	"github.com/maruel/genai"
	"github.com/maruel/genai/adapters"
	"github.com/maruel/genai/httprecord"
	"github.com/maruel/genai/providers"
	"github.com/maruel/genai/scoreboard"
	"github.com/maruel/genai/subprocessrecord"
	"github.com/maruel/genaitools/shelltool"
	"github.com/maruel/roundtrippers"
	"github.com/mattn/go-colorable"
	"golang.org/x/term"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

type stringsFlag []string

func (s *stringsFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func (s *stringsFlag) String() string {
	return strings.Join([]string(*s), ", ")
}

func loadProvider(ctx context.Context, provider string, opts ...genai.ProviderOption) (genai.Provider, error) {
	if provider == "" {
		provs := providers.Available(ctx)
		if len(provs) == 0 {
			return nil, errors.New("no providers available, make sure to set an FOO_API_KEY env var or install pi/codex/opencode/claude")
		}
		// If there's only one, use it directly.
		if len(provs) == 1 {
			for name, cfg := range provs {
				c, err := cfg.Factory(ctx, filterOpts(cfg.IsCLI, opts)...)
				if err != nil {
					return nil, fmt.Errorf("failed to connect to provider %q: %w", name, err)
				}
				return adapters.WrapReasoning(c), nil
			}
		}
		// Prefer CLI-based providers, then first alphabetically.
		order := append([]string{"pi", "codex", "opencode", "claudecode"}, slices.Sorted(maps.Keys(provs))...)
		for _, name := range order {
			cfg, ok := provs[name]
			if !ok {
				continue
			}
			c, err := cfg.Factory(ctx, filterOpts(cfg.IsCLI, opts)...)
			if err != nil {
				slog.Debug("provider skipped", "provider", name, "error", err)
				continue
			}
			return adapters.WrapReasoning(c), nil
		}
		return nil, errors.New("no providers could be loaded with the given options")
	}
	cfg := providers.All[provider]
	if cfg.Factory == nil {
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
	c, err := cfg.Factory(ctx, filterOpts(cfg.IsCLI, opts)...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to provider %q: %w", provider, err)
	}
	return adapters.WrapReasoning(c), nil
}

// filterOpts returns opts appropriate for the provider kind.
// CLI providers use ProviderOptionStarterWrapper; HTTP providers use ProviderOptionTransportWrapper.
func filterOpts(isCLI bool, opts []genai.ProviderOption) []genai.ProviderOption {
	out := make([]genai.ProviderOption, 0, len(opts))
	for _, o := range opts {
		switch o.(type) {
		case genai.ProviderOptionTransportWrapper:
			if isCLI {
				continue
			}
		case genai.ProviderOptionStarterWrapper:
			if !isCLI {
				continue
			}
		}
		out = append(out, o)
	}
	return out
}

const (
	reset   = "\x1b[0m"
	hiblack = "\x1b[90m"
)

// Main parses the command line and runs the ask command.
func Main() (err error) {
	flag.CommandLine.SetOutput(colorable.NewColorableStderr())
	ctx, stop := internal.Init()
	defer stop()

	flag.Usage = func() {
		w := flag.CommandLine.Output()
		_, _ = fmt.Fprintf(w, "Usage: %s [options] <prompt>\n\n", os.Args[0])
		flag.PrintDefaults()
		_, _ = fmt.Fprintf(w, "\nInput methods:\n")
		_, _ = fmt.Fprintf(w, "  - Prompt argument: ask \"your question\"\n")
		_, _ = fmt.Fprintf(w, "  - Files: ask -f file.txt -f image.jpg \"your question\"\n")
		_, _ = fmt.Fprintf(w, "  - Stdin: cat file.txt | ask \"analyze this\"\n")
		_, _ = fmt.Fprintf(w, "  - URLs: ask -f https://example.com/image.jpg \"what is this?\"\n")
		_, _ = fmt.Fprintf(w, "\nOn macOS, or linux when bubblewrap (bwrap) is installed, tool calling is enabled with a read-only file system.\n")
		_, _ = fmt.Fprintf(w, "\nEnvironment variables:\n")
		_, _ = fmt.Fprintf(w, "  ASK_API_KEY_NAME:  default value for -api-key-name\n")
		_, _ = fmt.Fprintf(w, "  ASK_MODEL:         default value for -model\n")
		_, _ = fmt.Fprintf(w, "  ASK_PROVIDER:      default value for -provider\n")
		_, _ = fmt.Fprintf(w, "  ASK_REMOTE:        default value for -remote\n")
		_, _ = fmt.Fprintf(w, "  ASK_SYSTEM_PROMPT: default value for -sys\n")
		_, _ = fmt.Fprintf(w, "\nPerformance:\n")
		_, _ = fmt.Fprintf(w, "  Model auto detection (%s, %s, %s) requires an HTTP request which will\n", genai.ModelCheap, genai.ModelGood, genai.ModelSOTA)
		_, _ = fmt.Fprintf(w, "  take ~100ms. If you want it to be fast, make sure to specify a model!\n")
	}
	// General.
	versionFlag := flag.Bool("version", false, "print version and exit")
	verbose := flag.Bool("v", false, "verbose logs about metadata and usage")
	quiet := flag.Bool("q", false, "silence the thinking and citations of the text output")
	asJSON := flag.Bool("json", false, "print the result as one JSON object instead of streaming text")
	timeout := flag.Duration("timeout", 0, "overall timeout, e.g. 30s or 5m; 0 means no timeout")
	record := flag.String("record", "", "record the HTTP requests in yaml files for inspection in the specified file.")

	// Provider.
	provider := flag.String("p", "", "(alias for -provider)")
	names := slices.Sorted(maps.Keys(providers.Available(ctx)))
	flag.StringVar(provider, "provider", os.Getenv("ASK_PROVIDER"), "backend to use: "+strings.Join(names, ", "))
	remote := flag.String("r", "", "(alias for -remote)")
	flag.StringVar(remote, "remote", os.Getenv("ASK_REMOTE"), "URL to use to access the backend, useful for local model")
	apiKeyName := flag.String("api-key-name", os.Getenv("ASK_API_KEY_NAME"), "name of the environment variable holding the key sent as \"Authorization: Bearer <key>\", useful with openaicompatible")

	// Commands.
	listModels := flag.Bool("list-models", false, "list available models and exit; scoreboard recommendations for \"CHEAP\", \"GOOD\" and \"SOTA\" are tagged")

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
	// set records the flags set on the command line, so that a default coming from an environment variable
	// does not make -list-models fail.
	set := map[string]bool{}
	flag.Visit(func(f *flag.Flag) {
		set[f.Name] = true
	})
	if *versionFlag {
		fmt.Println(version())
		return nil
	}
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}
	if *verbose {
		internal.Level.Set(slog.LevelDebug)
	}
	if *record != "" {
		// Strip known extensions; the base is used for both .yaml and .ndjson.
		for _, ext := range []string{".yaml", ".ndjson"} {
			*record = strings.TrimSuffix(*record, ext)
		}
	}
	apiKey := ""
	if *apiKeyName != "" {
		if apiKey = os.Getenv(*apiKeyName); apiKey == "" {
			return fmt.Errorf("environment variable %s named by -api-key-name is empty", *apiKeyName)
		}
	}
	// Validate the incompatible flags before loading a provider, so that a mistake fails without a network
	// request.
	if *listModels {
		if err := validateListModels(set, flag.Args(), files, *useShell, *useWeb); err != nil {
			return err
		}
	}
	var rr *recorder.Recorder
	var errRR error
	var sr *subprocessrecord.Recorder

	// Load provider.
	var provOpts []genai.ProviderOption
	if apiKey != "" || *verbose || *record != "" {
		// HTTP providers.
		provOpts = append(provOpts, genai.ProviderOptionTransportWrapper(func(h http.RoundTripper) http.RoundTripper {
			if apiKey != "" {
				// Innermost so logs and recordings never see the key.
				h = &roundtrippers.Header{Header: http.Header{"Authorization": {"Bearer " + apiKey}}, Transport: h}
			}
			if *verbose {
				h = &roundtrippers.Log{Transport: h, Logger: slog.Default()}
			}
			if *record != "" {
				slog.Info("recording HTTP", "file", *record+".yaml")
				rr, errRR = httprecord.New(*record, h)
				h = rr
			}
			return h
		}))
		// CLI providers.
		var wrappers []genai.ProviderOptionStarterWrapper
		if *verbose {
			wrappers = append(wrappers, func(inner genai.Starter) genai.Starter {
				return func(ctx context.Context, args []string) (io.WriteCloser, io.ReadCloser, func() error, error) {
					slog.Info("subprocess start", "args", args)
					stdin, stdout, wait, err := inner(ctx, args)
					if err != nil {
						return nil, nil, nil, err
					}
					return stdin, &logReader{ReadCloser: stdout}, wait, nil
				}
			})
		}
		if *record != "" {
			var err error
			sr, err = subprocessrecord.New(*record, nil)
			if err != nil {
				return err
			}
			wrappers = append(wrappers, sr.Wrap)
		}
		provOpts = append(provOpts, genai.ProviderOptionStarterWrapper(func(s genai.Starter) genai.Starter {
			for _, w := range wrappers {
				s = w(s)
			}
			return s
		}))
	}
	if *model != "" && !*listModels {
		// Listing models does not select one, so a CHEAP, GOOD or SOTA value must not trigger the model
		// detection of the provider.
		provOpts = append(provOpts, genai.ProviderOptionModel(*model))
	}
	if *remote != "" {
		provOpts = append(provOpts, genai.ProviderOptionRemote(*remote))
	}
	if *mod != "" {
		parts := strings.Split(*mod, ",")
		o := make(genai.Modalities, len(parts))
		for i, p := range parts {
			o[i] = genai.Modality(strings.TrimSpace(p))
		}
		provOpts = append(provOpts, genai.ProviderOptionModalities(o))
	}
	c, err := loadProvider(ctx, *provider, provOpts...)
	if err != nil {
		return err
	}
	slog.Info("loaded", "provider", c.Name(), "model", c.ModelID())
	if rr != nil {
		defer func() {
			if err2 := rr.Stop(); err2 != nil {
				slog.Error("failed to stop HTTP recorder", "error", err2)
			}
		}()
	}
	if sr != nil {
		defer func() {
			if err2 := sr.Stop(); err2 != nil {
				slog.Error("failed to stop subprocess recorder", "error", err2)
			}
		}()
	}

	defer func() { err = errors.Join(err, c.Close()) }()

	if *listModels {
		err = printProviderModels(ctx, c, colorable.NewColorableStdout())
	} else {
		err = sendRequest(ctx, c, flag.Args(), files, requestOptions{
			systemPrompt: *systemPrompt,
			useShell:     *useShell,
			useWeb:       *useWeb,
			quiet:        *quiet,
			asJSON:       *asJSON,
		})
	}
	if errRR != nil {
		return errRR
	}
	return err
}

// validateListModels rejects the flags that make no sense with -list-models.
//
// explicit holds the flags set on the command line, so that a value coming from an environment variable,
// e.g. ASK_SYSTEM_PROMPT, does not make the command fail.
func validateListModels(explicit map[string]bool, args []string, files stringsFlag, useShell, useWeb bool) error {
	switch {
	case len(args) != 0:
		return errors.New("cannot use -list-models with arguments")
	case len(files) != 0:
		return errors.New("cannot use -list-models with files")
	case explicit["sys"]:
		return errors.New("cannot use -list-models with -sys")
	case useShell:
		return errors.New("cannot use -list-models with -shell")
	case useWeb:
		return errors.New("cannot use -list-models with -web")
	}
	return nil
}

// printProviderModels lists the provider's models and prints them.
func printProviderModels(ctx context.Context, c genai.Provider, w io.Writer) error {
	models, err := c.ListModels(ctx)
	if err != nil {
		return err
	}
	s := c.Scoreboard()
	printModels(w, models, &s)
	return nil
}

// printModels prints one line per model, tagging scoreboard recommendations.
func printModels(w io.Writer, models []genai.Model, s *scoreboard.Score) {
	tags := modelTiers(s)
	for _, m := range models {
		if t := tags[m.GetID()]; len(t) != 0 {
			_, _ = fmt.Fprintf(w, "%s  [%s]\n", m, strings.Join(t, ", "))
		} else {
			_, _ = fmt.Fprintln(w, m)
		}
	}
}

// modelTiers maps scoreboard recommendations to their tags, e.g. "good" or "sota image".
//
// Providers with dynamic model selection may choose a different model from their live model list.
func modelTiers(s *scoreboard.Score) map[string][]string {
	out := map[string][]string{}
	for i := range s.Scenarios {
		sc := &s.Scenarios[i]
		var tiers []string
		if sc.Cheap {
			tiers = append(tiers, "cheap")
		}
		if sc.Good {
			tiers = append(tiers, "good")
		}
		if sc.SOTA {
			tiers = append(tiers, "sota")
		}
		mods := outputModalities(sc)
		for _, tier := range tiers {
			if len(mods) != 0 {
				tier += " " + strings.Join(mods, ",")
			}
			for _, m := range sc.Models {
				if !slices.Contains(out[m], tier) {
					out[m] = append(out[m], tier)
				}
			}
		}
	}
	for _, tiers := range out {
		slices.Sort(tiers)
	}
	return out
}

// outputModalities returns the non-text output modalities of a scenario, sorted.
//
// It returns nil for a text scenario, the common case, so that its tag stays short.
func outputModalities(sc *scoreboard.Scenario) []string {
	var mods []string
	for m := range sc.Out {
		if m != scoreboard.ModalityText {
			mods = append(mods, string(m))
		}
	}
	slices.Sort(mods)
	return mods
}

// requestOptions controls how a request is sent and how the result is printed.
type requestOptions struct {
	// systemPrompt is the system prompt to use.
	systemPrompt string
	// useShell enables the shell tool, useTools reports whether its sandbox was found.
	useShell bool
	useTools bool
	// useWeb enables the web search tool.
	useWeb bool
	// quiet silences the reasoning and the citations of the text output.
	quiet bool
	// asJSON prints one JSON object at the end instead of streaming text.
	asJSON bool
}

// hasStdinData reports whether stdin holds data to send with the request.
//
// stdin is skipped when it is a terminal, a character device such as /dev/null, or an empty regular
// file, so that a script or CI run that redirects stdin does not send an empty request.
func hasStdinData(f *os.File) bool {
	if term.IsTerminal(int(f.Fd())) {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return true
	}
	if st.Mode()&os.ModeCharDevice != 0 {
		return false
	}
	return !st.Mode().IsRegular() || st.Size() != 0
}

func sendRequest(ctx context.Context, c genai.Provider, args []string, files stringsFlag, opts requestOptions) error {
	// Process inputs
	msgs := make(genai.Messages, 0, 1)
	userMsg := genai.Message{}
	if query := strings.Join(args, " "); query != "" {
		userMsg.Requests = append(userMsg.Requests, genai.Request{Text: query})
	}
	var closers []io.Closer
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()
	for _, n := range files {
		if strings.HasPrefix(n, "http://") || strings.HasPrefix(n, "https://") {
			userMsg.Requests = append(userMsg.Requests, genai.Request{Doc: genai.Doc{URL: n}})
			continue
		}
		f, err := os.Open(n)
		if err != nil {
			return err
		}
		closers = append(closers, f)
		userMsg.Requests = append(userMsg.Requests, genai.Request{Doc: genai.Doc{Src: f}})
	}
	if hasStdinData(os.Stdin) {
		userMsg.Requests = append(userMsg.Requests, genai.Request{Doc: genai.Doc{Src: os.Stdin}})
	}
	if len(userMsg.Requests) == 0 {
		return errors.New("provide a prompt as an argument or input files")
	}
	msgs = append(msgs, userMsg)
	var genOpts []genai.GenOption
	if opts.systemPrompt != "" {
		genOpts = append(genOpts, &genai.GenOptionText{SystemPrompt: opts.systemPrompt})
	}
	if opts.useShell {
		if o, err := shelltool.New(false); o != nil {
			opts.useTools = true
			genOpts = append(genOpts, o)
		} else {
			fmt.Fprintf(os.Stderr, "warning: could not find sandbox: %v\n", err)
		}
	}
	if opts.useWeb {
		genOpts = append(genOpts, &genai.GenOptionWeb{Search: true})
	}
	return execRequest(ctx, c, msgs, genOpts, opts)
}

func execRequest(ctx context.Context, c genai.Provider, msgs genai.Messages, genOpts []genai.GenOption, opts requestOptions) error {
	w := colorable.NewColorableStdout()
	// Send request.
	var fragments iter.Seq[genai.Reply]
	var finishTools func() (genai.Messages, genai.Usage, error)
	var finishStream func() (genai.Result, error)
	if opts.useTools {
		fragments, finishTools = adapters.GenStreamWithToolCallLoop(ctx, c, msgs, genOpts...)
	} else {
		fragments, finishStream = c.GenStream(ctx, msgs, genOpts...)
	}
	// TODO: Another better form would be to keep track of the citations and print them at the bottom. That's
	// what most web uis do. Please send a PR to do that.
	out := newOutput(w, opts.quiet, opts.asJSON)
	for f := range fragments {
		out.add(&f)
	}
	out.finish()

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
	for i := range msg.Replies {
		r := &msg.Replies[i]
		if r.Doc.IsZero() {
			continue
		}
		n := findAvailable(r.Doc.GetFilename())

		// The image can be returned as an URL or inline, depending on the provider. Always save it since it won't
		// be available for long.
		b, err2 := downloadDoc(ctx, c, r)
		if err2 != nil {
			err = errors.Join(err, err2)
			break
		}
		if err2 := os.WriteFile(n, b, 0o644); err2 != nil {
			err = errors.Join(err, err2)
			break
		}
		out.addFile(n)
	}
	slog.Info("done", "usage", usage)
	if opts.asJSON {
		// The JSON carries the error, but a failed request must still exit non-zero.
		err = errors.Join(err, out.writeJSON(c.Name(), c.ModelID(), &usage, err))
	}
	return err
}

func downloadDoc(ctx context.Context, c genai.Provider, r *genai.Reply) ([]byte, error) {
	if r.Doc.URL == "" {
		return io.ReadAll(r.Doc.Src)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.Doc.URL, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got status code %d while retrieving %s", resp.StatusCode, r.Doc.URL)
	}
	return io.ReadAll(resp.Body)
}

// output renders the replies of a request and accumulates them for -json.
type output struct {
	w      io.Writer
	quiet  bool
	asJSON bool
	// Text rendering state.
	mode string
	last string
	// Collected content.
	text      strings.Builder
	reasoning strings.Builder
	citations []jsonCitation
	seen      map[string]bool
	written   []string
}

// newOutput returns an output printing to w.
func newOutput(w io.Writer, quiet, asJSON bool) *output {
	return &output{w: w, quiet: quiet, asJSON: asJSON, mode: "text"}
}

// add renders a reply, or only accumulates it when printing JSON.
func (o *output) add(f *genai.Reply) {
	if o.asJSON {
		o.text.WriteString(f.Text)
		o.reasoning.WriteString(f.Reasoning)
		for i := range f.Citation.Sources {
			o.addCitation(&f.Citation.Sources[i])
		}
		return
	}
	switch {
	case f.Text != "":
		o.section("text", hiblack+"Answer: "+reset)
		_, _ = io.WriteString(o.w, f.Text)
		o.last = f.Text
	case o.quiet:
		// The reasoning and the citations are silenced.
	case f.Reasoning != "":
		o.section("thinking", hiblack+"Reasoning: "+reset)
		_, _ = io.WriteString(o.w, f.Reasoning)
		o.last = f.Reasoning
	case !f.Citation.IsZero():
		o.section("citation", hiblack+"Citation:\n"+reset)
		for j := range f.Citation.Sources {
			src := &f.Citation.Sources[j]
			switch src.Type {
			case genai.CitationWeb:
				_, _ = fmt.Fprintf(o.w, "  - %s / %s\n", src.Title, src.URL)
			case genai.CitationWebImage:
				_, _ = fmt.Fprintf(o.w, "  - Image: %s\n", src.URL)
			case genai.CitationWebQuery, genai.CitationDocument, genai.CitationTool:
			default:
			}
		}
		o.last = "\n"
	}
}

// section switches the text output to a new section, printing blank lines and a label.
func (o *output) section(mode, label string) {
	if o.mode == mode {
		return
	}
	o.mode = mode
	if o.last != "" && !strings.HasSuffix(o.last, "\n\n") {
		if !strings.HasSuffix(o.last, "\n") {
			_, _ = io.WriteString(o.w, "\n")
		}
		_, _ = io.WriteString(o.w, "\n")
	}
	_, _ = io.WriteString(o.w, label)
}

// addCitation records a source, skipping a duplicate.
func (o *output) addCitation(src *genai.CitationSource) {
	key := citationTypeName(src.Type) + "\x00" + src.Title + "\x00" + src.URL
	if o.seen == nil {
		o.seen = map[string]bool{}
	}
	if o.seen[key] {
		return
	}
	o.seen[key] = true
	o.citations = append(o.citations, jsonCitation{Type: citationTypeName(src.Type), Title: src.Title, URL: src.URL})
}

// addFile records a written file and prints it in the text output.
func (o *output) addFile(name string) {
	if o.asJSON {
		o.written = append(o.written, name)
	} else {
		_, _ = fmt.Fprintf(o.w, "- Writing %s\n", name)
	}
}

// finish terminates the text output.
func (o *output) finish() {
	if o.asJSON || strings.HasSuffix(o.last, "\n") {
		return
	}
	_, _ = io.WriteString(o.w, "\n")
}

// writeJSON prints the accumulated result as one JSON object.
func (o *output) writeJSON(provider, model string, usage *genai.Usage, err error) error {
	r := jsonResult{
		Provider:  provider,
		Model:     model,
		Text:      o.text.String(),
		Reasoning: o.reasoning.String(),
		Citations: o.citations,
		Usage: jsonUsage{
			InputTokens:       usage.InputTokens,
			InputCachedTokens: usage.InputCachedTokens,
			ReasoningTokens:   usage.ReasoningTokens,
			OutputTokens:      usage.OutputTokens,
			TotalTokens:       usage.TotalTokens,
			FinishReason:      string(usage.FinishReason),
		},
		Files: o.written,
	}
	if err != nil {
		r.Error = err.Error()
	}
	b, err2 := json.Marshal(r)
	if err2 != nil {
		return err2
	}
	_, err2 = fmt.Fprintf(o.w, "%s\n", b)
	return err2
}

// jsonResult is the object printed by -json.
type jsonResult struct {
	Provider  string         `json:"provider"`
	Model     string         `json:"model"`
	Text      string         `json:"text"`
	Reasoning string         `json:"reasoning,omitempty"`
	Citations []jsonCitation `json:"citations,omitempty"`
	Usage     jsonUsage      `json:"usage"`
	Files     []string       `json:"files,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// jsonCitation is a source that supports the answer.
type jsonCitation struct {
	Type  string `json:"type"`
	Title string `json:"title,omitempty"`
	URL   string `json:"url,omitempty"`
}

// jsonUsage is the token usage of the request.
type jsonUsage struct {
	InputTokens       int64  `json:"input_tokens"`
	InputCachedTokens int64  `json:"input_cached_tokens,omitempty"`
	ReasoningTokens   int64  `json:"reasoning_tokens,omitempty"`
	OutputTokens      int64  `json:"output_tokens"`
	TotalTokens       int64  `json:"total_tokens"`
	FinishReason      string `json:"finish_reason,omitempty"`
}

// citationTypeName returns the name of a citation type for the JSON output.
func citationTypeName(t genai.CitationType) string {
	switch t {
	case genai.CitationWebQuery:
		return "web_query"
	case genai.CitationWeb:
		return "web"
	case genai.CitationWebImage:
		return "web_image"
	case genai.CitationDocument:
		return "document"
	case genai.CitationTool:
		return "tool"
	default:
		return "unknown"
	}
}

// findAvailable checks if a file with the given name exists, and if so, append an index number.
//
// TODO: O(n²); I'd fail the interview.
func findAvailable(filename string) string {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return filename
	}
	dir := filepath.Dir(filename)
	base := filepath.Base(filename)
	ext := filepath.Ext(base)
	name := base[:len(base)-len(ext)]
	for i := 1; ; i++ {
		newName := fmt.Sprintf("%s_%d%s", name, i, ext)
		newPath := filepath.Join(dir, newName)
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			return newPath
		}
	}
}

// logReader wraps an io.ReadCloser and logs each chunk read from it.
type logReader struct {
	io.ReadCloser
}

func (l *logReader) Read(p []byte) (int, error) {
	n, err := l.ReadCloser.Read(p)
	if n > 0 {
		slog.Debug("subprocess stdout", "data", strings.TrimRight(string(p[:n]), "\n"))
	}
	return n, err
}
