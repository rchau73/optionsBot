#!/usr/bin/env python3
"""Go / no-go scorecard for epic E5 (#305): the decision inputs PRD §6 names, side by side.

Reads the survey tally (survey_tally.py -json, #247), the fake-door result (fakedoor.py's
result.json, #303), the weekly evidence reports (cmd/evidence, data/evidence/<week>/<bot>.json)
and docs/legal/preliminary-answers.md, and prints one row per input: value, threshold, status
(meets / fails / missing), source file and date. A missing input stays `missing`, never assumed.
It recommends nothing beyond PRD §6's own kill rule. Percentages only — no coin or USD amount.
Rows, thresholds and windows: docs/product/go-no-go.md.

    python3 scripts/product/go_no_go.py
    python3 scripts/product/go_no_go.py --tally data/product/interviews/tally.json \\
        --fakedoor data/product/fakedoor/run1/result.json --evidence-root ~/dev/go/optionsBot-env/stable --bot btc
    python3 scripts/product/go_no_go.py -json

Beside the rows, one informational line shows the interviews' delegation share (interview_tally.py, #787);
it never enters the overall line.

    python3 scripts/product/go_no_go.py --reading    # sections 7–8 of the page, to stdout only (#886)
"""
import argparse
import csv
import glob
import json
import math
import os
import re
import sys
from datetime import datetime, timezone

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import interview_tally  # noqa: E402

REPO = os.path.dirname(os.path.dirname(HERE))

# PRD §6 kill criteria, quoted (changing them is a PRD pull request, not a code change).
SURVEY_KILL_PCT = 30.0      # < 30 % of interviewed cycle holders would let software trade their account
E2_MIN_WEEKS = 12           # go-no-go.md §3: weekly reports needed before the hold-the-coin row is read
NO_SPREAD = 1e-9            # a standard deviation at or under this has no ratio (internal/evidence noSpread)
LEGAL_GATE = ("L1", "L2", "L3", "L4", "L5", "L6", "L9")

MEETS, FAILS, MISSING, INFO = "meets", "fails", "missing", "info"
WEEK_RE = re.compile(r"^\d{4}-W\d{2}$")


def row(name, value, threshold, status, source, date, note=""):
    return {"input": name, "value": value, "threshold": threshold, "status": status,
            "source": source, "date": date, "note": note}


def file_date(path):
    """The file's modification date, UTC, YYYY-MM-DD."""
    return datetime.fromtimestamp(os.path.getmtime(path), timezone.utc).date().isoformat()


def load_json(path):
    """The parsed file, or (None, why) when it is absent or unreadable."""
    if not path or not os.path.isfile(path):
        return None, "no file"
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f), ""
    except (OSError, json.JSONDecodeError) as e:
        return None, f"unreadable: {e}"


def fmt_pct(v):
    return f"{v:.1f} %"


# --- rows ---------------------------------------------------------------------

def survey_row(path):
    """(1) PRD §6: share of cycle holders who would let software trade their account (Q6)."""
    name, threshold = "Survey: cycle holders who would delegate", f"≥ {SURVEY_KILL_PCT:.0f} % (kill if under)"
    data, why = load_json(path)
    if data is None:
        return row(name, "—", threshold, MISSING, path or "—", "—", why)
    d = (data.get("delegation") or {}).get("cycle holder") or {}
    share = d.get("share_pct")
    if share is None:
        return row(name, "—", threshold, MISSING, path, file_date(path), "no cycle holder answered Q6")
    note = f"n = {d.get('n', 0)}"
    if d.get("small_sample"):
        note += " (small sample)"
    missing = (data.get("quota_progress") or {}).get("cycle_holders_missing")
    if missing:
        note += f"; {missing} more cycle holders needed"
    status = FAILS if share < SURVEY_KILL_PCT else MEETS
    return row(name, fmt_pct(share), threshold, status, path, file_date(path), note)


def fakedoor_row(path):
    """(2) The fake-door outcome: Success meets, Kill or Iterate fails, not run or too little traffic is missing."""
    name, threshold = "Fake door outcome", "Success (fake-door brief §6)"
    data, why = load_json(path)
    if data is None:
        return row(name, "not run", threshold, MISSING, path or "—", "—", why)
    outcome = data.get("outcome")
    rate = (data.get("overall") or {}).get("rate")
    note = f"sign-up rate {fmt_pct(100 * rate)}" if isinstance(rate, (int, float)) else ""
    if outcome == "Success":
        status = MEETS
    elif outcome in ("Kill", "Iterate"):
        status = FAILS
    else:  # "Not enough traffic" or anything unknown: no verdict on demand
        status = MISSING
    return row(name, outcome or "—", threshold, status, path, file_date(path), note)


def weekly_reports(root, bot):
    """Every weekly evidence report of bot under <root>/data/evidence/<ISO week>/, oldest first."""
    out = []
    for path in sorted(glob.glob(os.path.join(root, "data", "evidence", "*", bot + ".json"))):
        week = os.path.basename(os.path.dirname(path))
        if not WEEK_RE.match(week):
            continue
        data, _ = load_json(path)
        if data is not None and data.get("week") == week:
            out.append((week, path, data))
    return out


def sharpe(values):
    """Mean ÷ sample standard deviation; None under two values or with no spread.
    The same calculation as internal/evidence.SharpeRatio (the weekly report's scoreboard, #537),
    pinned to it by tests/testdata/scoreboard/cases.json."""
    if len(values) < 2:
        return None
    mean = sum(values) / len(values)
    sd = math.sqrt(sum((v - mean) ** 2 for v in values) / (len(values) - 1))
    return mean / sd if sd > NO_SPREAD else None  # identical returns differ only by rounding


def e2_row(root, bot, min_weeks=E2_MIN_WEEKS):
    """(3) PRD §6: does the engine beat holding the coin on a risk-adjusted basis (go-no-go.md §3)."""
    name = "E2: engine vs holding the coin (risk-adjusted)"
    threshold = f"engine's weekly Sharpe > hold's, over the last {min_weeks} weeks"
    reports = weekly_reports(root, bot)
    usable = [(w, p, d) for w, p, d in reports
              if (d.get("hold") or {}).get("known") and (d.get("hold") or {}).get("strategy_known")]
    src = os.path.join(root, "data", "evidence", "<week>", bot + ".json")
    if len(usable) < min_weeks:
        return row(name, "not enough weeks", threshold, MISSING, src, usable[-1][0] if usable else "—",
                   f"{len(usable)} of {min_weeks} weeks with hold and strategy returns")
    window = usable[-min_weeks:]
    engine = [d["hold"]["return_usd_pct"] for _, _, d in window]
    hold = [d["hold"]["hold_return_usd_pct"] for _, _, d in window]
    s_engine, s_hold = sharpe(engine), sharpe(hold)
    drawdowns = [((d.get("live") or {}).get("perf") or {}).get("MaxDrawdownPct") for _, _, d in window]
    drawdowns = [x for x in drawdowns if isinstance(x, (int, float))]
    note = f"weeks {window[0][0]} to {window[-1][0]}"
    if drawdowns:
        note += f"; worst weekly drawdown {fmt_pct(min(drawdowns))} of equity"
    latest = window[-1][1]
    if s_engine is None or s_hold is None:
        return row(name, "no spread", threshold, MISSING, latest, window[-1][0],
                   note + "; a return series with no spread has no Sharpe")
    value = f"engine {s_engine:.2f} vs hold {s_hold:.2f}"
    return row(name, value, threshold, MEETS if s_engine > s_hold else FAILS, latest, window[-1][0], note)


def legal_status(text, qid):
    """'counsel' when the question's line carries counsel's answer, 'preliminary' when it does not,
    None when the question is not in the table."""
    for line in text.splitlines():
        if line.startswith(f"| **{qid}**"):
            return "counsel" if "**Counsel (" in line else "preliminary"
    return None


def legal_row(path):
    """(4) The legal gate: L1–L6 and L9 answered by counsel (preliminary-answers.md)."""
    name, threshold = "Legal gate (L1–L6, L9)", "every question answered by counsel"
    try:
        with open(path, encoding="utf-8") as f:
            text = f.read()
    except OSError as e:
        return row(name, "—", threshold, MISSING, path, "—", f"unreadable: {e}")
    statuses = {q: legal_status(text, q) for q in LEGAL_GATE}
    answered = [q for q, s in statuses.items() if s == "counsel"]
    pending = [q for q, s in statuses.items() if s != "counsel"]
    value = f"{len(answered)} of {len(LEGAL_GATE)} answered by counsel"
    note = "preliminary: " + ", ".join(pending) if pending else "read counsel's answers before deciding"
    return row(name, value, threshold, MEETS if not pending else MISSING, path, file_date(path), note)


def interview_row(path, survey):
    """Informational: the interviews' delegation share beside the survey's. It never enters the overall line
    (PRD §6 reads the survey and E2), and a missing or refused file is `missing`, not a failure (#787)."""
    name, threshold = "Interviews: delegation share (informational)", "none; the PRD §6 kill rule reads row 1"
    if not path or not os.path.isfile(path):
        return row(name, "—", threshold, MISSING, path or "—", "—", "no interview sheet")
    try:
        with open(path, encoding="utf-8-sig", newline="") as f:
            t = interview_tally.tally(interview_tally.read_rows(f))
    except (interview_tally.TallyError, OSError, csv.Error) as e:
        return row(name, "—", threshold, MISSING, path, file_date(path), f"refused: {e}")
    d = t["delegation"]
    if d["share_pct"] is None:
        return row(name, "—", threshold, MISSING, path, file_date(path), "no qualified interview coded a delegation answer")
    note = f"{t['qualified']} of ~{t['target']} qualified interviews"
    if d["small_sample"]:
        note += " (small sample)"
    if survey["value"] != "—":
        note += f"; survey {survey['value']}"
    return row(name, f"{fmt_pct(d['share_pct'])} (n = {d['n']})", threshold, INFO, path, file_date(path), note)


# --- the scorecard ------------------------------------------------------------

def overall(rows):
    """PRD §6's kill rule, nothing more: either kill input failing → kill criterion met;
    else any missing input → incomplete; else every input is in and the decision is the owner's."""
    survey, _, e2, _ = rows  # the four decision inputs; the interview line is not among them
    killed = [r for r in (survey, e2) if r["status"] == FAILS]
    if killed:
        return ("kill criterion met", "PRD §6: " + "; ".join(f"{r['input']} {r['value']} ({r['source']})"
                                                           for r in killed))
    missing = [r["input"] for r in rows if r["status"] == MISSING]
    if missing:
        return "incomplete", "missing: " + ", ".join(missing)
    return "no kill criterion met", "every input is in; the go / no-go decision is the product owner's"


def build(tally, fakedoor, evidence_root, bot, legal, interviews=None):
    rows = [survey_row(tally), fakedoor_row(fakedoor), e2_row(evidence_root, bot), legal_row(legal)]
    verdict, why = overall(rows)
    return {"rows": rows, "informational": [interview_row(interviews, rows[0])],
            "overall": verdict, "overall_reason": why,
            "thresholds": {"survey_kill_pct": SURVEY_KILL_PCT, "e2_min_weeks": E2_MIN_WEEKS,
                           "legal_gate": list(LEGAL_GATE)}}


def render_rows(rows):
    out = ["| Input | Value | Threshold | Status | Source | Date | Note |", "|---|---|---|---|---|---|---|"]
    for r in rows:
        out.append(f"| {r['input']} | {r['value']} | {r['threshold']} | {r['status']} | `{r['source']}` "
                   f"| {r['date']} | {r['note']} |")
    return out


def render(card):
    out = ["# Go / no-go scorecard (E5)", ""] + render_rows(card["rows"])
    out += ["", "Informational (never decides):", ""] + render_rows(card["informational"])
    out += ["", f"**Overall: {card['overall']}** — {card['overall_reason']}", ""]
    return "\n".join(out)


# --- the reading (#886): sections 7 and 8 of docs/product/go-no-go.md, from the same sources ---

EVIDENCE, ASSUMPTION, NO_DATA = "evidence", "assumption", "no data"
SMALL_SAMPLE = 15  # the survey tally's small-sample line (survey_tally.py)

FIRST_STEP = {
    "buyer": "Send the survey (discovery/survey.md); run survey_tally.py -json",
    "pain": "Code the survey's Q4 and F2 answers in discovery/synthesis.md §2",
    "delegation": "Send the survey; run survey_tally.py -json; rerun the scorecard",
    "wtp": "Read the survey's price questions and the fake door; decide after counsel answers L8",
    "demand": "Publish the fake door after counsel clears L5, L7, L9 and L13; run fakedoor.py",
    "competitors": "Save one entry per competitor in data/product/competitors/; run competitors.py --summary",
    "legal": "Get counsel's written answers to L1–L6 and L9; record them as in docs/legal/counsel-engagement.md",
    "e2": "Let the weekly reports reach the 12-week window; read the scorecard's row 3",
}


def competitors_count(directory):
    """(entries, newest date) of the competitor YAML files; names and contents are never read."""
    files = sorted(glob.glob(os.path.join(directory, "*.yaml")) + glob.glob(os.path.join(directory, "*.yml")))
    return len(files), (max(file_date(f) for f in files) if files else "—")


def reading_line(criterion, reading, value, status, source, date, step, note=""):
    """One reading row. The source is a file name only; a first step is given unless the reading is evidence."""
    shown = "—" if source in (None, "", "—") else os.path.basename(source)
    return {"criterion": criterion, "reading": reading, "value": value, "status": status,
            "source": shown, "date": date, "first_step": "" if reading == EVIDENCE else step, "note": note}


def build_reading(tally, fakedoor, evidence_root, bot, legal, competitors_dir):
    """Both tables of the reading, as data. Every private input that is absent reads `no data`."""
    survey = survey_row(tally)
    data, _ = load_json(tally)
    have_tally = data is not None
    src = tally if have_tally else None
    d = ((data or {}).get("delegation") or {}).get("cycle holder") or {}
    n = d.get("n") or 0
    survey_note = survey["note"].replace("(small sample)", "").strip(" ;")
    if d.get("share_pct") is not None and n < SMALL_SAMPLE:
        survey_note = f"sample under {SMALL_SAMPLE}; " + survey_note
    fd = fakedoor_row(fakedoor)
    if not fakedoor or not os.path.isfile(fakedoor):
        fd_reading = NO_DATA
    else:
        fd_reading = EVIDENCE if fd["status"] != MISSING else ASSUMPTION
    e2 = e2_row(evidence_root, bot)
    lg = legal_row(legal)
    n_comp, comp_date = competitors_count(competitors_dir)
    deleg = reading_line("Delegation", NO_DATA if survey["status"] == MISSING else EVIDENCE, survey["value"],
                         survey["status"], survey["source"], survey["date"], FIRST_STEP["delegation"], survey_note)
    e2_line = reading_line("E2: engine vs holding the coin", NO_DATA if e2["status"] == MISSING else EVIDENCE,
                           e2["value"], e2["status"], e2["source"], e2["date"], FIRST_STEP["e2"], e2["note"])
    criteria = [
        reading_line("Buyer: the cycle holder", EVIDENCE if n else NO_DATA,
                     f"{n} cycle holders in the tally" if n else "—", "—", src, survey["date"], FIRST_STEP["buyer"]),
        reading_line("Pain", ASSUMPTION if have_tally else NO_DATA, "—", "—", src, survey["date"],
                     FIRST_STEP["pain"], "the tally does not code pain"),
        deleg,
        reading_line("Willingness to pay", ASSUMPTION if have_tally else NO_DATA, "—", "—", src,
                     survey["date"], FIRST_STEP["wtp"], "options only, no customer answer read"),
        reading_line("Demand signal (fake door)", fd_reading, fd["value"], fd["status"], fakedoor, fd["date"],
                     FIRST_STEP["demand"], fd["note"]),
        reading_line("Competitors", EVIDENCE if n_comp else NO_DATA, f"{n_comp} entries" if n_comp else "—", "—",
                     os.path.join(competitors_dir, "entries") if n_comp else None, comp_date, FIRST_STEP["competitors"]),
        reading_line("Legal gate (L1–L6, L9)", EVIDENCE if lg["status"] == MEETS else ASSUMPTION, lg["value"],
                     lg["status"], legal, lg["date"], FIRST_STEP["legal"], lg["note"]),
        e2_line,
    ]
    kill = []
    for text, line in (("Fewer than 30 % of interviewed cycle holders would let software trade their account", deleg),
                       ("E2 shows the engine does not beat holding the coin on a risk-adjusted basis", e2_line)):
        # the first step is for a reading that is not in yet; a kill row with a verdict needs none
        step = FIRST_STEP["delegation"] if line is deleg else FIRST_STEP["e2"]
        kill.append({"kill": text, "status": line["status"], "value": line["value"], "source": line["source"],
                     "date": line["date"], "first_step": step if line["status"] == MISSING else "",
                     "note": line["note"]})
    return {"criteria": criteria, "kill": kill}


def render_reading(rd):
    def cell(v):
        return (str(v).replace("|", "/") or "—") if v != "" else "—"
    out = ["## Each go-to-market criterion: evidence or assumption", "",
           "| Criterion | Reading | Value | Scorecard status | Source | Date | First step | Note |",
           "|---|---|---|---|---|---|---|---|"]
    for r in rd["criteria"]:
        out.append("| " + " | ".join(cell(r[k]) for k in (
            "criterion", "reading", "value", "status", "source", "date", "first_step", "note")) + " |")
    out += ["", "## Kill criteria and the current reading", "",
            "| Kill criterion (PRD §6) | Status | Value | Source | Date | First step | Note |",
            "|---|---|---|---|---|---|---|"]
    for r in rd["kill"]:
        out.append("| " + " | ".join(cell(r[k]) for k in (
            "kill", "status", "value", "source", "date", "first_step", "note")) + " |")
    return "\n".join(out) + "\n"


def latest_fakedoor(private):
    """The newest result.json under data/product/fakedoor/, or None."""
    found = glob.glob(os.path.join(private, "fakedoor", "*", "result.json"))
    return max(found, key=os.path.getmtime) if found else None


def main(argv=None):
    private = os.path.join(REPO, "data", "product")
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--tally", default=os.path.join(private, "interviews", "tally.json"),
                    help="survey_tally.py -json output")
    ap.add_argument("--interviews", default=os.path.join(private, "interviews", "interviews.csv"),
                    help="the coded interview sheet read by interview_tally.py")
    ap.add_argument("--fakedoor", default=None, help="fakedoor.py result.json (default: the newest run's)")
    ap.add_argument("--evidence-root", default=REPO, help="an environment clone holding data/evidence/")
    ap.add_argument("--bot", default="btc")
    ap.add_argument("--legal", default=os.path.join(REPO, "docs", "legal", "preliminary-answers.md"))
    ap.add_argument("--reading", action="store_true",
                    help="print sections 7–8 of go-no-go.md (criteria; kill criteria) from the sources, to stdout")
    ap.add_argument("-json", action="store_true", help="print JSON instead of markdown")
    args = ap.parse_args(argv)
    fakedoor = args.fakedoor if args.fakedoor is not None else latest_fakedoor(private)
    if args.reading:
        rd = build_reading(args.tally, fakedoor, args.evidence_root, args.bot, args.legal,
                           os.path.join(private, "competitors"))
        print(json.dumps(rd, indent=2, ensure_ascii=False) if args.json else render_reading(rd), end="")
        return 0
    card = build(args.tally, fakedoor, args.evidence_root, args.bot, args.legal, args.interviews)
    print(json.dumps(card, indent=2, ensure_ascii=False) if args.json else render(card))
    return 0


if __name__ == "__main__":
    sys.exit(main())
