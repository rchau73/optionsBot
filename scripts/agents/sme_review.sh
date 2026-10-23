#!/usr/bin/env bash
# sme_review.sh — the SME agent's daily review (option-bot-specialist skill).
#
#   1. collects each bot's monitor data, the configs, the agent's last
#      reports and its open/recently closed issues into data/sme/<date>/;
#   2. runs Claude Code headless and READ-ONLY (no shell, edits, network or
#      MCP): it prints a report plus a machine-readable findings block;
#   3. the script (not the agent) publishes: one GitHub issue per new finding
#      on the board's Triage column, a comment on an issue seen again, a
#      comment on one no longer seen, and a verification comment on recently
#      closed issues (reopened when the fix did not work);
#   4. saves report.md and shows a macOS notification.
#
# On testnet the issues carry the full evidence (fake money). If any bot runs
# live, the agent is told to leave amounts out of issue bodies; the full
# report stays local.
#
# Env: MONITOR_URL, SME_MODEL, SME_KEEP_REPORTS (3), SME_PUBLISH (1 = issues),
#      AGENT_NOTIFY (1).
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
take_lock sme || exit 0

KEEP="${SME_KEEP_REPORTS:-3}"
PUBLISH="${SME_PUBLISH:-1}"
DATE="$(date +%Y-%m-%d)"
OUT="data/sme/$DATE"
mkdir -p "$OUT/previous"
exec > >(tee -a "$OUT/run.log") 2>&1
run_begin sme "" "" "$OUT" # the run ledger (lib.sh): every exit below ends it
log "SME review $DATE — repo $AGENT_REPO_DIR, monitor $MONITOR_URL"

# ── 1. Collect ──────────────────────────────────────────────────────────────
manifest="$OUT/MANIFEST.md"
printf '# Collected %s from %s\n\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$MONITOR_URL" > "$manifest"
fetch() { # url, file [token: default MONITOR_TOKEN, stable's]
  if monitor_curl "${3-$MONITOR_TOKEN}" -fsS -m 20 "$1" -o "$OUT/$2"; then echo "- ok: \`$2\` ← $1" >> "$manifest"
  else echo "- FAILED: \`$2\` ← $1" >> "$manifest"; rm -f "$OUT/$2"; fi
}
fetch "$MONITOR_URL/api/bots" bots.json
bots=""
[[ -f "$OUT/bots.json" ]] && bots="$(py 'import json,sys; print(" ".join(b["name"] for b in json.load(open(sys.argv[1])).get("bots", [])))' "$OUT/bots.json" || true)"
[[ -n "$bots" ]] || echo "- FAILED: no bot listed by the monitor — is \`docker compose up\` running?" >> "$manifest"
for b in $bots; do
  for ep in status positions orders pnl events trades; do fetch "$MONITOR_URL/api/bots/$b/$ep" "$b-$ep.json"; done
  fetch "$MONITOR_URL/api/bots/$b/pnl/history?range=1w" "$b-pnl-history-1w.json"
done
[[ -n "$bots" ]] && fetch "$MONITOR_URL/api/bots/${bots%% *}/account" account.json
# The mvp environment runs main (merged, not yet released): closed issues are
# verified there. Same files under mvp/; absent when mvp is not running.
mkdir -p "$OUT/mvp"
echo "- stable (performance, the track record): $MONITOR_URL · mvp (main, where fixes land first): $MVP_MONITOR_URL → mvp/" >> "$manifest"
if monitor_curl "$MVP_MONITOR_TOKEN" -fsS -m 10 "$MVP_MONITOR_URL/api/bots" -o "$OUT/mvp/bots.json" 2>/dev/null; then
  for b in $(py 'import json,sys; print(" ".join(b["name"] for b in json.load(open(sys.argv[1])).get("bots", [])))' "$OUT/mvp/bots.json" || true); do
    for ep in status positions events trades pnl; do fetch "$MVP_MONITOR_URL/api/bots/$b/$ep" "mvp/$b-$ep.json" "$MVP_MONITOR_TOKEN"; done
  done
else
  echo "- mvp: not reachable (not running?)" >> "$manifest"
fi
cp config_btc.yaml config_eth.yaml "$OUT/" 2>/dev/null || true

prev=$(find data/sme -mindepth 2 -maxdepth 2 -name report.md -not -path "$OUT/*" 2>/dev/null | sort -r | head -n "$KEEP" || true)
for r in $prev; do cp "$r" "$OUT/previous/$(basename "$(dirname "$r")").md"; done
echo "- previous reports: $(ls "$OUT/previous" | tr '\n' ' ')" >> "$manifest"

ENVIRONMENT="$(bots_environment)"
REDACT=0; [[ "$ENVIRONMENT" == "live" ]] && REDACT=1
echo "- environment: $ENVIRONMENT (issue bodies redacted: $REDACT)" >> "$manifest"

ISSUES="$AGENT_REPO_DIR/scripts/agents/sme_issues.py"
slug=""
if [[ "$PUBLISH" == "1" ]] && slug="$(repo_slug 2>/dev/null)"; then
  # Every issue, not only agent:sme ones: a ticket a human filed (often parked
  # in Triage to collect data) is the same problem when the agent sees it
  # again (2026-10-16: #106 and #107 re-filed #86/#103). Bodies and last
  # comments let the agent match by problem, not by title.
  board_cards > "$OUT/board.txt" 2>/dev/null || : > "$OUT/board.txt"
  if gh issue list -R "$slug" --state open -L 200 --json number,title,labels,body,comments > "$OUT/open_raw.json"; then
    python3 "$ISSUES" shape "$OUT/open_raw.json" "$OUT/board.txt" "$OUT/open_issues.json"
  else echo '[]' > "$OUT/open_issues.json"; fi
  since="$(date -v-14d +%Y-%m-%d 2>/dev/null || date -d '-14 days' +%Y-%m-%d)"
  if gh issue list -R "$slug" --state closed -L 100 --search "closed:>=$since" --json number,title,labels,body,comments,closedAt > "$OUT/closed_raw.json"; then
    python3 "$ISSUES" shape "$OUT/closed_raw.json" "$OUT/board.txt" "$OUT/closed_issues.json" closed
  else echo '[]' > "$OUT/closed_issues.json"; fi
  rm -f "$OUT/open_raw.json" "$OUT/closed_raw.json"
  echo "- open issues (every label, with board column, body and last comments): open_issues.json · closed in the last 14 days: closed_issues.json" >> "$manifest"
else
  PUBLISH=0
  echo '[]' > "$OUT/open_issues.json"; echo '[]' > "$OUT/closed_issues.json"
fi

# ── 2. Review (read-only agent) ─────────────────────────────────────────────
PROMPT="Use the option-bot-specialist skill and run its \"Daily review (agent mode)\" procedure.
Today's data is in $OUT/ (start with $OUT/MANIFEST.md; earlier reports are in $OUT/previous/;
every open issue and those closed in the last 14 days, with bodies and last comments, are in
$OUT/open_issues.json and $OUT/closed_issues.json — never open a duplicate of one of them).
Top-level files are the STABLE environment (a promoted release: judge performance and risk there);
$OUT/mvp/ holds the same data from the MVP environment (main: merged fixes run there first, so verify
closed issues against mvp/, and against stable only once a release containing the fix is promoted).
Environment: $ENVIRONMENT. Redact amounts in issue bodies: $REDACT.
You may also read CLAUDE.md, OptionStrategy.md and docs/ for how the bot works.
Print only the report in the exact output shape the skill defines, ending with the FINDINGS-JSON block."
model_args=()
[[ -n "${SME_MODEL:-}" ]] && model_args=(--model "$SME_MODEL")
# ${arr[@]+...}: macOS /bin/bash 3.2 treats an empty array as unset under set -u.
if ! claude -p "$PROMPT" ${model_args[@]+"${model_args[@]}"} \
  --permission-mode dontAsk \
  --allowedTools "Read" "Grep" "Glob" "Skill" \
  --disallowedTools "Bash" "Edit" "Write" "NotebookEdit" "WebFetch" "WebSearch" "Agent" \
  --strict-mcp-config --no-session-persistence \
  --output-format text < /dev/null > "$OUT/report.md.tmp"; then
  log "claude failed"
  notify "Options bot SME review FAILED" "Claude Code did not finish: see $OUT/run.log"
  if grep -qiE "$LIMIT_PATTERN" "$OUT/report.md.tmp" 2>/dev/null; then run_end skipped_limit "claude hit a usage limit"
  else run_end failed "claude did not finish"; fi
  exit 1
fi
mv "$OUT/report.md.tmp" "$OUT/report.md"

# The findings block → findings.json (the report stays human-readable).
py 'import json,re,sys
t = open(sys.argv[1]).read()
m = re.search(r"<!-- FINDINGS-JSON\s*(\{.*?\})\s*-->", t, re.S)
json.dump(json.loads(m.group(1)) if m else {"issues": [], "resolved": [], "verifications": []}, open(sys.argv[2], "w"), indent=2)
print("findings block:", "found" if m else "MISSING")' "$OUT/report.md" "$OUT/findings.json"

summary="$(grep -m1 '^SUMMARY:' "$OUT/report.md" | sed 's/^SUMMARY: *//' || true)"
severity="$(grep -m1 '^SEVERITY:' "$OUT/report.md" | sed 's/^SEVERITY: *//' || true)"
[[ -n "$summary" ]] || summary="report written (no SUMMARY line)"
log "$severity — $summary"

# ── 3. Publish (the script acts, never the agent) ───────────────────────────
footer="_Opened by the **SME agent** (option-bot-specialist skill, read-only) from the $DATE daily review. The **PM agent** reads it first: it places it in an epic of the approved PRD and moves it to **Ready** (the Dev agent builds it) only when that epic covers it; otherwise a person decides. See [AGENTIC.md](../blob/main/AGENTIC.md)._"
opened="" commented=""
if [[ "$PUBLISH" == "1" ]]; then
  n="$(py 'import json,sys; print(len(json.load(open(sys.argv[1]))["issues"]))' "$OUT/findings.json")"
  i=0
  while [[ $i -lt $n ]]; do
    py 'import json,sys; f=json.load(open(sys.argv[1]))["issues"][int(sys.argv[2])]
open(sys.argv[3],"w").write(f["body"])
print(f["fingerprint"]); print(f["severity"]); print(f["title"])' "$OUT/findings.json" "$i" "$OUT/issue-body.md" > "$OUT/issue-meta.txt"
    fp="$(sed -n 1p "$OUT/issue-meta.txt")"; sev="$(sed -n 2p "$OUT/issue-meta.txt")"; title="$(sed -n 3p "$OUT/issue-meta.txt")"
    where="$(python3 "$ISSUES" route "$OUT/findings.json" "$i" "$OUT/open_issues.json" "$OUT/closed_issues.json")"
    num="${where#* }"
    case "$where" in
      comment\ *)
        py 'import json,sys; open(sys.argv[3],"w").write(json.load(open(sys.argv[1]))["issues"][int(sys.argv[2])]["update"])' "$OUT/findings.json" "$i" "$OUT/issue-update.md"
        { echo "**Seen again in the $DATE review — what is new:**"; echo; cat "$OUT/issue-update.md"; } > "$OUT/issue-comment.md"
        gh issue comment "$num" -R "$slug" --body-file "$OUT/issue-comment.md" >/dev/null && log "commented #$num ($fp)" &&
          commented="$commented #$num" ;;
      quiet\ *)
        log "seen again, nothing new: #$num ($fp), no comment" ;;
      guard\ *)
        # The agent called it new, but an issue with a near-identical title exists.
        { echo "**Seen again in the $DATE review** (filed as new by the SME agent; matched to this issue by title, so no duplicate was opened)."
          echo; echo "<details><summary>The finding as written</summary>"; echo; cat "$OUT/issue-body.md"; echo; echo "</details>"; } > "$OUT/issue-comment.md"
        gh issue comment "$num" -R "$slug" --body-file "$OUT/issue-comment.md" >/dev/null && log "duplicate guard: commented #$num instead of opening \"$title\"" &&
          commented="$commented #$num" ;;
      *)
        { cat "$OUT/issue-body.md"; echo; echo "<!-- sme-fingerprint: $fp -->"; echo; echo "$footer"; } > "$OUT/issue-full.md"
        url="$(gh issue create -R "$slug" --title "$title" --label agent:sme --label "severity:$sev" --body-file "$OUT/issue-full.md")"
        log "opened $url ($fp)"
        opened="$opened #${url##*/}"
        add_to_board "$url" "Triage" >/dev/null || log "could not add $url to the board" ;;
    esac
    i=$((i + 1))
  done
  for fp in $(py 'import json,sys; print(" ".join(json.load(open(sys.argv[1])).get("resolved", [])))' "$OUT/findings.json"); do
    num="$(py 'import json,sys; print(next((str(o["number"]) for o in json.load(open(sys.argv[1])) if o["fingerprint"]==sys.argv[2]), ""))' "$OUT/open_issues.json" "$fp")"
    [[ -n "$num" ]] && gh issue comment "$num" -R "$slug" --body "Not seen in the $DATE review. If a fix shipped, this can be closed; otherwise it may have stopped by itself." >/dev/null && log "resolved note #$num"
  done
  py 'import json,sys
for v in json.load(open(sys.argv[1])).get("verifications", []):
    print(v["issue"], v["verdict"]); open(sys.argv[2] + "-" + str(v["issue"]) + ".md", "w").write(v["comment"])' "$OUT/findings.json" "$OUT/verify" |
  while read -r num verdict; do
    { echo "**SME verification ($DATE): $verdict**"; echo; cat "$OUT/verify-$num.md"; } > "$OUT/verify-comment.md"
    gh issue comment "$num" -R "$slug" --body-file "$OUT/verify-comment.md" >/dev/null && log "verified #$num: $verdict"
    if [[ "$verdict" == "not_fixed" ]]; then
      gh issue reopen "$num" -R "$slug" >/dev/null && log "reopened #$num"
      # scripts/promote.sh refuses a release while any issue carries it.
      gh issue edit "$num" -R "$slug" --add-label sme:not-fixed >/dev/null || true
      item="$(item_for_issue "$num")"; [[ -n "$item" ]] && set_status "$item" "Triage"
    elif [[ "$verdict" == "fixed" ]]; then
      gh issue edit "$num" -R "$slug" --remove-label sme:not-fixed >/dev/null 2>&1 || true
    fi
  done
fi

notify "Options bot SME review: ${severity:-?}" "$summary"
log "report: $AGENT_REPO_DIR/$OUT/report.md"
run_end reported "severity ${severity:-?}; opened${opened:- none}; commented${commented:- none}"
