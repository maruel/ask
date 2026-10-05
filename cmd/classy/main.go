// Copyright 2026 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Tool classy.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
)

func main() {
	err := Main()
	if err == nil {
		return
	}
	code := 1
	if ee, ok := errors.AsType[*exitError](err); ok {
		code = ee.code
	}
	if !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "%s: %s\n", os.Args[0], err)
	}
	os.Exit(code)
}
