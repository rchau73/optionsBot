#!/usr/bin/env python3
"""metrics.py — the weekly report of the agentic loop (#180): throughput and
quality, so E6's outcome ("no human toil except approvals") is measured.

    metrics.py week [<YYYY-MM-DD>]   → the ISO week before that day (today) and its Monday: 2026-W42 2026-10-12
    metrics.py report <week> <dir>   → the report (Markdown) on stdout

<dir> holds what metrics.sh fetched (offline, so tests run on fixtures):
  issues.json  gh issue list --state all --json number,title,labels,createdAt,closedAt,state,comments
  prs.json     gh pr list --state all --json number,title,headRefName,labels,createdAt,mergedAt,closedAt,state,reviews
  events.json  issue label events: a JSON array or one object per line,
               {"event": "labeled"|"unlabeled", "label": name, "created_at", "issue": number}
  gitlog.txt   git log origin/main --format='%cI%x09%s'

Every stage is read from what the agents leave on GitHub (a board move leaves
no trace on an issue): SME filed = an agent:sme issue created; PM triaged =
the issue's first 🧭 PM agent comment; Ready = a PM comment "→ **Ready**";
In progress = a Dev agent pickup comment; PR opened/merged = the agent/issue-N
pull request; deployed = the 🚀 mvp deploy comment; SME verified = its
"SME verification (date): fixed|not_fixed" comment. A card a person moved to
Ready has no PM comment: its Ready time is the first pickup. Waits for a person
are the time an issue carried agent:needs-human or prototype:review.

Standard library only: launchd runs it with the system python3.
"""
import datetime as dt
import json
import math
import os
import re
import statistics
import sys

PM = "🧭 **PM agent**"
PM_READY = "→ **Ready**"
PICKUP = "**Dev agent** picked this up"
BLOCKED = "Dev agent stopped"
DEPLOYED = "🚀 Deployed to **mvp**"
VERIFIED = re.compile(r"\*\*SME verification \([^)]*\): (\w+)\*\*")
REVIEW = re.compile(r"<!-- agent-review sha=[0-9a-f]* verdict=([a-z]+) -->")
BRANCH = re.compile(r"^agent/issue-(\d+)$")
WAIT_LABELS = ("agent:needs-human", "prototype:review")


def ts(s):
    """GitHub/git timestamp → aware UTC datetime."""
    return dt.datetime.fromisoformat(s.replace("Z", "+00:00")).astimezone(dt.timezone.utc)


def week_bounds(week):
    """'2026-W42' → [Monday 00:00 UTC, next Monday 00:00 UTC)."""
    y, w = week.split("-W")
    start = dt.datetime.fromisocalendar(int(y), int(w), 1).replace(tzinfo=dt.timezone.utc)
    return start, start + dt.timedelta(days=7)


def previous_week(day):
    """The ISO week before the one that holds day (a date)."""
    y, w, _ = (day - dt.timedelta(days=7)).isocalendar()
    return f"{y}-W{w:02d}"


def median(xs):
    return statistics.median(xs) if xs else None


def p90(xs):
    """Nearest rank: the smallest value with at least 90 % of the values at or under it."""
    if not xs:
        return None
    s = sorted(xs)
    return s[math.ceil(0.9 * len(s) - 1e-9) - 1]


def hours(a, b):
    return (b - a).total_seconds() / 3600


def first_line(body, skip):
    for line in body.splitlines():
        line = line.strip().lstrip(">").strip()
        if line and skip not in line and not line.startswith("_"):
            return line[:120].rstrip(" .:")
    return ""


def load_events(path):
    text = open(path).read().strip()
    if not text:
        return []
    if text.startswith("["):
        return json.loads(text)
    return [json.loads(l) for l in text.splitlines() if l.strip()]


def label_waits(events, end):
    """Per (issue, label) the intervals it carried a wait label: [(issue, label, start, stop|None)].
    stop is None while still labelled at `end`."""
    out, open_ = [], {}
    for e in sorted(events, key=lambda e: e["created_at"]):
        lab = e.get("label")
        if lab not in WAIT_LABELS:
            continue
        at = ts(e["created_at"])
        if at >= end:
            continue
        key = (e["issue"], lab)
        if e["event"] == "labeled" and key not in open_:
            open_[key] = at
        elif e["event"] == "unlabeled" and key in open_:
            out.append((key[0], lab, open_.pop(key), at))
    out += [(k[0], k[1], s, None) for k, s in open_.items()]
    return out


def stages(issues, prs):
    """Per issue number, the times of each stage (lists, oldest first)."""
    by = {}
    for i in issues:
        st = {"created": ts(i["createdAt"]), "sme": "agent:sme" in {l["name"] for l in i.get("labels", [])},
              "triaged": [], "ready": [], "pickup": [], "deployed": [], "verified": [], "blocks": [],
              "prs": []}
        for c in i.get("comments", []):
            at, body = ts(c["createdAt"]), c.get("body") or ""
            if body.startswith(PM):
                st["triaged"].append(at)
                if PM_READY in body.split("\n", 1)[0]:
                    st["ready"].append(at)
            if PICKUP in body:
                st["pickup"].append(at)
            if BLOCKED in body:
                st["blocks"].append((at, first_line(body, BLOCKED)))
            if body.startswith(DEPLOYED):
                st["deployed"].append(at)
            m = VERIFIED.search(body)
            if m:
                st["verified"].append((at, m.group(1)))
        by[i["number"]] = st
    for p in prs:
        m = BRANCH.match(p.get("headRefName") or "")
        if m and int(m.group(1)) in by:
            by[int(m.group(1))]["prs"].append(p)
    return by


def report(week, d):
    start, end = week_bounds(week)
    inside = lambda t: t is not None and start <= t < end  # noqa: E731
    issues = json.load(open(os.path.join(d, "issues.json")))
    prs = json.load(open(os.path.join(d, "prs.json")))
    events = load_events(os.path.join(d, "events.json"))
    gitlog = [l.split("\t", 1) for l in open(os.path.join(d, "gitlog.txt")).read().splitlines() if "\t" in l]
    by = stages(issues, prs)

    count = {k: 0 for k in ("sme", "triaged", "ready", "pickup", "pr", "merged", "deployed", "fixed", "reopened")}
    ready_pr, pr_merge, merge_deploy, blocks = [], [], [], []
    for n, st in by.items():
        count["sme"] += st["sme"] and inside(st["created"])
        count["triaged"] += bool(st["triaged"]) and inside(st["triaged"][0])
        count["ready"] += sum(inside(t) for t in st["ready"])
        count["pickup"] += sum(inside(t) for t in st["pickup"])
        count["deployed"] += sum(inside(t) for t in st["deployed"])
        count["fixed"] += sum(inside(t) and v == "fixed" for t, v in st["verified"])
        count["reopened"] += sum(inside(t) and v == "not_fixed" for t, v in st["verified"])
        blocks += [(n, t, why) for t, why in st["blocks"] if inside(t)]
        for p in st["prs"]:
            opened, merged = ts(p["createdAt"]), ts(p["mergedAt"]) if p.get("mergedAt") else None
            readies = [t for t in st["ready"] + st["pickup"] if t <= opened]
            if inside(opened) and readies:
                ready_pr.append(hours(min(readies), opened))
            if inside(merged):
                pr_merge.append(hours(opened, merged))
            dep = [t for t in st["deployed"] if merged and t >= merged]
            if dep and inside(dep[0]):
                merge_deploy.append(hours(merged, dep[0]))
    count["pr"] = sum(inside(ts(p["createdAt"])) for p in prs if BRANCH.match(p.get("headRefName") or ""))
    count["merged"] = sum(inside(ts(p["mergedAt"])) for p in prs if p.get("mergedAt") and BRANCH.match(p.get("headRefName") or ""))

    rounds, unmerged = [], []
    for p in prs:
        if not BRANCH.match(p.get("headRefName") or ""):
            continue
        done = ts(p["mergedAt"]) if p.get("mergedAt") else ts(p["closedAt"]) if p.get("closedAt") else None
        if not inside(done):
            continue
        verdicts = [m for r in p.get("reviews", []) for m in REVIEW.findall(r.get("body") or "")]
        rounds.append((p["number"], len(verdicts), sum(v == "blocking" for v in verdicts)))
        if not p.get("mergedAt"):
            unmerged.append(p["number"])
    reverts = [s for t, s in gitlog if inside(ts(t)) and s.startswith("Revert ")]

    waits_done, waits_open, wait_hours = [], [], {l: 0.0 for l in WAIT_LABELS}
    for n, lab, s, e in label_waits(events, end):
        stop = e or end
        if stop <= start:
            continue
        wait_hours[lab] += hours(max(s, start), stop)
        if e is None:
            waits_open.append((n, lab, hours(s, end)))
        elif inside(e):
            waits_done.append(hours(s, e))

    f = lambda x: "–" if x is None else f"{x:.1f} h"  # noqa: E731
    out = [f"# Agent metrics {week} ({start:%Y-%m-%d} → {end - dt.timedelta(days=1):%Y-%m-%d})", "",
           "## Stages (events in the week)", "",
           "| SME filed | PM triaged | → Ready | Dev picked up | PR opened | merged | deployed (mvp) | SME verified fixed | SME reopened |",
           "|---|---|---|---|---|---|---|---|---|",
           "| " + " | ".join(str(count[k]) for k in ("sme", "triaged", "ready", "pickup", "pr", "merged", "deployed", "fixed", "reopened")) + " |",
           "", "## Cycle time (ended in the week)", "",
           "| | n | median | p90 |", "|---|---|---|---|"]
    for name, xs in (("Ready → PR", ready_pr), ("PR → merge", pr_merge), ("merge → deploy", merge_deploy)):
        out.append(f"| {name} | {len(xs)} | {f(median(xs))} | {f(p90(xs))} |")
    if rounds:
        rs = [r for _, r, _ in rounds]
        rounds_line = (f"- Reviewer rounds per PR (PRs merged or closed in the week: {len(rounds)}): "
                       f"median {median(rs):g}, max {max(rs)}; "
                       + ", ".join(f"#{n} {r} ({b} blocking)" for n, r, b in rounds))
    else:
        rounds_line = "- Reviewer rounds per PR: no PR merged or closed in the week"
    out += ["", "## Quality", "", rounds_line,
            f"- PRs closed unmerged: {len(unmerged)}" + (" — " + ", ".join(f"#{n}" for n in unmerged) if unmerged else ""),
            f"- Reverts on main: {len(reverts)}" + ("".join(f"\n  - {s}" for s in reverts)),
            f"- Dev agent blocks (needs a person): {len(blocks)}" + "".join(f"\n  - #{n} {t:%m-%d %H:%M}: {why}" for n, t, why in blocks),
            "", "## Waiting for a person", "",
            f"- Hours in the week with {', '.join(f'`{l}` {wait_hours[l]:.1f} h' for l in WAIT_LABELS)}",
            f"- Waits that ended in the week: {len(waits_done)}, median {f(median(waits_done))}, p90 {f(p90(waits_done))}",
            f"- Still waiting at the week's end: {len(waits_open)}" + "".join(
                f"\n  - #{n} `{lab}` for {h:.1f} h" for n, lab, h in sorted(waits_open, key=lambda w: -w[2]))]
    return "\n".join(out) + "\n"


def main(argv):
    if len(argv) >= 2 and argv[1] == "week":
        day = dt.date.fromisoformat(argv[2]) if len(argv) > 2 else dt.datetime.now(dt.timezone.utc).date()
        week = previous_week(day)
        print(week, f"{week_bounds(week)[0]:%Y-%m-%d}")
    elif len(argv) == 4 and argv[1] == "report":
        sys.stdout.write(report(argv[2], argv[3]))
    else:
        raise SystemExit(__doc__)


if __name__ == "__main__":
    main(sys.argv)
