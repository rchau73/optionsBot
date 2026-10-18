#!/usr/bin/env bash
# pm_answer.sh — the PM agent acts on a person's answer: from the board, not chat.
#
#   1. finds the `agent:pm` ticket (a PRD change or a decision request) whose
#      newest message from a person — on the ticket, or on its open PRD pull
#      request — is newer than the PM agent's last comment (pm_board.py answered;
#      one ticket per run, the oldest answer first);
#   2. runs Claude Code headless with the product-manager skill ("Answers →
#      PRD (agent mode)") in a worktree on branch pm/issue-<n>. It may edit only
#      docs/product/ and commit — no push, no gh, no network;
#   3. the script checks that only docs/product/ changed and that the PRD's
#      epics still parse, pushes the branch and opens (or updates) the PRD pull
#      request "Closes #n" (label agent:pm) → In review, and comments the link;
#      when the answer needs no PRD change, or raises questions, it replies on
#      the ticket instead (questions add agent:needs-human).
#
# It never merges: a person's merge of the PRD pull request is the approval.
# Env: PM_MODEL, AGENT_WORK_DIR, AGENT_NOTIFY.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
take_lock pm-answer || exit 0
require_tools claude gh git python3 || exit 1

BOARD_PY="$AGENT_REPO_DIR/scripts/agents/pm_board.py"
PM_DIR="$AGENT_STATE_DIR/pm"
STATE="$PM_DIR/state.json"
mkdir -p "$PM_DIR"
slug="$(repo_slug)"

# ── 1. Which ticket has a person's answer ──────────────────────────────────
tmp="$PM_DIR/answer-scan"   # overwritten each run
rm -rf "$tmp"; mkdir -p "$tmp"
board_json > "$tmp/board.json"
: > "$tmp/tickets.jsonl"
for num in $(py 'import json,sys
for c in json.load(open(sys.argv[1])):
    if c["state"] == "OPEN" and "agent:pm" in c["labels"] and "epic" not in c["labels"]: print(c["number"])' "$tmp/board.json"); do
  gh issue view "$num" -R "$slug" --json number,state,labels,createdAt,body,comments > "$tmp/issue.json"
  pr="$(gh pr list -R "$slug" --head "pm/issue-$num" --state open --json number -q '.[0].number' || true)"
  if [[ -n "$pr" ]]; then gh pr view "$pr" -R "$slug" --json comments,reviews > "$tmp/pr.json"; else echo '{}' > "$tmp/pr.json"; fi
  py 'import json,sys
i = json.load(open(sys.argv[1])); pr = json.load(open(sys.argv[2]))
st = next(c["status"] for c in json.load(open(sys.argv[3])) if c["number"] == i["number"])
msgs = [{"at": c["createdAt"], "body": c["body"]} for c in i["comments"] + pr.get("comments", [])]
msgs += [{"at": r["submittedAt"], "body": r["body"]} for r in pr.get("reviews", []) if r.get("body")]
print(json.dumps({"number": i["number"], "state": i["state"], "status": st, "labels": [l["name"] for l in i["labels"]],
                  "created": i["createdAt"], "body": i["body"], "messages": msgs}))' "$tmp/issue.json" "$tmp/pr.json" "$tmp/board.json" >> "$tmp/tickets.jsonl"
done
py 'import json,sys; json.dump([json.loads(l) for l in open(sys.argv[1]) if l.strip()], open(sys.argv[2], "w"))' "$tmp/tickets.jsonl" "$tmp/tickets.json"
issue="$(python3 "$BOARD_PY" answered "$tmp/tickets.json" "$STATE")"
[[ -n "$issue" ]] || { log "pm-answer: no new answer on an agent:pm ticket"; exit 0; }
answer_at="$(py 'import json,sys,re
sys.path.insert(0, sys.argv[3]); import pm_board
t = next(t for t in json.load(open(sys.argv[1])) if t["number"] == int(sys.argv[2]))
msgs = t["messages"] + ([{"at": t["created"], "body": t["body"]}] if t["body"] else [])
print(max(m["at"] for m in msgs if not pm_board.is_agent_comment(m["body"])))' "$tmp/tickets.json" "$issue" "$AGENT_REPO_DIR/scripts/agents")"
item="$(py 'import json,sys; print(next(c["item"] for c in json.load(open(sys.argv[1])) if c["number"] == int(sys.argv[2])))' "$tmp/board.json" "$issue")"
status="$(py 'import json,sys; print(next(c["status"] for c in json.load(open(sys.argv[1])) if c["number"] == int(sys.argv[2])))' "$tmp/board.json" "$issue")"

branch="pm/issue-$issue"
run_dir="$PM_DIR/answer-$issue-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$run_dir"
exec > >(tee -a "$run_dir/run.log") 2>&1
log "pm-answer: #$issue has a person's answer ($answer_at) → $branch"

# Handled: the same answer is not acted on twice, whatever happens below.
mark_done() {
  py 'import json,sys
p = sys.argv[1]
try: s = json.load(open(p))
except FileNotFoundError: s = {}
s.setdefault("answered", {})[sys.argv[2]] = sys.argv[3]
json.dump(s, open(p, "w"), indent=1)' "$STATE" "$issue" "$answer_at"
}
reply() { # file, extra label args...
  local f="$1"; shift
  { echo "🧭 **PM agent**"; echo; cat "$f"; echo; echo "_The PM agent (product-manager skill) reads every answer on \`agent:pm\` tickets and their PRD pull requests. It never merges: your merge of the PRD pull request approves it. See [AGENTIC.md](../blob/main/AGENTIC.md)._"; echo "<!-- agent:pm -->"; } > "$run_dir/comment.md"
  gh issue comment "$issue" -R "$slug" --body-file "$run_dir/comment.md" >/dev/null
  [[ $# -eq 0 ]] || gh issue edit "$issue" -R "$slug" "$@" >/dev/null || true
}
fail() { # message
  echo "$1" > "$run_dir/fail.md"
  reply "$run_dir/fail.md" --add-label agent:needs-human
  mark_done; notify "PM agent: #$issue needs a person" "$1"; log "pm-answer: #$issue — $1"; exit 0
}

# ── 2. Worktree and task ────────────────────────────────────────────────────
git fetch -q origin
wt="$AGENT_WORK_DIR/pm-issue-$issue"
[[ -d "$wt" ]] && git worktree remove --force "$wt" 2>/dev/null || true
if git ls-remote --exit-code --heads origin "$branch" >/dev/null 2>&1; then
  git worktree add -q -B "$branch" "$wt" "origin/$branch"
  existing_pr="$(gh pr list -R "$slug" --head "$branch" --state open --json number -q '.[0].number' || true)"
else
  git worktree add -q -B "$branch" "$wt" origin/main
  existing_pr=""
fi
base="$(git -C "$wt" rev-parse HEAD)"

gh issue view "$issue" -R "$slug" --json number,title,body,labels,comments \
  -q '"# Ticket #\(.number): \(.title)\n\nLabels: \([.labels[].name] | join(", "))\n\n\(.body)\n\n## Comments (oldest first)\n\(.comments | map("### \(.createdAt)\n\(.body)") | join("\n\n"))"' > "$run_dir/task.md"
if [[ -n "$existing_pr" ]]; then
  gh pr view "$existing_pr" -R "$slug" --json comments,reviews \
    -q '"\n\n# Your open PRD pull request #'"$existing_pr"' on this branch — its comments and reviews\n\(.comments | map("### \(.createdAt)\n\(.body)") | join("\n\n"))\n\(.reviews | map(select(.body != "")) | map("### review \(.state) \(.submittedAt)\n\(.body)") | join("\n\n"))"' >> "$run_dir/task.md" || true
  gh api "repos/$slug/pulls/$existing_pr/comments" -q '.[] | "### on \(.path):\(.line // .original_line)\n\(.body)\n"' >> "$run_dir/task.md" 2>/dev/null || true
fi
{ echo; echo "# The answer to act on"; echo; echo "The newest message from a person is the one at $answer_at. Messages starting with 🧭, 🤖, 🚀 or **SME are agents'."; } >> "$run_dir/task.md"
cp "$run_dir/task.md" "$wt/.agent-task.md"
echo ".agent-task.md" >> "$(git -C "$wt" rev-parse --git-path info/exclude)"

PROMPT="Use the product-manager skill and run its \"Answers → PRD (agent mode)\" procedure.
The ticket, its comments and any open PRD pull request's feedback are in .agent-task.md (read it first).
You are on branch $branch in an isolated worktree of the repository; docs/product/PRD.md is the PRD.
Edit only files under docs/product/ and commit your work (git add docs/product + git commit).
Do not push, merge or call GitHub. End your answer with the exact RESULT block the skill defines."
allowed=( "Read" "Grep" "Glob" "Skill" "Edit(docs/product/**)" "Write(docs/product/**)"
  "Bash(git status:*)" "Bash(git diff:*)" "Bash(git log:*)" "Bash(git show:*)" "Bash(git add docs/product:*)" "Bash(git commit:*)"
  "Bash(ls:*)" "Bash(cat:*)" "Bash(head:*)" "Bash(wc:*)" "Bash(grep:*)" "Bash(python3 scripts/agents/pm_board.py epics:*)" )
denied=( "Bash(git push:*)" "Bash(git remote:*)" "Bash(git reset:*)" "Bash(git checkout:*)" "Bash(gh:*)" "Bash(curl:*)"
  "Bash(rm:*)" "WebFetch" "WebSearch" "Agent" "NotebookEdit" )
model_args=()
[[ -n "${PM_MODEL:-}" ]] && model_args=(--model "$PM_MODEL")

log "pm-answer: running the agent in $wt"
set +e
(cd "$wt" && claude -p "$PROMPT" ${model_args[@]+"${model_args[@]}"} \
  --permission-mode dontAsk --allowedTools "${allowed[@]}" --disallowedTools "${denied[@]}" \
  --strict-mcp-config --no-session-persistence --output-format text < /dev/null) > "$run_dir/agent.md" 2>&1
rc=$?
set -e
rm -f "$wt/.agent-task.md"
log "pm-answer: agent exit $rc"

result="$(grep -m1 '^RESULT:' "$run_dir/agent.md" | sed 's/^RESULT: *//' || true)"
py 'import re,sys
t = open(sys.argv[1]).read()
for tag, out in (("PM-REPLY", sys.argv[2]), ("PR-BODY", sys.argv[3])):
    m = re.search(r"<!-- " + tag + r"\s*(.*?)\s*-->", t, re.S)
    open(out, "w").write(m.group(1) if m else "")' "$run_dir/agent.md" "$run_dir/reply.md" "$run_dir/pr-body.md"

if [[ $rc -ne 0 || -z "$result" ]]; then
  why="the agent did not finish (exit $rc, result '${result:-none}')"
  grep -qiE "session limit|usage limit" "$run_dir/agent.md" && why="the Claude usage limit was reached: comment again on the ticket after it resets"
  fail "Stopped because $why. Run log: \`data/agents/pm/$(basename "$run_dir")\`."
fi
commits="$(git -C "$wt" rev-list --count "$base"..HEAD)"

case "$result" in
  reply|needs_human)
    [[ -s "$run_dir/reply.md" ]] || fail "The agent answered '$result' with no comment for the ticket."
    if [[ "$result" == "needs_human" ]]; then reply "$run_dir/reply.md" --add-label agent:needs-human
    else reply "$run_dir/reply.md" --remove-label agent:needs-human; fi
    # Ready is the Dev agent's queue: a decision ticket waits in Triage.
    [[ "$status" != "Ready" ]] || set_status "$item" "Triage" || true
    mark_done; log "pm-answer: replied on #$issue ($result)"
    notify "PM agent replied on #$issue" "$(head -c 120 "$run_dir/reply.md")"
    git worktree remove --force "$wt" 2>/dev/null || true
    exit 0 ;;
  pr) ;;
  *) fail "The agent ended with an unknown result '$result'." ;;
esac
[[ "$commits" != "0" ]] || fail "The agent reported a PRD change but made no commit."

# ── 3. Verify, then the pull request ────────────────────────────────────────
outside="$(git -C "$wt" diff --name-only origin/main...HEAD | grep -v '^docs/product/' || true)"
[[ -z "$outside" ]] || fail "The PRD change touched files outside docs/product/ ($(echo $outside)): not pushed."
if ! python3 "$BOARD_PY" epics "$wt/docs/product/PRD.md" "$run_dir/epics.json" 2> "$run_dir/epics.err"; then
  fail "The edited PRD has a malformed epic marker ($(tail -1 "$run_dir/epics.err")): not pushed."
fi
git -C "$wt" push -q -u origin "$branch"
{
  echo "Closes #$issue"; echo
  if [[ -s "$run_dir/pr-body.md" ]]; then cat "$run_dir/pr-body.md"; else cat "$run_dir/reply.md"; fi; echo
  echo "**Merging this PR approves the PRD change** (the roadmap, its epics and their business requirements). The PM agent then creates or updates the epic issues from \`main\`. To ask for changes, comment here or on #$issue: the PM agent revises this PR in its next run."
  echo; echo "_Opened by the **PM agent** (product-manager skill, \`scripts/agents/pm_answer.sh\`): only \`docs/product/\` changed, epic markers checked. It never merges. See [AGENTIC.md](https://github.com/$slug/blob/main/AGENTIC.md)._"
  echo; echo "🤖 Generated with [Claude Code](https://claude.com/claude-code)"
} > "$run_dir/pr.md"
title="$(gh issue view "$issue" -R "$slug" --json title -q .title)"
if [[ -n "$existing_pr" ]]; then
  gh pr edit "$existing_pr" -R "$slug" --body-file "$run_dir/pr.md" >/dev/null
  pr_url="$(gh pr view "$existing_pr" -R "$slug" --json url -q .url)"
  gh pr comment "$existing_pr" -R "$slug" --body "🧭 PM agent revised the PRD for the latest feedback (on this PR or #$issue). <!-- agent:pm -->" >/dev/null
else
  pr_url="$(gh pr create -R "$slug" --base main --head "$branch" --title "PRD: $title (#$issue)" --label agent:pm --body-file "$run_dir/pr.md")"
fi
log "pm-answer: PR $pr_url"
{ echo "PRD pull request: $pr_url — **your merge approves it**."; echo
  [[ -s "$run_dir/reply.md" ]] && cat "$run_dir/reply.md"; } > "$run_dir/opened.md"
reply "$run_dir/opened.md" --remove-label agent:needs-human
set_status "$item" "In review" || true
mark_done
notify "PM agent: PRD PR for #$issue" "$title"
git worktree remove --force "$wt" 2>/dev/null || true
