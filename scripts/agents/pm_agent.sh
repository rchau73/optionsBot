#!/usr/bin/env bash
# pm_agent.sh — the PM agent (product-manager skill): owns the board's product order.
#
#   1. reads the approved PRD — docs/product/PRD.md on origin/main: a person
#      merged it, which is the approval of the roadmap, its epics and their
#      business requirements — and keeps one `epic` issue per PRD epic on the
#      board (created, updated, closed when done);
#   2. when a Triage card is new or changed, or an open card has no epic,
#      runs Claude Code headless and READ-ONLY with the product-manager skill
#      ("Board triage (agent mode)"): it places each card in exactly one epic,
#      sets its priority, and proposes Ready only for work the PRD approved;
#   3. pm_board.py gates every proposal (approved epic, trading-rule area the
#      epic covers, approved prototype for UI stories); the script — never the
#      agent — links the card to its epic (sub-issue), sets Priority, moves it,
#      and comments why. Everything the PRD did not approve stays in Triage for
#      a person (agent:needs-human).
#
#   4. keeps every approved epic moving (product owner, 2026-10-19: "touch all
#      the epics … keep the thing moving forward 24h 7 days a week"):
#      - a card that must wait ("Depends on #N", "Waits until <date>") goes to
#        Blocked with pm:waiting instead of Ready; each run releases those
#        whose issues closed and dates passed → Ready (a dependency closed as
#        not planned → Triage for a person);
#      - an epic with fewer than 2 moving stories gets its next stories from the
#        agent ("Epic planning (agent mode)"), filed through the same gate;
#      - every card under an epic carries its E# label and [E#] title prefix;
#      - Ready is ordered by Priority, Triage puts cards waiting for a person last.
#
# Of In progress / In review / Blocked / Done it moves only Blocked cards with
# pm:waiting (its own parking), and moves a card out of Ready only if it put it
# there itself. The Dev agents build Ready in Priority order (lib.sh ready_next).
# Env: PM_MODEL, AGENT_NOTIFY.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
take_lock pm || exit 0
require_tools claude gh git python3 || exit 1

BOARD_PY="$AGENT_REPO_DIR/scripts/agents/pm_board.py"
PM_DIR="$AGENT_STATE_DIR/pm"
STATE="$PM_DIR/state.json"
mkdir -p "$PM_DIR"
slug="$(repo_slug)"

# ── 1. The approved PRD and its epics ───────────────────────────────────────
git fetch -q origin
run="$PM_DIR/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$run"
if ! git show origin/main:docs/product/PRD.md > "$run/PRD.md" 2>/dev/null; then
  log "pm: no approved PRD on main (docs/product/PRD.md): nothing to triage"; rm -rf "$run"; exit 0
fi
if ! python3 "$BOARD_PY" epics "$run/PRD.md" "$run/epics.json" 2> "$run/epics.err"; then
  log "pm: the PRD on main has a malformed epic: $(tail -1 "$run/epics.err")"
  notify "PM agent: PRD epic error" "$(tail -1 "$run/epics.err")"
  exit 1
fi
exec > >(tee -a "$run/run.log") 2>&1

gh issue list -R "$slug" --label epic --state all -L 200 --json number,id,title,body,state,labels > "$run/epic_issues.json"
footer="_Epic of the approved PRD ([docs/product/PRD.md](../blob/main/docs/product/PRD.md), merged on main = approved by a person). Its stories are its sub-issues; the **PM agent** places new tickets here and ranks them. UI stories go to Ready once a person adds **prototype:approved** to this epic._"
n="$(py 'import json,sys; print(len(json.load(open(sys.argv[1]))))' "$run/epics.json")"
i=0
while [[ $i -lt $n ]]; do
  # → "<id>\t<status>\t<title>\t<existing number or ->\t<existing state or ->\t<body same 0|1>"
  py 'import json,sys
e = json.load(open(sys.argv[1]))[int(sys.argv[2])]
body = e["body"] + "\n\n<!-- epic-id: " + e["id"] + " -->\n\n" + sys.argv[4]
open(sys.argv[5], "w").write(body)
title = "Epic " + e["id"] + " · " + e["name"]
cur = next((x for x in json.load(open(sys.argv[3])) if "<!-- epic-id: " + e["id"] + " -->" in (x["body"] or "")), None)
same = cur is not None and cur["title"] == title and cur["body"].strip() == body.strip()
print("\t".join([e["id"], e["status"], title, str(cur["number"]) if cur else "-", cur["state"] if cur else "-", "1" if same else "0"]))' \
    "$run/epics.json" "$i" "$run/epic_issues.json" "$footer" "$run/epic-body.md" > "$run/epic-meta.tsv"
  IFS=$'\t' read -r eid est title num state same < "$run/epic-meta.tsv"
  if [[ "$num" == "-" ]]; then
    if [[ "$est" != "done" ]]; then
      url="$(gh issue create -R "$slug" --title "$title" --label epic --body-file "$run/epic-body.md")"
      num="${url##*/}"; log "pm: created epic $eid → #$num"
      # On the board without a Status: the Dev agent reads only Ready.
      gh project item-add "$(project_info | cut -d' ' -f1)" --owner "$(repo_owner)" --url "$url" >/dev/null || log "pm: could not add #$num to the board"
    fi
  elif [[ "$same" == "0" ]]; then
    gh issue edit "$num" -R "$slug" --title "$title" --body-file "$run/epic-body.md" >/dev/null && log "pm: updated epic $eid #$num from the PRD" || log "pm: could not update epic #$num"
  fi
  if [[ "$num" != "-" ]]; then
    case "$est" in
      done) if [[ "$state" == "OPEN" ]]; then gh issue close "$num" -R "$slug" --comment "Done in the approved PRD." >/dev/null && log "pm: closed epic $eid #$num" || true; fi ;;
      parked) gh issue edit "$num" -R "$slug" --add-label epic:parked >/dev/null ;;
      approved) if [[ "$state" == "OPEN" ]]; then gh issue edit "$num" -R "$slug" --remove-label epic:parked >/dev/null 2>&1 || true; fi ;;
    esac
  fi
  i=$((i + 1))
done
# Epic numbers, node ids and prototype approval (a label only a person adds).
gh issue list -R "$slug" --label epic --state all -L 200 --json number,id,body,labels > "$run/epic_issues.json"
py 'import json,sys
issues = json.load(open(sys.argv[2])); es = json.load(open(sys.argv[1]))
for e in es:
    cur = next((x for x in issues if "<!-- epic-id: " + e["id"] + " -->" in (x["body"] or "")), None)
    e["number"] = cur["number"] if cur else None
    e["node"] = cur["id"] if cur else None
    e["prototype_approved"] = bool(cur) and any(l["name"] == "prototype:approved" for l in cur["labels"])
json.dump(es, open(sys.argv[1], "w"), indent=1)' "$run/epics.json" "$run/epic_issues.json"

# ── 2. What needs a decision ────────────────────────────────────────────────
board_json > "$run/board.json"
python3 "$BOARD_PY" pending "$run/board.json" "$STATE" "$run/epics.json" "$run/pending.json"
todo="$(py 'import json,sys; p=json.load(open(sys.argv[1])); print(" ".join(str(n) for n in p["triage"] + p["unparented"]))' "$run/pending.json")"

model_args=()
[[ -n "${PM_MODEL:-}" ]] && model_args=(--model "$PM_MODEL")

# state_set <python body using s> [args…] — update state.json under a mutex
# (pm_answer.sh instances write it too).
state_set() {
  local code="$1"; shift
  mutex pm-state || return 1
  py 'import json,sys
p = sys.argv[1]
try: s = json.load(open(p))
except FileNotFoundError: s = {}
'"$code"'
json.dump(s, open(p, "w"), indent=1)' "$STATE" "$@"
  unmutex pm-state
}

# The run ledger (lib.sh) records a PM run only when it asks Claude (triage or
# planning): the board passes alone run every tick and decide nothing new.
pm_outcome="" pm_detail=""
pm_note() { # outcome, detail — a failure or a limit outranks a success
  case "$pm_outcome:$1" in
    failed:*|skipped_limit:stories_filed|skipped_limit:triaged) ;;
    *) pm_outcome="$1" ;;
  esac
  pm_detail="${pm_detail:+$pm_detail; }$2"
}

triage() {
[[ -n "$todo" ]] || { log "pm: nothing new in Triage, every open card has an epic"; return 0; }
run_begin pm "" "" "$run"
log "pm: deciding on #${todo// / #}"
for num in $todo; do
  gh issue view "$num" -R "$slug" --json number,title,body,labels,comments \
    -q '{number, title, labels: [.labels[].name], body: .body[0:4000], comments: [.comments[-4:][] | {author: .author.login, at: .createdAt, body: .body[0:800]}]}'
done | py 'import json,sys; json.dump([json.loads(l) for l in sys.stdin if l.strip()], open(sys.argv[1], "w"), indent=1)' "$run/pending_issues.json"

PROMPT="Use the product-manager skill and run its \"Board triage (agent mode)\" procedure.
Inputs are in $run/: PRD.md (the approved PRD from main), epics.json (its epics with their issue numbers,
rule areas and prototype approval), pending.json (the cards to decide: 'triage' and 'unparented'),
pending_issues.json (their bodies and last comments) and board.json (every card with its column and priority).
You may also read CLAUDE.md, AGENTIC.md, OptionStrategy.md and docs/.
Print only the report in the exact output shape the skill defines, ending with the PM-JSON block."
started="$(date +%s)"
if ! claude -p "$PROMPT" ${model_args[@]+"${model_args[@]}"} \
  --permission-mode dontAsk \
  --allowedTools "Read" "Grep" "Glob" "Skill" \
  --disallowedTools "Bash" "Edit" "Write" "NotebookEdit" "WebFetch" "WebSearch" "Agent" \
  --strict-mcp-config --no-session-persistence \
  --output-format text < /dev/null > "$run/report.md"; then
  if note_stop 1 $(( $(date +%s) - started )) "$run/report.md"; then pm_note skipped_limit "triage: limit"
  else pm_note failed "triage: claude failed"; fi
  log "pm: claude failed"; notify "PM agent FAILED" "see $run/run.log"; return 1
fi
py 'import json,re,sys
t = open(sys.argv[1]).read()
m = re.search(r"<!-- PM-JSON\s*(\{.*?\})\s*-->", t, re.S)
json.dump(json.loads(m.group(1)) if m else {}, open(sys.argv[2], "w"), indent=1)
print("PM-JSON block:", "found" if m else "MISSING")' "$run/report.md" "$run/pm.json"
if [[ "$(cat "$run/pm.json")" == "{}" ]]; then
  # Nothing is marked decided, so the same cards are asked again next tick.
  pm_note failed "triage: no PM-JSON block"
  log "pm: no PM-JSON block in the report"; notify "PM agent: no decisions block" "see $run/report.md"; return 1
fi

# ── 3. Gate and act (the script acts, never the agent) ──────────────────────
python3 "$BOARD_PY" gate "$run/pm.json" "$run/board.json" "$run/epics.json" "$STATE" "$run/actions.json"
py 'import json,sys
acts = json.load(open(sys.argv[1])); cards = {c["number"]: c for c in json.load(open(sys.argv[2]))}
es = {e["id"]: e for e in json.load(open(sys.argv[3]))}
for k, a in enumerate(acts):
    c = cards[a["issue"]]; e = es.get(a.get("epic") or "")
    head = []
    if e: head.append("epic **" + e["id"] + " · " + e["name"] + "** (#" + str(e["number"]) + ")")
    if a.get("priority"): head.append("priority **" + a["priority"] + "**")
    if a.get("move"): head.append("→ **" + a["move"] + "**")
    elif a.get("refused"): head.append("stays in **Triage**: " + a["refused"])
    text = ""
    if a.get("move") or a.get("refused") or a["comment"]:
        text = "🧭 **PM agent** — " + " · ".join(head) + ("\n\n" + a["comment"] if a["comment"] else "")
        text += "\n\n_The PM agent (product-manager skill, read-only) moves to Ready only work the approved PRD covers. See [AGENTIC.md](../blob/main/AGENTIC.md)._"
    open(sys.argv[4] + "-" + str(k) + ".md", "w").write(text)
    print(k, a["issue"], c["item"], c["id"], e["node"] if e and c.get("parent") != e["number"] else "-",
          a.get("priority") or "-", a.get("move") or "-", "1" if a.get("refused") else "0")' \
  "$run/actions.json" "$run/board.json" "$run/epics.json" "$run/comment" > "$run/actions.txt"
moved=0
while read -r k num item node epic_node prio move refused; do
  if [[ "$epic_node" != "-" ]]; then
    gh api graphql -f query='mutation($p: ID!, $c: ID!) { addSubIssue(input: {issueId: $p, subIssueId: $c, replaceParent: true}) { issue { number } } }' \
      -f p="$epic_node" -f c="$node" >/dev/null && log "pm: #$num → its epic" || log "pm: could not link #$num to its epic"
  fi
  if [[ "$prio" != "-" ]]; then set_priority "$item" "$prio" && log "pm: #$num priority $prio" || true; fi
  if [[ "$move" == "Ready" ]]; then
    why="$(waiting_reason "$num")"
    if [[ "$why" == human:* ]]; then
      move="-"; gh issue edit "$num" -R "$slug" --add-label agent:needs-human >/dev/null || true
      printf '\n\nStays in **Triage**: %s.' "${why#human: }" >> "$run/comment-$k.md"
    elif [[ -n "$why" ]]; then
      move="Blocked"; gh issue edit "$num" -R "$slug" --add-label pm:waiting >/dev/null || true
      printf '\n\nParked in **Blocked** (`pm:waiting`): it %s. The PM agent moves it to Ready when that clears.' "$why" >> "$run/comment-$k.md"
    fi
  fi
  if [[ "$move" != "-" ]]; then
    set_status "$item" "$move" || { log "pm: could not move #$num to $move"; continue; }
    log "pm: #$num → $move"
    moved=$((moved + 1))
    state_set 'r = set(s.get("pm_ready", [])); n = int(sys.argv[2])
(r.add if sys.argv[3] == "Ready" else r.discard)(n)
s["pm_ready"] = sorted(r)' "$num" "$move"
  fi
  if [[ "$refused" == "1" ]]; then gh issue edit "$num" -R "$slug" --add-label agent:needs-human >/dev/null || true; fi
  if [[ -s "$run/comment-$k.md" ]]; then gh issue comment "$num" -R "$slug" --body-file "$run/comment-$k.md" >/dev/null || log "pm: could not comment #$num"; fi
done < "$run/actions.txt"

# Decided: a Triage card is asked again only when someone changes it after now.
state_set 'import datetime
now = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
seen = s.setdefault("seen", {})
for n in json.load(open(sys.argv[2]))["triage"]: seen[str(n)] = now' "$run/pending.json"

summary="$(grep -m1 '^SUMMARY:' "$run/report.md" | sed 's/^SUMMARY: *//' || true)"
log "pm: ${summary:-done} ($moved moved) — report $run/report.md"
pm_note triaged "triaged #${todo// / #}, $moved moved"
notify "PM agent" "${summary:-triaged $todo}"
}
triage || log "pm: triage step failed, continuing with the board passes"

# ── 4. Release cards parked on a dependency or a date ──────────────────────
board_json > "$run/board.json"
for num in $(py 'import json,sys
for c in json.load(open(sys.argv[1])):
    if c["status"] == "Blocked" and c["state"] == "OPEN" and "pm:waiting" in c["labels"]: print(c["number"])' "$run/board.json"); do
  why="$(waiting_reason "$num")"
  item="$(py 'import json,sys; print(next(c["item"] for c in json.load(open(sys.argv[1])) if c["number"] == int(sys.argv[2])))' "$run/board.json" "$num")"
  if [[ -z "$why" ]]; then
    set_status "$item" "Ready" && gh issue edit "$num" -R "$slug" --remove-label pm:waiting >/dev/null &&
      gh issue comment "$num" -R "$slug" --body "🧭 **PM agent** — what this card waited for is done. → **Ready**. <!-- agent:pm -->" >/dev/null &&
      log "pm: #$num released → Ready"
    state_set 'r = set(s.get("pm_ready", [])); r.add(int(sys.argv[2])); s["pm_ready"] = sorted(r)' "$num"
  elif [[ "$why" == human:* ]]; then
    set_status "$item" "Triage" && gh issue edit "$num" -R "$slug" --remove-label pm:waiting --add-label agent:needs-human >/dev/null &&
      gh issue comment "$num" -R "$slug" --body "🧭 **PM agent** — ${why#human: }, so this card may no longer make sense. → **Triage** for a person: keep it (remove the dependency line) or close it. <!-- agent:pm -->" >/dev/null &&
      log "pm: #$num → Triage (${why#human: })"
  fi
done

# ── 5. Epic labels and [E#] title prefixes ──────────────────────────────────
board_json > "$run/board.json"
python3 "$BOARD_PY" tags "$run/board.json" "$run/epics.json" "$run/tags.json"
py 'import json,sys
for a in json.load(open(sys.argv[1])):
    print(a["issue"], ",".join(a["add"]) or "-", ",".join(a["remove"]) or "-", a.get("title", ""), sep="\t")' "$run/tags.json" |
while IFS=$'\t' read -r num add remove title; do
  args=()
  [[ "$add" != "-" ]] && args+=(--add-label "$add")
  [[ "$remove" != "-" ]] && args+=(--remove-label "$remove")
  [[ -n "$title" ]] && args+=(--title "$title")
  gh issue edit "$num" -R "$slug" "${args[@]}" >/dev/null && log "pm: #$num tagged with its epic" || log "pm: could not tag #$num"
done

# ── 6. Plan the next stories of epics running dry ───────────────────────────
now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
due="$(python3 "$BOARD_PY" plan "$run/board.json" "$run/epics.json" "$STATE" "$now")"
if [[ -n "$due" && "$(parallel_max 1)" -gt 0 ]]; then
  log "pm: planning the next stories of $due"
  run_begin pm "" "" "$run" # a no-op when triage already opened the run
  echo "$due" > "$run/plan_epics.txt"
  PROMPT="Use the product-manager skill and run its \"Epic planning (agent mode)\" procedure for the epics in $run/plan_epics.txt.
Inputs are in $run/: PRD.md (the approved PRD), epics.json (epics with issue numbers, rule areas, prototype approval)
and board.json (every card with its column, priority, labels and parent epic). You may read CLAUDE.md, AGENTIC.md,
OptionStrategy.md, README.md, docs/, frontend/ and the code to write buildable stories.
Print only the report in the exact output shape the skill defines, ending with the PM-STORIES block."
  started="$(date +%s)"
  if claude -p "$PROMPT" ${model_args[@]+"${model_args[@]}"} --permission-mode dontAsk \
      --allowedTools "Read" "Grep" "Glob" "Skill" \
      --disallowedTools "Bash" "Edit" "Write" "NotebookEdit" "WebFetch" "WebSearch" "Agent" \
      --strict-mcp-config --no-session-persistence --output-format text < /dev/null > "$run/plan.md"; then
    py 'import json,re,sys
t = open(sys.argv[1]).read()
m = re.search(r"<!-- PM-STORIES\s*(\{.*?\})\s*-->", t, re.S)
json.dump(json.loads(m.group(1)) if m else {}, open(sys.argv[2], "w"), indent=1)' "$run/plan.md" "$run/plan.json"
    python3 "$BOARD_PY" stories "$run/plan.json" "$run/epics.json" "$run/stories.json"
    n_new="$(py 'import json,sys; print(len(json.load(open(sys.argv[1]))))' "$run/stories.json")"
    k=0 filed=""
    while [[ $k -lt $n_new ]]; do
      py 'import json,sys
s = json.load(open(sys.argv[1]))[int(sys.argv[2])]
body = s["body"]
if s.get("refused"): body += "\n\n**Waits for a person:** " + s["refused"] + "."
body += "\n\n_Filed by the **PM agent** while planning epic " + s["epic"] + " (product-manager skill, \"Epic planning\")._\n<!-- agent:pm-filed -->"
open(sys.argv[3], "w").write(body)
print("\t".join([s["epic"], s["title"], s["priority"], s["status"], ",".join(s["labels"])]))' "$run/stories.json" "$k" "$run/story-$k.md" > "$run/story-$k.tsv"
      IFS=$'\t' read -r eid title prio status labels < "$run/story-$k.tsv"
      largs=(); IFS=',' read -r -a ls <<< "$labels"; for l in "${ls[@]}"; do largs+=(--label "$l"); done
      if url="$(gh issue create -R "$slug" --title "$title" --body-file "$run/story-$k.md" "${largs[@]}")"; then
        num="${url##*/}"
        sitem="$(add_to_board "$url" "$status")" && set_priority "$sitem" "$prio" || true
        enode="$(py 'import json,sys; print(next(e["node"] for e in json.load(open(sys.argv[1])) if e["id"] == sys.argv[2]))' "$run/epics.json" "$eid")"
        gh api graphql -f query='mutation($p: ID!, $c: ID!) { addSubIssue(input: {issueId: $p, subIssueId: $c, replaceParent: true}) { issue { number } } }' \
          -f p="$enode" -f c="$(gh issue view "$num" -R "$slug" --json id -q .id)" >/dev/null || log "pm: could not link #$num to $eid"
        [[ "$status" == "Ready" ]] && state_set 'r = set(s.get("pm_ready", [])); r.add(int(sys.argv[2])); s["pm_ready"] = sorted(r)' "$num"
        log "pm: filed #$num in $eid → $status ($prio)"
        filed="$filed #$num"
      fi
      k=$((k + 1))
    done
    pm_note stories_filed "planned $due, filed${filed:- none}"
    state_set 'pl = s.setdefault("planned", {})
for e in sys.argv[2].split(): pl[e] = sys.argv[3]' "$due" "$now"
  else
    if note_stop 1 $(( $(date +%s) - started )) "$run/plan.md"; then pm_note skipped_limit "planning: limit"
    else pm_note failed "planning: claude failed"; fi
    log "pm: planning failed (see $run/plan.md)"
  fi
fi

# ── 7. Board order: Ready by priority; Triage free cards first ─────────────
board_json > "$run/board.json"
python3 "$BOARD_PY" order "$run/board.json" "$STATE" "$run/order.json"
for col in $(py 'import json,sys; print(" ".join(json.load(open(sys.argv[1]))))' "$run/order.json"); do
  prev=""
  ok=1
  for it in $(py 'import json,sys; print(" ".join(json.load(open(sys.argv[1]))[sys.argv[2]]))' "$run/order.json" "$col"); do
    set_position "$it" "$prev" || ok=0
    prev="$it"
  done
  if [[ $ok -eq 1 ]]; then
    state_set 's.setdefault("order", {})[sys.argv[2]] = json.load(open(sys.argv[3]))[sys.argv[2]]' "$col" "$run/order.json"
    log "pm: ordered $col"
  fi
done
run_end "${pm_outcome:-done}" "$pm_detail" # a no-op when Claude was not asked
