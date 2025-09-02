// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package ask

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/maruel/genai"
)

const sb = `(version 1)

; Default policy: deny everything
(deny default)

; Deny all network access
(deny network*)

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

func getShellTool() (*genai.OptionsTools, error) {
	if _, err := exec.LookPath("/usr/bin/sandbox-exec"); err != nil {
		return nil, fmt.Errorf("sandbox-exec not found: %w", err)
	}
	if _, err := exec.LookPath("/usr/zsh"); err != nil {
		return nil, fmt.Errorf("zsh not found: %w", err)
	}
	return &genai.OptionsTools{
		Tools: []genai.ToolDef{
			{
				Name:        "zsh",
				Description: "Writes the script to a file, executes it via zsh on the macOS computer, and returns the output",
				Callback: func(ctx context.Context, args *shellArguments) (string, error) {
					askSB, err := writeTempFile("ask.*.sb", sb)
					if err != nil {
						return "", err
					}
					defer os.Remove(askSB)
					script, err := writeTempFile("ask.*.sh", args.Script)
					if err != nil {
						return "", err
					}
					defer os.Remove(script)
					cmd := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", "-f", askSB, "/bin/zsh", script)
					// Increases odds of success on non-English installation.
					cmd.Env = append(os.Environ(), "LANG=C")
					out, err2 := cmd.CombinedOutput()
					slog.DebugContext(ctx, "bash", "command", args.Script, "output", string(out), "err", err2)
					return string(out), err2
				},
			},
		},
	}, nil
}
