#!/usr/bin/env bash
# dev_agent.sh — the Dev agent: implements one Ready issue as a pull request.
#
#   1. takes the Ready card the PM agent ranked first (Priority, then the
#      oldest issue; never an epic, never a card another Dev agent instance
#      holds — tick.sh starts up to DEV_PARALLEL of them) → In progress;
#   2. creates an isolated worktree on branch agent/issue-<n> (reused when a
#      PR is already open, so review comments are addressed on the same PR);
#   3. runs Claude Code headless with the developer skill in agent mode. It
#      may edit files, run the build/test/lint tools and commit — nothing
#      else: no push, no gh, no network, no docker, no merge;
#   4. the script re-runs every check itself (trust, but verify), pushes the
#      branch and opens (or updates) the PR "Closes #n" → In review.
#      A failure, or an agent that stops with questions, comments on the
#      issue, labels it agent:needs-human and moves it to Blocked — except a
#      usage limit or a run that failed at once with no answer (limit_lib.sh
#      note_stop): the card goes back to Ready after the pause.
#
# A person reviews and merges. The agent never merges or deploys.
# Env: DEV_MODEL, DEV_PARALLEL (3), AGENT_WORK_DIR (<envs>/agent-worktrees), AGENT_NOTIFY.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
require_tools claude gh git go gofmt python3 || exit 1

slug="$(repo_slug)"
# One instance picks at a time, so two never take the same card.
mutex dev-pick || exit 0
max="$(parallel_max "${DEV_PARALLEL:-1}")"
if [[ "$(claims_alive dev)" -ge "$max" ]]; then unmutex dev-pick; log "dev: $max instance(s) already running"; exit 0; fi
next="$(ready_next dev)"
[[ -n "$next" ]] || { unmutex dev-pick; log "dev: nothing in Ready"; exit 0; }
item="${next%% *}"; issue="${next##* }"
claim dev "$issue" || { unmutex dev-pick; exit 0; }
set_status "$item" "In progress"
unmutex dev-pick
branch="agent/issue-$issue"
run_dir="$AGENT_STATE_DIR/dev/issue-$issue-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$run_dir"
exec > >(tee -a "$run_dir/run.log") 2>&1
run_begin dev "$issue" "" "$run_dir" # the run ledger (lib.sh): every exit below ends it
log "dev: issue #$issue → $branch"

# The pickup comment says which attempt this is and what came before, so the
# issue alone tells the story even when a person moved the card back to Ready
# on the board (a board move leaves no trace on the issue).
gh issue view "$issue" -R "$slug" --json comments > "$run_dir/history.json"
python3 "$AGENT_REPO_DIR/scripts/agents/pickup_comment.py" "$run_dir/history.json" "$branch" \
  "data/agents/dev/$(basename "$run_dir")" > "$run_dir/pickup.md"
gh issue comment "$issue" -R "$slug" --body-file "$run_dir/pickup.md" >/dev/null

block() { # reason file, ledger detail (a short fixed phrase)
  run_end blocked "$2"
  { echo "🤖 **Dev agent stopped — a person is needed.**"; echo; cat "$1"; echo; echo "_Move the card back to **Ready** after answering, and the agent will try again on the same branch._"; } > "$run_dir/blocked.md"
  gh issue comment "$issue" -R "$slug" --body-file "$run_dir/blocked.md" >/dev/null
  gh issue edit "$issue" -R "$slug" --add-label agent:needs-human >/dev/null
  set_status "$item" "Blocked"
  notify "Dev agent blocked on #$issue" "$(head -c 120 "$1")"
  log "dev: blocked #$issue"
  exit 0
}

# ── Worktree ────────────────────────────────────────────────────────────────
git_locked fetch -q origin
wt="$AGENT_WORK_DIR/issue-$issue"
[[ -d "$wt" ]] && git_locked worktree remove --force "$wt" 2>/dev/null || true
if git ls-remote --exit-code --heads origin "$branch" >/dev/null 2>&1; then
  git_locked worktree add -q -B "$branch" "$wt" "origin/$branch"
  existing_pr="$(gh pr list -R "$slug" --head "$branch" --state open --json number -q '.[0].number' || true)"
else
  git_locked worktree add -q -B "$branch" "$wt" origin/main
  existing_pr=""
fi
base="$(git -C "$wt" rev-parse HEAD)"
RUN_PR="$existing_pr"
# An open PR starts from today's main: merge it in first. A conflict (another PR
# merged first, pr_gate.sh) is left in the files for the agent to resolve.
conflicts=""
if [[ -n "$existing_pr" ]]; then
  git -C "$wt" -c user.name="$(git config user.name)" -c user.email="$(git config user.email)" merge --no-edit -q origin/main >/dev/null 2>&1 ||
    conflicts="$(git -C "$wt" diff --name-only --diff-filter=U)"
fi

# ── Task for the agent ──────────────────────────────────────────────────────
gh issue view "$issue" -R "$slug" --json number,title,body,labels,comments \
  -q '"# Issue #\(.number): \(.title)\n\nLabels: \([.labels[].name] | join(", "))\n\n\(.body)\n\n## Comments\n\(.comments | map("### \(.author.login) (\(.createdAt))\n\(.body)") | join("\n\n"))"' > "$run_dir/task.md"
if [[ -n "$existing_pr" ]]; then
  gh pr view "$existing_pr" -R "$slug" --json reviews,comments \
    -q '"\n\n# Review feedback on PR #'"$existing_pr"' (address it)\n\(.reviews | map("### review by \(.author.login): \(.state)\n\(.body)") | join("\n\n"))\n\(.comments | map("### \(.author.login)\n\(.body)") | join("\n\n"))"' >> "$run_dir/task.md" || true
  gh api "repos/$slug/pulls/$existing_pr/comments" -q '.[] | "### on \(.path):\(.line // .original_line) by \(.user.login)\n\(.body)\n"' >> "$run_dir/task.md" 2>/dev/null || true
  # From main's skill: the branch may predate this instruction.
  { echo; echo "# How to answer the review (developer skill, from main)"; echo
    grep -m1 '^\*\*When `.agent-task.md` carries review feedback\*\*' "$AGENT_REPO_DIR/.claude/skills/developer/SKILL.md"; } >> "$run_dir/task.md"
fi
if [[ -n "$conflicts" ]]; then
  { echo; echo "# FIRST: resolve the merge with main"; echo
    echo "\`origin/main\` was merged into this branch and conflicts in:"; echo "$conflicts" | sed 's/^/- /'; echo
    echo "Another PR merged first. Resolve each conflict keeping both changes' intent (main's behaviour stays; this issue's change applies on top), remove every conflict marker, run the checks, then \`git add\` the files and \`git commit --no-edit\`. Say in the PR body how each conflict was resolved."; } >> "$run_dir/task.md"
fi
cp "$run_dir/task.md" "$wt/.agent-task.md"
echo ".agent-task.md" >> "$(git -C "$wt" rev-parse --git-path info/exclude)" # never committed"

PROMPT="Use the developer skill in its \"Agent mode\" and implement the GitHub issue described in .agent-task.md
(read it first; it includes any review feedback to address). You are on branch $branch in an isolated worktree.
Implement it with tests, run the checks the skill lists, and commit your work (git add + git commit).
Do not push, merge, deploy or call GitHub. End your answer with the exact RESULT block the skill defines."

allowed=( "Read" "Edit" "Write" "Grep" "Glob" "Skill"
  "Bash(go build:*)" "Bash(go test:*)" "Bash(go vet:*)" "Bash(gofmt:*)" "Bash(go run ./cmd/stress:*)" "Bash(go mod tidy)"
  "Bash(npm ci:*)" "Bash(npx jest:*)" "Bash(npx eslint:*)" "Bash(npx next build:*)" "Bash(mmdc:*)"
  "Bash(git status:*)" "Bash(git diff:*)" "Bash(git log:*)" "Bash(git show:*)" "Bash(git add:*)" "Bash(git commit:*)"
  "Bash(ls:*)" "Bash(cat:*)" "Bash(head:*)" "Bash(tail:*)" "Bash(wc:*)" "Bash(grep:*)" "Bash(find:*)" "Bash(sed -n:*)" )
denied=( "Bash(git push:*)" "Bash(git remote:*)" "Bash(git reset:*)" "Bash(git checkout:*)" "Bash(gh:*)" "Bash(curl:*)"
  "Bash(docker:*)" "Bash(rm:*)" "WebFetch" "WebSearch" "Agent" )
model_args=()
[[ -n "${DEV_MODEL:-}" ]] && model_args=(--model "$DEV_MODEL")
budget_take dev # the daily budget (limit_lib.sh)

log "dev: running the agent in $wt"
started="$(date +%s)"
set +e
(cd "$wt" && claude -p "$PROMPT" ${model_args[@]+"${model_args[@]}"} \
  --permission-mode dontAsk --allowedTools "${allowed[@]}" --disallowedTools "${denied[@]}" \
  --strict-mcp-config --no-session-persistence --output-format text < /dev/null) > "$run_dir/agent.md" 2>&1
rc=$?
set -e
elapsed=$(( $(date +%s) - started ))
rm -f "$wt/.agent-task.md"
log "dev: agent exit $rc after ${elapsed}s"

result="$(grep -m1 '^RESULT:' "$run_dir/agent.md" | sed 's/^RESULT: *//' || true)"
py 'import re,sys
t = open(sys.argv[1]).read()
m = re.search(r"<!-- PR-BODY\s*(.*?)\s*-->", t, re.S)
open(sys.argv[2], "w").write(m.group(1) if m else t[-6000:])' "$run_dir/agent.md" "$run_dir/pr-body.md"

if [[ $rc -ne 0 || "$result" != "done" ]]; then
  if [[ "$result" == "needs_human" ]]; then
    cp "$run_dir/pr-body.md" "$run_dir/reason.md" # the agent's questions
    detail="the agent asked questions"
  else
    why="the agent did not finish (exit $rc, result '${result:-none}')"
    if note_stop "$rc" "$elapsed" "$run_dir/agent.md"; then
      # Not the card's fault (a limit, or a run that failed at once with no
      # answer): back to Ready, retried after the pause, never agent:needs-human.
      # One comment per episode: none when the last one (pickups aside) says it.
      if ! python3 "$AGENT_REPO_DIR/scripts/agents/requeue.py" limit-noted "$run_dir/history.json"; then
        gh issue comment "$issue" -R "$slug" --body "🤖 Dev agent hit a usage/rate limit (exit $rc after ${elapsed}s). The card goes back to **Ready**; the agents pause 30 min and retry." >/dev/null
      fi
      gh issue edit "$issue" -R "$slug" --remove-label agent:needs-human >/dev/null 2>&1 || true
      run_end skipped_limit "exit $rc after ${elapsed}s: back to Ready"
      set_status "$item" "Ready"; git_locked worktree remove --force "$wt" 2>/dev/null || true; exit 0
    fi
    { echo "Stopped because $why."; echo; echo "Last lines:"; echo
      tail -30 "$run_dir/agent.md" | sed 's/^/    /'; } > "$run_dir/reason.md"
    detail="the agent did not finish (exit $rc)"
  fi
  block "$run_dir/reason.md" "$detail"
fi
if [[ "$(git -C "$wt" rev-list --count "$base"..HEAD)" == "0" ]]; then
  echo "The agent reported done but made no commit." > "$run_dir/reason.md"
  block "$run_dir/reason.md" "done without a commit"
fi
RUN_COMMIT="$(git -C "$wt" rev-parse --short HEAD)"

# ── Verify independently ────────────────────────────────────────────────────
log "dev: verifying"
checks="$run_dir/checks.md"
: > "$checks"
check() { # name, command...
  local name="$1"; shift
  if (cd "$wt" && "$@") > "$run_dir/check.log" 2>&1; then echo "- [x] $name" >> "$checks"
  else echo "- [ ] **$name — FAILED**" >> "$checks"; { echo '```'; tail -40 "$run_dir/check.log"; echo '```'; } >> "$checks"; return 1; fi
}
ok=1
check "gofmt" bash -c 'out="$(gofmt -l cmd internal tests)" && test -z "$out"' || ok=0
check "go vet ./..." go vet ./... || ok=0
check "go test -race ./tests/..." go test -race -count=1 ./tests/... || ok=0
if git -C "$wt" diff --name-only "origin/main"...HEAD | grep -q '^frontend/'; then
  check "frontend: npm ci" bash -c 'cd frontend && npm ci --no-audit --no-fund' || ok=0
  check "frontend: eslint" bash -c 'cd frontend && npx eslint .' || ok=0
  check "frontend: jest" bash -c 'cd frontend && npx jest' || ok=0
  check "frontend: next build" bash -c 'cd frontend && npx next build' || ok=0
fi

# ── Pull request ────────────────────────────────────────────────────────────
git_locked -C "$wt" push -q -u origin "$branch"
{
  echo "Closes #$issue"; echo
  cat "$run_dir/pr-body.md"; echo
  echo "## Checks re-run by the Dev agent script"; cat "$checks"; echo
  echo "_Opened by the **Dev agent** (developer skill, agent mode) for #$issue. It cannot merge or deploy: a person reviews and merges; merging deploys to **testnet** and the SME agent verifies the fix in its next review. See [AGENTIC.md](https://github.com/$slug/blob/main/AGENTIC.md)._"
  echo; echo "🤖 Generated with [Claude Code](https://claude.com/claude-code)"
} > "$run_dir/pr.md"
title="$(gh issue view "$issue" -R "$slug" --json title -q .title)"
draft=(); [[ $ok -eq 1 ]] || draft=(--draft)
if [[ -n "$existing_pr" ]]; then
  gh pr edit "$existing_pr" -R "$slug" --body-file "$run_dir/pr.md" >/dev/null
  # A PR opened as a draft on failing checks becomes reviewable once they pass.
  [[ $ok -eq 1 ]] && gh pr ready "$existing_pr" -R "$slug" >/dev/null 2>&1 || true
  pr_url="$(gh pr view "$existing_pr" -R "$slug" --json url -q .url)"
  gh pr comment "$existing_pr" -R "$slug" --body "🤖 Dev agent pushed changes for the review feedback. Checks: $([[ $ok -eq 1 ]] && echo passed || echo '**failing**')." >/dev/null
else
  pr_url="$(gh pr create -R "$slug" --base main --head "$branch" --title "$title (#$issue)" --label agent:dev ${draft[@]+"${draft[@]}"} --body-file "$run_dir/pr.md")"
fi
log "dev: PR $pr_url"
RUN_PR="${pr_url##*/}"

if [[ $ok -eq 1 ]]; then
  if [[ -n "$existing_pr" ]]; then run_end pr_updated "checks passed"; else run_end pr_opened "checks passed"; fi
  set_status "$item" "In review"
  gh issue edit "$issue" -R "$slug" --remove-label agent:needs-human --remove-label needs-rebase >/dev/null 2>&1 || true
  gh issue comment "$issue" -R "$slug" --body "🤖 Dev agent opened $pr_url — all checks passed. Ready for review." >/dev/null
  notify "Dev agent: PR ready for #$issue" "$title"
else
  { echo "The implementation is in $pr_url (draft), but the checks failed:"; echo; cat "$checks"; } > "$run_dir/reason.md"
  block "$run_dir/reason.md" "checks failed: draft PR"
fi
git_locked worktree remove --force "$wt" 2>/dev/null || true
