#!/usr/bin/env python3
"""stuck.py — cards that sit In progress or In review with no agent run behind
them (#329), so a stalled loop is seen the same day.

    stuck.py due <state dir> [--now TS]
        → exit 0 when the last check is CHECK_EVERY_H or more ago (or never ran)
    stuck.py check <board.json> <agent_runs.jsonl> <state dir> [--now TS]
                   [--budget REASON] [--limit-at EPOCH]
        → one JSON line {issue, key, body} per stuck card to comment on; every
          stuck card found (explained or not) is appended to
          <state dir>/reports.jsonl (read by metrics.py), and the check's time
          to <state dir>/last_check

board.json is lib.sh board_json; the ledger is read with runs.py's rules.
A card is stuck when it is open, In progress for more than 6 h or In review for
more than 24 h since its last activity (the card's update or its newest run on
the ledger), with no open run on it and none of the labels that mean it waits
for a person or a dependency (pm:waiting, agent:needs-human, ready-to-merge).
A pause or budget in agents.env (--budget, limit_lib.sh budget_reason) or a
usage limit in the last CHECK_EVERY_H hours (--limit-at, data/agents/limit_hit)
explains the idle time: such a card is listed with that reason, not commented
on. A fault is commented on once per (card, column, last run): stuck.sh writes
<state dir>/marks/<key> after the comment. Nothing is ever moved.

Standard library only: launchd runs it with the system python3.
"""
import argparse
import datetime as dt
import json
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import runs  # noqa: E402

IDLE_LIMIT_H = {"In progress": 6, "In review": 24}
WAITING_LABELS = ("pm:waiting", "agent:needs-human", "ready-to-merge")
CHECK_EVERY_H = 6
NO_RUN = "no agent run"
KEY = re.compile(r"^[A-Za-z0-9_.-]+$")


def ts(s):
    """GitHub or ledger timestamp → aware UTC datetime, or None."""
    try:
        return dt.datetime.fromisoformat(s.replace("Z", "+00:00")).astimezone(dt.timezone.utc)
    except (AttributeError, ValueError):
        return None


def hours(a, b):
    return (b - a).total_seconds() / 3600


def last_run_text(r):
    """'dev pr_opened 2026-10-24T10:00:00Z' (an open run: 'dev open since …'), or 'none'."""
    if r is None:
        return "none"
    if r["end"]:
        return f"{r['agent']} {r['outcome'] or 'ended'} {r['end']}"
    return f"{r['agent']} open since {r['start']}"


def explain(now, budget, limit_at):
    """Why the agents could not run → (reason, fault)."""
    if budget:
        return ("paused", False) if budget.startswith("paused") else (f"budget: {budget}", False)
    if limit_at is not None and hours(limit_at, now) < CHECK_EVERY_H:
        return f"usage limit {hours(limit_at, now):.1f} h ago", False
    return NO_RUN, True


def stuck(board, all_runs, now, budget="", limit_at=None):
    """The stuck cards → [{issue, column, hours_idle, last_run, reason, fault, key}], by issue."""
    by_issue = {}
    for r in all_runs:
        if r["issue"] is not None:
            by_issue.setdefault(r["issue"], []).append(r)
    reason, fault = explain(now, budget, limit_at)
    out = []
    for c in sorted(board, key=lambda c: c["number"]):
        column = c.get("status")
        if c.get("state") != "OPEN" or column not in IDLE_LIMIT_H:
            continue
        if any(l in WAITING_LABELS for l in c.get("labels", [])):
            continue
        rs = by_issue.get(c["number"], [])
        if any(r["end"] is None for r in rs):
            continue  # a run is on it (runs whose process died are closed by the sweep first)
        last = max(rs, key=lambda r: r["end"] or r["start"] or "") if rs else None
        times = [ts(c.get("updatedAt") or "")]
        if last:
            times.append(ts(last["end"] or last["start"] or ""))
        times = [t for t in times if t]
        if not times:
            continue
        idle = hours(max(times), now)
        if idle <= IDLE_LIMIT_H[column]:
            continue
        slug = column.lower().replace(" ", "-")
        out.append({"issue": c["number"], "column": column, "hours_idle": round(idle, 1),
                    "last_run": last_run_text(last), "reason": reason, "fault": fault,
                    "key": f"{c['number']}-{slug}-{last['run_id'] if last else 'none'}",
                    "log": (last or {}).get("log")})
    return out


def comment(item):
    """The issue comment for a stuck card (🤖 header: an agent's, for pm_board.is_agent_comment)."""
    lines = [f"🤖 **Stuck card** — #{item['issue']} has sat in **{item['column']}** for "
             f"**{item['hours_idle']:.1f} h** with no agent run behind it "
             f"(reported after {IDLE_LIMIT_H[item['column']]} h).", "",
             f"- Last run on it: {item['last_run']}" + (f" — log `{item['log']}`" if item.get("log") else ""),
             "- Nothing was moved. Check the run log and the loop (`scripts/agents/runs.py --issue "
             f"{item['issue']}`), then move the card or answer here.", "",
             f"<!-- agent:stuck key={item['key']} -->"]
    return "\n".join(lines)


def due(state_dir, now):
    try:
        last = dt.datetime.fromtimestamp(int(open(os.path.join(state_dir, "last_check")).read().strip()),
                                         dt.timezone.utc)
    except (OSError, ValueError):
        return True
    return hours(last, now) >= CHECK_EVERY_H


def check(board, all_runs, state_dir, now, budget="", limit_at=None):
    """Records the check → the faults to comment on (not yet marked)."""
    items = stuck(board, all_runs, now, budget, limit_at)
    marks = os.path.join(state_dir, "marks")
    os.makedirs(marks, exist_ok=True)
    stamp = now.strftime("%Y-%m-%dT%H:%M:%SZ")
    with open(os.path.join(state_dir, "reports.jsonl"), "a", encoding="utf-8") as f:
        for it in items:
            rec = {k: it[k] for k in ("issue", "column", "hours_idle", "last_run", "reason", "fault")}
            f.write(json.dumps(dict(ts=stamp, **rec), ensure_ascii=False) + "\n")
    with open(os.path.join(state_dir, "last_check"), "w") as f:
        f.write(f"{int(now.timestamp())}\n")
    return [it for it in items if it["fault"] and KEY.match(it["key"])
            and not os.path.exists(os.path.join(marks, it["key"]))]


def main(argv):
    p = argparse.ArgumentParser(prog="stuck.py")
    sub = p.add_subparsers(dest="cmd", required=True)
    d = sub.add_parser("due")
    d.add_argument("state_dir")
    d.add_argument("--now")
    c = sub.add_parser("check")
    for name in ("board", "runs_file", "state_dir"):
        c.add_argument(name)
    c.add_argument("--now")
    c.add_argument("--budget", default="")
    c.add_argument("--limit-at", default="")
    a = p.parse_args(argv)
    now = ts(a.now) if a.now else dt.datetime.now(dt.timezone.utc).replace(microsecond=0)
    if now is None:
        p.error(f"--now: not a timestamp: {a.now!r}")
    if a.cmd == "due":
        return 0 if due(a.state_dir, now) else 1
    limit_at = None
    if a.limit_at.strip().isdigit() and int(a.limit_at) > 0:
        limit_at = dt.datetime.fromtimestamp(int(a.limit_at), dt.timezone.utc)
    board = json.load(open(a.board))
    for it in check(board, runs.runs(runs.read(a.runs_file)[0]), a.state_dir, now, a.budget.strip(), limit_at):
        print(json.dumps({"issue": it["issue"], "key": it["key"], "body": comment(it)}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
