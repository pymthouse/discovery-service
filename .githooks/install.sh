#!/bin/sh
set -eu

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

git_common_dir="$(git rev-parse --git-common-dir)"
hooks_dir="$git_common_dir/hooks"
source_dir=".githooks"

if [ ! -d "$hooks_dir" ]; then
	echo "Error: hooks directory not found at $hooks_dir" >&2
	exit 1
fi

if [ ! -f "$source_dir/pre-commit" ]; then
	echo "Error: $source_dir/pre-commit not found" >&2
	exit 1
fi

cp "$source_dir/pre-commit" "$hooks_dir/pre-commit"
chmod +x "$hooks_dir/pre-commit" "$source_dir/pre-commit"

echo "Installed pre-commit hook -> $hooks_dir/pre-commit"
