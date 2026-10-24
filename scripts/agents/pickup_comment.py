#!/usr/bin/env python3
"""pickup_comment.py — the Dev agent's "picked this up" comment, with history.

    pickup_comment.py <comments.json> <branch> <run log dir>

Reads the issue's comments (`gh issue view --json comments`) and prints the
pickup comment: which attempt this is and what happened since the previous
one — a block, a review sent back, or a person's comment. A card moved back to
Ready on the board leaves no trace on the issue, so without this a re-take
looked exactly like the first pickup.

Standard library only: launchd runs it with the system python3.
"""
import json
import sys

PICKUP = "**Dev agent** picked this up"
BLOCKED = "Dev agent stopped"
SENT_BACK = "Reviewer agent found"
# Agent lines that are not a reason for a new attempt.
OTHER_AGENT = ("Reviewer agent", "Dev agent opened", "Dev agent pushed", "Deployed to",
               "Dev agent hit a usage/rate limit", "Re-queued:")


def first_line(body, skip=""):
    """First non-empty line of a comment, without quote marks or a header to skip."""
    for line in body.splitlines():
        line = line.strip().lstrip(">").strip()
        if line and (not skip or skip not in line):
            return line[:160].rstrip(" .:")
    return ""


def previous_event(comments):
    """The latest event since the previous pickup, as one sentence."""
    for c in reversed(comments):
        body = c["body"]
        at = c["createdAt"][:16].replace("T", " ") + " UTC"
        if PICKUP in body:
            break
        if BLOCKED in body:
            return f"blocked at {at}: {first_line(body, skip=BLOCKED)}"
        if SENT_BACK in body:
            return f"sent back by the Reviewer agent at {at}: {first_line(body)}"
        if any(tag in body for tag in OTHER_AGENT):
            continue
        return f"a person commented at {at}: {first_line(body)}"
    return "no comment since the last attempt (the card was moved back to Ready on the board)"


def main(comments_json, branch, run_log):
    comments = json.load(open(comments_json))["comments"]
    attempt = 1 + sum(PICKUP in c["body"] for c in comments)
    out = [f"🤖 {PICKUP} on branch `{branch}` — **attempt {attempt}**."]
    if attempt > 1:
        out.append(f"Previous: {previous_event(comments)}.")
    out += ["", "It will open or update a pull request for review; it never merges or deploys. "
                f"Run log on the runner: `{run_log}`."]
    print("\n".join(out))


if __name__ == "__main__":
    main(*sys.argv[1:])
