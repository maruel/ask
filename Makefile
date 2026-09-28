# Build, verify, and test the ask command-line tools and AGENTS.md index.
.DEFAULT_GOAL := help
.PHONY: build git-hooks help test verify

help:
	@printf '  %-14s - %s\n' 'make build' 'Build all Go packages'
	@printf '  %-14s - %s\n' 'make verify' 'Run the static checks'
	@printf '  %-14s - %s\n' 'make test' 'Run Go tests'
	@printf '  %-14s - %s\n' 'make git-hooks' 'Install Git hooks'

build:
	@go build ./...

verify:
	@go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
	@go vet ./...
	@go test -run '^$$' ./...
	@go tool addlicense -ignore pyproject.toml -ignore '**/testdata/**' -check .
	@python3 scripts/update_agents_file_index.py --check

test:
	@go test ./...

git-hooks:
	@./scripts/install-git-hooks.sh
