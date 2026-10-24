#!/usr/bin/env python3
"""Result of the fake-door landing page (#303, epic E5).

Turns a run's private waiting-list export into the result section (§10) of
docs/product/experiments/fake-door-landing.md, applying the brief's §6 decision
rule exactly as written — the same way every time, from the raw counts.

    python3 scripts/product/fakedoor.py data/product/fakedoor/run1
    python3 scripts/product/fakedoor.py data/product/fakedoor/run1 --survey-share 0.34

The run directory holds two CSV files:
    signups.csv  submitted_at, confirmed (yes/no), channel, country, first_bought,
                 kept_most (yes/no), first_wanted, fair_pay — one row per submission
    visits.csv   date (YYYY-MM-DD), channel, visits

Only those columns are read: any other (an `email` column, say) is ignored and
never printed. The result is written beside the input as result.md and
result.json; the run directory must be inside the gitignored data/product/.

Definitions (brief §4):
    rate                 confirmed sign-ups ÷ visits, with its Wilson 95 % interval
    cycle-holder share   confirmed sign-ups that first bought before 2022 and kept most
                         ÷ confirmed sign-ups that answered both questions
    confirmation rate    confirmed ÷ submitted
    US sign-ups          submissions from the United States (the residence check should
                         have refused every one: a guardrail, printed first)

Standard library only. Nothing is published: the tool reads and writes files.
"""
import argparse
import csv
import json
import math
import os
import sys
from datetime import date

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
PRIVATE_DIR = os.path.join(REPO, "data", "product")

# Thresholds copied from the brief (§5–§6). Fixed before any visit; never moved during a run.
MIN_VISITS = 400                # §5 sample
MAX_WEEKS = 4                   # §5 duration: stop at 4 weeks whatever the count
SUCCESS_RATE = 0.10             # §6 Success: rate ≥ 10 % ...
SUCCESS_LOWER_BOUND = 0.06      # ... and the interval's lower bound ≥ 6 %
CYCLE_HOLDER_MIN_SHARE = 0.5    # ... and cycle holders ≥ half of those that answered
KILL_RATE = 0.03                # §6 Kill: rate < 3 % ...
KILL_UPPER_BOUND = 0.06         # ... and the interval's upper bound < 6 %
SURVEY_KILL_SHARE = 0.30        # PRD §6: < 30 % of interviewed cycle holders would let software trade
Z95 = 1.959963984540054

SUCCESS, ITERATE, KILL, NOT_ENOUGH = "Success", "Iterate", "Kill", "Not enough traffic"

SIGNUP_COLUMNS = ("submitted_at", "confirmed", "channel", "country",
                  "first_bought", "kept_most", "first_wanted", "fair_pay")
VISIT_COLUMNS = ("date", "channel", "visits")
US_NAMES = {"us", "usa", "u.s.", "u.s.a.", "united states", "united states of america"}
NO_CHANNEL = "(none)"


class ExportError(ValueError):
    """The export cannot be read as given."""


# --- pure rules ---------------------------------------------------------------

def wilson(successes, n, z=Z95):
    """Wilson score interval (low, high) of successes ÷ n; None when n is 0."""
    if n <= 0:
        return None
    p = successes / n
    denom = 1 + z * z / n
    centre = (p + z * z / (2 * n)) / denom
    half = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / denom
    return max(0.0, centre - half), min(1.0, centre + half)


def outcome(visits, confirmed, cycle_holders, answered_both):
    """The §6 outcome and why, from the raw counts. Not enough traffic wins over any rate."""
    if visits < MIN_VISITS:
        return NOT_ENOUGH, f"{visits} visits, fewer than the {MIN_VISITS} of the sample (§5): no verdict on demand"
    rate = confirmed / visits
    low, high = wilson(confirmed, visits)
    if rate >= SUCCESS_RATE and low >= SUCCESS_LOWER_BOUND:
        if answered_both == 0:
            return ITERATE, "the rate meets Success but nobody answered both segment questions"
        share = cycle_holders / answered_both
        if share >= CYCLE_HOLDER_MIN_SHARE:
            return SUCCESS, (f"rate {pct(rate)} ≥ {pct(SUCCESS_RATE)}, lower bound {pct(low)} ≥ "
                             f"{pct(SUCCESS_LOWER_BOUND)}, cycle holders {pct(share)} ≥ half")
        return ITERATE, f"the rate meets Success but cycle holders are {pct(share)}, under half"
    if rate < KILL_RATE and high < KILL_UPPER_BOUND:
        return KILL, f"rate {pct(rate)} < {pct(KILL_RATE)}, upper bound {pct(high)} < {pct(KILL_UPPER_BOUND)}"
    return ITERATE, f"rate {pct(rate)} (interval {pct(low)}–{pct(high)}) is between Kill and Success"


def survey_sentence(result, survey_share):
    """The Kill row's reading with the survey (§6); None when the outcome is not Kill."""
    if result != KILL:
        return None
    if survey_share is None:
        return "survey share not given (--survey-share): PRD §6's kill criterion cannot be read yet"
    if survey_share < SURVEY_KILL_SHARE:
        return (f"survey share {pct(survey_share)} < {pct(SURVEY_KILL_SHARE)}: PRD §6's kill criterion is met "
                "→ no-go on the hosted product")
    return (f"survey share {pct(survey_share)} ≥ {pct(SURVEY_KILL_SHARE)}: PRD §6's kill criterion is not met — "
            "the page, not the demand, is the suspect: one rewrite, then decide")


def pct(x, digits=1):
    return "—" if x is None else f"{x * 100:.{digits}f} %"


# --- parsing ------------------------------------------------------------------

def yes_no(where, value):
    v = value.strip().lower()
    if v in ("yes", "y", "true", "1"):
        return True
    if v in ("no", "n", "false", "0"):
        return False
    raise ExportError(f"{where}: expected yes or no, got {value!r}")


def bought_before_2022(where, value):
    """True for "before 2022" or a year ≤ 2021; False for "2022 or later" or a later year."""
    v = value.strip().lower().replace("_", " ").replace("-", " ")
    if v.isdigit() and len(v) == 4:
        return int(v) < 2022
    if v.startswith("before"):
        return True
    if v.startswith("2022") or v.startswith("after") or v.startswith("since"):
        return False
    raise ExportError(f"{where}: expected a year, \"before 2022\" or \"2022 or later\", got {value!r}")


def _rows(path, columns):
    with open(path, encoding="utf-8-sig", newline="") as f:
        reader = csv.DictReader(f)
        header = [h.strip() for h in (reader.fieldnames or [])]
        missing = [c for c in columns if c not in header]
        if missing:
            raise ExportError(f"{os.path.basename(path)}: missing column(s) {', '.join(missing)}")
        reader.fieldnames = header
        for num, row in enumerate(reader, 2):
            # Only the named columns leave this function: an e-mail column never does.
            yield num, {c: (row.get(c) or "").strip() for c in columns}


def read_signups(path):
    name = os.path.basename(path)
    out = []
    for num, r in _rows(path, SIGNUP_COLUMNS):
        where = f"{name} line {num}"
        bought, kept = r["first_bought"], r["kept_most"]
        out.append({
            "confirmed": yes_no(f"{where} confirmed", r["confirmed"]),
            "channel": r["channel"] or NO_CHANNEL,
            "us": r["country"].lower() in US_NAMES,
            "before_2022": bought_before_2022(f"{where} first_bought", bought) if bought else None,
            "kept_most": yes_no(f"{where} kept_most", kept) if kept else None,
            "first_wanted": r["first_wanted"],
            "fair_pay": r["fair_pay"],
        })
    return out


def read_visits(path):
    name = os.path.basename(path)
    out = []
    for num, r in _rows(path, VISIT_COLUMNS):
        where = f"{name} line {num}"
        try:
            day = date.fromisoformat(r["date"])
        except ValueError as e:
            raise ExportError(f"{where} date: expected YYYY-MM-DD, got {r['date']!r}") from e
        try:
            n = int(r["visits"])
        except ValueError as e:
            raise ExportError(f"{where} visits: expected a whole number, got {r['visits']!r}") from e
        if n < 0:
            raise ExportError(f"{where} visits: negative")
        out.append({"date": day, "channel": r["channel"] or NO_CHANNEL, "visits": n})
    return out


# --- the result ---------------------------------------------------------------

def _rate(successes, n):
    iv = wilson(successes, n)
    return {"visits": n, "confirmed": successes,
            "rate": successes / n if n else None,
            "low": iv[0] if iv else None, "high": iv[1] if iv else None}


def _shares(values):
    """Share of each answer among those that answered, most chosen first."""
    answered = [v for v in values if v]
    counts = {}
    for v in answered:
        counts[v] = counts.get(v, 0) + 1
    return {"answered": len(answered),
            "options": [{"answer": k, "count": c, "share": c / len(answered)}
                        for k, c in sorted(counts.items(), key=lambda kv: (-kv[1], kv[0]))]}


def build(signups, visits, survey_share=None):
    """The §10 result as a dict, from parsed rows (see read_signups/read_visits)."""
    confirmed = [s for s in signups if s["confirmed"]]
    total_visits = sum(v["visits"] for v in visits)
    answered = [s for s in confirmed if s["before_2022"] is not None and s["kept_most"] is not None]
    holders = [s for s in answered if s["before_2022"] and s["kept_most"]]

    channels = sorted({v["channel"] for v in visits} | {s["channel"] for s in confirmed})
    per_channel = {}
    for ch in channels:
        per_channel[ch] = _rate(sum(1 for s in confirmed if s["channel"] == ch),
                                sum(v["visits"] for v in visits if v["channel"] == ch))

    days = sorted({v["date"] for v in visits})
    span_days = (days[-1] - days[0]).days + 1 if days else 0
    result, why = outcome(total_visits, len(confirmed), len(holders), len(answered))
    us = sum(1 for s in signups if s["us"])
    return {
        "guardrail_us_signups": us,
        "run": {"first_day": days[0].isoformat() if days else None,
                "last_day": days[-1].isoformat() if days else None,
                "days": span_days},
        "overall": _rate(len(confirmed), total_visits),
        "per_channel": per_channel,
        "cycle_holders": {"holders": len(holders), "answered_both": len(answered),
                          "share": len(holders) / len(answered) if answered else None},
        "first_wanted": _shares([s["first_wanted"] for s in confirmed]),
        "fair_pay": _shares([s["fair_pay"] for s in confirmed]),
        "confirmation": {"submitted": len(signups), "confirmed": len(confirmed),
                         "rate": len(confirmed) / len(signups) if signups else None},
        "survey_share": survey_share,
        "outcome": result,
        "outcome_reason": why,
        "survey_reading": survey_sentence(result, survey_share),
        "notes": _notes(result, span_days),
        "thresholds": thresholds(),
    }


def _notes(result, span_days):
    notes = []
    if result == NOT_ENOUGH and span_days < MAX_WEEKS * 7:
        notes.append(f"the run covers {span_days} days, under {MAX_WEEKS} weeks: keep it running to "
                     f"{MAX_WEEKS} weeks before reading this as a finding (§5)")
    if result == NOT_ENOUGH:
        notes.append("the interval is wide at this sample; it is printed as it is")
    return notes


def thresholds():
    return {"min_visits": MIN_VISITS, "max_weeks": MAX_WEEKS, "success_rate": SUCCESS_RATE,
            "success_lower_bound": SUCCESS_LOWER_BOUND, "cycle_holder_min_share": CYCLE_HOLDER_MIN_SHARE,
            "kill_rate": KILL_RATE, "kill_upper_bound": KILL_UPPER_BOUND,
            "survey_kill_share": SURVEY_KILL_SHARE}


def _rate_text(r):
    if r["rate"] is None:
        return f"{r['confirmed']} sign-ups, no visits counted"
    return f"{pct(r['rate'])} (Wilson 95 %: {pct(r['low'])}–{pct(r['high'])})"


def render_markdown(res):
    lines = []
    if res["guardrail_us_signups"]:
        lines += [f"**GUARDRAIL: {res['guardrail_us_signups']} sign-up(s) from the United States got through the "
                  "residence check — stop the run now and tell the product owner (brief §7).**", ""]
    run = res["run"]
    o, c, ch = res["overall"], res["confirmation"], res["cycle_holders"]
    lines += [
        "## 10. Result",
        "",
        "_Computed by `scripts/product/fakedoor.py` (#303) from the run's private export; counts and shares only._",
        "",
        f"- **Run:** {run['first_day'] or '—'} → {run['last_day'] or '—'} ({run['days']} days)",
        f"- **Visits:** {o['visits']}",
        f"- **Confirmed sign-ups:** {o['confirmed']}",
        f"- **Rate:** {_rate_text(o)}",
        f"- **Cycle-holder share:** {pct(ch['share'])} ({ch['holders']} of {ch['answered_both']} that answered both)",
        f"- **Confirmation rate:** {pct(c['rate'])} ({c['confirmed']} of {c['submitted']} submitted)",
        f"- **US sign-ups that got through:** {res['guardrail_us_signups']}",
        f"- **Survey's kill-criterion share:** {pct(res['survey_share'])}",
        f"- **Outcome (§6): {res['outcome']}** — {res['outcome_reason']}",
    ]
    if res["survey_reading"]:
        lines.append(f"- **With the survey:** {res['survey_reading']}")
    lines += [f"- _Note:_ {n}" for n in res["notes"]]
    lines += ["", "### Per channel", "", "| Channel | Visits | Confirmed | Rate (Wilson 95 %) |", "|---|---:|---:|---|"]
    for name, r in res["per_channel"].items():
        lines.append(f"| {name} | {r['visits']} | {r['confirmed']} | {_rate_text(r)} |")
    for key, title in (("first_wanted", "First thing wanted"), ("fair_pay", "Fair way to pay")):
        s = res[key]
        lines += ["", f"### {title} ({s['answered']} answered)", ""]
        if not s["options"]:
            lines.append("No answers.")
            continue
        lines += ["| Answer | Count | Share |", "|---|---:|---:|"]
        lines += [f"| {opt['answer']} | {opt['count']} | {pct(opt['share'])} |" for opt in s["options"]]
    lines += ["", "Decision taken by the product owner: _to fill_ · What we learned: _to fill_", ""]
    return "\n".join(lines)


def check_run_dir(path, private_dir=PRIVATE_DIR):
    """Returns the absolute run directory if it lies inside data/product/, else raises."""
    target = os.path.realpath(os.path.abspath(path))
    root = os.path.realpath(private_dir)
    if os.path.commonpath([target, root]) != root or target == root:
        raise ExportError(f"{path}: refused — the export and its result stay in {root}/ (gitignored)")
    return target


def main(argv=None, private_dir=PRIVATE_DIR):
    ap = argparse.ArgumentParser(description="Fake-door result (brief §10) from a private waiting-list export.")
    ap.add_argument("run_dir", help="run directory inside data/product/, e.g. data/product/fakedoor/run1")
    ap.add_argument("--signups", default="signups.csv", help="sign-ups file in the run directory")
    ap.add_argument("--visits", default="visits.csv", help="visits file in the run directory")
    ap.add_argument("--survey-share", type=float, help="survey's kill-criterion share as a fraction 0–1 (#247)")
    args = ap.parse_args(argv)
    try:
        if args.survey_share is not None and not 0 <= args.survey_share <= 1:
            raise ExportError(f"--survey-share {args.survey_share}: expected a fraction between 0 and 1")
        run = check_run_dir(args.run_dir, private_dir)
        res = build(read_signups(os.path.join(run, args.signups)),
                    read_visits(os.path.join(run, args.visits)), args.survey_share)
    except (ExportError, OSError) as e:
        print(f"fakedoor: {e}", file=sys.stderr)
        return 2
    md, js = os.path.join(run, "result.md"), os.path.join(run, "result.json")
    with open(md, "w", encoding="utf-8") as f:
        f.write(render_markdown(res))
    with open(js, "w", encoding="utf-8") as f:
        json.dump(res, f, indent=2, ensure_ascii=False)
        f.write("\n")
    if res["guardrail_us_signups"]:
        print(f"GUARDRAIL: {res['guardrail_us_signups']} US sign-up(s) got through — stop the run", file=sys.stderr)
    print(f"outcome: {res['outcome']} — wrote {md} and {js}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
