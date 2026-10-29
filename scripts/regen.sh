#!/usr/bin/env bash
# regen.sh — rewrites the generated files from their sources (#465): every file
# .gitattributes marks linguist-generated. They are never edited by hand, and
# two branches that each regenerated one always conflict in it, so a merge
# resolves that conflict by running the generators on the merged sources.
#
#   bash scripts/regen.sh             # regenerate every generated file
#   bash scripts/regen.sh --resolve   # during a merge: resolve the conflicts in
#       generated files by regenerating them, and stage them (never commits).
#       Exits 3, listing them, while another file still conflicts: resolve
#       those first (the generators read them), git add, then run it again.
#
# Used by dev_agent.sh after merging main into a PR branch and by pr_gate.sh
# before it sends a conflicting PR back to Ready.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

# generated: the paths on stdin that .gitattributes marks linguist-generated.
generated() {
  git check-attr --stdin linguist-generated | awk -F': ' '$3 == "true" || $3 == "set" { print $1 }'
}

regen() {
  go run ./cmd/paramdocs # docs/parameters.md — first: the help index reads it
  go run ./cmd/helpindex # frontend/content/help-index.json
  # tests/testdata/effective_config_*.json: the test rewrites them under UPDATE_GOLDEN.
  local out
  if ! out="$(UPDATE_GOLDEN=1 go test ./tests -count=1 -run '^TestConfig_EffectiveSettingsUnchanged$' 2>&1)"; then
    echo "$out" >&2
    return 1
  fi
  echo "wrote tests/testdata/effective_config_*.json"
}

case "${1:-}" in
  "") regen ;;
  --resolve)
    conflicted="$(git diff --name-only --diff-filter=U)"
    gen=""
    [[ -z "$conflicted" ]] || gen="$(printf '%s\n' "$conflicted" | generated)"
    others="$conflicted"
    [[ -z "$gen" ]] || others="$(printf '%s\n' "$conflicted" | grep -vxF -f <(printf '%s\n' "$gen") || true)"
    # Ours, for now: no conflict markers left in them while the rest is resolved.
    while IFS= read -r f; do
      [[ -z "$f" ]] || git checkout -q --ours -- "$f" 2>/dev/null || true
    done <<< "$gen"
    if [[ -n "$others" ]]; then
      echo "regen: resolve these conflicts first (git add each), then run bash scripts/regen.sh --resolve again:" >&2
      printf '%s\n' "$others" | sed 's/^/  - /' >&2
      exit 3
    fi
    regen
    git ls-files | sort -u | generated | while IFS= read -r f; do git add -- "$f"; done
    echo "regen: generated files rewritten and staged${gen:+ (conflicts resolved: $(echo "$gen" | tr '\n' ' '))}"
    ;;
  *) echo "usage: bash scripts/regen.sh [--resolve]" >&2; exit 2 ;;
esac
