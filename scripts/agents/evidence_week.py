#!/usr/bin/env python3
"""evidence_week.py — the pure parts of the weekly evidence job (#244):
evidence.sh runs cmd/evidence for every env × bot, this builds the E2 epic
comment from the reports it wrote.

    evidence_week.py week [<YYYY-MM-DD>]                     → the last complete ISO week before that day (today, UTC): 2026-W43
    evidence_week.py comment <week> <envs dir> <failures.tsv> <bots> [envs]
                                                             → the epic comment (Markdown) on stdout

<envs dir> holds one clone per environment (<envs dir>/<env>, as
scripts/env_lib.sh lays them out); each report is
<env>/data/evidence/<week>/<bot>.json (cmd/evidence). <failures.tsv> has one
line per failed step: env <TAB> bot <TAB> step <TAB> detail (bot "-" for the
whole env). <bots> is comma-separated; [envs] defaults to "stable,mvp".

Live accounts: an env whose .env says DERIBIT_ENV=live gets percentages and
the report's path only — never a coin or USD amount, nor a failure's detail
(the same rule as sme_review.sh: the repository is public).

Standard library only: launchd runs it with the system python3.
"""
import datetime as dt
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from metrics import previous_week  # noqa: E402  the same "last complete week"

HEADER = "🤖 **Evidence report**"


def env_value(path, key):
    """Last KEY=value of a dotenv file, quotes stripped; "" when absent."""
    val = ""
    try:
        with open(path) as f:
            for line in f:
                line = line.strip()
                if line.startswith("export "):
                    line = line[len("export "):].strip()
                k, sep, v = line.partition("=")
                if sep and k.strip() == key:
                    val = v.strip().strip("\"'")
    except OSError:
        pass
    return val


def is_live(env_root):
    return env_value(os.path.join(env_root, ".env"), "DERIBIT_ENV").lower() == "live"


def pct(v):
    return f"{v:+.2f} %"


def coin(v, cur):
    return f"{v:+.4f} {cur}"


def days_text(days, total=7):
    if not days:
        return "none"
    if len(days) >= total:
        return f"all {total}"
    return ", ".join(d[5:] for d in days)  # MM-DD


def net_cell(r, live, cur):
    p = r["live"]["perf"]
    if not p.get("Points"):
        return "— (no P&L history)"
    share = pct(p["ReturnPct"]) if p.get("EquityKnown") else None
    if live:
        return share or "— (equity unknown)"
    return coin(p["NetPnL"], cur) + (f" ({share})" if share else "")


def excess_cell(r):
    h = r["hold"]
    if h.get("strategy_known"):
        return f"{pct(h['excess_return_pct'])} (hold {pct(h['hold_return_usd_pct'])})"
    if h.get("known"):
        return f"— (equity unknown; hold {pct(h['hold_return_usd_pct'])})"
    return "— (no spot in the history)"


def worst_week_cell(r, live, cur):
    p = r["live"]["perf"]
    if not p.get("WorstWeekKnown"):
        return "— (under 7 days of history)"
    share = pct(p["WorstWeekPct"]) if p.get("EquityKnown") else None
    if live:
        return share or "— (equity unknown)"
    return coin(p["WorstWeek"], cur) + (f" ({share})" if share else "")


def gap_cell(r, live, cur):
    g = r["gap"]
    if not g.get("known"):
        note = r.get("replay", {}).get("note") or "replay did not run"
        return f"— ({note})"
    eq = r.get("equity") or 0
    share = pct(g["gap"] / eq * 100) if eq > 0 else None
    if live:
        return share or "— (equity unknown)"
    return coin(g["gap"], cur) + (f" ({share})" if share else "")


def bridge_cell(r):
    """The equity change the bot does not explain (#277): % of the starting
    equity only, never an amount."""
    b = r.get("bridge") or {}
    if not b.get("known"):
        return f"— ({b.get('note') or 'no equity recorded'})"
    return pct(b["residual_pct"])


def stress_cell(r):
    s = r.get("stress") or {}
    if not s.get("ran"):
        return f"— ({s.get('note') or 'not run'})"
    ok = [o for o in s.get("outcomes") or [] if not o.get("error")]
    if not ok:
        return "— (every shock failed)"
    w = min(ok, key=lambda o: o["return_pct"])
    return f"worst {w['shock']} {pct(w['return_pct'])}" + (" · stale book" if s.get("stale") else "")


def missing_cell(r):
    m = r.get("missing") or {}
    return (f"history {days_text(m.get('history_days'))} · books {days_text(m.get('book_days'))}"
            f" · GEX {days_text(m.get('gex_days'))}")


def load_failures(path):
    out = []
    try:
        with open(path) as f:
            for line in f:
                parts = line.rstrip("\n").split("\t")
                if len(parts) >= 3:
                    out.append((parts[0], parts[1], parts[2], parts[3] if len(parts) > 3 else ""))
    except OSError:
        pass
    return out


def comment(week, envs_dir, failures_path, bots, envs=("stable", "mvp")):
    failures = load_failures(failures_path)
    out = [f"{HEADER} — week {week} (`scripts/agents/evidence.sh`, #244)", "",
           "One row per environment × bot, from `cmd/evidence` (README \"Evidence report\"). "
           "Percentages are of the margin balance at the week's start; the replay gap is replay − live.", "",
           "| env | bot | net P&L | equity not explained by the bot | excess vs holding the coin | worst 7 days | replay gap | stress | missing days | report |",
           "|---|---|---|---|---|---|---|---|---|---|"]
    live_envs = set()
    for env in envs:
        root = os.path.join(envs_dir, env)
        live = is_live(root)
        if live:
            live_envs.add(env)
        for bot in bots:
            rel = f"data/evidence/{week}/{bot}.json"
            path = os.path.join(root, rel)
            label = env + (" (live)" if live else "")
            try:
                with open(path) as f:
                    r = json.load(f)
            except (OSError, ValueError):
                out.append(f"| {label} | {bot} | no report | | | | | | | — |")
                continue
            cur = bot.upper()
            out.append("| " + " | ".join([
                label, bot, net_cell(r, live, cur), bridge_cell(r), excess_cell(r), worst_week_cell(r, live, cur),
                gap_cell(r, live, cur), stress_cell(r), missing_cell(r), f"`{env}/{rel[:-5]}.md`"]) + " |")
    if failures:
        out += ["", "**Failed steps** (the other reports were still written):", ""]
        for env, bot, step, detail in failures:
            who = env if bot in ("", "-") else f"{env} {bot}"
            line = f"- {who}: {step}"
            if detail and env not in live_envs:
                line += f" — `{detail[:200].replace('`', '')}`"
            out.append(line)
    if live_envs:
        out += ["", f"_{', '.join(sorted(live_envs))}: live account — percentages only; amounts are in the report files._"]
    return "\n".join(out) + "\n"


def main(argv):
    if len(argv) >= 2 and argv[1] == "week":
        day = dt.date.fromisoformat(argv[2]) if len(argv) > 2 else dt.datetime.now(dt.timezone.utc).date()
        print(previous_week(day))
    elif len(argv) in (6, 7) and argv[1] == "comment":
        bots = [b.strip().lower() for b in argv[5].split(",") if b.strip()]
        envs = tuple(argv[6].split(",")) if len(argv) == 7 else ("stable", "mvp")
        sys.stdout.write(comment(argv[2], argv[3], argv[4], bots, envs))
    else:
        raise SystemExit(__doc__)


if __name__ == "__main__":
    main(sys.argv)
