// Copyright 2026 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Tests for the state loading and the answer printing.

package main

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/maruel/genai"
)

func TestLoadState(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		n := filepath.Join(t.TempDir(), "ticket.json")
		if err := os.WriteFile(n, []byte("  {\"subject\": \"charged twice\"}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		msg, err := loadState([]string{"is", "it", "urgent?"}, stringsFlag{n})
		if err != nil {
			t.Fatal(err)
		}
		if len(msg.Requests) != 2 {
			t.Fatalf("got %d requests, want 2", len(msg.Requests))
		}
		if got := msg.Requests[0].Text; got != "is it urgent?" {
			t.Fatalf("got %q, want the arguments joined", got)
		}
		// A JSON file is sent as a document so that the API evaluates it as structured state.
		if msg.Requests[1].Text != "" || msg.Requests[1].Doc.Filename != "state.json" {
			t.Fatalf("got %#v, want a JSON document", msg.Requests[1])
		}
	})
	t.Run("error", func(t *testing.T) {
		if _, err := loadState(nil, nil); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestKnownSystemOneProviders(t *testing.T) {
	known := knownSystemOneProviders(t.Context())
	want := []string{"cloudflare", "llamacpp", "ollama", "typesafe"}
	if !slices.Equal(known, want) {
		t.Fatalf("got %v, want %v", known, want)
	}
}

func TestLoadProvider(t *testing.T) {
	ctx := t.Context()
	t.Run("unknown provider", func(t *testing.T) {
		_, err := loadProvider(ctx, "nonexistent", "", nil)
		if err == nil || !strings.Contains(err.Error(), "unknown provider \"nonexistent\"") {
			t.Fatalf("got error %v, want unknown provider error", err)
		}
	})
	t.Run("unsupported provider", func(t *testing.T) {
		_, err := loadProvider(ctx, "openai", "", nil)
		if err == nil || !strings.Contains(err.Error(), "doesn't support System One") {
			t.Fatalf("got error %v, want unsupported System One error", err)
		}
	})
	t.Run("valid provider without key", func(t *testing.T) {
		t.Setenv("CLOUDFLARE_API_KEY", "")
		t.Setenv("CLOUDFLARE_ACCOUNT_ID", "")
		_, err := loadProvider(ctx, "cloudflare", "", nil)
		if err == nil || !strings.Contains(err.Error(), "failed to connect to provider \"cloudflare\"") {
			t.Fatalf("got error %v, want connection error", err)
		}
	})
}

func TestIsImageFile(t *testing.T) {
	tests := []struct {
		filename string
		want     bool
	}{
		{"photo.png", true},
		{"photo.PNG", true},
		{"photo.jpg", true},
		{"photo.jpeg", true},
		{"photo.webp", true},
		{"photo.gif", true},
		{"doc.txt", false},
		{"state.json", false},
		{"binary.bin", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := isImageFile(tc.filename); got != tc.want {
			t.Errorf("isImageFile(%q) = %t, want %t", tc.filename, got, tc.want)
		}
	}
}

func TestStateFromMessage(t *testing.T) {
	t.Run("text only", func(t *testing.T) {
		msg := &genai.Message{
			Requests: []genai.Request{{Text: "hello world"}},
		}
		state, docs, err := stateFromMessage(msg)
		if err != nil {
			t.Fatal(err)
		}
		if len(docs) != 0 {
			t.Fatalf("got %d docs, want 0", len(docs))
		}
		if got, ok := state.(genai.Text); !ok || string(got) != "hello world" {
			t.Fatalf("got %v, want 'hello world'", state)
		}
	})

	t.Run("json doc", func(t *testing.T) {
		msg := &genai.Message{
			Requests: []genai.Request{
				{Doc: genai.Doc{Filename: "state.json", Src: bytes.NewReader([]byte(`{"key":"value"}`))}},
			},
		}
		state, docs, err := stateFromMessage(msg)
		if err != nil {
			t.Fatal(err)
		}
		if len(docs) != 0 {
			t.Fatalf("got %d docs, want 0", len(docs))
		}
		obj, ok := state.(genai.Object)
		if !ok || len(obj) != 1 {
			t.Fatalf("got %T: %v, want Object", state, state)
		}
	})

	t.Run("image doc", func(t *testing.T) {
		msg := &genai.Message{
			Requests: []genai.Request{
				{Doc: genai.Doc{Filename: "image.png", Src: bytes.NewReader([]byte("fake png"))}},
			},
		}
		state, docs, err := stateFromMessage(msg)
		if err != nil {
			t.Fatal(err)
		}
		if len(docs) != 1 {
			t.Fatalf("got %d docs, want 1", len(docs))
		}
		if docs[0].Filename != "image.png" {
			t.Fatalf("got doc filename %q, want image.png", docs[0].Filename)
		}
		if text, ok := state.(genai.Text); !ok || string(text) != "" {
			t.Fatalf("got %v, want empty text state", state)
		}
	})

	t.Run("mixed text and image", func(t *testing.T) {
		msg := &genai.Message{
			Requests: []genai.Request{
				{Text: "describe this"},
				{Doc: genai.Doc{Filename: "diagram.jpg", Src: bytes.NewReader([]byte("fake jpg"))}},
			},
		}
		state, docs, err := stateFromMessage(msg)
		if err != nil {
			t.Fatal(err)
		}
		if len(docs) != 1 {
			t.Fatalf("got %d docs, want 1", len(docs))
		}
		if text, ok := state.(genai.Text); !ok || string(text) != "describe this" {
			t.Fatalf("got %v, want 'describe this'", state)
		}
	})

	t.Run("conversation rejected", func(t *testing.T) {
		msg := &genai.Message{
			Requests: []genai.Request{{Text: "prompt"}},
			Replies:  []genai.Reply{{Text: "response"}},
		}
		_, _, err := stateFromMessage(msg)
		if err == nil {
			t.Fatal("expected error for conversation replies")
		}
	})

	t.Run("invalid doc type", func(t *testing.T) {
		msg := &genai.Message{
			Requests: []genai.Request{
				{Doc: genai.Doc{Filename: "data.txt", Src: bytes.NewReader([]byte("plain text"))}},
			},
		}
		_, _, err := stateFromMessage(msg)
		if err == nil {
			t.Fatal("expected error for non-json non-image doc")
		}
	})
}

func TestRequestFromState(t *testing.T) {
	data := []struct {
		name  string
		state string
		doc   bool
	}{
		{name: "text", state: "buy now"},
		{name: "object", state: "{\"a\": 1}", doc: true},
		{name: "array", state: "[1, 2]", doc: true},
		{name: "truncated object", state: "{\"a\": 1"},
		{name: "number", state: "1"},
		{name: "image.png", state: "dummy", doc: true},
		{name: "image.jpg", state: "dummy", doc: true},
		{name: "image.webp", state: "dummy", doc: true},
		{name: "image.gif", state: "dummy", doc: true},
		{name: "stdin", state: "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR", doc: true},
	}
	for _, line := range data {
		t.Run(line.name, func(t *testing.T) {
			r := requestFromState([]byte(line.state), line.name)
			if got := !r.Doc.IsZero(); got != line.doc {
				t.Fatalf("got a document: %t, want %t", got, line.doc)
			}
		})
	}
}

func TestPrintAnswers(t *testing.T) {
	answers := genai.Answers{
		"billing": {Type: genai.QuestionNoul, Noul: 0.99},
		"tone": {
			Type: genai.QuestionChoice, Choice: "calm", Confidence: 0.84,
			Probabilities: map[string]float64{"angry": 0, "calm": 0.89, "frustrated": 0.11},
		},
		"urgency": {
			Type: genai.QuestionScore, Score: 1.8, Confidence: 0.61,
			Legend:        genai.ScoreLegend{"0": genai.Text("can wait"), "1": genai.Text("today")},
			Probabilities: map[string]float64{"0": 0.2, "1": 0.8},
		},
		"mystery": {Type: "quantum"},
	}
	t.Run("verbose", func(t *testing.T) {
		b := &bytes.Buffer{}
		if err := printAnswers(b, answers, false); err != nil {
			t.Fatal(err)
		}
		want := hiblack + "billing" + reset + ": 99% yes\n" +
			hiblack + "mystery" + reset + ": unknown answer type \"quantum\"\n" +
			hiblack + "tone" + reset + ": calm, 84% confidence\n" +
			"  angry      0.00\n" +
			"  calm       0.89\n" +
			"  frustrated 0.11\n" +
			hiblack + "urgency" + reset + ": 1.80 of 1, 61% confidence\n" +
			"  0 can wait 0.20\n" +
			"  1 today    0.80\n"
		if got := b.String(); got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})
	t.Run("quiet", func(t *testing.T) {
		b := &bytes.Buffer{}
		if err := printAnswers(b, answers, true); err != nil {
			t.Fatal(err)
		}
		want := hiblack + "billing" + reset + ": 99% yes\n" +
			hiblack + "mystery" + reset + ": unknown answer type \"quantum\"\n" +
			hiblack + "tone" + reset + ": calm, 84% confidence\n" +
			hiblack + "urgency" + reset + ": 1.80 of 1, 61% confidence\n"
		if got := b.String(); got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})
}

func TestValidateFormat(t *testing.T) {
	for _, f := range formats {
		if err := validateFormat(f); err != nil {
			t.Fatalf("%q: %v", f, err)
		}
	}
	err := validateFormat("yaml")
	if err == nil {
		t.Fatal("expected an error")
	}
	if got, want := err.Error(), `-format must be one of text, json, jsonl, tsv, got "yaml"`; got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestPrintAnswersFormats(t *testing.T) {
	answers := genai.Answers{
		"billing": {Type: genai.QuestionNoul, Noul: 0.99},
		"tone": {
			Type: genai.QuestionChoice, Choice: "calm", Confidence: 0.84,
			Probabilities: map[string]float64{"angry": 0, "calm": 0.89, "frustrated": 0.11},
		},
		"urgency": {
			Type: genai.QuestionScore, Score: 1.8, Confidence: 0.61,
			Legend:        genai.ScoreLegend{"0": genai.Text("can wait"), "1": genai.Text("today")},
			Probabilities: map[string]float64{"0": 0.2, "1": 0.8},
		},
		"mystery": {Type: "quantum"},
	}
	t.Run("jsonl", func(t *testing.T) {
		b := &bytes.Buffer{}
		if err := printAnswersJSONL(b, answers, false); err != nil {
			t.Fatal(err)
		}
		want := `{"name":"billing","type":"noul","value":0.99}` + "\n" +
			`{"name":"mystery","type":"quantum"}` + "\n" +
			`{"name":"tone","type":"choice","value":"calm","confidence":0.84,"probabilities":{"angry":0,"calm":0.89,"frustrated":0.11}}` + "\n" +
			`{"name":"urgency","type":"score","value":1.8,"confidence":0.61,"legend":{"0":"can wait","1":"today"},"probabilities":{"0":0.2,"1":0.8}}` + "\n"
		if got := b.String(); got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})
	t.Run("jsonl quiet", func(t *testing.T) {
		b := &bytes.Buffer{}
		if err := printAnswersJSONL(b, answers, true); err != nil {
			t.Fatal(err)
		}
		want := `{"name":"billing","type":"noul","value":0.99}` + "\n" +
			`{"name":"mystery","type":"quantum"}` + "\n" +
			`{"name":"tone","type":"choice","value":"calm","confidence":0.84}` + "\n" +
			`{"name":"urgency","type":"score","value":1.8,"confidence":0.61,"legend":{"0":"can wait","1":"today"}}` + "\n"
		if got := b.String(); got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})
	t.Run("tsv", func(t *testing.T) {
		b := &bytes.Buffer{}
		if err := printAnswersTSV(b, answers); err != nil {
			t.Fatal(err)
		}
		want := "billing\tnoul\t0.99\t\n" +
			"mystery\tquantum\t\t\n" +
			"tone\tchoice\tcalm\t0.84\n" +
			"urgency\tscore\t1.8\t0.61\n"
		if got := b.String(); got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})
	t.Run("tsv escapes", func(t *testing.T) {
		b := &bytes.Buffer{}
		if err := printAnswersTSV(b, genai.Answers{
			"a\tb": {Type: genai.QuestionChoice, Choice: "x\ny", Confidence: 0.5},
		}); err != nil {
			t.Fatal(err)
		}
		want := "a\\tb\tchoice\tx\\ny\t0.5\n"
		if got := b.String(); got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})
}

func TestParseRequirements(t *testing.T) {
	data := []struct {
		name     string
		in       string
		wantName string
		wantOp   string
		want     string
	}{
		{name: "greater or equal", in: "billing>=0.9", wantName: "billing", wantOp: ">=", want: "0.9"},
		{name: "less or equal", in: "urgency<=2.5", wantName: "urgency", wantOp: "<=", want: "2.5"},
		{name: "greater", in: "urgency>2", wantName: "urgency", wantOp: ">", want: "2"},
		{name: "less", in: "urgency<1", wantName: "urgency", wantOp: "<", want: "1"},
		{name: "quoted label", in: `tone=" calm "`, wantName: "tone", wantOp: "=", want: " calm "},
		{name: "quoted escapes", in: `tone="a\\b\"c"`, wantName: "tone", wantOp: "=", want: `a\b"c`},
		{name: "equal", in: "tone=frustrated", wantName: "tone", wantOp: "=", want: "frustrated"},
		{name: "spaces are trimmed", in: " tone = frustrated ", wantName: "tone", wantOp: "=", want: "frustrated"},
		// The value runs to the end, so a label can hold an equal sign.
		{name: "value holds an equal sign", in: "tone=a=b", wantName: "tone", wantOp: "=", want: "a=b"},
	}
	for _, line := range data {
		t.Run(line.name, func(t *testing.T) {
			reqs, err := parseRequirements(stringsFlag{line.in})
			if err != nil {
				t.Fatal(err)
			}
			if len(reqs) != 1 {
				t.Fatalf("got %d requirements, want 1", len(reqs))
			}
			if reqs[0].name != line.wantName || reqs[0].op != line.wantOp || reqs[0].value != line.want {
				t.Fatalf("got %q %q %q, want %q %q %q", reqs[0].name, reqs[0].op, reqs[0].value, line.wantName, line.wantOp, line.want)
			}
		})
	}
}

func TestParseRequirementsError(t *testing.T) {
	for _, in := range []string{"", "billing", ">=0.9", "billing>=", "billing<", `"unterminated=1`, `"name"junk=1`, `""=1`, `"name"=`, `"bad\q"=1`, `tone="unterminated`, `tone="calm"extra`, `tone="bad\q"`} {
		t.Run(in, func(t *testing.T) {
			if _, err := parseRequirements(stringsFlag{in}); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestValidateRequirements(t *testing.T) {
	questions := genai.Questions{
		"billing": {Type: genai.QuestionNoul, Instructions: genai.Text("Is this about billing?")},
		"tone": {
			Type: genai.QuestionChoice, Instructions: genai.Text("What is the tone?"),
			Choice: map[string]genai.DecisionContent{"calm": nil, "angry": nil},
		},
		"urgency": {
			Type: genai.QuestionScore, Instructions: genai.Text("How soon?"),
			Score: []genai.DecisionContent{genai.Text("can wait"), genai.Text("today")},
		},
	}
	t.Run("valid", func(t *testing.T) {
		reqs, err := parseRequirements(stringsFlag{"billing>=0.9", "tone=frustrated", "urgency>2"})
		if err != nil {
			t.Fatal(err)
		}
		if err = validateRequirements(reqs, questions); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("error", func(t *testing.T) {
		data := []struct {
			in   string
			want string
		}{
			{in: "missing>=1", want: `-require "missing>=1": no question named "missing"`},
			{in: "billing>yes", want: `-require "billing>yes": a noul answer compares a finite number, got "yes"`},
			{in: "billing>NaN", want: `-require "billing>NaN": a noul answer compares a finite number, got "NaN"`},
			{in: "billing>-Inf", want: `-require "billing>-Inf": a noul answer compares a finite number, got "-Inf"`},
			{in: "urgency<+Inf", want: `-require "urgency<+Inf": a score answer compares a finite number, got "+Inf"`},
			{in: "tone>=0.5", want: `-require "tone>=0.5": a choice answer only supports "="`},
		}
		for _, line := range data {
			t.Run(line.in, func(t *testing.T) {
				reqs, err := parseRequirements(stringsFlag{line.in})
				if err != nil {
					t.Fatal(err)
				}
				err = validateRequirements(reqs, questions)
				if err == nil {
					t.Fatal("expected an error")
				}
				if got := err.Error(); got != line.want {
					t.Fatalf("got  %s\nwant %s", got, line.want)
				}
			})
		}
	})
}

func TestUnmetRequirements(t *testing.T) {
	answers := genai.Answers{
		"billing": {Type: genai.QuestionNoul, Noul: 0.99},
		"tone":    {Type: genai.QuestionChoice, Choice: "calm", Confidence: 0.84},
		"urgency": {Type: genai.QuestionScore, Score: 1.8, Confidence: 0.61},
	}
	parse := func(t *testing.T, values ...string) []requirement {
		t.Helper()
		reqs, err := parseRequirements(stringsFlag(values))
		if err != nil {
			t.Fatal(err)
		}
		return reqs
	}
	t.Run("met", func(t *testing.T) {
		reqs := parse(t, "billing>=0.9", "tone=calm", "urgency>=1.5")
		if got := unmetRequirements(reqs, answers, 0.5); len(got) != 0 {
			t.Fatalf("got %q, want none", got)
		}
	})
	t.Run("unmet", func(t *testing.T) {
		reqs := parse(t, "billing>=0.999", "tone=frustrated", "urgency>=2")
		want := "billing: 99% yes, want >= 0.999\n" +
			"tone: calm, want frustrated\n" +
			"urgency: 1.80, want >= 2"
		if got := strings.Join(unmetRequirements(reqs, answers, 0), "\n"); got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})
	t.Run("missing answer", func(t *testing.T) {
		reqs := parse(t, "billing>=0.9")
		want := "billing: no answer"
		if got := strings.Join(unmetRequirements(reqs, genai.Answers{}, 0), "\n"); got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})
	t.Run("min confidence", func(t *testing.T) {
		want := "urgency: 61% confidence, want at least 70%"
		if got := strings.Join(unmetRequirements(nil, answers, 0.7), "\n"); got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})
}

func TestRequirementsExitError(t *testing.T) {
	answers := genai.Answers{
		"tone": {Type: genai.QuestionChoice, Choice: "angry", Confidence: 0.4},
	}
	t.Run("unmet", func(t *testing.T) {
		reqs, err := parseRequirements(stringsFlag{"tone=calm"})
		if err != nil {
			t.Fatal(err)
		}
		err = requirementsExitError(reqs, answers, 0)
		ee, ok := errors.AsType[*exitError](err)
		if !ok || ee.code != exitRequirementsNotMet {
			t.Fatalf("got %v, want an exitError with code %d", err, exitRequirementsNotMet)
		}
		if got, want := err.Error(), "tone: angry, want calm"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("met", func(t *testing.T) {
		if err := requirementsExitError(nil, answers, 0); err != nil {
			t.Fatalf("got %v, want nil", err)
		}
	})
}

func TestMainMinConfidence(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-Inf", "-0.1", "1.1"} {
		t.Run(value, func(t *testing.T) {
			args, flags, usage, logger := os.Args, flag.CommandLine, flag.Usage, slog.Default()
			t.Cleanup(func() {
				os.Args, flag.CommandLine, flag.Usage = args, flags, usage
				slog.SetDefault(logger)
			})
			os.Args = []string{"classy", "-min-confidence", value}
			flag.CommandLine = flag.NewFlagSet("classy", flag.ContinueOnError)
			if err := Main(); err == nil || !strings.Contains(err.Error(), "-min-confidence must be between 0 and 1") {
				t.Fatalf("got %v, want invalid confidence error", err)
			}
		})
	}
}

func TestRequirementsQuotedNames(t *testing.T) {
	n := filepath.Join(t.TempDir(), "questions.json")
	if err := os.WriteFile(n, []byte(`{
		"a=b": {"type":"choice", "instructions":"Tone?", "criteria":{" calm ":null}},
		"a>b<c": {"type":"noul", "instructions":"Yes?"},
		" spaced ": {"type":"noul", "instructions":"Yes?"},
		"a\\b\"c": {"type":"noul", "instructions":"Yes?"}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	questions, err := loadQuestions(n, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	reqs, err := parseRequirements(stringsFlag{`"a=b"=" calm "`, ` "a>b<c" >= 0.5 `, `" spaced "=1`, `"a\\b\"c"<0.5`})
	if err != nil {
		t.Fatal(err)
	}
	if err = validateRequirements(reqs, questions); err != nil {
		t.Fatal(err)
	}
	answers := genai.Answers{
		"a=b":   {Type: genai.QuestionChoice, Choice: " calm "},
		"a>b<c": {Type: genai.QuestionNoul, Noul: 0.75},
		`a\b"c`: {Type: genai.QuestionNoul, Noul: 0.25},
	}
	// Whitespace belongs to this file-declared question name.
	answers[" spaced "] = &genai.Answer{Type: genai.QuestionNoul, Noul: 1}
	if unmet := unmetRequirements(reqs, answers, 0); len(unmet) != 0 {
		t.Fatalf("unexpected unmet requirements: %v", unmet)
	}
	answers["a=b"].Choice = "angry"
	if unmet := unmetRequirements(reqs, answers, 0); len(unmet) != 1 || unmet[0] != "a=b: angry, want  calm " {
		t.Fatalf("got %v, want unmet choice requirement", unmet)
	}
}

// failingWriter simulates a closed output stream.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestPrintAnswersWriteError(t *testing.T) {
	for _, kind := range []genai.QuestionType{genai.QuestionNoul, genai.QuestionChoice, genai.QuestionScore, "unknown"} {
		t.Run(string(kind), func(t *testing.T) {
			answers := genai.Answers{"q": {Type: kind}}
			for _, quiet := range []bool{false, true} {
				if err := printAnswers(failingWriter{}, answers, quiet); !errors.Is(err, io.ErrClosedPipe) {
					t.Fatalf("quiet=%v: got %v, want closed pipe", quiet, err)
				}
			}
		})
	}
}

// closeErrorProvider supplies a provider cleanup result.
type closeErrorProvider struct {
	genai.Provider
	err error
}

func (c *closeErrorProvider) Close() error {
	return c.err
}

func TestCloseProvider(t *testing.T) {
	requirement := &exitError{code: exitRequirementsNotMet, err: errors.New("unmet requirement")}
	shutdown := errors.New("provider close failed")
	for _, tc := range []struct {
		name            string
		err             error
		closeErr        error
		wantRequirement bool
	}{
		{name: "success"},
		{name: "requirement only", err: requirement, wantRequirement: true},
		{name: "shutdown only", closeErr: shutdown},
		{name: "requirement and shutdown", err: requirement, closeErr: shutdown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := closeProvider(&closeErrorProvider{err: tc.closeErr}, tc.err)
			if ee, ok := errors.AsType[*exitError](err); ok != tc.wantRequirement || (ok && ee.code != 3) {
				t.Fatalf("got %v, want requirement exit status=%v", err, tc.wantRequirement)
			}
			if !errors.Is(err, tc.closeErr) && tc.closeErr != nil {
				t.Fatalf("lost cleanup error: %v", err)
			}
			if tc.err != nil && !strings.Contains(err.Error(), tc.err.Error()) {
				t.Fatalf("lost classification diagnostic: %v", err)
			}
			if tc.err == nil && tc.closeErr == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
