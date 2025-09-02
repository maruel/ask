// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

//go:build !windows && !darwin

package shelltool

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/maruel/genai"
)

func getShellTool(allowNetwork bool) (*genai.OptionsTools, error) {
	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, fmt.Errorf("bwrap not found (install with sudo apt install bubblewrap): %w", err)
	}
	if _, err := exec.LookPath("/bin/bash"); err != nil {
		return nil, fmt.Errorf("bash not found: %w", err)
	}
	return &genai.OptionsTools{
		Tools: []genai.ToolDef{
			{
				Name:        "bash",
				Description: "Writes the script to a file, executes it via bash on the macOS computer, and returns the output",
				Callback: func(ctx context.Context, args *shellArguments) (string, error) {
					script, err := writeTempFile("ask.*.sh", args.Script)
					if err != nil {
						return "", err
					}
					defer os.Remove(script)
					v := []string{
						"--ro-bind", "/", "/",
						"--tmpfs", "/tmp",
						"--dev", "/dev",
						"--proc", "/proc",
						"--bind", script, script,
					}
					if !allowNetwork {
						v = append(v, "--unshare-net")
					}
					v = append(v, "--", "/bin/bash", script)
					cmd := exec.CommandContext(ctx, bwrapPath, v...)
					// Increases odds of success on non-English installation.
					cmd.Env = append(os.Environ(), "LANG=C")
					out, err2 := cmd.CombinedOutput()
					slog.ErrorContext(ctx, "bash", "command", args.Script, "output", string(out), "err", err2)
					return string(out), err2
				},
			},
		},
	}, nil
}
