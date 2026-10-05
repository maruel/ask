// Copyright 2026 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Question declaration, from command line flags or from a JSON file.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/maruel/genai"
)

// questionsFromFile decodes the questions declared in a JSON file.
//
// The format is the one genai.Questions marshals to, i.e. an object keyed by question name, each value
// being {"type", "instructions", "criteria"}. It is the only way to describe the outcomes of a noul
// question and the options of a choice question with more than one line.
func questionsFromFile(name string) (genai.Questions, error) {
	raw, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	file := map[string]questionJSON{}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(&file); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	out := make(genai.Questions, len(file))
	for n, q := range file {
		if out[n], err = q.toQuestion(); err != nil {
			return nil, fmt.Errorf("%s: question %q: %w", name, n, err)
		}
	}
	return out, nil
}

// parseNoul parses a -noul flag value, "<name>=<instructions>".
func parseNoul(value string) (string, *genai.Question, error) {
	parts := splitEscaped(value, '=', 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", nil, errors.New("expected \"<name>=<instructions>\"")
	}
	q := &genai.Question{Type: genai.QuestionNoul, Instructions: genai.Text(unescape(parts[1]))}
	return unescape(parts[0]), q, nil
}

// parseChoice parses a -choice flag value, "<name>=<instructions>|<option>[:<description>]|...".
func parseChoice(value string) (string, *genai.Question, error) {
	n, instructions, rest, err := cutQuestion(value, "<option>[:<description>]|...")
	if err != nil {
		return "", nil, err
	}
	criteria := map[string]genai.DecisionContent{}
	for _, o := range splitEscaped(rest, '|', -1) {
		option := splitEscaped(o, ':', 2)
		label := strings.TrimSpace(unescape(option[0]))
		if label == "" {
			return "", nil, errors.New("an option is empty")
		}
		if _, dup := criteria[label]; dup {
			return "", nil, fmt.Errorf("option %q is specified twice", label)
		}
		if len(option) == 2 {
			criteria[label] = genai.Text(unescape(option[1]))
		} else {
			criteria[label] = nil
		}
	}
	q := &genai.Question{Type: genai.QuestionChoice, Instructions: genai.Text(instructions), Choice: criteria}
	return n, q, nil
}

// parseScore parses a -score flag value, "<name>=<instructions>|<level0>|<level1>|...".
func parseScore(value string) (string, *genai.Question, error) {
	n, instructions, rest, err := cutQuestion(value, "<level0>|<level1>|...")
	if err != nil {
		return "", nil, err
	}
	levels := splitEscaped(rest, '|', -1)
	criteria := make([]genai.DecisionContent, len(levels))
	for i, l := range levels {
		l = strings.TrimSpace(unescape(l))
		if l == "" {
			return "", nil, fmt.Errorf("level %d is empty", i)
		}
		criteria[i] = genai.Text(l)
	}
	q := &genai.Question{Type: genai.QuestionScore, Instructions: genai.Text(instructions), Score: criteria}
	return n, q, nil
}

// cutQuestion splits a flag value into its name, its unescaped instructions and its raw criteria.
//
// The name is chosen by the caller and only keys the answer; it is not sent to the model. criteria names
// the last field in the error message. The name ends at the first "=" and the instructions at the first
// "|", so the instructions can hold an "=" and the criteria a ",", which prose has and neither separator
// does. The criteria are returned as is because each parser splits them further; only the leaves are
// unescaped.
func cutQuestion(value, criteria string) (string, string, string, error) {
	name, rest, ok := cutEscaped(value, '=')
	if !ok {
		return "", "", "", fmt.Errorf("expected \"<name>=<instructions>|%s\"", criteria)
	}
	instructions, tail, ok := cutEscaped(rest, '|')
	if !ok || name == "" || instructions == "" || tail == "" {
		return "", "", "", fmt.Errorf("expected \"<name>=<instructions>|%s\"", criteria)
	}
	return unescape(name), unescape(instructions), tail, nil
}

// cutEscaped cuts s around the first sep that a backslash does not escape, like strings.Cut.
func cutEscaped(s string, sep byte) (string, string, bool) {
	parts := splitEscaped(s, sep, 2)
	if len(parts) != 2 {
		return s, "", false
	}
	return parts[0], parts[1], true
}

// escapable are the characters a backslash escapes in a question flag.
//
// A backslash before anything else is kept, so a Windows path or a LaTeX macro survives.
const escapable = `|:=\`

// splitEscaped splits s on sep, ignoring a sep that a backslash escapes.
//
// n limits the number of parts like strings.SplitN, the last part holding the rest of s. The parts keep
// their escapes, so that a part which is split further does not lose them; unescape the leaves.
func splitEscaped(s string, sep byte, n int) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && strings.IndexByte(escapable, s[i+1]) != -1:
			// Skip the escaped character so that it is never taken as a separator.
			i++
		case s[i] == sep && (n <= 0 || len(parts) < n-1):
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// unescape resolves the backslash escapes of a leaf field.
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && strings.IndexByte(escapable, s[i+1]) != -1 {
			i++
		}
		out = append(out, s[i])
	}
	return string(out)
}

// contentFromJSON decodes the API's string, object or array content union.
//
// It returns a nil DecisionContent for a JSON null, which is a criteria left undescribed.
//
//nolint:nilnil // a nil DecisionContent is a criteria left undescribed, not a missing value.
func contentFromJSON(b json.RawMessage) (genai.DecisionContent, error) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		return nil, nil
	}
	return genai.ParseDecisionContent(b)
}

// questionJSON is a question as declared in a -questions file.
//
// Criteria is whichever of the noul, choice and score criteria Type selects, exactly like the JSON
// genai.Question marshals to.
type questionJSON struct {
	Type         genai.QuestionType `json:"type"`
	Instructions json.RawMessage    `json:"instructions,omitzero"`
	Criteria     json.RawMessage    `json:"criteria,omitzero"`
}

// toQuestion converts the declaration to the question to ask.
func (q *questionJSON) toQuestion() (*genai.Question, error) {
	out := &genai.Question{Type: q.Type}
	var err error
	if out.Instructions, err = contentFromJSON(q.Instructions); err != nil {
		return nil, fmt.Errorf("field instructions: %w", err)
	}
	switch q.Type {
	case genai.QuestionNoul:
		if len(q.Criteria) == 0 {
			// The outcomes of a noul question are optional.
			return out, nil
		}
		var c struct {
			True  json.RawMessage `json:"true,omitzero"`
			False json.RawMessage `json:"false,omitzero"`
		}
		if err = json.Unmarshal(q.Criteria, &c); err != nil {
			return nil, fmt.Errorf("field criteria: %w", err)
		}
		out.Noul = &genai.NoulCriteria{}
		if out.Noul.True, err = contentFromJSON(c.True); err != nil {
			return nil, fmt.Errorf("field criteria.true: %w", err)
		}
		if out.Noul.False, err = contentFromJSON(c.False); err != nil {
			return nil, fmt.Errorf("field criteria.false: %w", err)
		}
	case genai.QuestionChoice:
		if len(q.Criteria) == 0 {
			return nil, errors.New("field criteria: at least one option is required")
		}
		options := map[string]json.RawMessage{}
		if err = json.Unmarshal(q.Criteria, &options); err != nil {
			return nil, fmt.Errorf("field criteria: %w", err)
		}
		out.Choice = make(map[string]genai.DecisionContent, len(options))
		for label, raw := range options {
			if out.Choice[label], err = contentFromJSON(raw); err != nil {
				return nil, fmt.Errorf("field criteria[%q]: %w", label, err)
			}
		}
	case genai.QuestionScore:
		if len(q.Criteria) == 0 {
			return nil, errors.New("field criteria: at least one level is required")
		}
		var levels []json.RawMessage
		if err = json.Unmarshal(q.Criteria, &levels); err != nil {
			return nil, fmt.Errorf("field criteria: %w", err)
		}
		out.Score = make([]genai.DecisionContent, len(levels))
		for i, raw := range levels {
			if out.Score[i], err = contentFromJSON(raw); err != nil {
				return nil, fmt.Errorf("field criteria[%d]: %w", i, err)
			}
		}
	default:
		return nil, fmt.Errorf("field type: must be %q, %q or %q, got %q",
			genai.QuestionNoul, genai.QuestionChoice, genai.QuestionScore, q.Type)
	}
	return out, nil
}
