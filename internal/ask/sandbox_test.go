// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package ask

import (
	"encoding/json"
	"runtime"
	"testing"

	"github.com/maruel/genai"
)

func TestGetSandbox(t *testing.T) {
	opts, err := getShellTool()
	if err != nil {
		t.Fatal(err)
	}
	if opts == nil {
		t.Fatal("excepted opts")
	}
	script, want := "", ""
	if runtime.GOOS == "windows" {
		script = "Write-Output \"hi\"\n[System.Console]::Error.WriteLine(\"hello\")\n"
		want = "hi\r\nhello\r\n"
	} else {
		script = "echo hi\necho hello >&2\n"
		want = "hi\nhello\n"
	}
	b, _ := json.Marshal(&shellArguments{Script: script})
	msg := genai.Message{Replies: []genai.Reply{{ToolCall: genai.ToolCall{Name: opts.Tools[0].Name, Arguments: string(b)}}}}
	res, err := msg.DoToolCalls(t.Context(), opts.Tools)
	if err != nil {
		t.Fatalf("Got error: %v", err)
	}
	if got := res.ToolCallResults[0].Result; got != want {
		t.Fatalf("unpexpected output\nwant: %q\ngot:  %q", want, got)
	}
}
