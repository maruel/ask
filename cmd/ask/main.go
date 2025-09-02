// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Tool ask.
package main

//go:generate go run github.com/akavel/rsrc@latest -manifest ask.manifest -arch amd64
//go:generate go run github.com/akavel/rsrc@latest -manifest ask.manifest -arch arm64

import (
	"context"
	"fmt"
	"os"
)

func main() {
	if err := Main(); err != nil {
		if err != context.Canceled {
			fmt.Fprintf(os.Stderr, "%s: %s\n", os.Args[0], err)
		}
		os.Exit(1)
	}
}
