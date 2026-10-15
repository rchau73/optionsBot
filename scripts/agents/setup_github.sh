#!/usr/bin/env bash
# setup_github.sh — one-time (idempotent) GitHub setup for the agentic workflow.
#
#   1. labels: agent:sme, agent:dev, agent:needs-human, severity:high|medium|low
#   2. the Project board "optionsBot · agentic workflow", linked to the repo,
#      Status columns: Triage → Ready → In progress → In review → Done, Blocked
#   3. a ruleset on main: changes only through pull requests with green CI;
#      no force pushes, no deletion. The agents cannot merge; a person does.
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
label "agent:needs-human" "d93f0b" "An agent stopped and needs a person"
label "severity:high"     "b60205" "Capital at risk or the bot not doing its job"
label "severity:medium"   "fbca04" "Money left on the table or a rule misbehaving"
label "severity:low"      "c2e0c6" "Hygiene"

# ── 2. Project board ────────────────────────────────────────────────────────
num="$(gh project list --owner "$owner" --format json -L 100 |
  py 'import json,sys; t=sys.argv[1]; print(next((str(p["number"]) for p in json.load(sys.stdin)["projects"] if p["title"]==t), ""))' "$PROJECT_TITLE")"
if [[ -z "$num" ]]; then
  num="$(gh project create --owner "$owner" --title "$PROJECT_TITLE" --format json -q .number)"
  log "created project #$num"
fi
gh project link "$num" --owner "$owner" --repo "${slug#*/}" >/dev/null 2>&1 || true
gh project edit "$num" --owner "$owner" --visibility PUBLIC \
  --description "SME agent finds → a person triages → Dev agent builds a PR → a person merges → testnet deploy → SME verifies. See AGENTIC.md." >/dev/null

fid="$(gh project field-list "$num" --owner "$owner" --format json |
  py 'import json,sys; print(next(f["id"] for f in json.load(sys.stdin)["fields"] if f["name"]=="Status"))')"
# Replace the default Todo/In Progress/Done with the workflow's columns.
gh api graphql -f query='
mutation($field: ID!) {
  updateProjectV2Field(input: {fieldId: $field, singleSelectOptions: [
    {name: "Triage",      color: GRAY,   description: "New finding: a person refines it, then moves it to Ready"},
    {name: "Ready",       color: BLUE,   description: "Approved: the Dev agent picks it up"},
    {name: "In progress", color: YELLOW, description: "The Dev agent is implementing it"},
    {name: "In review",   color: PURPLE, description: "PR open: a person reviews and merges"},
    {name: "Done",        color: GREEN,  description: "Merged and deployed to testnet"},
    {name: "Blocked",     color: RED,    description: "An agent stopped: see the issue comments"}
  ]}) { projectV2Field { ... on ProjectV2SingleSelectField { name options { name } } } }
}' -f field="$fid" >/dev/null
log "project #$num Status columns set"

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
log "done — board: https://github.com/users/$owner/projects/$num"
