// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package ask

import (
	"context"
	"log/slog"
	"os"
	"os/exec"

	"github.com/maruel/genai"
)

const sb = `(version 1)

; Default policy: deny everything
(deny default)

; Allow process execution
(allow process-exec)

; Allow read-only access to files
(allow file-read*)

; Deny all file write operations
(deny file-write*)

; Allow basic system services needed for execution
(allow sysctl-read)
(allow mach-lookup)

; Allow write to /tmp
(allow file-write* (subpath "/tmp"))
`

// sandbox-exec -f readonly.sb /bin/bash
func getSandbox(ctx context.Context) (*genai.OptionsTools, error) {
	return &genai.OptionsTools{
		Tools: []genai.ToolDef{
			{
				Name:        "bash",
				Description: "Runs the requested command via bash on the macOS computer and returns the output",
				Callback: func(ctx context.Context, args *bashArguments) (string, error) {
					f, err := os.CreateTemp("", "ask.*.sb")
					if err != nil {
						return "", err
					}
					n := f.Name()
					f.WriteString(sb)
					f.Close()
					cmd := exec.CommandContext(ctx, "sandbox-exec", "-f", n, "bash", "-c", args.CommandLine)
					// Increases odds of success on non-English installation.
					cmd.Env = append(os.Environ(), "LANG=C")
					out, err2 := cmd.Output()
					slog.DebugContext(ctx, "bash", "command", args.CommandLine, "output", string(out), "err", err2)
					_ = os.Remove(n)
					return string(out), err2
				},
			},
		},
	}, nil
}
