#!/usr/bin/env python3
"""sme_issues.py — what the SME agent knows about existing issues, and where each finding goes.

    sme_issues.py shape  <raw.json> <board.txt> <out.json> [closed]
    sme_issues.py route  <findings.json> <index> <open.json> <closed.json>

`shape` turns `gh issue list --json number,title,labels,body,comments[,closedAt]`
into what the agent reads: every issue (any label — tickets a human filed count
too), its board column, a body excerpt and its last comments, so a finding is
compared with the problem, not only the title. Closed issues keep their last
comment, which often names the follow-up ticket.

`route` prints where finding <index> goes, one line:
    comment <n>   an existing issue, with new data to add
    quiet <n>     an existing issue, nothing new: no comment (tickets in Triage
                  wait days for data; a daily repost is noise)
    guard <n>     the agent called it new, but its title is near-identical to
                  issue <n>: a comment there, never a duplicate issue
    new           no existing issue describes it
An existing issue is the agent's `existing` number, else an open (then recently
closed) issue with the same fingerprint, else (guard) an open or recently closed
issue with a near-identical title. 2026-10-16: #106 re-filed #86, closed at
collection, under its own fingerprint; #107 re-filed #103 under another title —
only the agent's own matching by problem (the skill) catches that one.

Standard library only: launchd runs it with the system python3.
"""
import difflib
import json
import re
import sys

BODY_CHARS = 2500
COMMENT_CHARS = 600
LAST_COMMENTS = 3
TITLE_GUARD = 0.8  # difflib ratio of normalised titles that counts as the same issue


def fingerprint(body):
    m = re.search(r"sme-fingerprint: ([a-z0-9-]+)", body or "")
    return m.group(1) if m else ""


def shape(raw_path, board_path, out_path, closed=""):
    status = {}
    try:
        for line in open(board_path):
            parts = line.split(maxsplit=2)
            if len(parts) == 3:
                status[parts[1]] = parts[2].strip()
    except FileNotFoundError:
        pass
    out = []
    for i in json.load(open(raw_path)):
        comments = i.get("comments") or []
        row = {
            "number": i["number"],
            "title": i["title"],
            "labels": [label["name"] for label in i.get("labels") or []],
            "column": status.get(str(i["number"]), ""),
            "fingerprint": fingerprint(i.get("body")),
            "body": (i.get("body") or "")[:BODY_CHARS],
            "last_comments": [
                {"at": c["createdAt"][:16], "body": c["body"][:COMMENT_CHARS]} for c in comments[-LAST_COMMENTS:]
            ],
        }
        if closed:
            row["closedAt"] = i.get("closedAt", "")
        out.append(row)
    json.dump(out, open(out_path, "w"), indent=1)


def norm(title):
    """Title without numbers, units and punctuation: '~2% IM' and 'about 2 % IM' compare equal."""
    t = re.sub(r"[\d.,~%×x]+", " ", title.lower())
    t = re.sub(r"\b(about|around|approx|the|a|an|of|on|in)\b", " ", t)
    return " ".join(re.sub(r"[^a-z ]", " ", t).split())


def similar(a, b):
    return difflib.SequenceMatcher(None, norm(a), norm(b)).ratio()


def route(findings_path, index, open_path, closed_path):
    f = json.load(open(findings_path))["issues"][int(index)]
    opened = json.load(open(open_path))
    closed = json.load(open(closed_path))
    known = {o["number"] for o in opened + closed}
    target = f.get("existing")
    if not (isinstance(target, int) and target in known):
        # Open first; a closed issue with the same fingerprint is the same
        # problem too (its verification reopens it) — 2026-10-16: #86 was
        # reopened as not fixed and re-filed as #106 in the same run.
        target = next((o["number"] for o in opened + closed if f.get("fingerprint") and o["fingerprint"] == f["fingerprint"]), None)
    if target is None:
        best = max(opened + closed, key=lambda o: similar(f["title"], o["title"]), default=None)
        if best and similar(f["title"], best["title"]) >= TITLE_GUARD:
            print(f"guard {best['number']}")
        else:
            print("new")
    elif (f.get("update") or "").strip():
        print(f"comment {target}")
    else:
        print(f"quiet {target}")


if __name__ == "__main__":
    {"shape": shape, "route": route}[sys.argv[1]](*sys.argv[2:])
