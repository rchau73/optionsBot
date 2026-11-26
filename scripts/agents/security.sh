#!/usr/bin/env bash
# security.sh — the admins' weekly security digest and same-day alerts (#717).
# tick.sh starts it every tick. For each environment (stable, mvp) it reads the
# monitor's audit trail — from the running monitor's volume (envctl.sh … cp,
# only the audit files kept), else from the clone's data/monitor, as
# backup_data.sh does — and runs cmd/security (pure rules: internal/security):
#   - the alert check, at most every SECURITY_ALERT_MIN (30) minutes:
#     `-alerts -sent <state>/<env>.alerts`; each new ALERT line is one notify
#     and one log line (the IDs already raised are in that file, so one
#     pattern alerts once);
#   - the weekly digest, on the first tick after an ISO week ends (Monday,
#     UTC) until <state>/<week>.done: `-week <week> -out <state>/<week>/<env>`
#     → digest.md|json; its HEADLINE is one notify per environment; digest.md
#     is the "Security" section of that environment's weekly usage report
#     (<clone>/data/monitor/reports/<week>.md: appended here when the report
#     exists, and by usage.sh when it writes the report later; lib.sh
#     security_section). A failed environment is tried once more on a later
#     tick, then the week is marked done.
# Only counts reach the notifications and the files: never a name, address,
# password, token or reset link. Read-only on the trail; no Claude, no GitHub.
# Ledger agent `security`: one run per digest, and one per check that raised an alert.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
state="$AGENT_STATE_DIR/security"
mkdir -p "$state"
week="$(python3 "$AGENT_REPO_DIR/scripts/agents/usage_week.py" week)"
checked="$state/alerts.checked"
due_alerts=1
[[ -n "$(find "$checked" -mmin "-${SECURITY_ALERT_MIN:-30}" 2>/dev/null)" ]] && due_alerts=0
due_digest=1
[[ -f "$state/$week.done" ]] && due_digest=0
[[ $due_alerts -eq 1 || $due_digest -eq 1 ]] || exit 0
bin="${SECURITY_BIN:-}" # tests pass a prebuilt cmd/security
[[ -n "$bin" ]] || require_tools go || exit 0
take_lock security || exit 0
if [[ -z "$bin" ]]; then
  bin="$state/security-bin"
  go build -o "$bin" ./cmd/security > "$state/build.log" 2>&1 || { log "security: go build ./cmd/security failed (log $state/build.log)"; exit 0; }
fi

AUDIT_ARCHIVE='audit.[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]T[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z.jsonl'
envctl="$AGENT_REPO_DIR/scripts/envctl.sh"

# audit_trail <env> <dir> — copies the env's audit trail (audit.jsonl and its
# archives, plain files only) into <dir>, from the running monitor's volume,
# else from the clone's data/monitor. The volume's other files (users,
# events) are copied out with it and removed at once.
audit_trail() {
  local edir src f
  edir="$(env_dir "$1")"
  rm -rf "$2"; mkdir -p "$2/volume"
  on_exit_remove "$2"
  src="$edir/data/monitor"
  if [[ -f "$edir/.env" && -f "$edir/deploy.env" ]] &&
    [[ -n "$(bash "$envctl" "$1" ps -q --status running monitor 2>/dev/null)" ]] &&
    bash "$envctl" "$1" cp monitor:/app/data/monitor/. "$2/volume" > /dev/null 2>&1; then
    src="$2/volume"
  fi
  for f in "$src"/audit.jsonl "$src"/$AUDIT_ARCHIVE; do
    if [[ -f "$f" && ! -L "$f" ]]; then cp -p "$f" "$2/"; fi
  done
  rm -rf "$2/volume"
}

envs=""
for env in $ENV_NAMES; do
  [[ -d "$(env_dir "$env")" ]] && envs="$envs $env"
done

if [[ $due_alerts -eq 1 ]]; then
  date -u +%Y-%m-%dT%H:%M:%SZ > "$checked"
  raised="$state/alerts.raised"
  : > "$raised"
  for env in $envs; do
    trail="$state/.trail-$env"
    audit_trail "$env" "$trail"
    if ! "$bin" -alerts -audit-dir "$trail" -sent "$state/$env.alerts" > "$state/$env.alerts.log" 2>&1; then
      log "security: $env: alert check failed (log $state/$env.alerts.log)"; continue
    fi
    while IFS= read -r line; do
      [[ "$line" == "ALERT "* ]] && printf '%s\t%s\n' "$env" "${line#ALERT }" >> "$raised"
    done < "$state/$env.alerts.log"
  done
  if [[ -s "$raised" ]]; then
    run_begin security "" "" "$raised"
    n=0
    while IFS=$'\t' read -r env text; do
      notify "optionsBot security alert ($env)" "$text"
      log "security: $env: $text"
      n=$((n + 1))
    done < "$raised"
    run_end notified "$n alert(s)"
  fi
fi

[[ $due_digest -eq 1 ]] || exit 0
attempts="$(cat "$state/$week.attempts" 2>/dev/null || echo 0)"
attempt=$((attempts + 1))
echo "$attempt" > "$state/$week.attempts"
run_begin security "" "" "$state/$week"
failed=""
for env in $envs; do
  out="$state/$week/$env"
  mkdir -p "$out"
  trail="$state/.trail-$env"
  audit_trail "$env" "$trail"
  if ! "$bin" -audit-dir "$trail" -week "$week" -out "$out" > "$out.log" 2>&1; then
    log "security: $env: the $week digest failed (log $out.log)"; failed="$failed $env"; continue
  fi
  headline="$({ grep '^HEADLINE ' "$out.log" || true; } | tail -1)"
  headline="${headline#HEADLINE }"
  notify "optionsBot security digest ($env)" "$headline"
  log "security: $env: $headline (digest $out/digest.md)"
  security_section "$(env_dir "$env")/data/monitor/reports/$week.md" "$out/digest.md"
done
if [[ -z "$failed" || $attempt -ge 2 ]]; then
  date -u +%Y-%m-%dT%H:%M:%SZ > "$state/$week.done"
  run_end "$([[ -z "$failed" ]] && echo notified || echo failed)" "$week${failed:+; failed:$failed}"
else
  log "security: the $week digest is tried once more on a later tick (failed:$failed)"
  run_end failed "retried once"
fi
