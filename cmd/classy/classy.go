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
	"math"
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
		_, _ = fmt.Fprintf(w, "\nOutput:\n")
		_, _ = fmt.Fprintf(w, "  -format %s\n", strings.Join(formats, ", "))
		_, _ = fmt.Fprintf(w, "  text: one line per answer; json: the JSON the API returned; jsonl: one\n")
		_, _ = fmt.Fprintf(w, "  object per answer; tsv: name, type, value and confidence columns.\n")
		_, _ = fmt.Fprintf(w, "  -json is an alias for -format json. -q drops the probabilities from text and\n")
		_, _ = fmt.Fprintf(w, "  jsonl.\n")
		_, _ = fmt.Fprintf(w, "\nRequirements:\n")
		_, _ = fmt.Fprintf(w, "  -require 'billing>=0.9'  -require 'tone=frustrated'  -require 'urgency>=2'\n")
		_, _ = fmt.Fprintf(w, "  JSON-quote names containing operators: -require '\"a=b\"=calm'.\n")
		_, _ = fmt.Fprintf(w, "  JSON-quote labels to preserve whitespace: -require 'tone=\" calm \"'.\n")
		_, _ = fmt.Fprintf(w, "  A noul probability and a score compare as numbers with >=, >, <=, < or =,\n")
		_, _ = fmt.Fprintf(w, "  a choice compares its label with =. -min-confidence requires every choice\n")
		_, _ = fmt.Fprintf(w, "  and score answer to be at least that confident, from 0 to 1.\n")
		_, _ = fmt.Fprintf(w, "  An unmet requirement prints the answers, reports each unmet requirement on\n")
		_, _ = fmt.Fprintf(w, "  stderr and exits with code %d.\n", exitRequirementsNotMet)
		_, _ = fmt.Fprintf(w, "\nEnvironment variables:\n")
		_, _ = fmt.Fprintf(w, "  CLASSY_PROVIDER:       default value for -provider\n")
		_, _ = fmt.Fprintf(w, "  CLASSY_MODEL:          default value for -model\n")
		_, _ = fmt.Fprintf(w, "  CLASSY_REMOTE:         default value for -remote\n")
		_, _ = fmt.Fprintf(w, "  CLOUDFLARE_API_KEY:    API key, from https://dash.cloudflare.com/profile/api-tokens\n")
		_, _ = fmt.Fprintf(w, "  CLOUDFLARE_ACCOUNT_ID: account ID for Cloudflare Workers AI\n")
		_, _ = fmt.Fprintf(w, "  TYPESAFE_API_KEY:      API key, from https://console.genai.ai/settings/keys\n")
	}
	known := knownSystemOneProviders(ctx)
	verbose := flag.Bool("v", false, "verbose logs about metadata and usage")
	asJSON := flag.Bool("json", false, "alias for -format json")
	format := flag.String("format", formatText, "output format: "+strings.Join(formats, ", "))
	quiet := flag.Bool("q", false, "print only the answers, not the probability of each option or level; affects text and jsonl")
	record := flag.String("record", "", "record the HTTP requests in yaml files for inspection in the specified file")
	provider := flag.String("p", "", "(alias for -provider)")
	flag.StringVar(provider, "provider", os.Getenv("CLASSY_PROVIDER"), "backend to use: "+strings.Join(known, ", "))
	model := flag.String("m", "", "(alias for -model)")
	flag.StringVar(model, "model", os.Getenv("CLASSY_MODEL"), "model to use, defaults to a good decision model")
	remote := flag.String("r", "", "(alias for -remote)")
	flag.StringVar(remote, "remote", os.Getenv("CLASSY_REMOTE"), "URL to use to access the backend, useful for local model")
	questionsFile := flag.String("questions", "", "JSON file declaring the questions to ask")
	minConfidence := flag.Float64("min-confidence", 0, "fail unless every choice and score answer is at least this confident, from 0 to 1")
	var nouls, choices, scores, files, require stringsFlag
	flag.Var(&nouls, "noul", "yes/no question, \"<name>=<instructions>\"; can be specified multiple times")
	flag.Var(&choices, "choice", "question picking one option, \"<name>=<instructions>|<option>[:<description>]|...\"; can be specified multiple times")
	flag.Var(&scores, "score", "question rating the state, \"<name>=<instructions>|<level0>|<level1>|...\"; can be specified multiple times")
	flag.Var(&require, "require", "predicate the answers must satisfy to exit 0, \"<name><op><value>\" with op >=, >, <=, < or =; can be specified multiple times")
	flag.Var(&files, "f", "file(s) holding the state; a .json file is sent as structured data; images (.png, .jpg, .webp, .gif) are sent as attachments; can be specified multiple times")
	flag.Parse()
	if *verbose {
		internal.Level.Set(slog.LevelDebug)
	}
	if *record != "" {
		*record = strings.TrimSuffix(*record, ".yaml")
	}
	// -json is the original spelling, -format is the general one. They may not disagree.
	formatSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "format" {
			formatSet = true
		}
	})
	if *asJSON {
		if formatSet && *format != formatJSON {
			return fmt.Errorf("-json conflicts with -format %q", *format)
		}
		*format = formatJSON
	}
	if err := validateFormat(*format); err != nil {
		return err
	}
	if !(*minConfidence >= 0 && *minConfidence <= 1) {
		return fmt.Errorf("-min-confidence must be between 0 and 1, got %v", *minConfidence)
	}
	requirements, err := parseRequirements(require)
	if err != nil {
		return err
	}

	questions, err := loadQuestions(*questionsFile, nouls, choices, scores)
	if err != nil {
		return err
	}
	if err := validateRequirements(requirements, questions); err != nil {
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
	defer func() { err = closeProvider(c, err) }()
	slog.Info("loaded", "provider", c.Name(), "model", c.ModelID())

	err = classify(ctx, c, &msg, questions, colorable.NewColorableStdout(), classifyOptions{
		format:        *format,
		quiet:         *quiet,
		requirements:  requirements,
		minConfidence: *minConfidence,
	})
	if errRR != nil {
		return errRR
	}
	return err
}

// closeProvider releases the provider, giving cleanup failures precedence over unmet requirements.
func closeProvider(c genai.Provider, err error) error {
	closeErr := c.Close()
	if closeErr == nil {
		return err
	}
	if ee, ok := errors.AsType[*exitError](err); ok {
		err = ee.err
	}
	return errors.Join(err, closeErr)
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

// exitRequirementsNotMet is the exit code used when -require or -min-confidence is not satisfied.
//
// It is distinct from the exit code 1 used when classy fails to run, so a caller can tell an answer
// that did not match from a broken configuration or a failed request.
const exitRequirementsNotMet = 3

// exitError asks main to exit with a specific code.
type exitError struct {
	code int
	err  error
}

// Error implements error.
func (e *exitError) Error() string {
	return e.err.Error()
}

// Unwrap returns the error the exit code is reported for.
func (e *exitError) Unwrap() error {
	return e.err
}

// requirement is one -require predicate checked against the answers.
type requirement struct {
	// raw is the flag value, used to report a parse error.
	raw string
	// name is the question the predicate applies to.
	name string
	// op is the comparison, one of >=, >, <=, < or =.
	op string
	// value is the right hand side, a number for a noul or score question, a label for a choice.
	value string
}

// requirementOps lists the operators, longest first so that ">=" is not read as ">".
var requirementOps = []string{">=", "<=", ">", "<", "="}

// parseRequirements parses the -require flag values, "<name><op><value>".
func parseRequirements(values stringsFlag) ([]requirement, error) {
	out := make([]requirement, 0, len(values))
	for _, v := range values {
		name, op, value, ok := cutRequirement(v)
		if !ok {
			return nil, fmt.Errorf("-require %q: expected \"<name><op><value>\" with op >=, >, <=, < or =", v)
		}
		if strings.HasPrefix(value, `"`) {
			var label string
			if err := json.Unmarshal([]byte(value), &label); err != nil {
				return nil, fmt.Errorf("-require %q: invalid quoted value: %w", v, err)
			}
			value = label
		}
		out = append(out, requirement{raw: v, name: name, op: op, value: value})
	}
	return out, nil
}

// cutRequirement cuts a -require flag value around its first operator.
//
// An unquoted name ends at the first operator. JSON-quoted names can contain operators and preserve
// whitespace. The value runs to the end, so a choice label can hold an equal sign. It returns false
// when either side is empty or a quoted name is malformed.
func cutRequirement(s string) (string, string, string, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, `"`) {
		var name string
		d := json.NewDecoder(strings.NewReader(s))
		if err := d.Decode(&name); err != nil || name == "" {
			return "", "", "", false
		}
		rest := strings.TrimSpace(s[d.InputOffset():])
		for _, op := range requirementOps {
			if value, ok := strings.CutPrefix(rest, op); ok {
				value = strings.TrimSpace(value)
				return name, op, value, value != ""
			}
		}
		return "", "", "", false
	}
	best, bestOp := -1, ""
	for _, op := range requirementOps {
		i := strings.Index(s, op)
		if i < 0 {
			continue
		}
		if best < 0 || i < best || (i == best && len(op) > len(bestOp)) {
			best, bestOp = i, op
		}
	}
	if best <= 0 {
		return "", "", "", false
	}
	name := strings.TrimSpace(s[:best])
	value := strings.TrimSpace(s[best+len(bestOp):])
	if name == "" || value == "" {
		return "", "", "", false
	}
	return name, bestOp, value, true
}

// validateRequirements ensures every requirement names a declared question and fits its type.
//
// It reports a configuration error, so classy exits before asking anything. An operator that does not
// fit the question type is caught here rather than reported as an answer that did not match.
func validateRequirements(reqs []requirement, questions genai.Questions) error {
	for _, r := range reqs {
		q, ok := questions[r.name]
		if !ok {
			return fmt.Errorf("-require %q: no question named %q", r.raw, r.name)
		}
		switch q.Type {
		case genai.QuestionNoul, genai.QuestionScore:
			if value, err := strconv.ParseFloat(r.value, 64); err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("-require %q: a %s answer compares a finite number, got %q", r.raw, q.Type, r.value)
			}
		case genai.QuestionChoice:
			if r.op != "=" {
				return fmt.Errorf("-require %q: a choice answer only supports \"=\"", r.raw)
			}
		default:
			return fmt.Errorf("-require %q: unknown question type %q", r.raw, q.Type)
		}
	}
	return nil
}

// unmetRequirements returns one message per requirement the answers do not satisfy.
//
// minConfidence is skipped when it is 0. A noul answer has no confidence, it is the probability that the
// answer is yes, so only choice and score answers are checked against it.
func unmetRequirements(reqs []requirement, answers genai.Answers, minConfidence float64) []string {
	var unmet []string
	for _, r := range reqs {
		a, ok := answers[r.name]
		if !ok {
			unmet = append(unmet, r.name+": no answer")
			continue
		}
		switch a.Type {
		case genai.QuestionNoul:
			want, _ := strconv.ParseFloat(r.value, 64)
			if !compareFloat(a.Noul, r.op, want) {
				unmet = append(unmet, fmt.Sprintf("%s: %.0f%% yes, want %s %s", r.name, 100*a.Noul, r.op, r.value))
			}
		case genai.QuestionScore:
			want, _ := strconv.ParseFloat(r.value, 64)
			if !compareFloat(a.Score, r.op, want) {
				unmet = append(unmet, fmt.Sprintf("%s: %.2f, want %s %s", r.name, a.Score, r.op, r.value))
			}
		case genai.QuestionChoice:
			if a.Choice != r.value {
				unmet = append(unmet, fmt.Sprintf("%s: %s, want %s", r.name, a.Choice, r.value))
			}
		default:
			unmet = append(unmet, fmt.Sprintf("%s: unknown answer type %q", r.name, a.Type))
		}
	}
	if minConfidence > 0 {
		for _, n := range slices.Sorted(maps.Keys(answers)) {
			a := answers[n]
			if a.Type != genai.QuestionChoice && a.Type != genai.QuestionScore {
				continue
			}
			if a.Confidence < minConfidence {
				unmet = append(unmet, fmt.Sprintf("%s: %.0f%% confidence, want at least %.0f%%", n, 100*a.Confidence, 100*minConfidence))
			}
		}
	}
	return unmet
}

// compareFloat compares got to want with the operator of a requirement.
func compareFloat(got float64, op string, want float64) bool {
	switch op {
	case ">=":
		return got >= want
	case ">":
		return got > want
	case "<=":
		return got <= want
	case "<":
		return got < want
	case "=":
		return got == want
	default:
		return false
	}
}

// Output formats, as accepted by -format.
const (
	// formatText prints one line per answer, with the probability of each option and level unless quiet.
	formatText = "text"
	// formatJSON prints the JSON the API returned.
	formatJSON = "json"
	// formatJSONL prints one stable JSON object per answer, one per line.
	formatJSONL = "jsonl"
	// formatTSV prints one tab separated row per answer.
	formatTSV = "tsv"
)

// formats lists the -format values, in the order they are documented.
var formats = []string{formatText, formatJSON, formatJSONL, formatTSV}

// validateFormat ensures the -format value is one classy can print.
func validateFormat(format string) error {
	if slices.Contains(formats, format) {
		return nil
	}
	return fmt.Errorf("-format must be one of %s, got %q", strings.Join(formats, ", "), format)
}

// classifyOptions controls how classify runs and reports.
type classifyOptions struct {
	// format selects how the answers are printed, one of the formats values.
	format string
	// quiet drops the probability of each option and level from the text and jsonl formats.
	quiet bool
	// requirements are the predicates the answers must satisfy to exit 0.
	requirements []requirement
	// minConfidence fails unless every choice and score answer is at least this confident, 0 to 1.
	minConfidence float64
}

// classify asks the questions about the state and prints one answer per question.
func classify(ctx context.Context, c genai.Provider, msg *genai.Message, questions genai.Questions, w io.Writer, opts classifyOptions) error {
	state, docs, err := stateFromMessage(msg)
	if err != nil {
		return err
	}
	res, err := c.SystemOne(ctx, &genai.SystemOneRequest{State: state, Questions: questions, Docs: docs})
	if err != nil {
		return err
	}
	slog.Info("done", "usage", res.Usage)
	answers := res.Answers
	switch opts.format {
	case formatJSON:
		b, err := json.Marshal(answers)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%s\n", b); err != nil {
			return err
		}
	case formatJSONL:
		if err := printAnswersJSONL(w, answers, opts.quiet); err != nil {
			return err
		}
	case formatTSV:
		if err := printAnswersTSV(w, answers); err != nil {
			return err
		}
	default:
		if err := printAnswers(w, answers, opts.quiet); err != nil {
			return err
		}
	}
	if err := requirementsExitError(opts.requirements, answers, opts.minConfidence); err != nil {
		return err
	}
	return nil
}

// requirementsExitError returns the error that makes classy exit with exitRequirementsNotMet, or nil.
func requirementsExitError(reqs []requirement, answers genai.Answers, minConfidence float64) error {
	unmet := unmetRequirements(reqs, answers, minConfidence)
	if len(unmet) == 0 {
		return nil
	}
	return &exitError{code: exitRequirementsNotMet, err: errors.New(strings.Join(unmet, "\n"))}
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
			if _, err := fmt.Fprintf(w, "%s%s%s: %.0f%% yes\n", hiblack, n, reset, 100*a.Noul); err != nil {
				return err
			}
			continue
		case genai.QuestionChoice:
			if _, err := fmt.Fprintf(w, "%s%s%s: %s, %.0f%% confidence\n", hiblack, n, reset, a.Choice, 100*a.Confidence); err != nil {
				return err
			}
		case genai.QuestionScore:
			if _, err := fmt.Fprintf(w, "%s%s%s: %.2f of %d, %.0f%% confidence\n", hiblack, n, reset, a.Score, len(a.Probabilities)-1, 100*a.Confidence); err != nil {
				return err
			}
		default:
			// An answer type added by the API after this tool was written.
			if _, err := fmt.Fprintf(w, "%s%s%s: unknown answer type %q\n", hiblack, n, reset, a.Type); err != nil {
				return err
			}
			continue
		}
		if quiet {
			continue
		}
		t := tabwriter.NewWriter(w, 0, 4, 1, ' ', 0)
		for _, k := range sortedKeys(a.Probabilities, a.Type == genai.QuestionScore) {
			if legend := a.Legend[k]; legend != nil {
				if _, err := fmt.Fprintf(t, "  %s\t%v\t%.2f\n", k, legend, a.Probabilities[k]); err != nil {
					return err
				}
			} else {
				if _, err := fmt.Fprintf(t, "  %s\t%.2f\n", k, a.Probabilities[k]); err != nil {
					return err
				}
			}
		}
		if err := t.Flush(); err != nil {
			return err
		}
	}
	return nil
}

// printAnswersJSONL prints one stable JSON object per answer, one per line.
//
// The fields are name, type and value, plus confidence and probabilities for a choice and a score, and
// legend for a score. quiet drops probabilities and legend. It differs from -format json, which prints
// the JSON the API returned.
func printAnswersJSONL(w io.Writer, answers genai.Answers, quiet bool) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, n := range slices.Sorted(maps.Keys(answers)) {
		a := answers[n]
		var v any
		switch a.Type {
		case genai.QuestionNoul:
			v = jsonlNoul{Name: n, Type: a.Type, Value: a.Noul}
		case genai.QuestionChoice:
			o := jsonlChoice{Name: n, Type: a.Type, Value: a.Choice, Confidence: a.Confidence}
			if !quiet {
				o.Probabilities = a.Probabilities
			}
			v = o
		case genai.QuestionScore:
			o := jsonlScore{Name: n, Type: a.Type, Value: a.Score, Confidence: a.Confidence, Legend: a.Legend}
			if !quiet {
				o.Probabilities = a.Probabilities
			}
			v = o
		default:
			v = jsonlUnknown{Name: n, Type: a.Type}
		}
		if err := enc.Encode(v); err != nil {
			return err
		}
	}
	return nil
}

// jsonlNoul is a noul answer in the JSONL format, the value being the probability that the answer is yes.
type jsonlNoul struct {
	Name  string             `json:"name"`
	Type  genai.QuestionType `json:"type"`
	Value float64            `json:"value"`
}

// jsonlChoice is a choice answer in the JSONL format.
type jsonlChoice struct {
	Name          string             `json:"name"`
	Type          genai.QuestionType `json:"type"`
	Value         string             `json:"value"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// jsonlScore is a score answer in the JSONL format.
type jsonlScore struct {
	Name          string             `json:"name"`
	Type          genai.QuestionType `json:"type"`
	Value         float64            `json:"value"`
	Confidence    float64            `json:"confidence"`
	Legend        genai.ScoreLegend  `json:"legend,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// jsonlUnknown is an answer type the API added after this tool was written.
type jsonlUnknown struct {
	Name string             `json:"name"`
	Type genai.QuestionType `json:"type"`
}

// printAnswersTSV prints one tab separated row per answer, for awk and cut.
//
// The columns are name, type, value and confidence, the last one empty for a noul or an unknown answer.
// Use -format jsonl for the probabilities and the score legend.
func printAnswersTSV(w io.Writer, answers genai.Answers) error {
	for _, n := range slices.Sorted(maps.Keys(answers)) {
		a := answers[n]
		var value, confidence string
		switch a.Type {
		case genai.QuestionNoul:
			value = formatNumber(a.Noul)
		case genai.QuestionChoice:
			value, confidence = a.Choice, formatNumber(a.Confidence)
		case genai.QuestionScore:
			value, confidence = formatNumber(a.Score), formatNumber(a.Confidence)
		default:
			// An answer type added by the API after this tool was written has no value.
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", tsvField(n), tsvField(string(a.Type)), tsvField(value), confidence); err != nil {
			return err
		}
	}
	return nil
}

// formatNumber formats a probability or a score for a machine readable column.
func formatNumber(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// tsvField escapes the tab and the newline of a TSV field so that a value keeps one row.
func tsvField(s string) string {
	if !strings.ContainsAny(s, "\\\t\n\r") {
		return s
	}
	return strings.NewReplacer("\\", "\\\\", "\t", "\\t", "\n", "\\n", "\r", "\\r").Replace(s)
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
