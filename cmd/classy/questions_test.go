// Copyright 2026 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Tests for the question declaration.

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadQuestions(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		data := []struct {
			name    string
			nouls   stringsFlag
			choices stringsFlag
			scores  stringsFlag
			want    string
		}{
			{
				name:  "noul",
				nouls: stringsFlag{"billing=Is this about billing?"},
				want:  `{"billing":{"type":"noul","instructions":"Is this about billing?"}}`,
			},
			{
				name:    "choice with a description",
				choices: stringsFlag{"tone=What is the tone?|calm|angry:openly hostile"},
				want:    `{"tone":{"type":"choice","instructions":"What is the tone?","criteria":{"angry":"openly hostile","calm":null}}}`,
			},
			{
				name:   "score trims the levels",
				scores: stringsFlag{"urgency=How soon?|can wait| today"},
				want:   `{"urgency":{"type":"score","instructions":"How soon?","criteria":["can wait","today"]}}`,
			},
			{
				// The instructions of a noul question run to the end, so "=" needs no escape there.
				name:  "noul instructions with an equal sign",
				nouls: stringsFlag{"math=Is 1=1?"},
				want:  `{"math":{"type":"noul","instructions":"Is 1=1?"}}`,
			},
			{
				// The instructions end at the first "|", so an "=" needs no escape there either.
				name:    "equal sign in the instructions",
				choices: stringsFlag{"math=Is 1=1?|yes|no"},
				want:    `{"math":{"type":"choice","instructions":"Is 1=1?","criteria":{"no":null,"yes":null}}}`,
			},
			{
				// A comma is only prose now, it separates nothing.
				name:    "comma in an option",
				choices: stringsFlag{"tone=What is the tone?|calm|angry, openly hostile"},
				want:    `{"tone":{"type":"choice","instructions":"What is the tone?","criteria":{"angry, openly hostile":null,"calm":null}}}`,
			},
			{
				name:   "comma in a level",
				scores: stringsFlag{"urgency=How soon?|can wait|today, or tomorrow"},
				want:   `{"urgency":{"type":"score","instructions":"How soon?","criteria":["can wait","today, or tomorrow"]}}`,
			},
			{
				name:    "escaped pipe in an option",
				choices: stringsFlag{`sep=Which separator?|a\|b|c`},
				want:    `{"sep":{"type":"choice","instructions":"Which separator?","criteria":{"a|b":null,"c":null}}}`,
			},
			{
				name:    "escaped colon in an option label",
				choices: stringsFlag{`ratio=What ratio?|1\:1|2\:1:twice as much`},
				want:    `{"ratio":{"type":"choice","instructions":"What ratio?","criteria":{"1:1":null,"2:1":"twice as much"}}}`,
			},
			{
				// Only the label is cut on ":", so a description keeps its colons.
				name:    "colon in an option description",
				choices: stringsFlag{"tone=What is the tone?|calm:like this: serene"},
				want:    `{"tone":{"type":"choice","instructions":"What is the tone?","criteria":{"calm":"like this: serene"}}}`,
			},
			{
				name:  "escaped backslash",
				nouls: stringsFlag{`esc=Is it a \\ backslash?`},
				want:  `{"esc":{"type":"noul","instructions":"Is it a \\ backslash?"}}`,
			},
			{
				// A backslash that escapes nothing is kept, so a path survives.
				name:  "backslash before another character",
				nouls: stringsFlag{`path=Is it C:\temp?`},
				want:  `{"path":{"type":"noul","instructions":"Is it C:\\temp?"}}`,
			},
		}
		for _, line := range data {
			t.Run(line.name, func(t *testing.T) {
				q, err := loadQuestions("", line.nouls, line.choices, line.scores)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := json.Marshal(q)
				if err != nil {
					t.Fatal(err)
				}
				if got := string(raw); got != line.want {
					t.Fatalf("got  %s\nwant %s", got, line.want)
				}
			})
		}
	})
	t.Run("error", func(t *testing.T) {
		data := []struct {
			name    string
			nouls   stringsFlag
			choices stringsFlag
			scores  stringsFlag
			want    string
		}{
			{
				name: "no question",
				want: "declare at least one question with -noul, -choice, -score or -questions",
			},
			{
				name:  "noul without instructions",
				nouls: stringsFlag{"billing"},
				want:  `-noul "billing": expected "<name>=<instructions>"`,
			},
			{
				name:    "choice without options",
				choices: stringsFlag{"tone=What is the tone?"},
				want:    `-choice "tone=What is the tone?": expected "<name>=<instructions>|<option>[:<description>]|..."`,
			},
			{
				name:    "choice with a duplicate option",
				choices: stringsFlag{"tone=What is the tone?|calm|calm"},
				want:    `-choice "tone=What is the tone?|calm|calm": option "calm" is specified twice`,
			},
			{
				name:   "score with an empty level",
				scores: stringsFlag{"urgency=How soon?|can wait||today"},
				want:   `-score "urgency=How soon?|can wait||today": level 1 is empty`,
			},
			{
				name:   "duplicate question",
				nouls:  stringsFlag{"urgency=Is it urgent?"},
				scores: stringsFlag{"urgency=How soon?|can wait|today"},
				want:   `question "urgency" is declared twice`,
			},
		}
		for _, line := range data {
			t.Run(line.name, func(t *testing.T) {
				_, err := loadQuestions("", line.nouls, line.choices, line.scores)
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

func TestQuestionsFromFile(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		// Every criteria form, including the structured content the flags cannot express.
		content := `{
			"billing": {"type": "noul", "instructions": "Is this about billing?",
			            "criteria": {"true": "a charge", "false": "anything else"}},
			"tone": {"type": "choice", "instructions": {"ask": "the tone"},
			         "criteria": {"calm": null, "angry": "openly hostile"}},
			"urgency": {"type": "score", "instructions": "How soon?", "criteria": ["can wait", ["today"]]}
		}`
		n := filepath.Join(t.TempDir(), "questions.json")
		if err := os.WriteFile(n, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		q, err := questionsFromFile(n)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(q)
		if err != nil {
			t.Fatal(err)
		}
		want := `{"billing":{"type":"noul","instructions":"Is this about billing?","criteria":{"true":"a charge","false":"anything else"}},` +
			`"tone":{"type":"choice","instructions":{"ask":"the tone"},"criteria":{"angry":"openly hostile","calm":null}},` +
			`"urgency":{"type":"score","instructions":"How soon?","criteria":["can wait",["today"]]}}`
		if got := string(raw); got != want {
			t.Fatalf("got  %s\nwant %s", got, want)
		}
		if err = q.Validate(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("error", func(t *testing.T) {
		data := []struct {
			name    string
			content string
			want    string
		}{
			{
				name:    "unknown type",
				content: `{"tone": {"type": "guess", "instructions": "What is the tone?"}}`,
				want:    `question "tone": field type: must be "noul", "choice" or "score", got "guess"`,
			},
			{
				name:    "unknown field",
				content: `{"tone": {"type": "choice", "options": ["calm"]}}`,
				want:    `json: unknown field "options"`,
			},
			{
				name:    "score criteria on a choice",
				content: `{"tone": {"type": "choice", "instructions": "What is the tone?", "criteria": ["calm"]}}`,
				want:    "field criteria: json: cannot unmarshal array",
			},
			{
				name:    "instructions of the wrong shape",
				content: `{"tone": {"type": "choice", "instructions": 1, "criteria": {"calm": null}}}`,
				want:    "field instructions: expected a string, a JSON object or a JSON array, got 1",
			},
		}
		for _, line := range data {
			t.Run(line.name, func(t *testing.T) {
				n := filepath.Join(t.TempDir(), "questions.json")
				if err := os.WriteFile(n, []byte(line.content), 0o600); err != nil {
					t.Fatal(err)
				}
				_, err := questionsFromFile(n)
				if err == nil {
					t.Fatal("expected an error")
				}
				if got := err.Error(); !strings.Contains(got, line.want) {
					t.Fatalf("got  %s\nwant it to contain %s", got, line.want)
				}
			})
		}
	})
}
