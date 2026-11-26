#!/usr/bin/env python3
"""recording_day.py — the pure parts of the daily recording check (#716):
recording_check.sh runs `cmd/datacoverage -yesterday -json` for every env × bot
that records the market, this reads what it wrote and builds the one
notification and the E2 epic comment.

    recording_day.py yesterday [<YYYY-MM-DD>]     → the UTC day before that day (today, UTC)
    recording_day.py recorders <envs dir> <envs>  → the envs (comma-separated in) whose clone exists
                                                    and records the market (deploy.env without
                                                    BOT_RECORD_MARKET=0), one per line
    recording_day.py short <dir>                  → one line per short dataset, empty when every
                                                    dataset meets the floor
    recording_day.py notify <day> <dir>           → the notification's message (the first short
                                                    dataset or failed check, "+n more")
    recording_day.py comment <day> <dir>          → the E2 epic comment (Markdown)

<dir> holds <env>-<bot>.json (cmd/datacoverage -yesterday -json: evidence.
DailyRecording) and failures.tsv (env <TAB> bot: a check that did not run).
A dataset the bot does not record (no folder) is never short; a folder with
no file for the day is 0 % (the Go check). Hours and percentages only — never
an amount, so the text is the same for a live bot.

Standard library only: launchd runs it with the system python3.
"""
import datetime as dt
import glob
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from evidence_week import env_value  # noqa: E402  the same dotenv reader

HEADER = "🤖 **Recording check**"


def marker(day):
    return f"<!-- agent:recording day={day} -->"


def yesterday(today=None):
    d = dt.date.fromisoformat(today) if today else dt.datetime.now(dt.timezone.utc).date()
    return (d - dt.timedelta(days=1)).isoformat()


def recorders(envs_dir, envs):
    out = []
    for env in envs:
        root = os.path.join(envs_dir, env)
        if os.path.isdir(root) and env_value(os.path.join(root, "deploy.env"), "BOT_RECORD_MARKET") != "0":
            out.append(env)
    return out


def hours_text(h):
    return f"{h * 60:.0f} min" if h < 1 else f"{h:.1f} h"


def clock(ts):
    """HH:MM of an RFC 3339 UTC time."""
    return ts[11:16]


def load(dirname):
    """[(env, bot, check)] from <dir>/<env>-<bot>.json, sorted; a file that does not parse is skipped."""
    out = []
    for path in sorted(glob.glob(os.path.join(dirname, "*-*.json"))):
        env, _, bot = os.path.basename(path)[:-len(".json")].partition("-")
        try:
            with open(path) as f:
                out.append((env, bot, json.load(f)))
        except (OSError, ValueError):
            continue
    return out


def failures(dirname):
    """[(env, bot)] of the checks that did not run."""
    try:
        with open(os.path.join(dirname, "failures.tsv")) as f:
            return [tuple((line.rstrip("\n").split("\t") + ["", ""])[:2]) for line in f if line.strip()]
    except OSError:
        return []


def short_lines(checks):
    """One line per short dataset: bot, dataset, day, covered share and the longest gap."""
    lines = []
    for env, bot, c in checks:
        for d in c.get("datasets", []):
            if not d.get("short"):
                continue
            line = (f"{env} {bot} {d['dataset']} {c['day']}: {d['covered_pct']:.1f} % covered "
                    f"({d['covered_hours']:.1f} of {d['market_hours']:.0f} h, floor {c['floor_pct']:.0f} %)")
            g = d.get("longest_gap") or {}
            if g.get("hours", 0) > 0:
                line += f", longest gap {hours_text(g['hours'])} from {clock(g['from'])} UTC"
            lines.append(line)
    return lines


def failure_lines(fails, day):
    return [f"{env} {bot} {day}: not checked (cmd/datacoverage failed, see the run log)" for env, bot in fails]


def notify_text(dirname, day):
    lines = short_lines(load(dirname)) + failure_lines(failures(dirname), day)
    if not lines:
        return ""
    more = f" (+{len(lines) - 1} more)" if len(lines) > 1 else ""
    return lines[0] + more


def comment(day, dirname):
    checks = load(dirname)
    fails = failures(dirname)
    out = [f"{HEADER} — {day} UTC (`scripts/agents/recording_check.sh`, #716)", "",
           "Yesterday's market recordings against the weekly report's floor "
           "(`ReplayFullCoveragePct`, a gap over `LongGapHours` named); hours and shares only.", ""]
    short = short_lines(checks)
    if short:
        out.append("**Short:**")
        out += [f"- {line}" for line in short]
        out.append("")
    for env, bot, c in checks:
        gaps = []
        for d in c.get("datasets", []):
            for g in d.get("long_gaps") or []:
                gaps.append(f"{d['dataset']} {clock(g['from'])} → {clock(g['to'])} ({hours_text(g['hours'])})")
        if gaps:
            out.append(f"- {env} {bot} gaps over {c['long_gap_hours']:.0f} h: " + ", ".join(gaps))
    for line in failure_lines(fails, day):
        out.append(f"- {line}")
    out += ["", "| Env | Bot | Dataset | Covered | Longest gap |", "|---|---|---|---|---|"]
    for env, bot, c in checks:
        for d in c.get("datasets", []):
            if d.get("status") != "recorded":
                continue
            g = d.get("longest_gap") or {}
            longest = f"{hours_text(g['hours'])} from {clock(g['from'])}" if g.get("hours", 0) > 0 else "none"
            flag = " **short**" if d.get("short") else ""
            out.append(f"| {env} | {bot} | {d['dataset']} | {d['covered_pct']:.1f} %{flag} | {longest} |")
    out += ["", "Fix the recorder (`docs/operations.md` \"Market history\") before the week's replay is labelled partial.",
            marker(day)]
    return "\n".join(out) + "\n"


def main(argv):
    if len(argv) < 2:
        sys.exit(__doc__)
    cmd, args = argv[1], argv[2:]
    if cmd == "yesterday":
        print(yesterday(args[0] if args else None))
    elif cmd == "recorders":
        for env in recorders(args[0], [e for e in args[1].split(",") if e]):
            print(env)
    elif cmd == "short":
        lines = short_lines(load(args[0]))
        if lines:
            print("\n".join(lines))
    elif cmd == "notify":
        print(notify_text(args[1], args[0]))
    elif cmd == "comment":
        sys.stdout.write(comment(args[0], args[1]))
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main(sys.argv)
