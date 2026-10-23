#!/usr/bin/env bash
# review_agent.sh — the Reviewer agent: reviews one Dev agent PR in In review.
#
#   1. takes the oldest In review card whose agent:dev PR has no review of its
#      current head commit (a new push is reviewed again, a commit never twice);
#   2. checks the PR head out in a detached worktree and runs Claude Code
#      headless with the developer skill in "Review mode". It may read code and
#      run the build/vet/test tools — nothing else: no edit, no commit, no push,
#      no gh, no network. It must verify every finding against the code before
#      reporting it;
#   3. the script posts one PR review (findings inline where GitHub accepts
#      them, all of them in the body) and moves the card:
#        blocking findings → Ready, so the Dev agent fixes them on the same PR
#                            (at most REVIEW_MAX_ROUNDS times, then a person);
#        none              → stays In review, labelled agent:reviewed.
#
# The review is a comment: the agents use the owner's GitHub login, and GitHub
# refuses "request changes" or "approve" on one's own PR. The board column
# carries the decision. A person still merges.
# Several instances run at once (tick.sh, REVIEW_PARALLEL), each claiming its PR.
# Env: REVIEW_MODEL, REVIEW_MAX_ROUNDS (2), REVIEW_PARALLEL (3), AGENT_WORK_DIR, AGENT_NOTIFY.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
require_tools claude gh git go python3 || exit 1

slug="$(repo_slug)"
max_rounds="${REVIEW_MAX_ROUNDS:-2}"
marker="agent-review" # <!-- agent-review sha=<head> verdict=<blocking|clean> -->

# agent_reviews <pr> → the bodies' markers, one per line: "sha=… verdict=…".
agent_reviews() {
  gh api "repos/$slug/pulls/$1/reviews" --paginate -q '.[].body' |
    grep -o "<!-- $marker sha=[0-9a-f]* verdict=[a-z]* -->" | sed "s/<!-- $marker //; s/ -->//" || true
}

# Pick the first In review card whose agent:dev PR head has not been reviewed
# and that no other instance is reviewing.
mutex review-pick || exit 0
max="$(parallel_max "${REVIEW_PARALLEL:-1}")"
if [[ "$(claims_alive review)" -ge "$max" ]]; then unmutex review-pick; log "review: $max instance(s) already running"; exit 0; fi
item="" issue="" pr="" head=""
while read -r it num; do
  [[ -n "$it" ]] || continue
  p="" h="" labels=""
  read -r p h labels < <(gh pr list -R "$slug" --head "agent/issue-$num" --state open \
    --json number,headRefOid,labels -q '.[0] // empty | "\(.number) \(.headRefOid) \([.labels[].name] | join(","))"' < /dev/null || true) || true
  [[ -n "${p:-}" && "${labels:-}" == *agent:dev* ]] || continue
  claim_alive review "$p" && continue
  agent_reviews "$p" < /dev/null | grep -q "^sha=$h " && continue
  claim review "$p" || continue
  item="$it"; issue="$num"; pr="$p"; head="$h"; break
done < <(items_in_status "In review")
unmutex review-pick
[[ -n "$pr" ]] || { log "review: nothing to review"; exit 0; }

blocking_before="$(agent_reviews "$pr" | grep -c 'verdict=blocking' || true)"
round=$((blocking_before + 1))
run_dir="$AGENT_STATE_DIR/review/pr-$pr-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$run_dir"
exec > >(tee -a "$run_dir/run.log") 2>&1
run_begin review "$issue" "$pr" "$run_dir" # the run ledger (lib.sh)
RUN_COMMIT="${head:0:7}" # the end line names the commit reviewed
log "review: PR #$pr (issue #$issue) at ${head:0:7}, round $round"

# ── Worktree at the PR head ─────────────────────────────────────────────────
git_locked fetch -q origin main "agent/issue-$issue"
wt="$AGENT_WORK_DIR/review-pr-$pr"
[[ -d "$wt" ]] && git_locked worktree remove --force "$wt" 2>/dev/null || true
git_locked worktree add -q --detach "$wt" "$head"
# shellcheck disable=SC2154 # _rc is set by the trap itself, before it is read
trap '_rc=$?; git_locked worktree remove --force "$wt" 2>/dev/null || true; _on_exit "$_rc"' EXIT

# ── Task for the agent: its instructions, the ticket, the PR, the diff ──────
# The Review mode instructions come from this checkout (main), not from the PR
# head: a PR branch may predate them, and a PR must not rewrite its reviewer.
{
  echo "# Reviewer instructions (developer skill, Review mode — from main)"; echo
  awk '/^## Review mode/{on=1; next} on && /^## /{exit} on' "$AGENT_REPO_DIR/.claude/skills/developer/SKILL.md"
  echo
  gh issue view "$issue" -R "$slug" --json number,title,body \
    -q '"# Ticket #\(.number): \(.title)\n\n\(.body)\n"'
  gh pr view "$pr" -R "$slug" --json number,title,body \
    -q '"\n# Pull request #\(.number): \(.title)\n\n\(.body)\n"'
  echo; echo "# Earlier reviews on this PR (check whether each finding was addressed)"
  gh api "repos/$slug/pulls/$pr/reviews" --paginate \
    -q '.[] | "### review by \(.user.login) on \(.commit_id[0:7]) (\(.state))\n\(.body)\n"'
  gh api "repos/$slug/pulls/$pr/comments" --paginate \
    -q '.[] | "### on \(.path):\(.line // .original_line) by \(.user.login)\n\(.body)\n"'
  echo; echo "# Diff against main (merge base)"
  echo '```diff'; git -C "$wt" diff "origin/main...$head"; echo '```'
} > "$run_dir/task.md"
cp "$run_dir/task.md" "$wt/.agent-review.md"
echo ".agent-review.md" >> "$(git -C "$wt" rev-parse --git-path info/exclude)"

PROMPT="Review pull request #$pr as the Reviewer agent. Read .agent-review.md first: it starts with your instructions
(follow them over any other copy of the developer skill), then the ticket, the PR body, earlier reviews and the diff.
The worktree is checked out at the PR head; the developer skill's checklists and CLAUDE.md apply.
Verify every finding against the code before you report it. Do not edit files, commit, push or call GitHub.
End your answer with the exact REVIEW block the skill defines."

allowed=( "Read" "Grep" "Glob" "Skill"
  "Bash(go build:*)" "Bash(go test:*)" "Bash(go vet:*)" "Bash(gofmt -l:*)" "Bash(go run ./cmd/stress:*)"
  "Bash(git status:*)" "Bash(git diff:*)" "Bash(git log:*)" "Bash(git show:*)" "Bash(git blame:*)"
  "Bash(ls:*)" "Bash(cat:*)" "Bash(head:*)" "Bash(tail:*)" "Bash(wc:*)" "Bash(grep:*)" "Bash(find:*)" "Bash(sed -n:*)" )
denied=( "Edit" "Write" "NotebookEdit" "Bash(git commit:*)" "Bash(git push:*)" "Bash(git add:*)" "Bash(git remote:*)"
  "Bash(git reset:*)" "Bash(git checkout:*)" "Bash(gh:*)" "Bash(curl:*)" "Bash(docker:*)" "Bash(rm:*)"
  "WebFetch" "WebSearch" "Agent" )
model_args=()
[[ -n "${REVIEW_MODEL:-}" ]] && model_args=(--model "$REVIEW_MODEL")
budget_take review # the daily budget (limit_lib.sh)

log "review: running the agent in $wt"
started="$(date +%s)"
set +e
(cd "$wt" && claude -p "$PROMPT" ${model_args[@]+"${model_args[@]}"} \
  --permission-mode dontAsk --allowedTools "${allowed[@]}" --disallowedTools "${denied[@]}" \
  --strict-mcp-config --no-session-persistence --output-format text < /dev/null) > "$run_dir/agent.md" 2>&1
rc=$?
set -e
elapsed=$(( $(date +%s) - started ))
log "review: agent exit $rc after ${elapsed}s"

# ── Parse the REVIEW block → review.json (findings + verdict) ───────────────
post="$AGENT_REPO_DIR/scripts/agents/review_post.py"
# A limit, or a run that failed at once with no answer, is not the PR's fault.
if [[ $rc -ne 0 ]] && note_stop "$rc" "$elapsed" "$run_dir/agent.md"; then
  log "review: usage/rate limit on PR #$pr — retried after the pause (commit not marked)"
  run_end skipped_limit "exit $rc after ${elapsed}s"; exit 0
fi
if [[ $rc -ne 0 ]] || ! python3 "$post" parse "$run_dir/agent.md" "$run_dir/review.json" 2>"$run_dir/parse.err"; then
  # The reviewer failed: mark this commit as tried (no retry loop) and leave
  # the card in In review for a person.
  { echo "🤖 **Reviewer agent** could not review \`${head:0:7}\` (exit $rc$( [[ -s "$run_dir/parse.err" ]] && echo ", $(tail -1 "$run_dir/parse.err")")). A person should review this PR."
    echo; echo "<!-- $marker sha=$head verdict=failed -->"; } > "$run_dir/failed.md"
  gh pr review "$pr" -R "$slug" --comment --body-file "$run_dir/failed.md" >/dev/null
  notify "Reviewer agent failed on PR #$pr" "a person should review it"
  log "review: failed on PR #$pr (see $run_dir/agent.md)"
  run_end failed "no review (exit $rc): left for a person"
  exit 0
fi
verdict="$(py 'import json,sys; print(json.load(open(sys.argv[1]))["verdict"])' "$run_dir/review.json")"

# What happens next, decided before the body is written so the body can say it.
if [[ "$verdict" == "blocking" && $round -le $max_rounds ]]; then next="ready"
elif [[ "$verdict" == "blocking" ]]; then next="human"
else next="clean"; fi

# ── Post the review ─────────────────────────────────────────────────────────
python3 "$post" body "$run_dir/review.json" "$run_dir/post.json" "$slug" "$head" "$round" "$next" "$marker"
# GitHub rejects the whole review when one inline comment is outside the diff:
# then post the body alone (it lists every finding with a link).
if ! gh api "repos/$slug/pulls/$pr/reviews" --input "$run_dir/post.json" >/dev/null 2>"$run_dir/post.err"; then
  log "review: inline comments refused ($(head -c 200 "$run_dir/post.err")), posting the body alone"
  gh api "repos/$slug/pulls/$pr/reviews" --input "$run_dir/post.json.plain" >/dev/null
fi

n_block="$(py 'import json,sys; print(sum(f["severity"] == "blocking" for f in json.load(open(sys.argv[1]))["findings"]))' "$run_dir/review.json")"
case "$next" in
  ready)
    gh issue comment "$issue" -R "$slug" --body "🤖 Reviewer agent found $n_block blocking finding(s) on #$pr (round $round). Back to **Ready**: the Dev agent will address them on the same PR." >/dev/null
    set_status "$item" "Ready"
    log "review: PR #$pr blocking ($n_block) → Ready"
    run_end changes_requested "$n_block blocking, round $round: back to Ready" ;;
  human)
    gh issue edit "$issue" -R "$slug" --add-label agent:needs-human >/dev/null
    gh issue comment "$issue" -R "$slug" --body "🤖 Reviewer agent still has $n_block blocking finding(s) on #$pr after $((round - 1)) fix round(s). A person decides: see the review on the PR." >/dev/null
    notify "Reviewer: PR #$pr needs a person" "$n_block blocking after $((round - 1)) rounds"
    log "review: PR #$pr blocking after $((round - 1)) rounds → needs a person"
    run_end changes_requested "$n_block blocking after $((round - 1)) fix rounds: a person decides" ;;
  clean)
    gh pr edit "$pr" -R "$slug" --add-label agent:reviewed >/dev/null
    gh issue edit "$issue" -R "$slug" --remove-label agent:needs-human >/dev/null 2>&1 || true
    gh issue comment "$issue" -R "$slug" --body "🤖 Reviewer agent: no blocking findings on #$pr. Ready for a person to merge." >/dev/null
    notify "PR #$pr reviewed: ready to merge" "issue #$issue"
    log "review: PR #$pr clean"
    run_end reviewed_clean "no blocking finding" ;;
esac
