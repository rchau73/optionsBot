#!/usr/bin/env python3
"""pr_gate.py — decides, for each open Dev agent pull request, whether a person
may pick it up (product owner, 2026-10-19: "I'd rather pick only ready tickets
for review"). Pure rules here; pr_gate.sh reads GitHub and acts.

    pr_gate.py decide <pr.json> <markers.txt> <open_deps.txt>   → one line: ACTION reason

pr.json: gh pr view --json number,state,isDraft,labels,headRefOid,mergeable,
mergeStateStatus,statusCheckRollup. markers.txt: the Reviewer's markers on the
PR, one "sha=<sha> verdict=<v>" per line. open_deps.txt: issue numbers the PR's
ticket still waits for, one per line.

Actions:
  conflict  — conflicts with main: the card goes back to Ready (needs-rebase)
              and the Dev agent merges main and resolves.
  update    — behind main without conflict: the script updates the branch.
  ready     — reviewed clean on the current head, CI green, GitHub CLEAN, no
              open dependency: label ready-to-merge.
  wait      — anything else (reason given): not ready, no action but the
              label comes off.

Standard library only.
"""
import json
import sys

sys.path.insert(0, __import__("os").path.dirname(__file__))
from pm_board import checks_refusal  # noqa: E402


def decide(pr, markers, open_deps):
    labels = {l["name"] for l in pr.get("labels", [])}
    if pr.get("state") != "OPEN" or pr.get("isDraft"):
        return "wait", "not an open, ready pull request"
    if pr.get("mergeable") == "CONFLICTING" or pr.get("mergeStateStatus") == "DIRTY":
        return "conflict", "conflicts with main"
    if open_deps:
        return "wait", "its ticket waits for " + ", ".join(f"#{n}" for n in open_deps)
    if pr.get("mergeStateStatus") == "BEHIND":
        return "update", "behind main"
    head = pr.get("headRefOid", "")
    verdict = None
    for m in markers:
        parts = dict(p.split("=", 1) for p in m.split() if "=" in p)
        if parts.get("sha") == head:
            verdict = parts.get("verdict")
    if verdict != "clean":
        return "wait", "not yet reviewed clean on its current commit" if verdict is None else f"review verdict {verdict}"
    if "agent:needs-human" in labels:
        return "wait", "labelled agent:needs-human"
    why = checks_refusal(pr.get("statusCheckRollup"))
    if why:
        return "wait", why
    if pr.get("mergeStateStatus") not in ("CLEAN", "HAS_HOOKS"):
        return "wait", f"GitHub reports {pr.get('mergeStateStatus')}"
    return "ready", "reviewed clean on its current commit, CI green, no conflict"


def main(argv):
    if argv[1] != "decide":
        raise SystemExit(f"unknown command {argv[1]}")
    pr = json.load(open(argv[2]))
    markers = [l.strip() for l in open(argv[3]) if l.strip()]
    deps = [int(l) for l in open(argv[4]) if l.strip()]
    action, why = decide(pr, markers, deps)
    print(action, why)


if __name__ == "__main__":
    main(sys.argv)
