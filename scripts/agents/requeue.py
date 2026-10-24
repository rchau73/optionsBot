#!/usr/bin/env python3
"""requeue.py — cards a usage limit blocked go back to Ready (#237).

    requeue.py candidates <board.json> <dev runs dir>
        → issue numbers worth reading the comments of (card + run log checks)
    requeue.py select <board.json> <comments.json> <dev runs dir>
        → "ITEM_ID ISSUE_NUMBER" of each card to re-queue
    requeue.py limit-noted <comments.json>
        → exit 0 when the issue's last comment (pickups aside) is already the
          Dev agent's limit comment: one limit comment per episode

board.json is lib.sh board_json; comments.json maps an issue number to its
comments (`gh issue view --json comments`), or is that object for one issue
(limit-noted). The dev runs dir is data/agents/dev, one issue-<n>-<time>/
directory per run, holding the agent's output agent.md.

A limit worded in a way LIMIT_PATTERN did not know made dev_agent.sh block the
card ("the agent did not finish") with agent:needs-human: 28 approved cards sat
in Blocked on 2026-10-19..21. A card is re-queued only when every sign says
the stop was a limit and nobody has spoken since; anything else stays with the
person. Standard library only: launchd runs it with the system python3.
"""
import json
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from pm_board import is_agent_comment  # noqa: E402

PICKUP = "**Dev agent** picked this up"
STOPPED = "Dev agent stopped"
DID_NOT_FINISH = "the agent did not finish"
LIMIT_COMMENT = "Dev agent hit a usage/rate limit"
REQUEUED = "🤖 Re-queued: the earlier stop was a usage limit, not a question."

LIMIT_LIB = os.path.join(os.path.dirname(os.path.abspath(__file__)), "limit_lib.sh")


def limit_pattern(path=LIMIT_LIB):
    """LIMIT_PATTERN as limit_lib.sh defines it (one source for shell and Python)."""
    m = re.search(r'^LIMIT_PATTERN="([^"]*)"', open(path, encoding="utf-8").read(), re.M)
    return re.compile(m.group(1), re.I)


def newest_run_output(runs_dir, number):
    """Text of the newest data/agents/dev/issue-<n>-<time>/agent.md, or None."""
    prefix = f"issue-{number}-"
    try:
        runs = sorted(d for d in os.listdir(runs_dir) if d.startswith(prefix))
    except OSError:
        return None
    for d in reversed(runs):
        path = os.path.join(runs_dir, d, "agent.md")
        if os.path.isfile(path):
            return open(path, encoding="utf-8", errors="replace").read()
    return None


def card_qualifies(card):
    """An open Blocked card labelled agent:needs-human and not waiting on a dependency."""
    return (card.get("state") == "OPEN" and card.get("status") == "Blocked"
            and "agent:needs-human" in card["labels"] and "pm:waiting" not in card["labels"])


def stopped_by_failure(comments):
    """True when the newest Dev stop is a "did not finish" one and no person spoke after it.

    A stop that carries the agent's own questions has no "did not finish" line.
    """
    for i in range(len(comments) - 1, -1, -1):
        body = comments[i]["body"]
        if STOPPED in body:
            return DID_NOT_FINISH in body and all(is_agent_comment(c["body"]) for c in comments[i + 1:])
    return False


def candidates(board, outputs, pattern):
    """Numbers of the cards whose newest run output shows a limit (comments not read yet).

    outputs: issue number → its newest agent.md text (or None).
    """
    return [c["number"] for c in board
            if card_qualifies(c) and outputs.get(c["number"]) and pattern.search(outputs[c["number"]])]


def select(board, comments, outputs, pattern):
    """(item, number) of each card to re-queue; comments: issue number → comments."""
    keep = set(candidates(board, outputs, pattern))
    return [(c["item"], c["number"]) for c in board
            if c["number"] in keep and stopped_by_failure(comments.get(c["number"], []))]


def limit_noted(comments):
    """True when the last comment, pickups aside, is the Dev agent's limit comment."""
    for c in reversed(comments):
        if PICKUP in c["body"]:
            continue
        return LIMIT_COMMENT in c["body"]
    return False


def _outputs(board, runs_dir):
    return {c["number"]: newest_run_output(runs_dir, c["number"]) for c in board if card_qualifies(c)}


def main(argv):
    cmd = argv[1]
    if cmd == "candidates":
        board = json.load(open(argv[2]))
        for n in candidates(board, _outputs(board, argv[3]), limit_pattern()):
            print(n)
    elif cmd == "select":
        board = json.load(open(argv[2]))
        comments = {int(k): v for k, v in json.load(open(argv[3])).items()}
        for item, n in select(board, comments, _outputs(board, argv[4]), limit_pattern()):
            print(item, n)
    elif cmd == "limit-noted":
        data = json.load(open(argv[2]))
        sys.exit(0 if limit_noted(data.get("comments", [])) else 1)
    else:
        sys.exit(f"unknown command {cmd}")


if __name__ == "__main__":
    main(sys.argv)
