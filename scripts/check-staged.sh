#!/bin/bash
# Checks that the staged snapshot has a current AGENTS.md file index.

set -euo pipefail

script_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly script_dir
repo_root="$(CDPATH='' cd -- "$script_dir/.." && pwd)"
readonly repo_root

cd -- "$repo_root"

if git diff --cached --quiet; then
  exit 0
fi
if ! git diff --quiet; then
  printf '%s\n' 'Stage all tracked changes before committing; the index check reads the worktree.' >&2
  exit 1
fi
python3 scripts/update_agents_file_index.py --check
