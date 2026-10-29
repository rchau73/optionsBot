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
#   4. the script merges origin/main last (#467), runs the checks the change
#      touches itself (trust, but verify; checkfail.py picks them, #472) —
#      gofmt/vet, the scripts, the Go tests and jest files the change touches,
#      eslint, and the repo-wide checks (glossary, responsive audit, empty
#      states, menu, help index: checkfail.py repo-wide) — pushes the branch and
#      opens (or updates) the PR "Closes #n" as a draft → In review. CI runs
#      every suite; the PR gate marks the draft ready when CI is green, and the
#      Reviewer starts then. A failing test the change does not touch is re-run
#      up to 3 times; passing makes it flaky (named on the PR and its issue),
#      not the PR's fault. A real failure sends the card back to Ready once
#      (needs-fix, the failing checks' log tails in a comment the next run
#      reads first); the second one goes to a person (Blocked, as below).
#      A PR that merged while the agent worked gets its new commits moved onto
#      agent/issue-<n>-r<k> from main and a new PR "Refs #n"; one closed
#      unmerged stops and asks (#478). "Pushed changes" is commented only once
#      the PR's head on GitHub is the pushed commit.
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
# An open PR on agent/issue-<n>-r<k> (review fixes moved after the first PR
# merged, #478) is the one to update.
open_pr="" open_branch=""
read -r open_pr open_branch _ < <(open_dev_pr "$issue" || true) || true
[[ -n "$open_branch" ]] && branch="$open_branch"
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
  { echo "🤖 **Dev agent stopped — a person is needed.**"; echo; cat "$1"; echo; echo "_Move the card back to **Ready** after answering, and the agent will try again on the same branch._"
    echo "<!-- dev:blocked -->"; } > "$run_dir/blocked.md" # resets the check-failure count (checkfail.py)
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
  existing_pr="$open_pr"
else
  git_locked worktree add -q -B "$branch" "$wt" origin/main
  existing_pr=""
fi
base="$(git -C "$wt" rev-parse HEAD)"
RUN_PR="$existing_pr"
# An open PR starts from today's main: merge it in first. A conflict (another PR
# merged first, pr_gate.sh) in a generated file is resolved by its generator
# (scripts/regen.sh, #465); any other is left in the files for the agent.
conflicts=""
if [[ -n "$existing_pr" ]]; then
  as_me=(-c user.name="$(git config user.name)" -c user.email="$(git config user.email)")
  if ! git -C "$wt" "${as_me[@]}" merge --no-edit -q origin/main >/dev/null 2>&1; then
    if (cd "$wt" && bash scripts/regen.sh --resolve) >> "$run_dir/regen.log" 2>&1; then
      git -C "$wt" "${as_me[@]}" commit -q --no-edit
      log "dev: #$issue merge with main: generated files regenerated"
    fi
    conflicts="$(git -C "$wt" diff --name-only --diff-filter=U)"
  fi
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
    echo "Another PR merged first. Resolve each conflict keeping both changes' intent (main's behaviour stays; this issue's change applies on top), remove every conflict marker, run the checks, then \`git add\` the files and \`git commit --no-edit\`. Say in the PR body how each conflict was resolved."
    echo "Never edit a generated file by hand (\`.gitattributes\` \`linguist-generated\`: the help index, \`docs/parameters.md\`, the effective-config goldens): once the other files are resolved and added, run \`bash scripts/regen.sh --resolve\` before the commit. In a sorted list (the glossary, \`golden.jsonl\`, the analytics plan, the responsive audit) keep both entries, each in its alphabetical place."; } >> "$run_dir/task.md"
fi
# The last check failure on this card (this script's or CI's, #472), when no
# pass came after it: read first, as review feedback is.
CHECKFAIL="$AGENT_REPO_DIR/scripts/agents/checkfail.py"
python3 "$CHECKFAIL" pending "$run_dir/history.json" > "$run_dir/pending-fix.md" || : > "$run_dir/pending-fix.md"
if grep -q . "$run_dir/pending-fix.md"; then
  { echo; echo "# FIRST: fix the failing checks"; echo
    echo "The checks failed on this card's last run (the Dev agent script's own, or CI on the PR). Fix them on this branch first and rerun those checks, then go on with anything else in this file. The failure:"; echo
    sed 's/^/> /' "$run_dir/pending-fix.md"; } >> "$run_dir/task.md"
fi
cp "$run_dir/task.md" "$wt/.agent-task.md"
echo ".agent-task.md" >> "$(git -C "$wt" rev-parse --git-path info/exclude)" # never committed"

PROMPT="Use the developer skill in its \"Agent mode\" and implement the GitHub issue described in .agent-task.md
(read it first; it includes any review feedback to address). You are on branch $branch in an isolated worktree.
Implement it with tests and commit your work (git add + git commit).
Tests: run only the tests your change touches (e.g. go test ./tests/ -run 'TestName|Group', npx jest <file>,
python3 -m unittest <file>), plus gofmt and go vet. Do NOT run the whole go test ./tests/... suite: the script runs
the tests your change touches after you finish and CI runs every suite; a failure comes back to you.
You are already in the worktree: run each command directly (no cd prefix); only the listed commands are
allowed, so a line mixing in another command is refused whole. You may run python3 and go run (scripts you write
in the worktree, the cmd/* generators), the repo's scripts (bash scripts/<name>.sh), shellcheck and make check;
to delete or rename a tracked file use git rm or git mv (plain rm is refused);
set GOMAXPROCS with go test -cpu=N, not an env prefix; keep helper scripts under scripts/
with a test, or delete them before you commit. Do not push, merge, deploy or call GitHub.
When the issue is a bug and you found the merged PR that caused it, put a line 'Caused by #<PR number>' in the
PR-BODY's Plan: the script labels the issue regression (the delivery report's change failure rate, #470).
A change a person can see or do in the monitor is done only with its section in frontend/content/monitor-guide.md,
a golden question in docs/product/assistant/golden.jsonl and go run ./cmd/helpindex (#483); answer the PR
template's 'User-facing?' line in the PR body.
Before you hand over (#467), run the checks that read the whole repo, not only the tests your change touches:
for a frontend change npx jest __tests__/glossary.test.js __tests__/responsive-audit.test.js
__tests__/empty-states.test.js __tests__/menu.test.jsx (from frontend/), and go run ./cmd/helpindex when a
help source changed (OptionStrategy.md, docs/parameters.md, the glossary, the monitor guide, internal/api's
catalogue or explain). As your last step, git merge --no-edit origin/main (resolve any conflict as main's
behaviour plus yours), then rerun the tests your change touches and those checks, and commit.
End your answer with the exact RESULT block the skill defines."

allowed=( "Read" "Edit" "Write" "Grep" "Glob" "Skill"
  "Bash(go build:*)" "Bash(go test:*)" "Bash(go vet:*)" "Bash(gofmt:*)" "Bash(go run:*)" "Bash(go mod tidy)"
  # Scripts and generators (product owner 2026-10-29, #463: doc reflows, sentence checkers, cmd/* tools):
  # any go run, python3 and the repo's shell checks. Like go test, these run code the agent wrote; the
  # denied list below still refuses push, gh, curl, docker and rm as commands.
  "Bash(python3:*)" "Bash(bash scripts/:*)" "Bash(shellcheck:*)" "Bash(make check:*)"
  "Bash(npm ci:*)" "Bash(npx jest:*)" "Bash(npx eslint:*)" "Bash(npx next build:*)" "Bash(mmdc:*)"
  "Bash(git status:*)" "Bash(git diff:*)" "Bash(git log:*)" "Bash(git show:*)" "Bash(git add:*)" "Bash(git commit:*)"
  "Bash(git merge:*)" # the last step: merge origin/main (fetched by the script) and rerun the checks (#467)
  "Bash(git rm:*)" "Bash(git mv:*)" # tracked files only, every one shown in the PR diff (#466 could not drop a file it had split, #515)
  "Bash(ls:*)" "Bash(cat:*)" "Bash(head:*)" "Bash(tail:*)" "Bash(wc:*)" "Bash(grep:*)" "Bash(find:*)" "Bash(sed -n:*)"
  "Bash(cd:*)" ) # cd: a "cd <dir> && go test …" line was refused whole (#257, 2026-10-27); the other part is still checked
denied=( "Bash(git push:*)" "Bash(git remote:*)" "Bash(git reset:*)" "Bash(git checkout:*)" "Bash(gh:*)" "Bash(curl:*)"
  "Bash(docker:*)" "Bash(rm:*)" "WebFetch" "WebSearch" "Agent" )
# The model: DEV_MODEL when set, else the lighter one for a documents/research
# card (label documentation, or an epic in DEV_LIGHT_EPICS), else the default.
labels="$(gh issue view "$issue" -R "$slug" --json labels -q '[.labels[].name] | join(",")' 2>/dev/null || true)"
model="$(python3 "$AGENT_REPO_DIR/scripts/agents/pm_board.py" model "$labels" "${DEV_MODEL:-}" "${DEV_LIGHT_MODEL-sonnet}" "${DEV_LIGHT_EPICS-E5,E11}")"
model_args=()
[[ -n "$model" ]] && model_args=(--model "$model")
log "dev: model ${model:-account default}"
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
  # Commits ahead of main go to origin (no PR), so the next run on the card
  # resumes them instead of restarting from main (#517). With an open PR the
  # branch is the PR's: a half-done push would reach its review, so not there.
  if [[ -z "$existing_pr" ]] && git_locked fetch -q origin && mutex git; then
    if bash "$AGENT_REPO_DIR/scripts/agents/branch_work.sh" keep "$wt" "$branch" 2> "$run_dir/keep.log"; then
      { echo; echo "_Its commits are pushed to \`$branch\` (no PR): the next run continues from them._"; } >> "$run_dir/reason.md"
    fi
    unmutex git
    cat "$run_dir/keep.log"
  fi
  block "$run_dir/reason.md" "$detail"
fi
# Without an open PR, every commit ahead of main counts: a run that resumed a
# branch a blocked run pushed, and had nothing left to add, opens the PR (#517).
if [[ "$(bash "$AGENT_REPO_DIR/scripts/agents/branch_work.sh" count "$wt" "$base" "$existing_pr")" == "0" ]]; then
  echo "The agent reported done but made no commit." > "$run_dir/reason.md"
  block "$run_dir/reason.md" "done without a commit"
fi

# ── Merge main last (#467) ──────────────────────────────────────────────────
# Other PRs merged while the agent worked: merge today's main so the checks
# below run on what the PR will be. A conflict only in generated files is
# regenerated (#465); any other is left to the PR gate (needs-rebase), which
# sends the card back to the agent.
if git_locked fetch -q origin; then
  as_me=(-c user.name="$(git config user.name)" -c user.email="$(git config user.email)")
  if ! git -C "$wt" "${as_me[@]}" merge --no-edit -q origin/main >/dev/null 2>&1; then
    if (cd "$wt" && bash scripts/regen.sh --resolve) >> "$run_dir/regen.log" 2>&1 &&
        git -C "$wt" "${as_me[@]}" commit -q --no-edit; then
      log "dev: last merge with main: generated files regenerated"
    else
      git -C "$wt" merge --abort 2>/dev/null || true
      log "dev: last merge with main conflicts; left to the PR gate"
    fi
  fi
fi
RUN_COMMIT="$(git -C "$wt" rev-parse --short HEAD)"

# ── The PR merged or closed meanwhile (#478) ────────────────────────────────
# PR #474 merged while the agent fixed two review findings on it; the fixes
# were pushed to its branch, "pushed changes" was commented on the merged PR
# and they never reached main. Re-read the PR's state before pushing:
# merged → the new commits move onto agent/issue-<n>-r<k> from origin/main
# (rescue_commits.sh) and go to a new PR "Refs #n"; closed unmerged → stop
# and ask. Returns 0 when the commits moved (the checks must run again).
merged_pr=""
rescue_if_merged() {
  [[ -n "$existing_pr" ]] || return 1
  local state k=1 rc=0
  state="$(gh pr view "$existing_pr" -R "$slug" --json state -q .state)" || return 1 # unknown: push as before
  case "$state" in
    OPEN) return 1 ;;
    MERGED) ;;
    *) { echo "PR #$existing_pr was **closed without merging** while the Dev agent worked on its review feedback."
         echo; echo "Its new commits are on the local branch \`$branch\` only (not pushed). Should they go to a new PR, or is the work dropped?"; } > "$run_dir/reason.md"
       block "$run_dir/reason.md" "PR closed unmerged meanwhile" ;;
  esac
  if ! git_locked fetch -q origin; then # origin/main must hold the merge, or the merged PR's commits would move too
    echo "PR #$existing_pr **merged** while the Dev agent worked on its review feedback, and fetching \`main\` failed, so its new commits (on the local branch \`$branch\` only) were not moved." > "$run_dir/reason.md"
    block "$run_dir/reason.md" "PR merged meanwhile: fetch failed"
  fi
  while git ls-remote --exit-code --heads origin "agent/issue-$issue-r$k" >/dev/null 2>&1; do k=$((k + 1)); done
  bash "$AGENT_REPO_DIR/scripts/agents/rescue_commits.sh" "$wt" "$base" "agent/issue-$issue-r$k" 2> "$run_dir/rescue.log" || rc=$?
  cat "$run_dir/rescue.log"
  if [[ $rc -eq 3 ]]; then # nothing main lacks: no new PR
    run_end skipped "PR #$existing_pr merged meanwhile; the new commits are already in main"
    gh issue comment "$issue" -R "$slug" --body "🤖 Dev agent: #$existing_pr merged while it worked on the review feedback, and main already holds every change it made. Nothing to push." >/dev/null
    set_status "$item" "Done"; git_locked worktree remove --force "$wt" 2>/dev/null || true; exit 0
  elif [[ $rc -ne 0 ]]; then
    { echo "PR #$existing_pr **merged** while the Dev agent worked on its review feedback, and its new commits do not apply cleanly on \`main\`:"
      echo; sed 's/^/    /' "$run_dir/rescue.log"; echo
      echo "They are on the local branch \`$branch\` only (not pushed). A person moves them to a new PR."; } > "$run_dir/reason.md"
    block "$run_dir/reason.md" "PR merged meanwhile: fixes do not apply on main"
  fi
  merged_pr="$existing_pr"; existing_pr=""; RUN_PR=""
  branch="agent/issue-$issue-r$k"
  RUN_COMMIT="$(git -C "$wt" rev-parse --short HEAD)"
  log "dev: #$merged_pr merged meanwhile → its new commits moved to $branch"
  return 0
}

# ── Verify independently ────────────────────────────────────────────────────
checks="$run_dir/checks.md"
check() { # name, command...
  local name="$1"; shift
  if (cd "$wt" && "$@") > "$run_dir/check.log" 2>&1; then echo "- [x] $name" >> "$checks"
  else echo "- [ ] **$name — FAILED**" >> "$checks"; python3 "$CHECKFAIL" tails "$run_dir/check.log" >> "$checks"; return 1; fi
}
# retry_untouched <log> — 0 when every test that failed in <log> is one the
# change does not touch and passed on one of up to 3 re-runs (#472): each goes
# to flaky.txt (named on the PR and its flaky issue once the PR exists).
retry_untouched() {
  local kind name i passed
  python3 "$CHECKFAIL" retry "$wt" "$run_dir/files.txt" "$1" > "$run_dir/retry.txt" || return 1
  while read -r kind name; do
    passed=0
    for i in 1 2 3; do
      log "dev: $name failed, untouched by the change: re-run $i of 3"
      if [[ "$kind" == "go" ]]; then
        (cd "$wt" && go test ./tests/ -race -count=1 -run "^${name}\$") >> "$run_dir/retry.log" 2>&1 < /dev/null && { passed=1; break; }
      else
        (cd "$wt/frontend" && npx jest --ci "$name") >> "$run_dir/retry.log" 2>&1 < /dev/null && { passed=1; break; }
      fi
    done
    [[ $passed -eq 1 ]] || return 1
    echo "$kind $name" >> "$run_dir/flaky.txt"
  done < "$run_dir/retry.txt"
}
test_check() { # name, command... — check(), with untouched failures re-run
  local name="$1"; shift
  if (cd "$wt" && "$@") > "$run_dir/check.log" 2>&1; then echo "- [x] $name" >> "$checks"; return 0; fi
  cp "$run_dir/check.log" "$run_dir/check-failed.log"
  if retry_untouched "$run_dir/check-failed.log"; then
    echo "- [x] $name — flaky, passed on a re-run: $(cut -d' ' -f2 "$run_dir/retry.txt" | paste -sd, -)" >> "$checks"; return 0
  fi
  echo "- [ ] **$name — FAILED**" >> "$checks"; python3 "$CHECKFAIL" tails "$run_dir/check-failed.log" >> "$checks"; return 1
}
# go_touched — the Go tests the change touches (checkfail.py go-tests), split
# as CI splits them (scripts/go_tests.sh): the core under -race, the reports without.
go_touched() {
  local plan="$run_dir/go-tests.txt" core reports
  python3 "$CHECKFAIL" go-tests "$wt" "$run_dir/files.txt" "$(bash "$wt/scripts/go_tests.sh" regex)" > "$plan"
  if grep -qx all "$plan"; then
    test_check "go tests: all (go.mod/go.sum changed)" bash scripts/go_tests.sh all; return
  fi
  core="$(sed -n 's/^core //p' "$plan")"; reports="$(sed -n 's/^reports //p' "$plan")"
  if [[ -z "$core$reports" ]]; then echo "- [x] go tests: none touched by the change" >> "$checks"; return 0; fi
  local rc=0
  [[ -z "$core" ]] || test_check "go tests the change touches (core, -race)" \
    go test ./tests/ -race -count=1 -timeout 30m -run "$core" || rc=1
  [[ -z "$reports" ]] || test_check "go tests the change touches (reports)" \
    go test ./tests/ -count=1 -timeout 30m -run "$reports" || rc=1
  return $rc
}
# The checks the change touches (#472): CI runs every suite on the PR, so the
# Mac no longer runs them twice (2.5–30 min a run). checkfail.py suites picks
# them by what each suite builds or reads; next build runs in CI only.
verify() { # sets ok
  log "dev: verifying $branch"
  : > "$checks"; : > "$run_dir/flaky.txt"
  ok=1
  git -C "$wt" diff --name-only "origin/main"...HEAD > "$run_dir/files.txt"
  python3 "$CHECKFAIL" suites "$run_dir/files.txt" > "$run_dir/suites.txt"
  need() { grep -qx "$1=true" "$run_dir/suites.txt"; }
  if need go_lint; then
    check "gofmt" bash -c 'out="$(gofmt -l cmd internal tests)" && test -z "$out"' || ok=0
    check "go vet ./..." go vet ./... || ok=0
  fi
  if need scripts; then
    check "shell scripts (bash -n, shellcheck)" bash scripts/check_shell.sh || ok=0
    check "script tests (agents, docs, product)" bash -c 'for d in agents docs product; do python3 -m unittest discover -s "scripts/$d" -p "test_*.py" || exit 1; done' || ok=0
  fi
  if need go || need docs; then go_touched || ok=0; fi
  if need frontend; then
    check "frontend: npm ci" bash -c 'cd frontend && npm ci --no-audit --no-fund' || ok=0
    check "frontend: eslint" bash -c 'cd frontend && npx eslint .' || ok=0
    local jest_files=() f
    while IFS= read -r f; do [[ -n "$f" ]] && jest_files+=("$f"); done < <(python3 "$CHECKFAIL" jest-files "$run_dir/files.txt")
    if [[ ${#jest_files[@]} -eq 0 || "${jest_files[0]}" == "all" ]]; then
      test_check "frontend: jest" bash -c 'cd frontend && npx jest --ci' || ok=0
    else
      # --passWithNoTests: a change no test imports (a page, globals.css) has no related
      # test, and jest would exit 1 on it; CI still runs every jest test.
      # shellcheck disable=SC2016 # $@ expands in the inner bash: the changed files
      test_check "frontend: jest (the tests related to the changed files)" \
        bash -c 'cd frontend && npx jest --ci --passWithNoTests --findRelatedTests "$@"' _ "${jest_files[@]}" || ok=0
    fi
  fi
  # The checks that read every file (#467), whatever the change imports.
  local line wide_jest=()
  while IFS= read -r line; do
    case "$line" in
      "jest "*) wide_jest+=("${line#jest }") ;;
      helpindex) check "help index up to date (go run ./cmd/helpindex)" \
          bash -c 'go run ./cmd/helpindex && git diff --exit-code -- frontend/content/help-index.json' || ok=0 ;;
    esac
  done < <(python3 "$CHECKFAIL" repo-wide "$run_dir/files.txt")
  if [[ ${#wide_jest[@]} -gt 0 ]]; then
    # shellcheck disable=SC2016 # $@ expands in the inner bash: the test files
    test_check "frontend: repo-wide tests (glossary, responsive audit, empty states, menu)" \
      bash -c 'cd frontend && npx jest --ci "$@"' _ "${wide_jest[@]}" || ok=0
  fi
  echo "- CI runs every suite on the PR (and \`next build\`); the PR gate marks it ready for review when CI is green." >> "$checks"
}
rescue_if_merged || true
verify
# The checks take up to an hour: the PR may have merged during them. Then the
# commits move and the checks run again on the new base.
if rescue_if_merged; then verify; fi

# ── Pull request ────────────────────────────────────────────────────────────
# pr_head_is <pr> <sha> — true once GitHub shows <sha> as the PR's head (it
# can lag a push by seconds).
pr_head_is() {
  local i h=""
  for i in 1 2 3 4 5 6; do
    h="$(gh pr view "$1" -R "$slug" --json headRefOid -q .headRefOid 2>/dev/null || true)"
    [[ "$h" == "$2" ]] && return 0
    [[ $i -lt 6 ]] && sleep 5
  done
  log "dev: PR #$1 head on GitHub is ${h:0:7}, not the pushed ${2:0:7}"
  return 1
}
git_locked -C "$wt" push -q -u origin "$branch"
pushed="$(git -C "$wt" rev-parse HEAD)"
# "Pushed changes" only when the PR's head is the pushed commit: a PR merged
# in the seconds before the push keeps its merged head, and the commits move.
if [[ -n "$existing_pr" ]] && ! pr_head_is "$existing_pr" "$pushed"; then
  if rescue_if_merged; then
    verify
    git_locked -C "$wt" push -q -u origin "$branch"
    pushed="$(git -C "$wt" rev-parse HEAD)"
  else
    echo "The Dev agent pushed \`${pushed:0:7}\` to \`$branch\`, but PR #$existing_pr's head on GitHub is not that commit. A person checks the PR before it is reviewed." > "$run_dir/reason.md"
    block "$run_dir/reason.md" "pushed commit is not the PR head"
  fi
fi
{
  if [[ -n "$merged_pr" ]]; then
    echo "Refs #$issue"; echo
    echo "_Review fixes for #$merged_pr, which merged while the Dev agent was making them (#478): its new commits, cherry-picked onto \`main\`._"; echo
  else
    echo "Closes #$issue"; echo
  fi
  cat "$run_dir/pr-body.md"; echo
  echo "## Checks the Dev agent script ran (the ones the change touches)"; cat "$checks"; echo
  echo "_Opened by the **Dev agent** (developer skill, agent mode) for #$issue. It cannot merge or deploy: a person reviews and merges; merging deploys to **testnet** and the SME agent verifies the fix in its next review. See [AGENTIC.md](https://github.com/$slug/blob/main/AGENTIC.md)._"
  echo; echo "🤖 Generated with [Claude Code](https://claude.com/claude-code)"
} > "$run_dir/pr.md"
title="$(gh issue view "$issue" -R "$slug" --json title -q .title)"
if [[ -n "$existing_pr" ]]; then
  gh pr edit "$existing_pr" -R "$slug" --body-file "$run_dir/pr.md" >/dev/null
  pr_url="$(gh pr view "$existing_pr" -R "$slug" --json url -q .url)"
  gh pr comment "$existing_pr" -R "$slug" --body "🤖 Dev agent pushed changes for the review feedback. Checks: $([[ $ok -eq 1 ]] && echo passed || echo '**failing**')." >/dev/null
else
  # Always a draft (#472): CI runs every suite, and the PR gate marks it
  # ready once CI is green on its head; only then does the Reviewer start.
  pr_title="$title (#$issue)"
  [[ -n "$merged_pr" ]] && pr_title="$title (#$issue, review fixes for #$merged_pr)"
  pr_url="$(gh pr create -R "$slug" --base main --head "$branch" --title "$pr_title" --label agent:dev --draft --body-file "$run_dir/pr.md")"
fi
log "dev: PR $pr_url"
RUN_PR="${pr_url##*/}"
if [[ -n "$merged_pr" ]]; then
  gh pr comment "$merged_pr" -R "$slug" --body "🤖 Dev agent: this PR merged while it was fixing the review findings. The fixes are in $pr_url (on \`$branch\`, from \`main\`)." >/dev/null || true
  gh issue comment "$issue" -R "$slug" --body "🤖 Dev agent: #$merged_pr merged while it was fixing the review findings, so the fixes never reached \`main\` on that PR. They are in a new PR on top of \`main\`: $pr_url (#478)." >/dev/null
fi
# A bug the agent traced to an earlier PR (a "Caused by #N" line in its PR
# body) is a regression: label the issue and name the cause on it, once, for
# cmd/dora's change failure rate (#470).
caused="$(grep -m1 -E '^[^[:alnum:]]*[Cc]aused by:? *#[0-9]+' "$run_dir/pr-body.md" |
  sed -E 's/^[^[:alnum:]]*[Cc]aused by:? *#([0-9]+).*/\1/')" || caused=""
if [[ -n "$caused" && ",$labels," != *",regression,"* ]]; then # $labels: the issue's, read before the run
  gh issue edit "$issue" -R "$slug" --add-label regression >/dev/null 2>&1 &&
    gh issue comment "$issue" -R "$slug" --body "🤖 Dev agent: this bug is a regression. Caused by #$caused" >/dev/null &&
    log "dev: #$issue labelled regression, caused by #$caused" || log "dev: could not label #$issue regression"
fi

# Untouched tests that failed and then passed on a re-run: flaky, not this PR's (#472).
while read -r _ name; do
  [[ -n "$name" ]] && note_flaky "$name" "$RUN_PR" "the Dev agent's checks" && log "dev: $name flaky (passed on a re-run)"
done < "$run_dir/flaky.txt"

if [[ $ok -eq 1 ]]; then
  if [[ -n "$existing_pr" ]]; then run_end pr_updated "checks passed"; else run_end pr_opened "checks passed"; fi
  set_status "$item" "In review"
  gh issue edit "$issue" -R "$slug" --remove-label agent:needs-human --remove-label needs-rebase --remove-label needs-fix >/dev/null 2>&1 || true
  gh issue comment "$issue" -R "$slug" --body "🤖 Dev agent opened $pr_url — the checks the change touches passed. CI runs every suite; the PR gate marks it ready for review once CI is green.
<!-- dev:check-pass -->" >/dev/null
  notify "Dev agent: PR opened for #$issue" "$title"
else
  # The first failure goes back to the agent (#472), the second to a person.
  { echo "🤖 **Dev agent** — the implementation is in $pr_url (draft), but these checks failed:"; echo; cat "$checks"; } > "$run_dir/reason.md"
  if [[ "$(check_failed "$issue" "$item" "$run_dir/reason.md" dev "${pushed:0:7}")" == "needs-fix" ]]; then
    run_end sent_back "checks failed: back to Ready with needs-fix"
    notify "Dev agent: checks failed on #$issue" "back to Ready (needs-fix)"
    log "dev: #$issue checks failed → Ready (needs-fix)"
  else
    { echo "The checks failed again (the second failure on this card):"; echo; cat "$run_dir/reason.md"; } > "$run_dir/reason2.md"
    block "$run_dir/reason2.md" "checks failed twice: draft PR"
  fi
fi
git_locked worktree remove --force "$wt" 2>/dev/null || true
