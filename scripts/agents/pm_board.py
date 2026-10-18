#!/usr/bin/env python3
"""pm_board.py — the PM agent's rules for the board, kept apart from GitHub calls so they can be tested.

    pm_board.py epics   <PRD.md> <out.json>
    pm_board.py pending <board.json> <state.json> <epics.json> <out.json>
    pm_board.py gate    <pm.json> <board.json> <epics.json> <state.json> <out.json>
    pm_board.py answered <tickets.json> <state.json>

`epics` reads the approved PRD (docs/product/PRD.md on main — a person merged it,
so every epic there is approved by that merge) into a list of epics. Each epic is
a `### <id> · <name>` section with a machine-readable marker right under the heading:

    <!-- epic: {"id": "E1", "status": "approved", "rank": 1, "rule_areas": ["exits"], "ui": false} -->

status: approved (stories may go to Ready) · parked (kept, nothing goes to Ready) · done (closed).
rule_areas: the trading-rule areas a story of this epic may change (RULE_AREAS); a story
that changes another area stays in Triage for a person. ui: the epic's UI stories need a
prototype a person approved (label prototype:approved on the epic issue) before Ready.

`pending` lists what the agent has to decide this run: Triage cards that are new or
changed since its last decision, and cards with no epic in any open column.

`answered` prints the `agent:pm` ticket whose person's message the agent must act on
next (pm_answer.sh): the newest message from a person, on the ticket or its PRD pull
request, is newer than the PM agent's last comment and than its last run on it.

`gate` turns the agent's decisions into actions, refusing (and saying why) everything
the PRD has not approved. The agent proposes, this file decides what is allowed, the
shell script acts. Only Triage cards move to Ready; only cards the PM itself put in
Ready go back to Triage; In progress / In review / Blocked / Done are never moved.

Standard library only: launchd runs it with the system python3.
"""
import json
import re
import sys

# Every comment comes from the same GitHub account, so an agent's comment is told
# apart by its header (🧭 PM, 🤖 Dev/Reviewer, 🚀 deploy, **SME …) or a marker.
AGENT_COMMENT = re.compile(r"^\s*(🧭|🤖|🚀|🔎|🩺|\*\*SME\b)|<!--\s*agent:", re.S)
PM_COMMENT = re.compile(r"^\s*🧭|<!--\s*agent:pm\s*-->", re.S)

RULE_AREAS = {
    "entry", "exits", "repair", "rebalance", "margin", "gex", "sizing",
    "orders", "reconcile", "killswitch", "gateway", "marketdata",
}
PRIORITIES = ("P0", "P1", "P2", "P3")
EPIC_STATUSES = ("approved", "parked", "done")
OPEN_COLUMNS = ("Triage", "Ready", "In progress", "In review", "Blocked")

MARKER = re.compile(r"<!--\s*epic:\s*(\{.*?\})\s*-->", re.S)
HEADING = re.compile(r"^###\s+(.+?)\s*$", re.M)


def parse_epics(text):
    """Epics of a PRD, in document order. A malformed marker raises: a PRD a person
    approved must not lose an epic silently."""
    out, seen = [], set()
    heads = list(HEADING.finditer(text))
    for n, h in enumerate(heads):
        end = heads[n + 1].start() if n + 1 < len(heads) else len(text)
        nxt = re.search(r"^##\s", text[h.end():end], re.M)  # a level-2 heading ends the section
        section = text[h.end(): h.end() + nxt.start()] if nxt else text[h.end():end]
        m = MARKER.search(section)
        if not m:
            continue
        meta = json.loads(m.group(1))
        eid = str(meta.get("id", "")).strip()
        if not re.fullmatch(r"E\d+", eid):
            raise ValueError(f"epic id must look like E1, got {eid!r} in '{h.group(1)}'")
        if eid in seen:
            raise ValueError(f"epic {eid} appears twice")
        seen.add(eid)
        status = meta.get("status", "approved")
        if status not in EPIC_STATUSES:
            raise ValueError(f"epic {eid}: status must be one of {EPIC_STATUSES}, got {status!r}")
        areas = meta.get("rule_areas", [])
        bad = [a for a in areas if a not in RULE_AREAS]
        if bad:
            raise ValueError(f"epic {eid}: unknown rule_areas {bad}; known: {sorted(RULE_AREAS)}")
        name = re.sub(r"^E\d+\s*[·:\-—]\s*", "", h.group(1)).strip()
        body = MARKER.sub("", section).strip()
        out.append({
            "id": eid, "name": name, "status": status, "rank": int(meta.get("rank", 99)),
            "rule_areas": areas, "ui": bool(meta.get("ui", False)), "body": body,
        })
    return out


def pending(board, state, epics):
    """Cards the agent decides on this run."""
    seen = state.get("seen", {})
    epic_numbers = {e.get("number") for e in epics if e.get("number")}
    triage, unparented = [], []
    for c in board:
        if "epic" in c["labels"] or c["number"] in epic_numbers or c["state"] != "OPEN":
            continue
        if c["status"] == "Triage":
            if "agent:pm" in c["labels"] and c.get("parent"):
                continue  # placed already: a person's answer goes to pm_answer.sh
            # Decided already and unchanged (e.g. waiting for a person): not asked again.
            if c["updatedAt"] > seen.get(str(c["number"]), ""):
                triage.append(c["number"])
        elif c["status"] in OPEN_COLUMNS and not c.get("parent"):
            unparented.append(c["number"])
    return {"triage": sorted(triage), "unparented": sorted(unparented)}


def gate(pm, board, epics, state):
    """Allowed actions for the agent's output. Each action:
    {issue, epic?, priority?, move?, comment, refused?}. `refused` explains why a
    proposed move did not happen; the comment tells the issue so."""
    cards = {c["number"]: c for c in board}
    by_id = {e["id"]: e for e in epics}
    pm_ready = set(state.get("pm_ready", []))
    actions = []

    for d in pm.get("decisions", []):
        n = d.get("issue")
        c = cards.get(n)
        if c is None or "epic" in c["labels"] or c["state"] != "OPEN":
            continue
        a = {"issue": n, "comment": (d.get("comment") or "").strip()}
        epic = by_id.get(d.get("epic") or "")
        if epic and epic.get("number") and epic["status"] != "done":
            a["epic"] = epic["id"]
        if d.get("priority") in PRIORITIES and c["status"] in ("Triage", "Ready"):
            a["priority"] = d["priority"]  # cards in flight keep their order
        if c["status"] == "Triage" and d.get("action") == "ready":
            why = ready_refusal(d, c, epic)
            if not why and "agent:pm" in c["labels"]:
                why = "an agent:pm ticket is the PM agent's own work (a PRD change or a decision), not the Dev agent's"
            if why:
                a["refused"] = why
            else:
                a["move"] = "Ready"
        actions.append(a)

    for u in pm.get("unready", []):
        n = u.get("issue")
        c = cards.get(n)
        if c and c["status"] == "Ready" and n in pm_ready:
            actions.append({"issue": n, "move": "Triage", "comment": (u.get("comment") or "").strip()})

    for p in pm.get("priorities", []):
        c = cards.get(p.get("issue"))
        if c and c["status"] in ("Triage", "Ready") and p.get("priority") in PRIORITIES and p["priority"] != c.get("priority"):
            if not any(a["issue"] == c["number"] for a in actions):
                actions.append({"issue": c["number"], "priority": p["priority"], "comment": ""})
    return actions


def ready_refusal(d, card, epic):
    """Why a Triage card may not go to Ready, or "" when it may."""
    if epic is None:
        return "it fits no epic of the approved PRD: a person decides (new epic, or reject)"
    if epic["status"] != "approved":
        return f"epic {epic['id']} is {epic['status']} in the PRD"
    if not epic.get("number"):
        return f"epic {epic['id']} has no issue yet"
    if d.get("priority") not in PRIORITIES:
        return "no priority"
    area = d.get("rule_area")
    if area is None and "agent:sme" in card["labels"]:
        return "an SME finding must name the trading-rule area it changes (or \"none\")"
    area = area or "none"
    if area != "none" and area not in RULE_AREAS:
        return f"unknown trading-rule area '{area}'"
    if area != "none" and area not in epic["rule_areas"]:
        return f"it changes the trading rules ({area}), which epic {epic['id']} does not cover: a person decides"
    if d.get("ui") and epic["ui"] and not epic.get("prototype_approved"):
        return f"UI story: epic {epic['id']}'s prototype is not approved yet (label prototype:approved)"
    return ""


def is_agent_comment(body):
    return bool(AGENT_COMMENT.search(body or ""))


def answered(tickets, state):
    """Number of the agent:pm ticket to act on next, or None. Each ticket:
    {number, state, status, labels, created, body, messages: [{at, body}]}, the
    messages being the ticket's comments plus its open PRD pull request's comments
    and reviews. The body counts as a person's message unless an agent wrote it."""
    done = state.get("answered", {})
    due = []
    for t in tickets:
        if (t["state"] != "OPEN" or "agent:pm" not in t["labels"] or "epic" in t["labels"]
                or t["status"] in ("In progress", "Done")):
            continue
        msgs = list(t.get("messages", []))
        if t.get("body") and not is_agent_comment(t["body"]):
            msgs.append({"at": t["created"], "body": t["body"]})
        person = max((m["at"] for m in msgs if not is_agent_comment(m["body"])), default="")
        pm = max((m["at"] for m in msgs if PM_COMMENT.search(m["body"] or "")), default="")
        if person and person > pm and person > done.get(str(t["number"]), ""):
            due.append((person, t["number"]))
    return min(due)[1] if due else None


def _load(path, default):
    try:
        return json.load(open(path))
    except FileNotFoundError:
        return default


def main(argv):
    cmd = argv[1]
    if cmd == "epics":
        json.dump(parse_epics(open(argv[2]).read()), open(argv[3], "w"), indent=1)
    elif cmd == "pending":
        board, state, epics = json.load(open(argv[2])), _load(argv[3], {}), json.load(open(argv[4]))
        json.dump(pending(board, state, epics), open(argv[5], "w"), indent=1)
    elif cmd == "gate":
        pm, board, epics, state = (json.load(open(argv[2])), json.load(open(argv[3])),
                                   json.load(open(argv[4])), _load(argv[5], {}))
        json.dump(gate(pm, board, epics, state), open(argv[6], "w"), indent=1)
    elif cmd == "answered":
        n = answered(json.load(open(argv[2])), _load(argv[3], {}))
        if n is not None:
            print(n)
    else:
        raise SystemExit(f"unknown command {cmd}")


if __name__ == "__main__":
    main(sys.argv)
