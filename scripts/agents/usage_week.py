#!/usr/bin/env python3
"""usage_week.py — the pure parts of the weekly usage job (#340, #341, #252):
usage.sh runs `cmd/usage -alert` for every environment with an events file,
this reads what it wrote and builds the E13 epic comment and the drop issue.

    usage_week.py week [<YYYY-MM-DD>]       → the last complete ISO week before that day (today, UTC)
    usage_week.py events <report.json>      → the report's events_in_week (0 when unreadable)
    usage_week.py comment <week> <envs dir> <state dir> [envs]
                                            → the epic comment (Markdown) on stdout
    usage_week.py issue <week> <envs dir> <state dir> <issues.json> <out dir> [envs]
                                            → <out dir>/drop.title and .body when a drop is
                                              new this week; a log line on stdout

<envs dir> holds one clone per environment; each report is
<env>/data/monitor/reports/<week>.json (cmd/usage). <state dir> holds
<env>.drops.json, the JSON of cmd/usage's DROPS line. An environment with no
report, or none with events in the week, is left out. issues.json: gh issue
list --state all --json number,title,state,closedAt.

Counts and percentages only, read from named fields: nothing else of the
report is copied, so a user, session or amount field can never reach GitHub.
An environment whose .env says DERIBIT_ENV=live gets percentages only (the
rule of evidence_week.py). One drop issue per week, every falling metric of
every environment inside; an open issue, or one closed in the last
DEDUP_DAYS days, whose title ends with the week is a duplicate.

Standard library only: launchd runs it with the system python3.
"""
import datetime as dt
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from evidence_week import is_live  # noqa: E402  DERIBIT_ENV=live in the clone's .env
from metrics import previous_week  # noqa: E402  the same "last complete week"

HEADER = "🤖 **Usage report**"
TITLE_PREFIX = "[E13] Engagement drop:"
DEDUP_DAYS = 14

METRIC_NAMES = {
    "active_users": "weekly active users",
    "help_used": "help used (tips opened + videos played)",
    "first_answers": "first sessions that saw the four answers",
    "manual_close": "manual-close dialogs confirmed",
    "rebalance": "rebalances confirmed after a preview",
}


def load_json(path):
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def num(v):
    """An int or float from the report, else None (never a string: an id is never a figure)."""
    return v if isinstance(v, (int, float)) and not isinstance(v, bool) else None


def events_in_week(report):
    return num((report or {}).get("events_in_week")) or 0


def pct(v):
    return "—" if v is None else f"{v:.0f} %"


def signed_pct(v):
    return "—" if v is None else f"{v:+.0f} %"


def share(n, of):
    return None if not of else 100 * n / of


def figures(report, drops):
    """The comment's figures from named fields only: (counts, shares)."""
    active = num(((report.get("active") or {}).get("week") or {}).get("users")) or 0
    prev = None
    weeks = (drops or {}).get("weeks") or []
    if len(weeks) >= 2 and not weeks[-2].get("missing"):
        prev = num(weeks[-2].get("active_users"))
    wow = share(active - prev, prev) if prev else None

    # Week-1 retention of the latest cohort that has a +1 week.
    ret_users = ret_pct = cohort = cohort_users = None
    for c in reversed(report.get("cohorts") or []):
        r1 = next((r for r in c.get("retained") or [] if r.get("offset") == 1), None)
        if r1 is not None:
            cohort, cohort_users = str(c.get("week", ""))[:10], num(c.get("users"))
            ret_users, ret_pct = num(r1.get("users")), num(r1.get("pct"))
            break

    fa = report.get("first_answers") or {}
    helped = report.get("help") or {}
    help_opened = sum(num(v) or 0 for k in ("tips", "videos_played") for v in (helped.get(k) or {}).values())
    return {
        "active": active, "prev": prev, "wow": wow,
        "cohort": cohort, "cohort_users": cohort_users, "ret_users": ret_users, "ret_pct": ret_pct,
        "fa_n": num(fa.get("with_answers")) or 0, "fa_of": num(fa.get("first_sessions")) or 0, "fa_pct": num(fa.get("pct")),
        "help_opened": help_opened, "help_answers": num(helped.get("answers")) or 0, "helped_pct": num(helped.get("helped_pct")),
    }


def drop_text(d, live):
    """One drop in words. Live: shares and the change only."""
    name = METRIC_NAMES.get(d.get("metric"), "a metric")
    cur, base = num(d.get("current")), num(d.get("base"))
    if d.get("kind") == "funnel":
        text = f"{name}: {pct(cur)} of starts, was {pct(base)} ({num(d.get('change_pts')) or 0:+.0f} pts)"
        if not live:
            text += f" — {num(d.get('starts')) or 0} starts this week, {num(d.get('base_starts')) or 0} in the base weeks"
        return text
    change = signed_pct(num(d.get("change_pct")))
    if live:
        return f"{name}: {change} against the mean of the base weeks"
    return f"{name}: {cur or 0:.0f}, mean of the base weeks {base or 0:.1f} ({change})"


def env_reports(week, envs_dir, state_dir, envs):
    """[(env, live, report, drops)] for every env with events in the week."""
    out = []
    for env in envs:
        root = os.path.join(envs_dir, env)
        report = load_json(os.path.join(root, "data", "monitor", "reports", f"{week}.json"))
        if not isinstance(report, dict) or events_in_week(report) == 0:
            continue
        drops = load_json(os.path.join(state_dir, f"{env}.drops.json"))
        out.append((env, is_live(root), report, drops if isinstance(drops, dict) else None))
    return out


def comment(week, envs_dir, state_dir, envs=("stable", "mvp")):
    rows = env_reports(week, envs_dir, state_dir, envs)
    out = [f"{HEADER} — week {week} (`scripts/agents/usage.sh`, #340)", "",
           "From `cmd/usage` over each environment's `data/monitor/events.jsonl` (README \"Usage report\"). "
           "Counts and percentages only: no user or session id.", "",
           "| env | active users | vs last week | week-1 retention (latest cohort) | first answers found | help used | help answers (helped) | drops | report |",
           "|---|---|---|---|---|---|---|---|---|"]
    live_envs, drop_lines, notes = [], [], []
    for env, live, report, drops in rows:
        f = figures(report, drops)
        label = env + (" (live)" if live else "")
        if live:
            live_envs.append(env)
        if f["cohort"] is None:
            ret = "— (no cohort has a full week after it)"
        elif live:
            ret = f"{pct(f['ret_pct'])} ({f['cohort']})"
        else:
            ret = f"{f['ret_users']} of {f['cohort_users']} ({pct(f['ret_pct'])}, {f['cohort']})"
        wow = signed_pct(f["wow"]) if f["prev"] else "— (no events the week before)"
        if live:
            cells = ["—", wow, ret, pct(f["fa_pct"]), "—", pct(f["helped_pct"])]
        else:
            cells = [str(f["active"]), wow, ret, f"{f['fa_n']} of {f['fa_of']} ({pct(f['fa_pct'])})",
                     str(f["help_opened"]), f"{f['help_answers']} ({pct(f['helped_pct'])})"]
        if drops is None:
            n_drops = "— (drop check did not run)"
        else:
            ds = drops.get("drops") or []
            n_drops = str(len(ds))
            drop_lines += [f"- {env}: {drop_text(d, live)}" for d in ds]
            missing = [str(w)[:10] for w in drops.get("missing") or []]
            if missing:
                notes.append(f"- {env}: no events in {', '.join(missing)} — left out of the base, never counted as zero")
        out.append("| " + " | ".join([label] + cells + [n_drops, f"`{env}/data/monitor/reports/{week}.md`"]) + " |")
    if drop_lines:
        out += ["", "**Engagement drops** (one issue in Triage for the week):", ""] + drop_lines
    if notes:
        out += ["", "**Missing weeks:**", ""] + notes
    if live_envs:
        out += ["", f"_{', '.join(live_envs)}: live account — percentages only; counts are in the report files._"]
    return "\n".join(out) + "\n"


def _parse_time(s):
    try:
        return dt.datetime.fromisoformat((s or "").replace("Z", "+00:00"))
    except ValueError:
        return None


def find(week, issues, now):
    """Number of an issue that already reports this week's drop: open, or
    closed in the last DEDUP_DAYS days; None otherwise."""
    since = now - dt.timedelta(days=DEDUP_DAYS)
    for i in issues:
        t = i.get("title", "").rstrip()
        if not (t.startswith(TITLE_PREFIX) and t.endswith(" " + week)):
            continue
        if i.get("state", "OPEN").upper() == "OPEN":
            return i["number"]
        closed = _parse_time(i.get("closedAt"))
        if closed is None or closed >= since:
            return i["number"]
    return None


def issue(week, envs_dir, state_dir, issues, now, envs=("stable", "mvp")):
    """(log line, (title, body) or None)."""
    rows = [(env, live, (drops or {}).get("drops") or []) for env, live, _, drops in env_reports(week, envs_dir, state_dir, envs)]
    rows = [r for r in rows if r[2]]
    if not rows:
        return "no engagement drop", None
    n = find(week, issues, now)
    if n is not None:
        return f"engagement drop {week}: #{n} already reports it, nothing filed", None
    metrics = []
    for _, _, ds in rows:
        for d in ds:
            if d.get("metric") not in metrics:
                metrics.append(d.get("metric"))
    title = f"{TITLE_PREFIX} {', '.join(str(m) for m in metrics)} {week}"
    lines = [f"📉 **Engagement drop** — week {week}, against the weeks before it (`cmd/usage -alert`, #341).", ""]
    for env, live, ds in rows:
        lines.append(f"**{env}**" + (" (live: percentages only)" if live else ""))
        lines += [f"- {drop_text(d, live)}" for d in ds]
        lines.append("")
    lines += [
        "**Rules** (fixed in `internal/usage/drops.go`): a count (weekly active users, help used) at least 30 % under "
        "the mean of the 3 weeks before; a funnel's completion share at least 20 points under theirs. "
        "A base under 10 users or 10 funnel starts is too few to judge; a week with no events is left out.",
        "",
        "Find the why while it is fresh: a broken flow, a release (the report's `app_version`), or nobody signed in. "
        f"The week's reports: `<env>/data/monitor/reports/{week}.md`.",
        "",
        "Filed by `scripts/agents/usage.sh`, the weekly usage report (#340). Counts and percentages only.",
        f"<!-- engagement-drop: {week} -->",
    ]
    return f"engagement drop {week}: filing ({', '.join(str(m) for m in metrics)})", (title, "\n".join(lines) + "\n")


def main(argv):
    cmd = argv[1] if len(argv) > 1 else ""
    if cmd == "week":
        day = dt.date.fromisoformat(argv[2]) if len(argv) > 2 else dt.datetime.now(dt.timezone.utc).date()
        print(previous_week(day))
    elif cmd == "events" and len(argv) == 3:
        print(events_in_week(load_json(argv[2])))
    elif cmd == "comment" and len(argv) in (5, 6):
        envs = tuple(argv[5].split(",")) if len(argv) == 6 else ("stable", "mvp")
        sys.stdout.write(comment(argv[2], argv[3], argv[4], envs))
    elif cmd == "issue" and len(argv) in (7, 8):
        week, envs_dir, state_dir, issues_path, out_dir = argv[2:7]
        envs = tuple(argv[7].split(",")) if len(argv) == 8 else ("stable", "mvp")
        issues = load_json(issues_path) or []
        line, filing = issue(week, envs_dir, state_dir, issues, dt.datetime.now(dt.timezone.utc), envs)
        if filing:
            os.makedirs(out_dir, exist_ok=True)
            for ext, text in zip(("title", "body"), filing):
                with open(os.path.join(out_dir, f"drop.{ext}"), "w", encoding="utf-8") as f:
                    f.write(text)
        print(line)
    else:
        raise SystemExit(__doc__)


if __name__ == "__main__":
    main(sys.argv)
