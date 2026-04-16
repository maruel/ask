// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"runtime/debug"
	"strings"
)

// version returns the running binary's version from Go's embedded build info.
// Tagged builds return e.g. "1.2.3". Dev builds return "devel-abc1234".
// Appends "+dirty" when built from a modified working tree.
func version() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	var revision string
	var dirty bool
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	suffix := ""
	if dirty {
		suffix = "+dirty"
	}
	moduleVersion := bi.Main.Version
	if moduleVersion == "" || moduleVersion == "(devel)" {
		if revision == "" {
			return ""
		}
		short := revision
		if len(short) > 8 {
			short = short[:8]
		}
		return "devel-" + short + suffix
	}
	v := strings.TrimPrefix(moduleVersion, "v")
	if strings.HasSuffix(v, "+dirty") {
		return v
	}
	return v + suffix
}
