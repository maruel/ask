// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Package ask is an alias to make it easier to install.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/maruel/ask/internal/ask"
)

func main() {
	if err := ask.Main(); err != nil {
		if err != context.Canceled {
			fmt.Fprintf(os.Stderr, "%s: %s\n", os.Args[0], err)
		}
		os.Exit(1)
	}
}
