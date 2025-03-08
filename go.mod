module github.com/maruel/ask

go 1.24.1

require (
	github.com/lmittmann/tint v1.0.7
	github.com/maruel/genai v0.0.0-20250308171658-9a8171775900
	github.com/mattn/go-colorable v0.1.14
	github.com/mattn/go-isatty v0.0.20
)

require (
	github.com/DataDog/zstd v1.5.6 // indirect
	github.com/andybalholm/brotli v1.1.1 // indirect
	github.com/maruel/httpjson v0.0.0-20250308153423-d73fcd966e61 // indirect
	golang.org/x/sys v0.31.0 // indirect
)

// replace github.com/maruel/genai => ../genai
// replace github.com/maruel/httpjson => ../httpjson
