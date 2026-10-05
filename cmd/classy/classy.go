// Copyright 2026 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// CLI flag parsing, state loading and answer printing.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/maruel/ask/internal"
	"github.com/maruel/genai"
	"github.com/maruel/genai/httprecord"
	"github.com/maruel/genai/providers"
	"github.com/maruel/roundtrippers"
	"github.com/mattn/go-colorable"
	"golang.org/x/term"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

const (
	reset   = "\x1b[0m"
	hiblack = "\x1b[90m"
)

// Main runs the classifier.
func Main() (err error) {
	flag.CommandLine.SetOutput(colorable.NewColorableStderr())
	ctx, stop := internal.Init()
	defer stop()

	flag.Usage = func() {
		w := flag.CommandLine.Output()
		_, _ = fmt.Fprintf(w, "Usage: %s [options] <state>\n\n", os.Args[0])
		_, _ = fmt.Fprintf(w, "Asks typed questions about one state with a System One decision model and prints\n")
		_, _ = fmt.Fprintf(w, "one answer per question. It does not generate text; use ask for that.\n\n")
		flag.PrintDefaults()
		_, _ = fmt.Fprintf(w, "\nState:\n")
		_, _ = fmt.Fprintf(w, "  - Argument: classy -noul spam=\"Is this spam?\" \"buy now\"\n")
		_, _ = fmt.Fprintf(w, "  - Files: classy -f ticket.json ...\n")
		_, _ = fmt.Fprintf(w, "  - Stdin: cat ticket.json | classy ...\n")
		_, _ = fmt.Fprintf(w, "  A state that is a JSON object or array is sent as structured data, anything\n")
		_, _ = fmt.Fprintf(w, "  else is sent as text.\n")
		_, _ = fmt.Fprintf(w, "\nQuestions:\n")
		_, _ = fmt.Fprintf(w, "  <name> is yours to pick: it keys the answer, it is not sent to the model.\n")
		_, _ = fmt.Fprintf(w, "  -noul   spam=\"Is this spam?\"\n")
		_, _ = fmt.Fprintf(w, "  -choice tone=\"What is the tone?\"|calm|frustrated:annoyed but polite|angry\n")
		_, _ = fmt.Fprintf(w, "  -score  urgency=\"How soon?\"|can wait|this week|today|right now\n")
		_, _ = fmt.Fprintf(w, "  The name ends at the first \"=\" and the instructions at the first \"|\", so\n")
		_, _ = fmt.Fprintf(w, "  both a comma and an equal sign are free to use. A \\ escapes a \"|\" or a \":\".\n")
		_, _ = fmt.Fprintf(w, "  -questions questions.json declares the same with full criteria:\n")
		_, _ = fmt.Fprintf(w, "    {\"tone\": {\"type\": \"choice\", \"instructions\": \"What is the tone?\",\n")
		_, _ = fmt.Fprintf(w, "              \"criteria\": {\"calm\": null, \"angry\": \"openly hostile\"}}}\n")
		_, _ = fmt.Fprintf(w, "\nEnvironment variables:\n")
		_, _ = fmt.Fprintf(w, "  CLASSY_PROVIDER:       default value for -provider\n")
		_, _ = fmt.Fprintf(w, "  CLASSY_MODEL:          default value for -model\n")
		_, _ = fmt.Fprintf(w, "  CLASSY_REMOTE:         default value for -remote\n")
		_, _ = fmt.Fprintf(w, "  CLOUDFLARE_API_KEY:    API key, from https://dash.cloudflare.com/profile/api-tokens\n")
		_, _ = fmt.Fprintf(w, "  CLOUDFLARE_ACCOUNT_ID: account ID for Cloudflare Workers AI\n")
		_, _ = fmt.Fprintf(w, "  TYPESAFE_API_KEY:      API key, from https://console.typesafe.ai/settings/keys\n")
	}
	known := knownSystemOneProviders(ctx)
	verbose := flag.Bool("v", false, "verbose logs about metadata and usage")
	asJSON := flag.Bool("json", false, "print the answers as the JSON the API returned")
	quiet := flag.Bool("q", false, "print only the answers, not the probability of each option or level")
	record := flag.String("record", "", "record the HTTP requests in yaml files for inspection in the specified file")
	provider := flag.String("p", "", "(alias for -provider)")
	flag.StringVar(provider, "provider", os.Getenv("CLASSY_PROVIDER"), "backend to use: "+strings.Join(known, ", "))
	model := flag.String("m", "", "(alias for -model)")
	flag.StringVar(model, "model", os.Getenv("CLASSY_MODEL"), "model to use, defaults to a good decision model")
	remote := flag.String("r", "", "(alias for -remote)")
	flag.StringVar(remote, "remote", os.Getenv("CLASSY_REMOTE"), "URL to use to access the backend, useful for local model")
	questionsFile := flag.String("questions", "", "JSON file declaring the questions to ask")
	var nouls, choices, scores, files stringsFlag
	flag.Var(&nouls, "noul", "yes/no question, \"<name>=<instructions>\"; can be specified multiple times")
	flag.Var(&choices, "choice", "question picking one option, \"<name>=<instructions>|<option>[:<description>]|...\"; can be specified multiple times")
	flag.Var(&scores, "score", "question rating the state, \"<name>=<instructions>|<level0>|<level1>|...\"; can be specified multiple times")
	flag.Var(&files, "f", "file(s) holding the state; a .json file is sent as structured data; images (.png, .jpg, .webp, .gif) are sent as attachments; can be specified multiple times")
	flag.Parse()
	if *verbose {
		internal.Level.Set(slog.LevelDebug)
	}
	if *record != "" {
		*record = strings.TrimSuffix(*record, ".yaml")
	}

	questions, err := loadQuestions(*questionsFile, nouls, choices, scores)
	if err != nil {
		return err
	}
	msg, err := loadState(flag.Args(), files)
	if err != nil {
		return err
	}

	var rr *recorder.Recorder
	var errRR error
	var popts []genai.ProviderOption
	if *remote != "" {
		popts = append(popts, genai.ProviderOptionRemote(*remote))
	}
	if *verbose || *record != "" {
		popts = append(popts, genai.ProviderOptionTransportWrapper(func(h http.RoundTripper) http.RoundTripper {
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
	}
	c, err := loadProvider(ctx, *provider, *model, popts)
	if err != nil {
		return err
	}
	if rr != nil {
		defer func() {
			if err2 := rr.Stop(); err2 != nil {
				slog.Error("failed to stop HTTP recorder", "err", err2)
			}
		}()
	}
	defer func() { err = errors.Join(err, c.Close()) }()
	slog.Info("loaded", "provider", c.Name(), "model", c.ModelID())

	err = classify(ctx, c, &msg, questions, colorable.NewColorableStdout(), *asJSON, *quiet)
	if errRR != nil {
		return errRR
	}
	return err
}

// knownSystemOneProviders returns the sorted list of provider names that support System One inference.
func knownSystemOneProviders(ctx context.Context) []string {
	var names []string
	for name, cfg := range providers.All {
		c, _ := cfg.Factory(ctx)
		if c != nil {
			if c.Capabilities().SystemOne {
				names = append(names, name)
			}
			_ = c.Close()
		}
	}
	slices.Sort(names)
	return names
}

// listAvailableSystemOne returns available providers that support System One decision inference.
func listAvailableSystemOne(ctx context.Context) ([]string, error) {
	var names []string
	for name, cfg := range providers.Available(ctx) {
		c, err := cfg.Factory(ctx)
		if err != nil {
			continue
		}
		if c.Capabilities().SystemOne {
			names = append(names, name)
		}
		if err := c.Close(); err != nil {
			return nil, fmt.Errorf("failed to close provider %q: %w", name, err)
		}
	}
	slices.Sort(names)
	return names, nil
}

// loadProvider loads the requested decision provider or auto-detects one from available providers.
func loadProvider(ctx context.Context, provider, model string, popts []genai.ProviderOption) (genai.Provider, error) {
	known := knownSystemOneProviders(ctx)
	if provider == "" {
		names, err := listAvailableSystemOne(ctx)
		if err != nil {
			return nil, err
		}
		if len(names) == 0 {
			return nil, errors.New("no System One decision providers available; set TYPESAFE_API_KEY or CLOUDFLARE_API_KEY, or run ollama or llama-server")
		}
		if len(names) == 1 {
			provider = names[0]
		} else {
			// Prefer cloud decision endpoints, then local servers.
			preference := []string{"typesafe", "cloudflare", "ollama", "llamacpp"}
			for _, p := range preference {
				if slices.Contains(names, p) {
					provider = p
					break
				}
			}
			if provider == "" {
				provider = names[0]
			}
		}
	} else if !slices.Contains(known, provider) {
		if _, ok := providers.All[provider]; ok {
			return nil, fmt.Errorf("provider %q doesn't support System One decision inference", provider)
		}
		return nil, fmt.Errorf("unknown provider %q", provider)
	}

	cfg := providers.All[provider]
	opts := slices.Clone(popts)
	opts = append(opts, genai.ProviderOptionModalities{genai.ModalityDecision})
	switch {
	case model != "":
		opts = append(opts, genai.ProviderOptionModel(model))
	case provider == "cloudflare":
		// Cloudflare CLEF requires an explicit Workers AI model instead of a chat tier alias.
		opts = append(opts, genai.ProviderOptionModel("@cf/cloudflare/clef"))
	default:
		opts = append(opts, genai.ModelGood)
	}

	c, err := cfg.Factory(ctx, opts...)
	if err != nil {
		if c != nil {
			_ = c.Close()
		}
		return nil, fmt.Errorf("failed to connect to provider %q: %w", provider, err)
	}
	if !c.Capabilities().SystemOne {
		return nil, errors.Join(fmt.Errorf("provider %q doesn't support System One decision inference", provider), c.Close())
	}
	return c, nil
}

// classify asks the questions about the state and prints one answer per question.
func classify(ctx context.Context, c genai.Provider, msg *genai.Message, questions genai.Questions, w io.Writer, asJSON, quiet bool) error {
	state, docs, err := stateFromMessage(msg)
	if err != nil {
		return err
	}
	res, err := c.SystemOne(ctx, &genai.SystemOneRequest{State: state, Questions: questions, Docs: docs})
	if err != nil {
		return err
	}
	slog.Info("done", "usage", res.Usage)
	if asJSON {
		b, err := json.Marshal(res.Answers)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s\n", b)
		return err
	}
	return printAnswers(w, res.Answers, quiet)
}

// stateFromMessage converts a Message into DecisionContent state and document attachments.
func stateFromMessage(msg *genai.Message) (genai.DecisionContent, []genai.Doc, error) {
	if len(msg.Replies) != 0 || len(msg.ToolCallResults) != 0 {
		return nil, nil, errors.New("system one decision models do not support conversations; pass the full state as text or as a JSON document instead of assistant replies or tool call results")
	}
	if len(msg.Requests) == 0 {
		return nil, nil, errors.New("the message must have the state as text, as a JSON document or as an image")
	}
	var stateParts []genai.DecisionContent
	var docs []genai.Doc
	for i := range msg.Requests {
		req := &msg.Requests[i]
		if !req.Doc.IsZero() {
			if isImageFile(req.Doc.GetFilename()) {
				docs = append(docs, req.Doc)
				continue
			}
			state, err := stateFromDoc(&req.Doc)
			if err != nil {
				return nil, nil, fmt.Errorf("request #%d: %w", i, err)
			}
			stateParts = append(stateParts, state)
			continue
		}
		if req.Text != "" {
			stateParts = append(stateParts, genai.Text(req.Text))
		}
	}
	if len(stateParts) == 0 && len(docs) == 0 {
		return nil, nil, errors.New("must have the state as text, as a JSON document or as an image")
	}
	var state genai.DecisionContent
	switch len(stateParts) {
	case 0:
		state = genai.Text("")
	case 1:
		state = stateParts[0]
	default:
		arr := make(genai.Array, len(stateParts))
		for i, part := range stateParts {
			arr[i] = part
		}
		state = arr
	}
	return state, docs, nil
}

// isImageFile returns true if the filename has a supported image extension.
func isImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return true
	default:
		return false
	}
}

// stateFromDoc returns the state held by a document.
func stateFromDoc(d *genai.Doc) (genai.DecisionContent, error) {
	if d.URL != "" {
		return nil, errors.New("the state document must be inline")
	}
	mimeType, data, err := d.Read(10 * 1024 * 1024)
	if err != nil {
		return nil, err
	}
	if mimeType != "application/json" {
		return nil, fmt.Errorf("the state document must be application/json, got %q; name it with a .json extension", mimeType)
	}
	state, err := genai.ParseDecisionContent(data)
	if err != nil {
		return nil, fmt.Errorf("the state document must contain a JSON object or array: %w", err)
	}
	return state, nil
}

// loadQuestions returns the questions declared by the flags.
func loadQuestions(file string, nouls, choices, scores stringsFlag) (genai.Questions, error) {
	questions := genai.Questions{}
	if file != "" {
		var err error
		if questions, err = questionsFromFile(file); err != nil {
			return nil, err
		}
	}
	for _, decls := range []struct {
		values stringsFlag
		flag   string
		parse  func(string) (string, *genai.Question, error)
	}{
		{nouls, "-noul", parseNoul},
		{choices, "-choice", parseChoice},
		{scores, "-score", parseScore},
	} {
		for _, v := range decls.values {
			n, q, err := decls.parse(v)
			if err != nil {
				return nil, fmt.Errorf("%s %q: %w", decls.flag, v, err)
			}
			if _, ok := questions[n]; ok {
				return nil, fmt.Errorf("question %q is declared twice", n)
			}
			questions[n] = q
		}
	}
	if len(questions) == 0 {
		return nil, errors.New("declare at least one question with -noul, -choice, -score or -questions")
	}
	return questions, questions.Validate()
}

// loadState returns the message holding the state to evaluate.
//
// Each argument, file and stdin becomes one part of the state. A part that is a JSON object or array is
// sent as structured data, image files are sent as attachments, and anything else is sent as text.
func loadState(args []string, files stringsFlag) (genai.Message, error) {
	msg := genai.Message{}
	if query := strings.Join(args, " "); query != "" {
		msg.Requests = append(msg.Requests, requestFromState([]byte(query), "argument"))
	}
	for _, n := range files {
		b, err := os.ReadFile(n)
		if err != nil {
			return msg, err
		}
		msg.Requests = append(msg.Requests, requestFromState(b, n))
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return msg, err
		}
		if len(bytes.TrimSpace(b)) != 0 {
			msg.Requests = append(msg.Requests, requestFromState(b, "stdin"))
		}
	}
	if len(msg.Requests) == 0 {
		return msg, errors.New("provide the state as an argument, as files or on stdin")
	}
	return msg, nil
}

// requestFromState returns the request holding one part of the state.
//
// name is only used for logging and to name the document sent for structured state or images.
func requestFromState(b []byte, name string) genai.Request {
	if isImageFile(name) {
		slog.Debug("state", "name", name, "kind", "image")
		return genai.Request{Doc: genai.Doc{Filename: filepath.Base(name), Src: bytes.NewReader(b)}}
	}
	switch http.DetectContentType(b) {
	case "image/png":
		slog.Debug("state", "name", name, "kind", "image")
		return genai.Request{Doc: genai.Doc{Filename: "image.png", Src: bytes.NewReader(b)}}
	case "image/jpeg":
		slog.Debug("state", "name", name, "kind", "image")
		return genai.Request{Doc: genai.Doc{Filename: "image.jpg", Src: bytes.NewReader(b)}}
	case "image/gif":
		slog.Debug("state", "name", name, "kind", "image")
		return genai.Request{Doc: genai.Doc{Filename: "image.gif", Src: bytes.NewReader(b)}}
	case "image/webp":
		slog.Debug("state", "name", name, "kind", "image")
		return genai.Request{Doc: genai.Doc{Filename: "image.webp", Src: bytes.NewReader(b)}}
	}
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) != 0 && (trimmed[0] == '{' || trimmed[0] == '[') && json.Valid(trimmed) {
		slog.Debug("state", "name", name, "kind", "json")
		return genai.Request{Doc: genai.Doc{Filename: "state.json", Src: bytes.NewReader(trimmed)}}
	}
	slog.Debug("state", "name", name, "kind", "text")
	return genai.Request{Text: string(b)}
}

// printAnswers prints one line per answer, and the probability of each option and level unless quiet.
func printAnswers(w io.Writer, answers genai.Answers, quiet bool) error {
	for _, n := range slices.Sorted(maps.Keys(answers)) {
		a := answers[n]
		switch a.Type {
		case genai.QuestionNoul:
			_, _ = fmt.Fprintf(w, "%s%s%s: %.0f%% yes\n", hiblack, n, reset, 100*a.Noul)
			continue
		case genai.QuestionChoice:
			_, _ = fmt.Fprintf(w, "%s%s%s: %s, %.0f%% confidence\n", hiblack, n, reset, a.Choice, 100*a.Confidence)
		case genai.QuestionScore:
			_, _ = fmt.Fprintf(w, "%s%s%s: %.2f of %d, %.0f%% confidence\n", hiblack, n, reset, a.Score, len(a.Probabilities)-1, 100*a.Confidence)
		default:
			// An answer type added by the API after this tool was written.
			_, _ = fmt.Fprintf(w, "%s%s%s: unknown answer type %q\n", hiblack, n, reset, a.Type)
			continue
		}
		if quiet {
			continue
		}
		t := tabwriter.NewWriter(w, 0, 4, 1, ' ', 0)
		for _, k := range sortedKeys(a.Probabilities, a.Type == genai.QuestionScore) {
			if legend := a.Legend[k]; legend != nil {
				_, _ = fmt.Fprintf(t, "  %s\t%v\t%.2f\n", k, legend, a.Probabilities[k])
			} else {
				_, _ = fmt.Fprintf(t, "  %s\t%.2f\n", k, a.Probabilities[k])
			}
		}
		if err := t.Flush(); err != nil {
			return err
		}
	}
	return nil
}

// sortedKeys returns the keys of the probabilities, as numbers when they are the levels of a score.
func sortedKeys(probabilities map[string]float64, numeric bool) []string {
	keys := slices.Sorted(maps.Keys(probabilities))
	if numeric {
		slices.SortFunc(keys, func(a, b string) int {
			x, err1 := strconv.Atoi(a)
			y, err2 := strconv.Atoi(b)
			if err1 != nil || err2 != nil {
				return strings.Compare(a, b)
			}
			return x - y
		})
	}
	return keys
}

// stringsFlag is a flag that can be specified multiple times.
type stringsFlag []string

// Set implements flag.Value.
func (s *stringsFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

// String implements flag.Value.
func (s *stringsFlag) String() string {
	return strings.Join([]string(*s), ", ")
}
