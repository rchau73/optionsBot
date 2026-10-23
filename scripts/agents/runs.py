"""runs.py — the agent run ledger (#206): <envs>/history/agent_runs.jsonl.

Every SME, PM, PM-answer, Dev, Reviewer, PR-gate, deploy and backup run appends a
`start` and an `end` line (lib.sh run_begin / run_end), so "which agent did
what, when, on which card, and how did it end?" is answered from one file:

  {"ts", "agent", "run_id", "issue", "pr", "event": "start|end", "outcome",
   "detail", "log", "commit"}

Paths and ids only: no secrets, no amounts, no prompt text (the detail is a
short fixed phrase from the script, cut to DETAIL_MAX characters).

  runs.py line --agent=dev --run-id=… --event=start [--issue=… --pr=… --outcome=…
               --detail=… --log=… --commit=… --ts=…]      one JSON line (lib.sh)
  runs.py [--agent A] [--issue N] [--pr N] [--date YYYY-MM-DD] [--open]
          [--json] [--file F]                              list runs, oldest first

--open lists the runs with no `end` line: crashed (killed hard) or still running.
Tests: python3 -m unittest discover -s scripts/agents -p 'test_*.py'
"""
import argparse
import datetime
import json
import os
import sys

FIELDS = ("ts", "agent", "run_id", "issue", "pr", "event", "outcome", "detail", "log", "commit")
AGENTS = ("sme", "pm", "pm_answer", "dev", "review", "pr_gate", "deploy", "backup")
EVENTS = ("start", "end")
DETAIL_MAX = 200


def default_file():
    """<envs>/history/agent_runs.jsonl, <envs> found the way scripts/env_lib.sh does."""
    repo = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    envs = os.environ.get("OPTIONSBOT_ENVS")
    if not envs:
        parent = os.path.dirname(repo)
        envs = parent if os.path.isfile(os.path.join(repo, "deploy.env")) else os.path.join(parent, "optionsBot-env")
    return os.path.join(envs, "history", "agent_runs.jsonl")


def _number(value, name):
    """'' or None → None; '#123' or '123' → 123; anything else is refused."""
    if value is None or str(value).strip() == "":
        return None
    text = str(value).strip().lstrip("#")
    if not text.isdigit():
        raise ValueError(f"{name} must be a number, got {value!r}")
    return int(text)


def make_line(agent, run_id, event, issue=None, pr=None, outcome="", detail="", log="", commit="", ts=None):
    """One ledger line (JSON, no newline). Raises ValueError on a bad field."""
    if agent not in AGENTS:
        raise ValueError(f"unknown agent {agent!r} (one of {', '.join(AGENTS)})")
    if event not in EVENTS:
        raise ValueError(f"event must be start or end, got {event!r}")
    if not run_id:
        raise ValueError("run_id is required")
    if event == "end" and not outcome:
        raise ValueError("an end line needs an outcome")
    if ts is None:
        ts = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    # One short line: whitespace (newlines included) collapsed, then cut.
    detail = " ".join(str(detail or "").split())[:DETAIL_MAX]
    rec = {"ts": ts, "agent": agent, "run_id": run_id, "issue": _number(issue, "issue"), "pr": _number(pr, "pr"),
           "event": event, "outcome": outcome or None, "detail": detail or None,
           "log": log or None, "commit": commit or None}
    return json.dumps(rec, ensure_ascii=False, separators=(",", ":"))


def read(path):
    """→ (records in file order, number of lines that are not a ledger record)."""
    records, bad = [], 0
    try:
        f = open(path, encoding="utf-8")
    except FileNotFoundError:
        return [], 0
    with f:
        for line in f:
            if not line.strip():
                continue
            try:
                rec = json.loads(line)
            except ValueError:
                bad += 1
                continue
            if not isinstance(rec, dict) or not rec.get("run_id") or rec.get("event") not in EVENTS:
                bad += 1
                continue
            records.append(rec)
    return records, bad


def runs(records):
    """Pairs start and end lines by run_id → runs ordered by start time (file
    order breaks ties). A run without a start (a truncated file) starts at its end."""
    by_id, order = {}, []
    for pos, rec in enumerate(records):
        rid = rec["run_id"]
        run = by_id.get(rid)
        if run is None:
            run = {"run_id": rid, "agent": rec.get("agent"), "issue": None, "pr": None, "start": None,
                   "end": None, "outcome": None, "detail": None, "log": None, "commit": None, "_pos": pos}
            by_id[rid] = run
            order.append(run)
        for k in ("issue", "pr", "log", "commit"):
            if rec.get(k) is not None:
                run[k] = rec[k]  # the end line knows more (the PR opened, the commit pushed)
        if rec["event"] == "start":
            run["start"] = rec.get("ts")
        else:
            run["end"] = rec.get("ts")
            run["outcome"] = rec.get("outcome")
            run["detail"] = rec.get("detail")
    order.sort(key=lambda r: (r["start"] or r["end"] or "", r["_pos"]))
    for r in order:
        del r["_pos"]
    return order


def select(all_runs, agent=None, issue=None, pr=None, date=None, open_only=False):
    """The runs matching every filter given. date (YYYY-MM-DD, UTC) matches a
    run that started or ended that day."""
    out = []
    for r in all_runs:
        if agent and r["agent"] != agent:
            continue
        if issue is not None and r["issue"] != issue:
            continue
        if pr is not None and r["pr"] != pr:
            continue
        if date and not any((t or "").startswith(date) for t in (r["start"], r["end"])):
            continue
        if open_only and r["end"] is not None:
            continue
        out.append(r)
    return out


def table(rows):
    """Plain text, one run per line."""
    if not rows:
        return "no runs"
    head = ("start", "end", "agent", "issue", "pr", "outcome", "detail", "log")
    lines = ["\t".join(head)]
    for r in rows:
        lines.append("\t".join([
            r["start"] or "-", r["end"] or "(open)", r["agent"] or "-",
            f"#{r['issue']}" if r["issue"] is not None else "-",
            f"#{r['pr']}" if r["pr"] is not None else "-",
            r["outcome"] or "-", r["detail"] or "", r["log"] or "-"]))
    return "\n".join(lines)


def main(argv):
    if argv and argv[0] == "line":
        p = argparse.ArgumentParser(prog="runs.py line")
        for name in ("agent", "run-id", "event", "issue", "pr", "outcome", "detail", "log", "commit", "ts"):
            p.add_argument("--" + name, default=None)
        a = p.parse_args(argv[1:])
        try:
            print(make_line(a.agent, a.run_id, a.event, a.issue, a.pr, a.outcome, a.detail, a.log, a.commit, a.ts))
        except ValueError as e:
            print(f"runs.py line: {e}", file=sys.stderr)
            return 2
        return 0
    p = argparse.ArgumentParser(prog="runs.py", description="List the agents' runs (the run ledger, #206).")
    p.add_argument("--file", default=None, help="ledger (default <envs>/history/agent_runs.jsonl)")
    p.add_argument("--agent", choices=AGENTS)
    p.add_argument("--issue", type=lambda v: _number(v, "issue"))
    p.add_argument("--pr", type=lambda v: _number(v, "pr"))
    p.add_argument("--date", help="UTC day, YYYY-MM-DD")
    p.add_argument("--open", action="store_true", help="only runs without an end line (crashed or running)")
    p.add_argument("--json", action="store_true", help="one JSON object per run")
    a = p.parse_args(argv)
    records, bad = read(a.file or default_file())
    if bad:
        print(f"runs.py: skipped {bad} line(s) that are not ledger records", file=sys.stderr)
    rows = select(runs(records), a.agent, a.issue, a.pr, a.date, a.open)
    if a.json:
        for r in rows:
            print(json.dumps(r, ensure_ascii=False))
    else:
        print(table(rows))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
