# Build, verify, and test the ask command-line tools and AGENTS.md index.
.DEFAULT_GOAL := verify
.PHONY: build test verify

build:
	@go build ./...

verify:
	@go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
	@go vet ./...
	@go test -run '^$$' ./...
	@python3 scripts/update_agents_file_index.py --check

test:
	@go test ./...
