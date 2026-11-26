#!/usr/bin/env python3
"""lab_queue.py — the rules of the lab step of the agents' tick (lab.sh, #876):
what the tick does with the monitor's lab queue (data/compare/requests/, the
files cmd/labrun reads and writes, internal/compare/requests.go) and the
Triage bug for a stalled queue. Files only: no gh, no model call.

  lab_queue.py action <queue>            → "run", "busy" (the runner's lock is held) or "none"
  lab_queue.py queued <queue>            → the queued request ids, one per line
  lab_queue.py stalled <queue>           → "<id>\\t<status>\\t<age minutes>" per stalled request
  lab_queue.py issue <queue> <title> <body>   → writes the stalled-queue bug's title and body; exit 1 when nothing is stalled
  lab_queue.py started <ids file> <queue> <log>   → appends one line per id of <ids file> the runner took
                                         (no longer queued) to <log>, prints their number
  lab_queue.py count <log> <queue> [--days 28]   → requests the tick started for <queue> in the last days

A stalled request is one queued over QUEUED_MAX_MIN (2 h), or one left
running while the runner's lock is free (the runner died). Ids, statuses and
times only: never a setting value, a requester, an account or an amount.
Tests: python3 -m unittest discover -s scripts/agents -p 'test_lab*.py'
"""
import datetime
import json
import os
import re
import sys

QUEUED_MAX_MIN = 120  # the lab's target is a median ≤ 30 min to done (docs/product/lab/README.md)
LOCK_MAX_AGE_MIN = 24 * 60  # compare.RequestMaxAge: an older lock is taken over by the next runner
RUNNER_LOCK = ".runner.lock"  # compare.RunnerLock
REQUEST_ID = re.compile(r"^\d{8}T\d{6}Z-[0-9a-f]{8}$")  # compare's requestIDRe
TITLE = "[E9] Lab queue stalled"


def now_utc():
    return datetime.datetime.now(datetime.timezone.utc)


def parse_time(s):
    try:
        t = datetime.datetime.fromisoformat(str(s).replace("Z", "+00:00"))
    except ValueError:
        return None
    return t if t.tzinfo else t.replace(tzinfo=datetime.timezone.utc)


def read_requests(queue):
    """The queue's requests, oldest first; a file that does not parse or is not a request id is skipped."""
    try:
        names = sorted(os.listdir(queue))
    except OSError:
        return []
    reqs = []
    for name in names:
        rid = name[:-5] if name.endswith(".json") else ""
        if not REQUEST_ID.match(rid):
            continue
        try:
            with open(os.path.join(queue, name)) as f:
                r = json.load(f)
        except (OSError, ValueError):
            continue
        if isinstance(r, dict) and r.get("id") == rid:
            reqs.append(r)
    return reqs


def lock_held(queue, now):
    """Whether a runner holds the queue's lock: the folder exists and is younger than a day."""
    try:
        mtime = os.stat(os.path.join(queue, RUNNER_LOCK)).st_mtime
    except OSError:
        return False
    age = now - datetime.datetime.fromtimestamp(mtime, datetime.timezone.utc)
    return age <= datetime.timedelta(minutes=LOCK_MAX_AGE_MIN)


def age_min(r, field, now):
    t = parse_time(r.get(field))
    return None if t is None else int((now - t).total_seconds() // 60)


def stalled(reqs, held, now):
    """[(id, status, age minutes)]: queued over QUEUED_MAX_MIN, or running with the lock free."""
    out = []
    for r in reqs:
        status = r.get("status")
        if status == "queued":
            age = age_min(r, "requested_at", now)
            if age is not None and age > QUEUED_MAX_MIN:
                out.append((r["id"], status, age))
        elif status == "running" and not held:
            out.append((r["id"], status, age_min(r, "requested_at", now)))
    return out


def action(reqs, held, now):
    """run: a queued request, or a running one the runner left (the runner marks it failed); busy: the lock is held."""
    if held:
        return "busy"
    if any(r.get("status") == "queued" for r in reqs) or stalled(reqs, held, now):
        return "run"
    return "none"


def age_text(minutes):
    if minutes is None:
        return "age unknown"
    if minutes < 120:
        return f"{minutes} min"
    return f"{minutes / 60:.1f} h"


def issue(items):
    """(title, body) of the stalled-queue bug, or None with nothing stalled."""
    if not items:
        return None
    first = items[0]
    title = f"{TITLE}: {first[0]} {first[1]} {age_text(first[2])}"
    lines = ["🧪 **Lab queue stalled** — the agents' tick (`scripts/agents/lab.sh`, #876) found requests "
             "the lab runner did not finish:", ""]
    for rid, status, age in items:
        why = (f"queued over {QUEUED_MAX_MIN // 60} h" if status == "queued"
               else "left running while the runner's lock is free (the runner stopped)")
        lines.append(f"- `{rid}`: {status}, {age_text(age)} since requested — {why}")
    lines += ["",
              "Look at the runner's log (`data/agents/tick-lab.log` and the lab run's log under "
              "`data/agents/lab/`), the lock `data/compare/requests/.runner.lock` and "
              "`python3 scripts/agents/runs.py --agent lab`.",
              "The runner expires a request queued a day and marks a stopped run failed on its next pass.", "",
              "Filed by `scripts/agents/lab.sh`. Request ids and times only; no setting value, account or amount; "
              "no second issue while this one is open.",
              "<!-- agent:lab-stalled -->"]
    return title, "\n".join(lines) + "\n"


def started(ids, reqs):
    """The ids, of those queued before the runner, that are no longer queued."""
    now_status = {r["id"]: r.get("status") for r in reqs}
    return [i for i in ids if i in now_status and now_status[i] != "queued"]


def count(log, queue, days, now):
    since = now - datetime.timedelta(days=days)
    n = 0
    try:
        with open(log) as f:
            for line in f:
                try:
                    d = json.loads(line)
                except ValueError:
                    continue
                t = parse_time(d.get("ts"))
                if d.get("queue") == queue and t is not None and t >= since:
                    n += 1
    except OSError:
        return 0
    return n


def main(argv):
    now = now_utc()
    cmd, args = argv[1], argv[2:]
    if cmd == "count":
        days = int(args[args.index("--days") + 1]) if "--days" in args else 28
        print(count(args[0], os.path.abspath(args[1]), days, now))
        return 0
    queue = args[0]
    if cmd == "started":
        queue = args[1]
    reqs = read_requests(queue)
    held = lock_held(queue, now)
    if cmd == "action":
        print(action(reqs, held, now))
    elif cmd == "queued":
        for r in reqs:
            if r.get("status") == "queued":
                print(r["id"])
    elif cmd == "stalled":
        for rid, status, age in stalled(reqs, held, now):
            print(f"{rid}\t{status}\t{'' if age is None else age}")
    elif cmd == "issue":
        got = issue(stalled(reqs, held, now))
        if got is None:
            return 1
        with open(args[1], "w") as f:
            f.write(got[0] + "\n")
        with open(args[2], "w") as f:
            f.write(got[1])
    elif cmd == "started":
        with open(args[0]) as f:
            ids = [l.strip() for l in f if l.strip()]
        took = started(ids, reqs)
        with open(args[2], "a") as f:
            for rid in took:
                f.write(json.dumps({"ts": now.strftime("%Y-%m-%dT%H:%M:%SZ"), "queue": os.path.abspath(queue),
                                    "id": rid}) + "\n")
        print(len(took))
    else:
        print(f"unknown command {cmd}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
