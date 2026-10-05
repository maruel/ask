// Copyright 2026 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Tests for the state loading and the answer printing.

package main

import (
	"bytes"
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
