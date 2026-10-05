// Copyright 2026 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Smoke test replaying a recorded HTTP session.

package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/maruel/genai"
	"github.com/maruel/genai/httprecord"
	"github.com/maruel/genai/providers/typesafe"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// TestClassify asks one question of each type about a JSON state.
//
// It replays testdata/TestClassify/*.yaml, so it needs no API key. Set RECORD=all and TYPESAFE_API_KEY to
// record it again. The recording matches on the request body, so it also asserts the questions and the
// state that are sent.
func TestClassify(t *testing.T) {
	mode := recorder.ModeRecordOnce
	if os.Getenv("RECORD") == "all" {
		mode = recorder.ModeRecordOnly
	}
	ticket := []byte(`{"subject":"Charged twice this month","body":"I see two charges of $49 for August. Please fix this ASAP, I am pretty frustrated."}`)
	questions, err := loadQuestions("",
		stringsFlag{"billing=Is this request about billing?"},
		stringsFlag{"tone=What is the tone of the customer?|calm|frustrated:annoyed but polite|angry:openly hostile"},
		stringsFlag{"urgency=How soon does this need to be handled?|can wait|this week|today|right now"})
	if err != nil {
		t.Fatal(err)
	}
	// Each subtest needs a fresh document because its contents are read once.
	state := func() *genai.Message {
		return &genai.Message{Requests: []genai.Request{requestFromState(ticket, "ticket")}}
	}

	t.Run("answers", func(t *testing.T) {
		reqs, err := parseRequirements(stringsFlag{"billing>=0.9", "tone=frustrated", "urgency>=2"})
		if err != nil {
			t.Fatal(err)
		}
		if err := validateRequirements(reqs, questions); err != nil {
			t.Fatal(err)
		}
		b := &bytes.Buffer{}
		if err = classify(t.Context(), newClient(t, "testdata/"+t.Name(), mode), state(), questions, b, classifyOptions{format: formatText, requirements: reqs, minConfidence: 0.5}); err != nil {
			t.Fatal(err)
		}
		want := hiblack + "billing" + reset + ": 99% yes\n" +
			hiblack + "tone" + reset + ": frustrated, 100% confidence\n" +
			"  angry      0.00\n" +
			"  calm       0.00\n" +
			"  frustrated 1.00\n" +
			hiblack + "urgency" + reset + ": 2.46 of 3, 52% confidence\n" +
			"  0 can wait  0.00\n" +
			"  1 this week 0.01\n" +
			"  2 today     0.52\n" +
			"  3 right now 0.47\n"
		if got := b.String(); got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})
	t.Run("json", func(t *testing.T) {
		b := &bytes.Buffer{}
		if err := classify(t.Context(), newClient(t, "testdata/"+t.Name(), mode), state(), questions, b, classifyOptions{format: formatJSON}); err != nil {
			t.Fatal(err)
		}
		want := `{"billing":{"type":"noul","noul":0.99},` +
			`"tone":{"type":"choice","choice":"frustrated","confidence":1,"probabilities":{"angry":0,"calm":0,"frustrated":1}},` +
			`"urgency":{"type":"score","score":2.52,"confidence":0.52,"legend":{"0":"can wait","1":"this week","2":"today","3":"right now"},` +
			`"probabilities":{"0":0,"1":0.01,"2":0.46,"3":0.53}}}` + "\n"
		if got := b.String(); got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})
	// Output-only cases reuse the answers cassette and never record requests.
	t.Run("write failure precedes unmet requirements", func(t *testing.T) {
		reqs, err := parseRequirements(stringsFlag{"billing<0.1"})
		if err != nil {
			t.Fatal(err)
		}
		if err := validateRequirements(reqs, questions); err != nil {
			t.Fatal(err)
		}
		err = classify(t.Context(), newClient(t, "testdata/TestClassify/answers", recorder.ModeReplayOnly), state(), questions, failingWriter{}, classifyOptions{format: formatText, quiet: true, requirements: reqs})
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("got %v, want output error", err)
		}
	})
	t.Run("jsonl", func(t *testing.T) {
		b := &bytes.Buffer{}
		if err := classify(t.Context(), newClient(t, "testdata/TestClassify/answers", recorder.ModeReplayOnly), state(), questions, b, classifyOptions{format: formatJSONL}); err != nil {
			t.Fatal(err)
		}
		want := `{"name":"billing","type":"noul","value":0.99}` + "\n" +
			`{"name":"tone","type":"choice","value":"frustrated","confidence":1,"probabilities":{"angry":0,"calm":0,"frustrated":1}}` + "\n" +
			`{"name":"urgency","type":"score","value":2.46,"confidence":0.52,"legend":{"0":"can wait","1":"this week","2":"today","3":"right now"},"probabilities":{"0":0,"1":0.01,"2":0.52,"3":0.47}}` + "\n"
		if got := b.String(); got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})
	t.Run("tsv", func(t *testing.T) {
		b := &bytes.Buffer{}
		if err := classify(t.Context(), newClient(t, "testdata/TestClassify/answers", recorder.ModeReplayOnly), state(), questions, b, classifyOptions{format: formatTSV}); err != nil {
			t.Fatal(err)
		}
		want := "billing\tnoul\t0.99\t\n" +
			"tone\tchoice\tfrustrated\t1\n" +
			"urgency\tscore\t2.46\t0.52\n"
		if got := b.String(); got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})
}

// newClient returns a client using the named cassette and recording mode.
func newClient(t *testing.T, name string, mode recorder.Mode) genai.Provider {
	opts := []genai.ProviderOption{
		genai.ModelGood,
		genai.ProviderOptionTransportWrapper(func(h http.RoundTripper) http.RoundTripper {
			r, err := httprecord.New(name, h, recorder.WithMode(mode))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := r.Stop(); err != nil {
					t.Error(err)
				}
			})
			return r
		}),
	}
	if mode == recorder.ModeReplayOnly || os.Getenv("TYPESAFE_API_KEY") == "" {
		opts = append(opts, genai.ProviderOptionAPIKey("<insert_api_key_here>"))
	}
	c, err := typesafe.New(t.Context(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	return c
}
