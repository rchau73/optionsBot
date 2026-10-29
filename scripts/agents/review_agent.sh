#!/usr/bin/env bash
# review_agent.sh — the Reviewer agent: reviews one Dev agent PR in In review.
#
#   1. takes the oldest In review card whose agent:dev PR has no review of its
#      current head commit (a new push is reviewed again, a commit never twice
#      — except once more after an incomplete review);
#   2. writes the diff one file per changed path (.agent-review/diff/, with
#      an index), checks the PR head out in a detached worktree and runs Claude Code
#      headless with the developer skill in "Review mode". It may read code and
#      run the build/vet/test tools — nothing else: no edit, no commit, no push,
#      no gh, no network. It must verify every finding against the code before
#      reporting it;
#   3. the script posts one PR review (findings inline where GitHub accepts
#      them, all of them in the body) and moves the card:
#        blocking findings → Ready, so the Dev agent fixes them on the same PR
#                            (at most REVIEW_MAX_ROUNDS times, then a person);
#        a changed file unread → "incomplete" (#476): stays In review, not
#                            reviewed for the gate; retried on the next tick,
#                            the second time agent:needs-human;
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
marker="agent-review" # <!-- agent-review sha=<head> verdict=<blocking|clean|incomplete|failed> policy=<yes|no> -->
post="$AGENT_REPO_DIR/scripts/agents/review_post.py"

# agent_reviews <pr> → the bodies' markers, one per line: "sha=… verdict=…".
agent_reviews() {
  gh api "repos/$slug/pulls/$1/reviews" --paginate -q '.[].body' |
    grep -oE "<!-- $marker sha=[0-9a-f]* verdict=[a-z]*( policy=[a-z]*)? -->" | sed "s/<!-- $marker //; s/ -->//" || true
}

# Pick the first In review card whose agent:dev PR head has not been reviewed
# and that no other instance is reviewing.
mutex review-pick || exit 0
max="$(parallel_max "${REVIEW_PARALLEL:-1}")"
if [[ "$(claims_alive review)" -ge "$max" ]]; then unmutex review-pick; log "review: $max instance(s) already running"; exit 0; fi
item="" issue="" pr="" head="" pr_branch=""
while read -r it num; do
  [[ -n "$it" ]] || continue
  p="" b="" h="" labels=""
  read -r p b h labels < <(open_dev_pr "$num" < /dev/null || true) || true # agent/issue-<n>[-r<k>] (#478)
  [[ -n "${p:-}" && "${labels:-}" == *agent:dev* ]] || continue
  claim_alive review "$p" && continue
  # A head is reviewed once — an incomplete review (a changed file unread,
  # #476) once more on the next tick, then it is left for a person.
  [[ "$(agent_reviews "$p" < /dev/null | python3 "$post" due "$h" /dev/stdin)" == "yes" ]] || continue
  # A head that only adds GitHub's update-with-main merges to the last review
  # keeps that review (pr_gate.py equiv, #468): nothing new to read — unless
  # that review was incomplete.
  last="$(agent_reviews "$p" < /dev/null | tail -1 | sed -n 's/^sha=\([0-9a-f]*\) verdict=\([a-z]*\).*/\1 \2/p')"
  last_verdict="${last#* }"; last="${last%% *}"
  if [[ -n "$last" && "$last_verdict" != "incomplete" ]] && gh api "repos/$slug/compare/$last...$h" < /dev/null 2>/dev/null |
      python3 "$AGENT_REPO_DIR/scripts/agents/pr_gate.py" equiv /dev/stdin | grep -q .; then continue; fi
  claim review "$p" || continue
  item="$it"; issue="$num"; pr="$p"; pr_branch="$b"; head="$h"; break
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
git_locked fetch -q origin main "$pr_branch"
wt="$AGENT_WORK_DIR/review-pr-$pr"
[[ -d "$wt" ]] && git_locked worktree remove --force "$wt" 2>/dev/null || true
git_locked worktree add -q --detach "$wt" "$head"
# shellcheck disable=SC2154 # _rc is set by the trap itself, before it is read
trap '_rc=$?; git_locked worktree remove --force "$wt" 2>/dev/null || true; _on_exit "$_rc"' EXIT

# ── The diff, one file per changed path, and its index (#476) ───────────────
# One big file was read only to the Read tool's 2,000 lines (PR #474: 2,381
# lines, the rest unread, verdict clean). Per file, every diff is a short read
# or paged on purpose, and `checked` must name each file of files.txt as read.
review_dir="$wt/.agent-review"
mkdir -p "$review_dir/diff"
git -C "$wt" diff --no-renames --name-only "origin/main...$head" > "$run_dir/files.txt"
{
  echo "# Changed files: read every diff listed here"; echo
  echo "Diff against main (merge base), one file per path. Read each one; a diff over 2,000 lines needs"
  echo "several Reads (offset/limit). List every file in \`checked\` with \`read: yes|no\`."; echo
  while IFS= read -r f; do
    mkdir -p "$(dirname "$review_dir/diff/$f.diff")"
    git -C "$wt" diff --no-renames "origin/main...$head" -- "$f" > "$review_dir/diff/$f.diff"
    echo "- \`$f\` — $(wc -l < "$review_dir/diff/$f.diff" | tr -d ' ') lines → \`.agent-review/diff/$f.diff\`"
  done < "$run_dir/files.txt"
} > "$review_dir/index.md"
cp -R "$review_dir" "$run_dir/agent-review"

# ── Task for the agent: its instructions, the ticket, the PR, earlier reviews ─
# The Review mode instructions come from this checkout (main), not from the PR
# head: a PR branch may predate them, and a PR must not rewrite its reviewer.
{
  echo "# Reviewer instructions (developer skill, Review mode — from main)"; echo
  # Review mode plus the quality bar it enforces (#468).
  awk '/^## (Quality bar|Review mode)/{on=1; print; next} on && /^## /{on=0} on' "$AGENT_REPO_DIR/.claude/skills/developer/SKILL.md"
  echo
  # #476: these override the skill's description of the diff and of `checked`.
  echo "## Reading the diff, and \`checked\` (#476 — overrides the lines above)"; echo
  echo "- The diff is not in this file: it is one file per changed path under \`.agent-review/diff/<path>.diff\`,"
  echo "  listed with its line count in \`.agent-review/index.md\`. Read every file in the index, to its end. The Read"
  echo "  tool returns 2,000 lines at a time: page a longer diff with \`offset\`/\`limit\`."
  echo "- In the REVIEW-JSON block, \`checked\` is a list with one entry per file of the index:"
  echo "  \`\"checked\": [{\"file\": \"internal/x/y.go\", \"read\": \"yes\"}, …]\` (\`read\`: yes | no), and what you ran"
  echo "  (tests and their result) goes in \`\"ran\": \"<bullets>\"\`."
  echo "- A changed file that is \`read: no\` or missing from \`checked\` makes the review \`incomplete\`, never \`clean\`:"
  echo "  the PR gate treats it as not reviewed, it is retried once on the next tick, then a person reviews it."
  echo "  Say \`no\` honestly for a file you did not read."
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
  echo; echo "# The diff"
  echo "Not here: one file per changed path, listed with its line count in \`.agent-review/index.md\`."
} > "$run_dir/task.md"
cp "$run_dir/task.md" "$wt/.agent-review.md"
printf '%s\n' ".agent-review.md" ".agent-review/" >> "$(git -C "$wt" rev-parse --git-path info/exclude)"

PROMPT="Review pull request #$pr as the Reviewer agent. Read .agent-review.md first: it starts with your instructions
(follow them over any other copy of the developer skill), then the ticket, the PR body and earlier reviews.
Then read .agent-review/index.md and every diff file it lists, to the end (page any over 2,000 lines); a changed
file you leave unread makes the review incomplete, never clean. Fill \"checked\" with one {\"file\", \"read\": \"yes|no\"}
entry per file of the index, as .agent-review.md says.
The worktree is checked out at the PR head; the developer skill's checklists and CLAUDE.md apply.
Verify every finding against the code before you report it. Do not edit files, commit, push or call GitHub.
A change a person can see or do in the monitor without its section in frontend/content/monitor-guide.md or without
a golden question in docs/product/assistant/golden.jsonl is a blocking finding (#483).
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
# A limit, or a run that failed at once with no answer, is not the PR's fault.
if [[ $rc -ne 0 ]] && note_stop "$rc" "$elapsed" "$run_dir/agent.md"; then
  log "review: usage/rate limit on PR #$pr — retried after the pause (commit not marked)"
  run_end skipped_limit "exit $rc after ${elapsed}s"; exit 0
fi
if [[ $rc -ne 0 ]] || ! python3 "$post" parse "$run_dir/agent.md" "$run_dir/review.json" "$run_dir/files.txt" 2>"$run_dir/parse.err"; then
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
elif [[ "$verdict" == "incomplete" ]]; then
  # The first incomplete review of this head is retried on the next tick; the
  # second leaves it for a person (review_post.py due, MAX_INCOMPLETE).
  if [[ "$(agent_reviews "$pr" | grep -c "^sha=$head verdict=incomplete" || true)" -ge 1 ]]; then next="incomplete-human"
  else next="incomplete"; fi
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
  incomplete)
    n_unread="$(py 'import json,sys; print(len(json.load(open(sys.argv[1]))["unread"]))' "$run_dir/review.json")"
    log "review: PR #$pr incomplete ($n_unread changed file(s) unread) → retried on the next tick"
    run_end incomplete "$n_unread changed file(s) unread: retried on the next tick" ;;
  incomplete-human)
    n_unread="$(py 'import json,sys; print(len(json.load(open(sys.argv[1]))["unread"]))' "$run_dir/review.json")"
    gh issue edit "$issue" -R "$slug" --add-label agent:needs-human >/dev/null
    gh issue comment "$issue" -R "$slug" --body "🤖 Reviewer agent could not read every changed file of #$pr in two tries ($n_unread unread). A person reviews it: see the review on the PR." >/dev/null
    notify "Reviewer: PR #$pr needs a person" "$n_unread changed file(s) unread after a retry"
    log "review: PR #$pr incomplete twice → needs a person"
    run_end incomplete "$n_unread changed file(s) unread after a retry: a person reviews" ;;
  clean)
    gh pr edit "$pr" -R "$slug" --add-label agent:reviewed >/dev/null
    gh issue edit "$issue" -R "$slug" --remove-label agent:needs-human >/dev/null 2>&1 || true
    policy="$(py 'import json,sys; print(json.load(open(sys.argv[1]))["policy"]["changes"])' "$run_dir/review.json")"
    if [[ "$policy" == "no" ]]; then gh issue comment "$issue" -R "$slug" --body "🤖 Reviewer agent: no blocking findings on #$pr and no policy change. The PR gate merges it once CI is green and it is up to date with main." >/dev/null
    else gh issue comment "$issue" -R "$slug" --body "🤖 Reviewer agent: no blocking findings on #$pr. It changes a business rule or policy, so a person merges it." >/dev/null; fi
    notify "PR #$pr reviewed: ready to merge" "issue #$issue"
    log "review: PR #$pr clean"
    run_end reviewed_clean "no blocking finding" ;;
esac
