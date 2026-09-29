# Build, verify, and test the ask command-line tools and AGENTS.md index.
.DEFAULT_GOAL := help
.PHONY: build custom-gcl git-hooks help test verify

custom-gcl:
	@version=$$(go list -m -f '{{.Version}}' github.com/golangci/golangci-lint/v2) || exit 1; \
	want=$$({ sha256sum .custom-gcl.yml | cut -d" " -f1; echo "$$version"; go env GOVERSION; } | sha256sum | cut -d" " -f1); \
	if [ -x custom-gcl ] && [ "$$want" = "$$(cat .custom-gcl.sha 2>/dev/null)" ]; then exit 0; fi; \
	echo 'Building custom-gcl with methodfilecheck and commentcheck...'; \
	go tool golangci-lint custom --version "$$version" && echo "$$want" > .custom-gcl.sha

help:
	@printf '  %-14s - %s\n' 'make build' 'Build all Go packages'
	@printf '  %-14s - %s\n' 'make verify' 'Run the static checks'
	@printf '  %-14s - %s\n' 'make test' 'Run Go tests'
	@printf '  %-14s - %s\n' 'make git-hooks' 'Install Git hooks'

build:
	@go build ./...

verify: custom-gcl
	@go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
	@./custom-gcl run --show-stats=false ./...
	@go vet ./...
	@go test -run '^$$' ./...
	@go tool addlicense -ignore pyproject.toml -ignore '**/testdata/**' -check .
	@python3 scripts/lint_binaries.py
	@python3 scripts/update_agents_file_index.py --check

test:
	@go test ./...

git-hooks:
	@./scripts/install-git-hooks.sh
