// Copyright 2026 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Smoke test replaying a recorded HTTP session.

package main

import (
	"bytes"
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
	ticket := []byte(`{"subject":"Charged twice this month","body":"I see two charges of $49 for August. Please fix this ASAP, I am pretty frustrated."}`)
	questions, err := loadQuestions("",
		stringsFlag{"billing=Is this request about billing?"},
		stringsFlag{"tone=What is the tone of the customer?|calm|frustrated:annoyed but polite|angry:openly hostile"},
		stringsFlag{"urgency=How soon does this need to be handled?|can wait|this week|today|right now"})
	if err != nil {
		t.Fatal(err)
	}
	// The state is a JSON object, so it is sent as a document. It is rebuilt per subtest because the
	// document is read once.
	state := func() *genai.Message {
		return &genai.Message{Requests: []genai.Request{requestFromState(ticket, "ticket")}}
	}

	t.Run("answers", func(t *testing.T) {
		b := &bytes.Buffer{}
		if err := classify(t.Context(), newClient(t), state(), questions, b, false, false); err != nil {
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
		if err := classify(t.Context(), newClient(t), state(), questions, b, true, false); err != nil {
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
}

// newClient returns a client replaying, or recording, the HTTP session of the test.
func newClient(t *testing.T) genai.Provider {
	mode := recorder.ModeRecordOnce
	if os.Getenv("RECORD") == "all" {
		mode = recorder.ModeRecordOnly
	}
	opts := []genai.ProviderOption{
		genai.ModelGood,
		genai.ProviderOptionTransportWrapper(httprecord.Wrap(t, recorder.WithMode(mode))),
	}
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		opts = append(opts, genai.ProviderOptionAPIKey("<insert_api_key_here>"))
	}
	c, err := typesafe.New(t.Context(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
