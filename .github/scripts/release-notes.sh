#!/usr/bin/env bash
# Writes release notes for the commits in FROM..TO to OUT, following
# .github/release-notes.md. An OpenCode agent reads the commits and writes
# the notes; if it fails, OUT gets a plain list of the commits instead.
#
#   release-notes.sh FROM TO OUT
#
# NOTES_MODEL picks the model (provider/model) and the provider's API key
# comes from its usual environment variable, such as OPENCODE_API_KEY for
# OpenCode Go.
set -euo pipefail

from=$1 to=$2 out=$3
repo=${GITHUB_REPOSITORY:-kyledickey/shed}
model=${NOTES_MODEL:-opencode-go/kimi-k3}
rm -f "$out"

# The agent may read history and write files, nothing else.
config="$(mktemp)"
trap 'rm -f "$config"' EXIT
cat >"$config" <<'JSON'
{
  "$schema": "https://opencode.ai/config.json",
  "permission": {
    "edit": "allow",
    "webfetch": "deny",
    "bash": { "*": "deny", "git log*": "allow", "git show*": "allow", "git diff*": "allow" }
  }
}
JSON

prompt="Write the release notes for the commits in $from..$to of github.com/$repo \
and save them to the file $out. Follow .github/release-notes.md exactly. Read the \
commits with git log and git show, and read the files they change when a message \
doesn't say what changed for operators. The file must hold only the notes."

if ! OPENCODE_CONFIG="$config" timeout 15m bunx opencode-ai@1.19.5 run --model "$model" "$prompt" >&2 ||
  [ ! -s "$out" ]; then
  echo "::warning::The model did not write release notes; using the commit list."
  git log --no-merges --format="- %s ([%h](https://github.com/$repo/commit/%h))" "$from..$to" >"$out"
fi
