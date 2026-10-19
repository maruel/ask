# Verify and test the ask command-line tools and AGENTS.md index.
.DEFAULT_GOAL := help
.PHONY: custom-gcl fix git-hooks help test verify

custom-gcl:
	@version=$$(go list -m -f '{{.Version}}' github.com/golangci/golangci-lint/v2) || exit 1; \
	want=$$(uv run --locked python -c 'import hashlib, pathlib, sys; print(hashlib.sha256(pathlib.Path(sys.argv[1]).read_bytes() + "\n".join(sys.argv[2:]).encode()).hexdigest())' .custom-gcl.yml "$$version" "$$(go env GOVERSION)") || exit 1; \
	binary=custom-gcl; if [ "$$(go env GOOS)" = windows ]; then binary=custom-gcl.exe; fi; \
	if [ -x "$$binary" ] && [ "$$want" = "$$(cat .custom-gcl.sha 2>/dev/null)" ]; then exit 0; fi; \
	echo 'Building custom-gcl with methodfilecheck and commentcheck...'; \
	go tool golangci-lint custom --version "$$version" && echo "$$want" > .custom-gcl.sha

help:
	@printf '  %-14s - %s\n' 'make help' 'Show available targets'
	@printf '  %-14s - %s\n' 'make fix' 'Format sources and refresh the agent index'
	@printf '  %-14s - %s\n' 'make verify' 'Run static checks without tests'
	@printf '  %-14s - %s\n' 'make test' 'Run tests with coverage and benchmark smoke tests'
	@printf '  %-14s - %s\n' 'make git-hooks' 'Install Git hooks'

verify: custom-gcl
	@go mod tidy -diff
	@go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
	@./custom-gcl run --show-stats=false ./...
	@go test -run '^$$' ./...
	@go tool addlicense -ignore pyproject.toml -ignore '**/testdata/**' -ignore '.venv/**' -ignore '.ruff_cache/**' -check .
	@uv run --locked python scripts/lint_binaries.py
	@uv run --locked python scripts/update_agents_file_index.py --check
	@uv run --locked ruff check --quiet scripts
	@uv run --locked ruff format --check --quiet scripts

test:
	@go test -timeout=600s -v -covermode=count -coverprofile coverage.txt -bench=. -benchtime=1x ./...

git-hooks:
	@./scripts/install-git-hooks.sh

fix: custom-gcl
	@./custom-gcl fmt
	@./custom-gcl run --show-stats=false --fix ./...
	@uv run --locked ruff check --quiet --fix scripts
	@uv run --locked ruff format scripts >/dev/null
	@uv run --locked python scripts/update_agents_file_index.py
