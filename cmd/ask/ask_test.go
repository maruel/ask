// Copyright 2026 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Tests for the model listing.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/maruel/genai"
	"github.com/maruel/genai/scoreboard"
)

// fakeModel implements genai.Model for the listing tests.
type fakeModel struct {
	id  string
	str string
	ctx int64
}

// GetID implements genai.Model.
func (m fakeModel) GetID() string { return m.id }

// String implements genai.Model.
func (m fakeModel) String() string {
	if m.str != "" {
		return m.str
	}
	return m.id
}

// Context implements genai.Model.
func (m fakeModel) Context() int64 { return m.ctx }

// textScenario returns a text scenario selecting the given models.
func textScenario(models []string, sota, good, cheap bool) scoreboard.Scenario {
	return scoreboard.Scenario{
		Models: models,
		SOTA:   sota,
		Good:   good,
		Cheap:  cheap,
		Out:    map[scoreboard.Modality]scoreboard.ModalCapability{scoreboard.ModalityText: {}},
	}
}

func TestModelTiers(t *testing.T) {
	s := scoreboard.Score{Scenarios: []scoreboard.Scenario{
		textScenario([]string{"best"}, true, false, false),
		textScenario([]string{"everyday"}, false, true, false),
		textScenario([]string{"cheapo"}, false, false, true),
		textScenario([]string{"shared"}, true, true, true),
		textScenario([]string{"shared"}, false, true, true),
		{
			Models: []string{"painter"},
			SOTA:   true,
			Out:    map[scoreboard.Modality]scoreboard.ModalCapability{scoreboard.ModalityImage: {}},
		},
		// A plain scenario selects nothing automatically.
		textScenario([]string{"plain", "another"}, false, false, false),
	}}
	got := modelTiers(&s)
	want := map[string][]string{
		"best":     {"sota"},
		"everyday": {"good"},
		"cheapo":   {"cheap"},
		"painter":  {"sota image"},
		"shared":   {"cheap", "good", "sota"},
	}
	if !maps.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}
}

func TestPrintModels(t *testing.T) {
	s := scoreboard.Score{Scenarios: []scoreboard.Scenario{
		textScenario([]string{"best"}, true, false, false),
	}}
	models := []genai.Model{
		fakeModel{id: "best", str: "best: The Best (2026-01-01)"},
		fakeModel{id: "plain", str: "plain: Plain (2026-01-01)"},
	}
	b := &bytes.Buffer{}
	printModels(b, models, &s)
	want := "best: The Best (2026-01-01)  [sota]\n" +
		"plain: Plain (2026-01-01)\n"
	if got := b.String(); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestValidateListModels(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		if err := validateListModels(nil, nil, nil, false, false); err != nil {
			t.Fatal(err)
		}
	})
	// A system prompt in the environment is not set on the command line, so it must not fail.
	t.Run("environment system prompt", func(t *testing.T) {
		if err := validateListModels(map[string]bool{}, nil, nil, false, false); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("error", func(t *testing.T) {
		data := []struct {
			name     string
			explicit map[string]bool
			args     []string
			files    stringsFlag
			shell    bool
			web      bool
			want     string
		}{
			{name: "arguments", args: []string{"hi"}, want: "cannot use -list-models with arguments"},
			{name: "files", files: stringsFlag{"a.txt"}, want: "cannot use -list-models with files"},
			{name: "system prompt", explicit: map[string]bool{"sys": true}, want: "cannot use -list-models with -sys"},
			{name: "shell", shell: true, want: "cannot use -list-models with -shell"},
			{name: "web", web: true, want: "cannot use -list-models with -web"},
		}
		for _, line := range data {
			t.Run(line.name, func(t *testing.T) {
				err := validateListModels(line.explicit, line.args, line.files, line.shell, line.web)
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

func TestCitationTypeName(t *testing.T) {
	data := []struct {
		in   genai.CitationType
		want string
	}{
		{genai.CitationWebQuery, "web_query"},
		{genai.CitationWeb, "web"},
		{genai.CitationWebImage, "web_image"},
		{genai.CitationDocument, "document"},
		{genai.CitationTool, "tool"},
		{genai.CitationType(0), "unknown"},
	}
	for _, line := range data {
		if got := citationTypeName(line.in); got != line.want {
			t.Fatalf("%d: got %q, want %q", line.in, got, line.want)
		}
	}
}

func TestHasStdinData(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = devNull.Close() }()
	if hasStdinData(devNull) {
		t.Fatal("got true for /dev/null, want false")
	}
	empty, err := os.CreateTemp(t.TempDir(), "empty")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = empty.Close() }()
	if hasStdinData(empty) {
		t.Fatal("got true for an empty file, want false")
	}
	full, err := os.CreateTemp(t.TempDir(), "full")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = full.Close() }()
	if _, err = full.WriteString("data"); err != nil {
		t.Fatal(err)
	}
	if got := hasStdinData(full); got != true {
		t.Fatalf("got %t for a file with data, want true", got)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()
	if got := hasStdinData(r); got != true {
		t.Fatalf("got %t for a pipe, want true", got)
	}
}

func TestOutputText(t *testing.T) {
	frags := []genai.Reply{
		{Reasoning: "let me think"},
		{Text: "Hello"},
		{Text: " world"},
		{Reasoning: "more"},
		{Citation: genai.Citation{Sources: []genai.CitationSource{{Type: genai.CitationWeb, Title: "T", URL: "https://example.com"}}}},
	}
	t.Run("verbose", func(t *testing.T) {
		b := &bytes.Buffer{}
		out := newOutput(b, false, false)
		for i := range frags {
			out.add(&frags[i])
		}
		out.finish()
		if out.text.Len() != 0 || out.reasoning.Len() != 0 || len(out.citations) != 0 || len(out.seen) != 0 {
			t.Fatal("text streaming retains JSON content")
		}
		want := hiblack + "Reasoning: " + reset + "let me think\n\n" +
			hiblack + "Answer: " + reset + "Hello world\n\n" +
			hiblack + "Reasoning: " + reset + "more\n\n" +
			hiblack + "Citation:\n" + reset + "  - T / https://example.com\n"
		if got := b.String(); got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})
	t.Run("quiet", func(t *testing.T) {
		b := &bytes.Buffer{}
		out := newOutput(b, true, false)
		for i := range frags {
			out.add(&frags[i])
		}
		out.finish()
		if out.text.Len() != 0 || out.reasoning.Len() != 0 || len(out.citations) != 0 || len(out.seen) != 0 {
			t.Fatal("quiet streaming retains JSON content")
		}
		want := "Hello world\n"
		if got := b.String(); got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})
}

func TestOutputJSON(t *testing.T) {
	frags := []genai.Reply{
		{Text: "Hello"},
		{Reasoning: "think"},
		{Citation: genai.Citation{Sources: []genai.CitationSource{
			{Type: genai.CitationWeb, Title: "T", URL: "https://example.com"},
			{Type: genai.CitationWeb, Title: "T", URL: "https://example.com"},
		}}},
		{Text: " world"},
	}
	t.Run("ok", func(t *testing.T) {
		b := &bytes.Buffer{}
		// quiet is ignored when printing JSON.
		out := newOutput(b, true, true)
		for i := range frags {
			out.add(&frags[i])
		}
		out.finish()
		out.addFile("out.png")
		if s := b.String(); s != "" {
			t.Fatalf("got %q, want nothing before writeJSON", s)
		}
		usage := genai.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5, FinishReason: genai.FinishedStop}
		if err := out.writeJSON("prov", "model", &usage, nil); err != nil {
			t.Fatal(err)
		}
		want := `{"provider":"prov","model":"model","text":"Hello world","reasoning":"think",` +
			`"citations":[{"type":"web","title":"T","url":"https://example.com"}],` +
			`"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5,"finish_reason":"stop"},` +
			`"files":["out.png"]}` + "\n"
		if got := b.String(); got != want {
			t.Fatalf("got  %s\nwant %s", got, want)
		}
	})
	t.Run("error", func(t *testing.T) {
		b := &bytes.Buffer{}
		out := newOutput(b, false, true)
		if err := out.writeJSON("p", "m", &genai.Usage{}, errors.New("boom")); err != nil {
			t.Fatal(err)
		}
		want := `{"provider":"p","model":"m","text":"","usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0},"error":"boom"}` + "\n"
		if got := b.String(); got != want {
			t.Fatalf("got  %s\nwant %s", got, want)
		}
	})
}

func TestFindAvailable(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "content.png")
	if got := findAvailable(first); got != first {
		t.Fatalf("got %q, want %q", got, first)
	}
	if err := os.WriteFile(first, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(dir, "content_1.png")
	if got := findAvailable(first); got != second {
		t.Fatalf("got %q, want %q", got, second)
	}
	if err := os.WriteFile(second, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	third := filepath.Join(dir, "content_2.png")
	if got := findAvailable(first); got != third {
		t.Fatalf("got %q, want %q", got, third)
	}
}

// fakeHTTPProvider implements just enough of genai.Provider for downloadDoc.
type fakeHTTPProvider struct {
	genai.Provider
	client *http.Client
}

// HTTPClient implements genai.Provider.
func (f *fakeHTTPProvider) HTTPClient() *http.Client { return f.client }

func TestDownloadDoc(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte("data"))
		case "/bad":
			http.Error(w, "nope", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	p := &fakeHTTPProvider{client: srv.Client()}

	t.Run("url", func(t *testing.T) {
		b, err := downloadDoc(t.Context(), p, &genai.Reply{Doc: genai.Doc{URL: srv.URL + "/ok"}})
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != "data" {
			t.Fatalf("got %q, want data", b)
		}
	})
	t.Run("inline", func(t *testing.T) {
		b, err := downloadDoc(t.Context(), p, &genai.Reply{Doc: genai.Doc{Src: strings.NewReader("inline")}})
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != "inline" {
			t.Fatalf("got %q, want inline", b)
		}
	})
	t.Run("status", func(t *testing.T) {
		if _, err := downloadDoc(t.Context(), p, &genai.Reply{Doc: genai.Doc{URL: srv.URL + "/bad"}}); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("deadline", func(t *testing.T) {
		for _, timeout := range []time.Duration{0, 5 * time.Minute} {
			t.Run(timeout.String(), func(t *testing.T) {
				ctx := t.Context()
				if timeout != 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, timeout)
					t.Cleanup(cancel)
				}
				p := &fakeHTTPProvider{client: &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
					got, gotOK := r.Context().Deadline()
					want, wantOK := ctx.Deadline()
					if gotOK != wantOK || !got.Equal(want) {
						t.Errorf("download deadline = %v (%t), want %v (%t)", got, gotOK, want, wantOK)
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data"))}, nil
				})}}
				if _, err := downloadDoc(ctx, p, &genai.Reply{Doc: genai.Doc{URL: "https://example.com/media"}}); err != nil {
					t.Fatal(err)
				}
			})
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := downloadDoc(ctx, p, &genai.Reply{Doc: genai.Doc{URL: srv.URL + "/ok"}})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context.Canceled", err)
		}
	})
}

// testTransport implements an HTTP transport at the download boundary.
type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fakeRequestProvider returns a fixed stream and result without a network request.
type fakeRequestProvider struct {
	fakeHTTPProvider
	result genai.Result
	err    error
}

func (f *fakeRequestProvider) Name() string    { return "test" }
func (f *fakeRequestProvider) ModelID() string { return "test-model" }

func (f *fakeRequestProvider) GenStream(context.Context, genai.Messages, ...genai.GenOption) (iter.Seq[genai.Reply], func() (genai.Result, error)) {
	return slices.Values(f.result.Replies), func() (genai.Result, error) { return f.result, f.err }
}

func TestExecRequest(t *testing.T) {
	streamErr := errors.New("stream failed")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no media", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	cases := []struct {
		name    string
		docs    []genai.Doc
		err     error
		wantErr string
		files   []string
	}{
		{name: "success", docs: []genai.Doc{{Filename: "saved.png", Src: strings.NewReader("media")}}, files: []string{"saved.png"}},
		{name: "stream error", err: streamErr, wantErr: "stream failed"},
		{name: "download error", docs: []genai.Doc{{Filename: "failed.png", URL: srv.URL}}, wantErr: "status code 500"},
		{name: "write error", docs: []genai.Doc{{Filename: "missing/failed.png", Src: strings.NewReader("media")}}, wantErr: "missing"},
		{
			name: "partial success and joined errors",
			docs: []genai.Doc{
				{Filename: "saved.png", Src: strings.NewReader("media")},
				{Filename: "failed.png", URL: srv.URL},
			},
			err: streamErr, wantErr: "status code 500", files: []string{"saved.png"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			f, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			old := os.Stdout
			os.Stdout = f
			t.Cleanup(func() {
				os.Stdout = old
				_ = f.Close()
			})
			p := &fakeRequestProvider{
				client: srv.Client(),
				result: genai.Result{
					Message: genai.Message{Replies: []genai.Reply{{Text: "answer"}}},
					Usage:   genai.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5},
				},
				err: tc.err,
			}
			for _, d := range tc.docs {
				p.result.Replies = append(p.result.Replies, genai.Reply{Doc: d})
			}
			err = execRequest(t.Context(), p, nil, nil, requestOptions{asJSON: true})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("got %v, want error containing %q", err, tc.wantErr)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("lost stream error: %v", err)
			}
			if _, err2 := f.Seek(0, io.SeekStart); err2 != nil {
				t.Fatal(err2)
			}
			dec := json.NewDecoder(f)
			var got jsonResult
			if err2 := dec.Decode(&got); err2 != nil {
				t.Fatalf("no JSON result: %v", err2)
			}
			if got.Text != "answer" || got.Provider != "test" || got.Model != "test-model" || got.Usage.TotalTokens != 5 {
				t.Fatalf("incomplete result: %+v", got)
			}
			if err != nil && got.Error != err.Error() || err == nil && got.Error != "" {
				t.Fatalf("JSON error %q differs from returned error %v", got.Error, err)
			}
			if !slices.Equal(got.Files, tc.files) {
				t.Fatalf("files = %v, want %v", got.Files, tc.files)
			}
			for _, n := range got.Files {
				b, err2 := os.ReadFile(n)
				if err2 != nil || string(b) != "media" {
					t.Fatalf("file %s: %q, %v", n, b, err2)
				}
			}
			if err2 := dec.Decode(&got); !errors.Is(err2, io.EOF) {
				t.Fatalf("unexpected trailing output: %v", err2)
			}
		})
	}
}
