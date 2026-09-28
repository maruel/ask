# Build, verify, and test the ask command-line tools and AGENTS.md index.
.DEFAULT_GOAL := verify
.PHONY: build test verify

build:
	@go build ./...

verify:
	@go vet ./...
	@go test -run '^$$' ./...
	@python3 scripts/update_agents_file_index.py --check

test:
	@go test ./...
