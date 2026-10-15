#!/usr/bin/env bash
# sme_review.sh — the Option Bot SME's unattended daily review.
#
# Run on the machine where the bots and the monitor run (the monitor API is
# only on localhost). It:
#   1. collects each bot's monitor data into data/sme/<date>/ (read-only GETs),
#   2. runs Claude Code headless with the option-bot-specialist skill, which
#      may only READ that folder, the configs and the docs (no shell, no
#      edits, no network, no MCP) and prints a report,
#   3. saves data/sme/<date>/report.md and shows a macOS notification,
#   4. optionally opens a GitHub issue (SME_ISSUE_REPO; refused for a public
#      repo unless SME_ALLOW_PUBLIC=1 — the report shows positions and P&L).
#
# The agent never places orders or changes config: it reports, you decide.
#
# Usage:  scripts/sme_review.sh            (normally run daily by launchd,
#                                           see scripts/install_sme_review.sh)
# Env:    MONITOR_URL       default http://127.0.0.1:3000
#         SME_MODEL         Claude model (default: Claude Code's default)
#         SME_KEEP_REPORTS  previous reports given to the agent (default 3)
#         SME_ISSUE_REPO    owner/repo to open the report as an issue (off)
#         SME_ALLOW_PUBLIC  1 = allow SME_ISSUE_REPO to be a public repo
#         SME_NOTIFY        0 = no macOS notification (default 1)
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"
MONITOR_URL="${MONITOR_URL:-http://127.0.0.1:3000}"
KEEP="${SME_KEEP_REPORTS:-3}"
DATE="$(date +%Y-%m-%d)"
OUT="data/sme/$DATE"
mkdir -p "$OUT"
LOG="$OUT/run.log"
exec > >(tee -a "$LOG") 2>&1
echo "== SME review $DATE $(date +%H:%M:%S) — repo $REPO, monitor $MONITOR_URL"

notify() { # title, message
  [[ "${SME_NOTIFY:-1}" == "1" ]] || return 0
  command -v osascript >/dev/null || return 0
  local t="${1//\"/\'}" m="${2//\"/\'}"
  osascript -e "display notification \"$m\" with title \"$t\"" >/dev/null 2>&1 || true
}

# ── 1. Collect ──────────────────────────────────────────────────────────────
manifest="$OUT/MANIFEST.md"
{
  echo "# Collected $(date -u +%Y-%m-%dT%H:%M:%SZ) from $MONITOR_URL"
  echo
} > "$manifest"

fetch() { # url, file
  if curl -fsS -m 20 "$1" -o "$OUT/$2"; then
    echo "- ok: \`$2\` ← $1" >> "$manifest"
  else
    echo "- FAILED: \`$2\` ← $1" >> "$manifest"
    rm -f "$OUT/$2"
  fi
}

fetch "$MONITOR_URL/api/bots" bots.json
bots=""
if [[ -f "$OUT/bots.json" ]]; then
  bots="$(python3 -c 'import json,sys; print(" ".join(b["name"] for b in json.load(open(sys.argv[1])).get("bots", [])))' "$OUT/bots.json" || true)"
fi
if [[ -z "$bots" ]]; then
  echo "- FAILED: no bot listed by the monitor — is \`docker compose up\` running?" >> "$manifest"
fi
for b in $bots; do
  for ep in status positions orders pnl events trades; do
    fetch "$MONITOR_URL/api/bots/$b/$ep" "$b-$ep.json"
  done
  fetch "$MONITOR_URL/api/bots/$b/pnl/history?range=1w" "$b-pnl-history-1w.json"
done
[[ -n "$bots" ]] && fetch "$MONITOR_URL/api/bots/${bots%% *}/account" account.json
cp config_btc.yaml config_eth.yaml "$OUT/" 2>/dev/null || true

mkdir -p "$OUT/previous"
# Earlier reports, newest first: the agent's memory between runs.
prev=$(find data/sme -mindepth 2 -maxdepth 2 -name report.md -not -path "$OUT/*" 2>/dev/null | sort -r | head -n "$KEEP" || true)
for r in $prev; do cp "$r" "$OUT/previous/$(basename "$(dirname "$r")").md"; done
echo "- previous reports: $(ls "$OUT/previous" | tr '\n' ' ')" >> "$manifest"

# ── 2. Review ───────────────────────────────────────────────────────────────
PROMPT="Use the option-bot-specialist skill and run its \"Daily review (agent mode)\" procedure.
Today's data is in $OUT/ (start with $OUT/MANIFEST.md; earlier reports are in $OUT/previous/).
You may also read CLAUDE.md, OptionStrategy.md and docs/ for how the bot works.
Print only the report, in the exact output shape the skill defines."

model_args=()
[[ -n "${SME_MODEL:-}" ]] && model_args=(--model "$SME_MODEL")

# ${arr[@]+...}: macOS /bin/bash 3.2 treats an empty array as unset under set -u.
if ! claude -p "$PROMPT" ${model_args[@]+"${model_args[@]}"} \
  --permission-mode dontAsk \
  --allowedTools "Read" "Grep" "Glob" "Skill" \
  --disallowedTools "Bash" "Edit" "Write" "NotebookEdit" "WebFetch" "WebSearch" "Agent" \
  --strict-mcp-config \
  --no-session-persistence \
  --output-format text < /dev/null > "$OUT/report.md.tmp"; then
  echo "claude failed — see $LOG"
  notify "Options bot SME review FAILED" "Claude Code did not finish: see $LOG"
  exit 1
fi
mv "$OUT/report.md.tmp" "$OUT/report.md"

summary="$(grep -m1 '^SUMMARY:' "$OUT/report.md" | sed 's/^SUMMARY: *//' || true)"
severity="$(grep -m1 '^SEVERITY:' "$OUT/report.md" | sed 's/^SEVERITY: *//' || true)"
[[ -n "$summary" ]] || summary="report written (no SUMMARY line)"
echo "== $severity — $summary"
echo "== report: $REPO/$OUT/report.md"

# ── 3. Tell the human ───────────────────────────────────────────────────────
notify "Options bot SME review: ${severity:-?}" "$summary"

if [[ -n "${SME_ISSUE_REPO:-}" ]]; then
  vis="$(gh repo view "$SME_ISSUE_REPO" --json visibility -q .visibility 2>/dev/null || echo UNKNOWN)"
  if [[ "$vis" != "PRIVATE" && "${SME_ALLOW_PUBLIC:-0}" != "1" ]]; then
    echo "not opening an issue: $SME_ISSUE_REPO is $vis (the report shows positions and P&L); set SME_ALLOW_PUBLIC=1 to override"
  else
    gh issue create -R "$SME_ISSUE_REPO" --title "SME review $DATE [$severity]: $summary" --body-file "$OUT/report.md" \
      && echo "issue opened in $SME_ISSUE_REPO"
  fi
fi
