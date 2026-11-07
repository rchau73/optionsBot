#!/usr/bin/env bash
# backup_data.sh — the nightly backup of the bots' history, with a restore
# check (#328). tick.sh starts it every tick; it does nothing once today's
# (UTC) done-marker exists, so the first tick after 00:00 UTC runs it:
#   for each environment (stable, mvp) and bot (BACKUP_BOTS, "btc eth") whose
#   folder <clone>/data/<bot> exists, copy orders.log, data/pnl_history.jsonl,
#   data/regime_history.jsonl, data/direction_history.jsonl, data/shadow_legs.jsonl and data/archive/
#   to <envs>/history/backups/<date>/<env>/<bot>/ (orders.log, the four
#   .jsonl files, archive/) — named files only: no .env, no secret; and each
#   environment's monitor files to <date>/<env>/monitor/ (#650): users
#   (auth.db), analytics events, the assistant's review log (#438), request
#   queue (#409) and audit trail with its archives (#619), read from the
#   running monitor's monitor-auth volume (envctl.sh … cp, read-only), else —
#   the container not running, logged — from the clone's data/monitor;
#   then go run ./cmd/backupcheck on each copy: the journal must replay
#   (orders.ReplayFile) and hold no fewer events than the previous good copy;
#   every monitor .jsonl must parse and the audit chain must hold.
# The copy is built in <backups>/.<date>.partial and becomes <date> only when
# every check passes; then the newest BACKUP_KEEP_DAYS (14) are kept. A failed
# check keeps the copy as <date>.failed, notifies the person, ends the run
# failed, prunes nothing (the previous good backups stay) and is retried once on
# a later tick; after the second attempt the day is marked done whatever
# happened: data/agents/backup/<date>.done. A bot folder that the previous good
# backup had and is now gone fails the check too.
# Reads the live files only (cp, docker compose cp out of the container): never
# writes to them or to the volume, never deletes anything in a clone's data/.
# No Claude call, no git, no Deribit call.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
BOTS="${BACKUP_BOTS:-btc eth}"
KEEP="${BACKUP_KEEP_DAYS:-14}"
day="$(date -u +%Y-%m-%d)"
state="$AGENT_STATE_DIR/backup"
done_marker="$state/$day.done"
[[ -f "$done_marker" ]] && exit 0
check="${BACKUP_CHECK_BIN:-}" # tests pass a prebuilt cmd/backupcheck
[[ -n "$check" ]] || require_tools go || exit 0
take_lock backup || exit 0
mkdir -p "$state"
backups="$ENV_HISTORY/backups"
DATED='[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'

# The newest good backup before today: what each copy is compared with.
prev=""
for d in "$backups"/$DATED; do
  if [[ -d "$d" && "${d##*/}" < "$day" ]]; then prev="$d"; fi
done

# What there is to copy: "env bot" lines. Nothing at all → no backup, no run.
sources=""
for env in $ENV_NAMES; do
  for bot in $BOTS; do
    if [[ -d "$(env_dir "$env")/data/$bot" ]] || [[ -n "$prev" && -d "$prev/$env/$bot" ]]; then
      sources="$sources$env $bot"$'\n'
    fi
  done
done
if [[ -z "$sources" ]]; then
  log "backup: no bot folder in any environment, nothing to back up"
  date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"; exit 0
fi

attempts="$(cat "$state/$day.attempts" 2>/dev/null || echo 0)"
if [[ $attempts -ge 2 ]]; then # the second attempt died before its end
  log "backup: $day already tried twice, giving up"; date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"; exit 0
fi
attempt=$((attempts + 1))
echo "$attempt" > "$state/$day.attempts"
run_begin backup "" "" "$state/$day.log"
log "backup: $day, attempt $attempt${prev:+, compared with ${prev##*/}}"

work="$backups/.$day.partial"
rm -rf "$work"
mkdir -p "$work"
: > "$state/$day.log"
failed=""
fail() { # env, bot, reason
  failed="$failed $1/$2"
  printf '%s %s: %s\n' "$1" "$2" "$3" >> "$state/$day.log"
  log "backup: $1 $2 failed: $3"
}

if [[ -z "$check" ]]; then
  check="$state/backupcheck"
  go build -o "$check" ./cmd/backupcheck >> "$state/$day.log" 2>&1 || fail - - "go build ./cmd/backupcheck"
fi

copied=0
if [[ -n "$failed" ]]; then sources=""; fi # no checker: nothing can be checked
while read -r env bot; do
  [[ -n "$env" ]] || continue
  src="$(env_dir "$env")/data/$bot"
  dst="$work/$env/$bot"
  if [[ ! -d "$src" ]]; then
    fail "$env" "$bot" "folder $src is gone (the previous backup had it)"; continue
  fi
  mkdir -p "$dst"
  [[ ! -f "$src/orders.log" ]] || cp -p "$src/orders.log" "$dst/"
  for f in pnl_history.jsonl regime_history.jsonl direction_history.jsonl shadow_legs.jsonl; do
    [[ ! -f "$src/data/$f" ]] || cp -p "$src/data/$f" "$dst/"
  done
  [[ ! -d "$src/data/archive" ]] || cp -Rp "$src/data/archive" "$dst/archive"
  # The archive holds journals and histories only; never carry an env file.
  find "$dst" -name '.env*' -exec rm -rf {} + 2>/dev/null || true
  prev_args=()
  [[ -z "$prev" ]] || prev_args=(-previous "$prev/$env/$bot")
  if out="$("$check" -dir "$dst" ${prev_args[@]+"${prev_args[@]}"} 2>&1)"; then
    log "backup: $env $bot ok — ${out#RESULT }"; copied=$((copied + 1))
  else
    fail "$env" "$bot" "restore check: $(printf '%s' "$out" | grep -v '^[[:space:]]*$' | tail -1)"
  fi
done <<< "$sources"

# The monitor's own files (#650), read from where the deployed monitor keeps
# them — its monitor-auth volume, /app/data/monitor — through the running
# container (envctl.sh … cp, read-only), else from the clone's data/monitor.
# Only these names are copied (never a key, token or env file):
#   auth.db                users, sign-in lockouts and survey answers (password and token hashes only)
#   events.jsonl           analytics events (#193)
#   assistant.jsonl        the assistant's review log (#438, L11/L12)
#   requests.jsonl, requests_status.jsonl   the assistant's request queue (#409)
#   audit.jsonl, audit.<UTC time>.jsonl     the audit trail and its rotated archives (#619)
# Every .jsonl copied must parse (backupcheck -lines; a torn last line is
# allowed) and the audit trail's hash chain must hold on the copy (-audit).
MONITOR_FILES="auth.db events.jsonl assistant.jsonl requests.jsonl requests_status.jsonl audit.jsonl"
AUDIT_ARCHIVE='audit.[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]T[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z.jsonl'
envctl="$AGENT_REPO_DIR/scripts/envctl.sh"

# monitor_volume <env> <dir> — copies the running monitor's /app/data/monitor
# into <dir>; fails (nothing copied) when the stack is not set up or the
# monitor container is not running.
monitor_volume() {
  local edir; edir="$(env_dir "$1")"
  [[ -f "$edir/.env" && -f "$edir/deploy.env" ]] || return 1
  [[ -n "$(bash "$envctl" "$1" ps -q --status running monitor 2>/dev/null)" ]] || return 1
  bash "$envctl" "$1" cp monitor:/app/data/monitor/. "$2" >> "$state/$day.log" 2>&1
}

for env in $ENV_NAMES; do
  vol="$state/.volume-$env"
  rm -rf "$vol"; mkdir -p "$vol"
  if monitor_volume "$env" "$vol"; then
    msrc="$vol"; log "backup: $env monitor files from the monitor-auth volume"
  else
    msrc="$(env_dir "$env")/data/monitor"
    log "backup: $env monitor not running, monitor files from the clone's $msrc"
  fi
  mdst="$work/$env/monitor"
  for f in $MONITOR_FILES "$msrc"/$AUDIT_ARCHIVE; do
    f="${f##*/}"
    # A plain file only: a link could point anywhere.
    if [[ -f "$msrc/$f" && ! -L "$msrc/$f" ]]; then
      mkdir -p "$mdst"
      cp -p "$msrc/$f" "$mdst/"
    fi
  done
  rm -rf "$vol"
  # The review's closures (#704) are a person's record, kept only in the clone
  # beside the review files: the weekly job cannot rebuild them.
  closures="$(env_dir "$env")/data/monitor/assistant_reviews/closures.jsonl"
  if [[ -f "$closures" && ! -L "$closures" ]]; then
    mkdir -p "$mdst"
    cp -p "$closures" "$mdst/"
  fi
  [[ -d "$mdst" && -z "$failed" ]] || continue
  for f in "$mdst"/*.jsonl; do
    [[ -f "$f" ]] || continue
    if out="$("$check" -lines "$f" 2>&1)"; then
      log "backup: $env ${f##*/} ok — ${out#RESULT }"
    else
      fail "$env" monitor "${f##*/} check: $(printf '%s' "$out" | grep -v '^[[:space:]]*$' | tail -1)"
    fi
  done
  if [[ -f "$mdst/audit.jsonl" ]] || compgen -G "$mdst/$AUDIT_ARCHIVE" > /dev/null; then
    if out="$("$check" -audit "$mdst" 2>&1)"; then
      log "backup: $env audit chain intact — ${out#RESULT }"
    else
      fail "$env" monitor "audit chain check: $(printf '%s' "$out" | grep -v '^[[:space:]]*$' | tail -1)"
    fi
  fi
done

if [[ -n "$failed" ]]; then
  rm -rf "$backups/$day.failed"
  mv "$work" "$backups/$day.failed"
  log "backup: restore check failed for$failed — copy kept in $backups/$day.failed, earlier backups untouched"
  notify "optionsBot backup failed" "Restore check failed for$failed ($day). See $state/$day.log"
  [[ $attempt -lt 2 ]] || date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"
  run_end failed "restore check"
  exit 1
fi

rm -rf "${backups:?}/$day"
mv "$work" "$backups/$day"
rm -rf "$backups/$day.failed"

# prune_oldest <dir>... (sorted, oldest first) — keep the newest KEEP.
prune_oldest() {
  local n=0 d
  for d in "$@"; do if [[ -d "$d" ]]; then n=$((n + 1)); fi; done
  for d in "$@"; do
    [[ -d "$d" ]] || continue
    [[ $n -gt $KEEP ]] || break
    rm -rf "$d"; log "backup: pruned ${d##*/}"; n=$((n - 1))
  done
}
prune_oldest "$backups"/$DATED
prune_oldest "$backups"/$DATED.failed

date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"
log "backup: $day done, $copied bot(s) in $backups/$day"
run_end backed_up "$copied bot(s)"
