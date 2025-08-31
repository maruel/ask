// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

//go:build !windows && !darwin

package ask

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/maruel/genai"
)

func getSandbox(ctx context.Context) (*genai.OptionsTools, error) {
	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, fmt.Errorf("bwrap not found (install with sudo apt install bubblewrap): %w", err)
	}
	slog.DebugContext(ctx, "bwrap", "path", bwrapPath)
	return &genai.OptionsTools{
		Tools: []genai.ToolDef{
			{
				Name:        "bash",
				Description: "Runs the requested command via bash on the linux computer and returns the output",
				Callback: func(ctx context.Context, args *bashArguments) (string, error) {
					v := []string{"--ro-bind", "/", "/", "--tmpfs", "/tmp", "--dev", "/dev", "--proc", "/proc", "--", "bash", "-c", args.CommandLine}
					cmd := exec.CommandContext(ctx, bwrapPath, v...)
					// Increases odds of success on non-English installation.
					cmd.Env = append(os.Environ(), "LANG=C")
					out, err2 := cmd.Output()
					slog.DebugContext(ctx, "bash", "command", args.CommandLine, "output", string(out), "err", err2)
					return string(out), err2
				},
			},
		},
	}, nil
}
