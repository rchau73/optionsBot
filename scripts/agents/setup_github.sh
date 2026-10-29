#!/usr/bin/env bash
# setup_github.sh — one-time (idempotent) GitHub setup for the agentic workflow.
#
#   1. labels: agent:sme, agent:dev, agent:pm, agent:reviewed, agent:needs-human,
#      severity:high|medium|low, pm:self-merge, epic, epic:parked, prototype:review, prototype:approved,
#      regression (#470), release:hotfix (#471), needs-fix (#472)
#   2. the Project board "optionsBot · agentic workflow", linked to the repo,
#      Status columns: Triage → Ready → In progress → In review → Done, Blocked,
#      and a Priority field (P0…P3) the PM agent sets and the Dev agent follows
#   3. a ruleset on main: changes only through pull requests with green CI;
#      no force pushes, no deletion. A person merges code; the PM agent merges
#      only its own PRD pull requests with no strategy policy change (pm_answer.sh).
#   4. delete_branch_on_merge: GitHub deletes a PR's branch when it is merged
#      (scripts/prune_branches.sh deletes the local copies).
#
# Needs: gh logged in with the `project` scope (gh auth refresh -s project).
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"

slug="$(repo_slug)"; owner="$(repo_owner)"
log "repo $slug"

# ── 1. Labels ────────────────────────────────────────────────────────────────
label() { gh label create "$1" -R "$slug" --color "$2" --description "$3" --force >/dev/null && log "label $1"; }
label "agent:sme"         "5319e7" "Opened by the SME agent (daily review)"
label "agent:dev"         "1d76db" "Pull request opened by the Dev agent"
label "agent:reviewed"     "0e8a16" "Reviewer agent found no blocking issue: ready to merge"
label "agent:needs-human" "d93f0b" "An agent stopped and needs a person"
label "severity:high"     "b60205" "Capital at risk or the bot not doing its job"
label "severity:medium"   "fbca04" "Money left on the table or a rule misbehaving"
label "severity:low"      "c2e0c6" "Hygiene"
label "agent:pm"          "0052cc" "Triaged by the PM agent (product-manager skill)"
label "pm:self-merge"     "0052cc" "PRD pull request with no strategy policy change: the PM agent merges it once CI is green (remove to merge it yourself)"
label "epic"              "3e4b9e" "Epic of the approved PRD (docs/product/PRD.md): stories are its sub-issues"
label "epic:parked"       "bfd4f2" "Epic parked in the PRD: nothing goes to Ready"
label "prototype:review"  "f9d0c4" "Prototype ready for a person to review (link on the epic)"
label "prototype:approved" "0e8a16" "A person approved the epic's prototype: its UI stories may go to Ready"
label "regression"        "b60205" "A bug a merged PR caused: its body names it on a \"Caused by #N\" line (cmd/dora's change failure rate, #470)"
label "sme:not-fixed"    "b60205" "The SME agent found the merged fix did not work: blocks scripts/promote.sh"
label "release:hotfix"   "d93f0b" "With bug: release.sh releases its merged fix to stable after 1 h on mvp, before the 3-day soak (#471)"
label "needs-fix"        "fbca04" "Checks failed (the Dev agent's or CI): back to the Dev agent once, with the failing log tails (#472)"

# ── 2. Project board ────────────────────────────────────────────────────────
num="$(gh project list --owner "$owner" --format json -L 100 |
  py 'import json,sys; t=sys.argv[1]; print(next((str(p["number"]) for p in json.load(sys.stdin)["projects"] if p["title"]==t), ""))' "$PROJECT_TITLE")"
if [[ -z "$num" ]]; then
  num="$(gh project create --owner "$owner" --title "$PROJECT_TITLE" --format json -q .number)"
  log "created project #$num"
fi
gh project link "$num" --owner "$owner" --repo "${slug#*/}" >/dev/null 2>&1 || true
gh project edit "$num" --owner "$owner" --visibility PUBLIC \
  --description "A person approves the PRD → SME agent finds → PM agent triages into approved epics → Dev agent builds a PR → a person merges → testnet deploy → SME verifies. See AGENTIC.md." >/dev/null

fid="$(gh project field-list "$num" --owner "$owner" --format json |
  py 'import json,sys; print(next(f["id"] for f in json.load(sys.stdin)["fields"] if f["name"]=="Status"))')"
# Replace the default Todo/In Progress/Done with the workflow's columns — only
# when they differ: rewriting the options can clear every card's column.
have="$(gh project field-list "$num" --owner "$owner" --format json |
  py 'import json,sys; print(",".join(o["name"] for o in next(f for f in json.load(sys.stdin)["fields"] if f["name"]=="Status")["options"]))')"
if [[ "$have" == "Triage,Ready,In progress,In review,Done,Blocked" ]]; then
  log "project #$num Status columns already set"
else
gh api graphql -f query='
mutation($field: ID!) {
  updateProjectV2Field(input: {fieldId: $field, singleSelectOptions: [
    {name: "Triage",      color: GRAY,   description: "New ticket: the PM agent places it in an epic and moves approved work to Ready; the rest waits for a person"},
    {name: "Ready",       color: BLUE,   description: "Approved: the Dev agent picks it up"},
    {name: "In progress", color: YELLOW, description: "The Dev agent is implementing it"},
    {name: "In review",   color: PURPLE, description: "PR open: a person reviews and merges"},
    {name: "Done",        color: GREEN,  description: "Merged and deployed to testnet"},
    {name: "Blocked",     color: RED,    description: "An agent stopped: see the issue comments"}
  ]}) { projectV2Field { ... on ProjectV2SingleSelectField { name options { name } } } }
}' -f field="$fid" >/dev/null
log "project #$num Status columns set"
fi
if gh project field-list "$num" --owner "$owner" --format json -q '.fields[].name' | grep -qx Priority; then
  log "Priority field already present"
else
  gh project field-create "$num" --owner "$owner" --name Priority --data-type SINGLE_SELECT \
    --single-select-options "P0,P1,P2,P3" >/dev/null
  log "Priority field created (P0 first: the Dev agent builds Ready in this order)"
fi
rm -f "$PROJECT_CACHE" # ids may have changed: the agents re-read them

# ── 3. Ruleset on main ──────────────────────────────────────────────────────
if gh api "repos/$slug/rulesets" -q '.[].name' | grep -qx "main: PR + green CI"; then
  log "ruleset already present"
else
  gh api -X POST "repos/$slug/rulesets" --input - >/dev/null <<'JSON'
{
  "name": "main: PR + green CI",
  "target": "branch",
  "enforcement": "active",
  "conditions": { "ref_name": { "include": ["~DEFAULT_BRANCH"], "exclude": [] } },
  "rules": [
    { "type": "deletion" },
    { "type": "non_fast_forward" },
    { "type": "pull_request", "parameters": {
        "required_approving_review_count": 0,
        "dismiss_stale_reviews_on_push": false,
        "require_code_owner_review": false,
        "require_last_push_approval": false,
        "required_review_thread_resolution": false } },
    { "type": "required_status_checks", "parameters": {
        "strict_required_status_checks_policy": false,
        "required_status_checks": [
          { "context": "Lint, test & coverage" },
          { "context": "Frontend lint, test & build" } ] } }
  ]
}
JSON
  log "ruleset created: main only through PRs with green CI"
fi
# ── 4. Merged branches are deleted ─────────────────────────────────────────
gh api -X PATCH "repos/$slug" -F delete_branch_on_merge=true >/dev/null
log "merged PR branches are deleted automatically"
log "done — board: https://github.com/users/$owner/projects/$num"
