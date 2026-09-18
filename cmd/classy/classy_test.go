// Copyright 2026 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Tests for the state loading and the answer printing.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/maruel/genai/providers/typesafe"
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
	answers := typesafe.Answers{
		"billing": {Type: typesafe.QuestionNoul, Noul: 0.99},
		"tone": {
			Type: typesafe.QuestionChoice, Choice: "calm", Confidence: 0.84,
			Probabilities: map[string]float64{"angry": 0, "calm": 0.89, "frustrated": 0.11},
		},
		"urgency": {
			Type: typesafe.QuestionScore, Score: 1.8, Confidence: 0.61,
			Legend:        typesafe.ScoreLegend{"0": typesafe.Text("can wait"), "1": typesafe.Text("today")},
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
