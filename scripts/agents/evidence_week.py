#!/usr/bin/env python3
"""evidence_week.py — the pure parts of the weekly evidence job (#244):
evidence.sh runs cmd/evidence for every env × bot, this builds the E2 epic
comment from the reports it wrote.

    evidence_week.py week [<YYYY-MM-DD>]                     → the last complete ISO week before that day (today, UTC): 2026-W43
    evidence_week.py comment <week> <envs dir> <failures.tsv> <bots> [envs]
                                                             → the epic comment (Markdown) on stdout
    evidence_week.py gaps <week> <envs dir> <bots> [envs]    → one line per env × bot × dataset with a
                                                               recorder gap over 3 h (#362), for the notify
    evidence_week.py direction-weeks <week> <stable root>    → the direction trial weeks to (re)build (#440):
                                                               <week> and every week since TRIAL_START
                                                               without a scorecard (the back-fill)
    evidence_week.py tickets <week> <stable root> <issues.json> <out dir>
                                                             → <out dir>/<n>.title and <n>.body for every
                                                               direction trial decision ticket (first GEX
                                                               shed, early stop, review due) no issue
                                                               carries yet; issues.json: gh issue list
                                                               --state all --json number,title,state

<envs dir> holds one clone per environment (<envs dir>/<env>, as
scripts/env_lib.sh lays them out); each report is
<env>/data/evidence/<week>/<bot>.json (cmd/evidence). <failures.tsv> has one
line per failed step: env <TAB> bot <TAB> step <TAB> detail (bot "-" for the
whole env). <bots> is comma-separated; [envs] defaults to "stable,mvp".

Live accounts: an env whose .env says DERIBIT_ENV=live gets percentages and
the report's path only — never a coin or USD amount, nor a failure's detail
(the same rule as sme_review.sh: the repository is public).

The direction trial (docs/direction-trial.md, #324, #440): cmd/evidence
-compare-direction writes <stable>/data/evidence/direction/<week>.json, which
holds percentages, counts and hours only. The comment's "Direction trial"
block and the decision tickets are built from those files with the brief's
rules (minimum span, noise, kill rule, review date); a ticket only asks a
person to decide — no agent changes gex_direction.

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


def rom_cell(r):
    """Return on margin (#278), live then replay: a %, shown for any env."""
    rom = r.get("return_on_margin") or {}

    def one(m):
        m = m or {}
        if not m.get("Known"):
            return f"— ({m.get('Reason') or 'not computed'})"
        return f"{pct(m['ReturnPct'])} ({pct(m['Return30dPct'])} / 30 d)"

    live = one(rom.get("live"))
    replay = one(rom.get("replay")) if rom.get("replay") else "— (replay did not run)"
    return f"live {live} · replay {replay}"


def bridge_cell(r):
    """The equity change the bot does not explain (#277): % of the starting
    equity only, never an amount."""
    b = r.get("bridge") or {}
    if not b.get("known"):
        return f"— ({b.get('note') or 'no equity recorded'})"
    return pct(b["residual_pct"])


def recon_cell(r):
    """The account's balance change against the journal's booked P&L (#566):
    the gap in % of the starting balance, flagged over the tolerance."""
    c = r.get("reconciliation")
    if c is None:
        return "— (not measured)"
    if not c.get("known"):
        return f"— ({c.get('note') or 'not enough history'})"
    cell = pct(c["gap_pct"])
    return f"⚠ {cell} over {c.get('tolerance_pct', 1):g} %" if c.get("flagged") else f"{cell} (within tolerance)"


def stress_cell(r):
    s = r.get("stress") or {}
    if not s.get("ran"):
        return f"— ({s.get('note') or 'not run'})"
    ok = [o for o in s.get("outcomes") or [] if not o.get("error")]
    if not ok:
        return "— (every shock failed)"
    w = min(ok, key=lambda o: o["return_pct"])
    return f"worst {w['shock']} {pct(w['return_pct'])}" + (" · stale book" if s.get("stale") else "")


def e1_cell(r, live):
    """E1 reliability (#361): margin use against the target, the top idle
    cause and the startups by class. Percentages and counts; lots only for a
    testnet env."""
    e = r.get("e1_reliability")
    if not e:
        return "— (report without the E1 section)"
    m = e.get("margin_use") or {}
    if m.get("known"):
        verdict = "met" if m.get("target_met") else "not met"
        allowed = m.get("allowed_share")
        margin = f"IM {m['mean_pct']:.1f} % of limit (target {e.get('target_pct', 60):.0f} %: {verdict})"
        if allowed is not None:
            margin += f", entries allowed {allowed * 100:.0f} %"
    else:
        margin = f"margin use not measured ({m.get('note') or 'no data'})"
    d = e.get("idle_margin") or {}
    if d.get("known"):
        causes = d.get("causes") or []
        idle = f"idle top: {causes[0]['cause']} {causes[0]['share'] * 100:.0f} %" if causes else "no idle time"
    else:
        idle = f"idle not measured ({d.get('note') or 'no data'})"
    s = e.get("restarts") or {}
    if s.get("known"):
        restarts = f"startups {s.get('startups', 0)}, restart-made {s.get('restart_made', 0)}"
        if not live and s.get("restart_made"):
            restarts += f" ({s.get('restart_made_lots', 0):.4g} lots)"
    else:
        restarts = f"startups not measured ({s.get('note') or 'no data'})"
    return " · ".join([margin, idle, restarts])


def missing_cell(r):
    m = r.get("missing") or {}
    cell = (f"history {days_text(m.get('history_days'))} · books {days_text(m.get('book_days'))}"
            f" · GEX {days_text(m.get('gex_days'))}")
    c = r.get("coverage") or {}
    if c.get("known"):
        if c.get("market_pct") is None:
            cell += " · market hours not recorded"
        else:
            cell += f" · market hours {c['market_pct']:.1f} %"
        if c.get("partial"):
            cell += " (replay partial)"
    return cell


def when(ts):
    """2026-10-14T03:00:00Z → 2026-10-14 03:00 UTC."""
    return ts[:16].replace("T", " ") + " UTC"


def gap_notices(r):
    """One line per recorded dataset with a gap over the report's threshold
    (#362): the dataset, how many such gaps, the longest with its start. Times
    and hours only, so the same for a live env."""
    c = r.get("coverage") or {}
    by = {}
    for g in c.get("long_gaps") or []:
        by.setdefault(g["dataset"], []).append(g)
    out = []
    for ds, gaps in by.items():
        g = max(gaps, key=lambda x: x["hours"])
        n = f"{len(gaps)} gaps over {c.get('long_gap_hours', 3):.0f} h, longest " if len(gaps) > 1 else "gap "
        out.append(f"{ds} {n}{g['hours']:.1f} h from {when(g['from'])}")
    return out


def notices(week, envs_dir, bots, envs=("stable", "mvp")):
    """Every env × bot's gap notices, as "env bot: notice" lines."""
    out = []
    for env in envs:
        for bot in bots:
            try:
                with open(os.path.join(envs_dir, env, "data", "evidence", week, f"{bot}.json")) as f:
                    r = json.load(f)
            except (OSError, ValueError):
                continue
            out += [f"{env} {bot}: {n}" for n in gap_notices(r)]
    return out


# The direction trial's brief (docs/direction-trial.md, #324): the first week
# both setups ran live, the minimum span, the book days a week may miss and
# still count, the kill rule's streak and the review date.
TRIAL_START = "2026-W43"
TRIAL_WEEKS = 6
TRIAL_MAX_MISSING_BOOK_DAYS = 2
TRIAL_STREAK = 3
TRIAL_REVIEW_DATE = dt.date(2026, 12, 7)
TRIAL_SOURCES = ("bias", "sma")
TICKET_PREFIX = "[E2] Direction trial: "
TICKET_KINDS = {  # kind → the title's start; one ticket of each kind, ever
    "first-shed": TICKET_PREFIX + "first GEX shed",
    "early-stop": TICKET_PREFIX + "early stop",
    "review": TICKET_PREFIX + "review due",
}


def week_monday(label):
    y, w = label.split("-W")
    return dt.date.fromisocalendar(int(y), int(w), 1)


def trial_weeks(week):
    """TRIAL_START … week, oldest first; [] before the trial."""
    out, d, last = [], week_monday(TRIAL_START), week_monday(week)
    while d <= last:
        y, w, _ = d.isocalendar()
        out.append(f"{y}-W{w:02d}")
        d += dt.timedelta(days=7)
    return out


def scorecard_path(stable_root, week):
    return os.path.join(stable_root, "data", "evidence", "direction", week + ".json")


def load_scorecard(stable_root, week):
    try:
        with open(scorecard_path(stable_root, week)) as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def direction_weeks(week, stable_root):
    """The weeks evidence.sh runs -compare-direction for: <week> itself and
    every trial week before it without a scorecard (the back-fill)."""
    return [w for w in trial_weeks(week) if w == week or not os.path.exists(scorecard_path(stable_root, w))]


def _row(rows, src):
    return next((r for r in rows or [] if r.get("source") == src), {})


def trial_week(week, card):
    """One week of the trial: its status (counted | excluded | no replay |
    no scorecard), bias − sma of the replays' Net %, the live gaps and the sheds."""
    e = {"week": week, "card": card, "d": None, "gaps": [], "shed": False}
    if card is None:
        e["status"] = "no scorecard"
        return e
    rep = {s: _row(card.get("replay"), s) for s in TRIAL_SOURCES}
    live = {s: _row(card.get("live"), s) for s in TRIAL_SOURCES}
    e["shed"] = any((r.get("sheds") or 0) > 0 for r in list(rep.values()) + list(live.values()))
    e["gaps"] = [abs(r["live_minus_replay_pp"]) for r in live.values() if r.get("live_minus_replay_pp") is not None]
    nets = [rep[s].get("net_pct") for s in TRIAL_SOURCES]
    if len((card.get("missing") or {}).get("book_days") or []) > TRIAL_MAX_MISSING_BOOK_DAYS:
        e["status"] = "excluded"
    elif None in nets:
        e["status"] = "no replay"  # counts toward neither d nor noise (the brief)
    else:
        e["status"] = "counted"
        e["d"] = nets[0] - nets[1]
    return e


def trial_state(stable_root, week):
    """The brief's running counters up to <week>, from the scorecard files."""
    weeks = [trial_week(w, load_scorecard(stable_root, w)) for w in trial_weeks(week)]
    counted = [e for e in weeks if e["status"] == "counted"]
    gaps = [g for e in counted for g in e["gaps"]]
    noise = sum(gaps) / len(gaps) if gaps else None
    streak = 0
    if noise is not None:
        for e in reversed(counted):
            if e["d"] >= -noise:
                break
            streak += 1
    first_shed = next((e["week"] for e in weeks if e["shed"]), None)
    review_reason = None
    if len(counted) >= TRIAL_WEEKS:
        review_reason = f"{len(counted)} weeks counted (the brief's minimum is {TRIAL_WEEKS})"
    elif weeks and week_monday(week) + dt.timedelta(days=7) >= TRIAL_REVIEW_DATE:
        review_reason = f"the review date {TRIAL_REVIEW_DATE} is reached with {len(counted)} of {TRIAL_WEEKS} weeks counted"
    return {
        "week": week, "weeks": weeks, "counted": counted, "noise": noise, "streak": streak,
        "mean_d": sum(e["d"] for e in counted) / len(counted) if counted else None,
        "first_shed": first_shed, "early_stop": streak >= TRIAL_STREAK, "review_reason": review_reason,
    }


def pp(v):
    return "—" if v is None else f"{v:+.2f} pp"


def opt_pct(v):
    return "—" if v is None else pct(v)


def week_list(weeks):
    return ", ".join(e["week"] for e in weeks) or "none"


def direction_block(stable_root, week):
    """The comment's "Direction trial" block: percentages and counts only
    (the scorecard holds nothing else). [] before the trial."""
    if not trial_weeks(week):
        return []
    s = trial_state(stable_root, week)
    out = ["", f"**Direction trial** — bias (stable BTC) vs sma (mvp BTC), "
               f"`stable/data/evidence/direction/{week}.md` (rules: `docs/direction-trial.md`; a person decides):", ""]
    card = s["weeks"][-1]["card"]
    if card is None:
        out.append(f"No scorecard for {week} (see the failed steps).")
    else:
        out += ["| source | replay net % | replay max drawdown % | replay sheds | live sheds | live − replay |",
                "|---|---|---|---|---|---|"]
        for src in TRIAL_SOURCES:
            r, l = _row(card.get("replay"), src), _row(card.get("live"), src)
            if r.get("ran"):
                net, dd, sheds = opt_pct(r.get("net_pct")), opt_pct(r.get("max_drawdown_pct")), str(r.get("sheds", 0))
            else:
                net, dd, sheds = "— (replay did not run)", "—", "—"
            live_sheds = str(l["sheds"]) if l else "— (no live row)"
            out.append(f"| {src} | {net} | {dd} | {sheds} | {live_sheds} | {pp(l.get('live_minus_replay_pp'))} |")
        out.append("")
    excluded = [e for e in s["weeks"] if e["status"] == "excluded"]
    unbuilt = [e for e in s["weeks"] if e["status"] in ("no scorecard", "no replay")]
    out.append(f"- Weeks counted toward {TRIAL_WEEKS}: {len(s['counted'])} ({week_list(s['counted'])})"
               f" · excluded, books missing over {TRIAL_MAX_MISSING_BOOK_DAYS} days: {week_list(excluded)}"
               f" · no scorecard or replay: {week_list(unbuilt)}")
    out.append(f"- bias − sma, replay net (mean of the counted weeks): {pp(s['mean_d'])} · noise (mean live − replay): {pp(s['noise'])}")
    streak = f"{s['streak']} of {TRIAL_STREAK}" if s["noise"] is not None else "not measured (no live − replay gap yet)"
    out.append(f"- Early-stop streak (bias's replay net under sma's by more than the noise): {streak}"
               + (" — **early stop: decision ticket filed**" if s["early_stop"] else ""))
    out.append("- First GEX shed: " + (s["first_shed"] or "none yet (the review waits for one)"))
    out.append("- Review: " + (f"**due** — {s['review_reason']}" if s["review_reason"] else f"{TRIAL_REVIEW_DATE}"))
    out.append("- The kill rule's live drawdown (stable over 2× mvp) is not in the scorecard: read the weekly reports above.")
    return out


def trial_table(s):
    out = ["| week | status | bias net % | sma net % | bias − sma | sheds (replay bias / sma · live bias / sma) | live − replay |",
           "|---|---|---|---|---|---|---|"]
    for e in s["weeks"]:
        c = e["card"] or {}
        rep = [_row(c.get("replay"), x) for x in TRIAL_SOURCES]
        live = [_row(c.get("live"), x) for x in TRIAL_SOURCES]
        sheds = " / ".join(str(r.get("sheds", "—")) for r in rep) + " · " + " / ".join(str(r.get("sheds", "—")) for r in live)
        gaps = ", ".join(pp(r.get("live_minus_replay_pp")) for r in live)
        out.append(f"| {e['week']} | {e['status']} | {opt_pct(rep[0].get('net_pct'))} | {opt_pct(rep[1].get('net_pct'))}"
                   f" | {pp(e['d'])} | {sheds} | {gaps} |")
    return out


def trial_tickets(week, stable_root, issues):
    """The decision tickets due at <week> that no issue (any state) carries
    yet: [(kind, title, body)]. Each kind is filed once, ever."""
    titles = [i.get("title", "") for i in issues]
    s = trial_state(stable_root, week)
    due = []
    if s["first_shed"]:
        due.append(("first-shed", f"{TICKET_KINDS['first-shed']} in {s['first_shed']}, review bias vs sma",
                    f"The direction trial's first week with a GEX shed is **{s['first_shed']}** (a replay or a live "
                    "row of the scorecard has sheds > 0). The product owner held the bias-vs-sma review until one (#88)."))
    if s["early_stop"]:
        due.append(("early-stop", f"{TICKET_KINDS['early-stop']} in {week}, decide bias vs sma",
                    f"The brief's kill rule: bias's replay net was under sma's by more than the noise "
                    f"({pp(s['noise'])}) for {s['streak']} counted weeks in a row, up to {week}."))
    if s["review_reason"]:
        due.append(("review", f"{TICKET_KINDS['review']} in {week}, decide bias vs sma",
                    f"The trial's review is due: {s['review_reason']}."))
    out = []
    for kind, title, why in due:
        if any(t.startswith(TICKET_KINDS[kind]) for t in titles):
            continue
        body = [why, "", f"- Weeks counted toward {TRIAL_WEEKS}: {len(s['counted'])} ({week_list(s['counted'])})",
                f"- d = bias − sma, replay net, mean of the counted weeks: {pp(s['mean_d'])}",
                f"- noise = mean |live − replay|: {pp(s['noise'])}",
                f"- Early-stop streak: {s['streak']} of {TRIAL_STREAK}", "", *trial_table(s), "",
                "Decide by the outcomes in `docs/direction-trial.md` (no difference / bias better / sma better; "
                "the live drawdown kill rule is in the weekly evidence reports): keep `bias` on stable, move it to "
                "`sma`, or extend the trial. A change of `gex_direction` is a config PR a person merges — no agent "
                "changes it.", "",
                "Scorecards: `stable/data/evidence/direction/<week>.md|json` (`cmd/evidence -compare-direction`). "
                "Percentages and counts only.", "",
                "_Filed by `scripts/agents/evidence.sh` (#440)._",
                f"<!-- direction-trial: {kind} {week} -->"]
        out.append((kind, title, "\n".join(body) + "\n"))
    return out


def write_tickets(week, stable_root, issues_path, out_dir):
    try:
        with open(issues_path) as f:
            issues = json.load(f)
    except (OSError, ValueError) as err:
        raise SystemExit(f"issues list unreadable ({err}): nothing filed")  # never file blind
    os.makedirs(out_dir, exist_ok=True)
    tickets = trial_tickets(week, stable_root, issues)
    for n, (kind, title, body) in enumerate(tickets, 1):
        with open(os.path.join(out_dir, f"{n}.title"), "w") as f:
            f.write(title)
        with open(os.path.join(out_dir, f"{n}.body"), "w") as f:
            f.write(body)
        print(f"{kind}: {title}")


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
    stable_root = os.path.join(envs_dir, "stable")
    first = f"{HEADER} — week {week} (`scripts/agents/evidence.sh`, #244)"
    if "stable" in envs and trial_weeks(week) and trial_state(stable_root, week)["first_shed"] == week:
        first += " · 🚩 **first GEX shed of the direction trial: review bias vs sma**"
    out = [first, "",
           "One row per environment × bot, from `cmd/evidence` (README \"Evidence report\"). "
           "Percentages are of the margin balance at the week's start; the replay gap is replay − live. "
           "Return on margin = net P&L ÷ mean IM used (IM % × the margin balance at the week's start, an approximation).", "",
           "Account vs booked P&L = the margin balance's change minus the journal's booked P&L, net of fees, in % of the starting balance "
           "(measured on the account; ⚠ over the tolerance, the report lists the possible causes, #566).", "",
           "E1 reliability = mean IM ÷ limit while entries were allowed against the 60 % target, "
           "the largest cause of idle slot time, and the startups whose first 30 minutes changed the book (restart-made).", "",
           "| env | bot | net P&L | return on margin | equity not explained by the bot | account vs booked P&L | excess vs holding the coin | worst 7 days | replay gap | stress | E1 reliability | missing days | report |",
           "|---|---|---|---|---|---|---|---|---|---|---|---|---|"]
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
                out.append(f"| {label} | {bot} | no report | | | | | | | | | | — |")
                continue
            cur = bot.upper()
            out.append("| " + " | ".join([
                label, bot, net_cell(r, live, cur), rom_cell(r), bridge_cell(r), recon_cell(r), excess_cell(r), worst_week_cell(r, live, cur),
                gap_cell(r, live, cur), stress_cell(r), e1_cell(r, live), missing_cell(r), f"`{env}/{rel[:-5]}.md`"]) + " |")
    if "stable" in envs:
        out += direction_block(stable_root, week)
    gaps = notices(week, envs_dir, bots, envs)
    if gaps:
        out += ["", "**Recorder gaps** (over 3 h in the week; fix the recorder — the report's \"Recorded-history coverage\" lists every missing hour):", ""]
        out += [f"- {g}" for g in gaps]
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
    elif len(argv) in (5, 6) and argv[1] == "gaps":
        bots = [b.strip().lower() for b in argv[4].split(",") if b.strip()]
        envs = tuple(argv[5].split(",")) if len(argv) == 6 else ("stable", "mvp")
        for line in notices(argv[2], argv[3], bots, envs):
            print(line)
    elif len(argv) in (6, 7) and argv[1] == "comment":
        bots = [b.strip().lower() for b in argv[5].split(",") if b.strip()]
        envs = tuple(argv[6].split(",")) if len(argv) == 7 else ("stable", "mvp")
        sys.stdout.write(comment(argv[2], argv[3], argv[4], bots, envs))
    elif len(argv) == 4 and argv[1] == "direction-weeks":
        for w in direction_weeks(argv[2], argv[3]):
            print(w)
    elif len(argv) == 6 and argv[1] == "tickets":
        write_tickets(argv[2], argv[3], argv[4], argv[5])
    else:
        raise SystemExit(__doc__)


if __name__ == "__main__":
    main(sys.argv)
