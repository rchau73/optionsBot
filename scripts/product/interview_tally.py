#!/usr/bin/env python3
"""Interview tally (#787, epic E5): the coded interview notes → counts and shares.

Reads data/product/interviews/interviews.csv (gitignored), one row per interview, coded as the
"Coding sheet" of docs/product/discovery/interview-guide.md says, and prints:

    qualified interviews against the ~15 target (a sample under 15 is noted, never hidden)
    delegation share among the qualified (survey Q6's three answers) with its n
    pain-code counts and shares (a pain counts only when pain_happened is yes, synthesis.md §2)
    willingness-to-pay bucket shares and testnet-trial shares

    python3 scripts/product/interview_tally.py
    python3 scripts/product/interview_tally.py data/product/interviews/interviews.csv -json

Standard library only. The file holds participant codes (I01…) and closed codes, never a name, an
amount or private text: a row with an unknown code, a missing or extra column, a duplicate
participant, or a cell that looks like an e-mail address or a currency amount is refused (exit 2),
naming the row and the column, and the offending cell is never printed. Percentages and counts only.
"""
import argparse
import csv
import json
import os
import re
import sys

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
DEFAULT_CSV = os.path.join(REPO, "data", "product", "interviews", "interviews.csv")

TARGET = 15  # screener.md §1: ~15 qualified people

COLUMNS = ("participant", "date", "qualified", "delegation", "pain_codes", "pain_happened",
           "would_pay_bucket", "trusted_testnet_trial")

# The closed sets of the interview guide's coding sheet (a test keeps the guide and these equal).
YES_NO = ("yes", "no")
DELEGATION = ("yes still do", "yes took it back", "no")  # survey Q6's three answers
DELEGATED = DELEGATION[:2]
PAIN_CODES = ("opaque_yield", "no_risk_control", "too_complex", "surprise_on_account", "fees", "other")
WOULD_PAY = ("nothing", "subscription", "per_account", "unsure")
TRIAL = ("yes", "no", "unsure")

PARTICIPANT = re.compile(r"I\d{2,3}")
DATE = re.compile(r"\d{4}-\d{2}-\d{2}")
EMAIL = re.compile(r"\S+@\S+")
CURRENCY = r"(?:usd|eur|brl|gbp|btc|eth|reais|dollars?|euros?|k|m)"
AMOUNT = re.compile(rf"[$€£]|\bR\$|\d\s*{CURRENCY}\b|\b{CURRENCY}\s*\d", re.I)


class TallyError(ValueError):
    """The sheet cannot be tallied as given. The message never carries a cell's content."""


# --- input --------------------------------------------------------------------

def check_personal(line, column, value):
    """Refuse a cell that looks like an e-mail address or a currency amount (the cell is not echoed)."""
    if EMAIL.search(value):
        raise TallyError(f"row {line}, column {column}: looks like an e-mail address — the sheet holds codes only")
    if AMOUNT.search(value):
        raise TallyError(f"row {line}, column {column}: looks like an amount — the sheet holds no amounts")


def check_code(line, column, value, allowed, required=False):
    if not value:
        if required:
            raise TallyError(f"row {line}, column {column}: empty")
        return
    if value not in allowed:
        raise TallyError(f"row {line}, column {column}: not one of {', '.join(allowed)}")


def parse_row(line, raw):
    """One validated row: {column: text}, with pain_codes as a list."""
    row = {c: (raw.get(c) or "").strip() for c in COLUMNS}
    for column, value in row.items():
        check_personal(line, column, value)
    if not PARTICIPANT.fullmatch(row["participant"]):
        raise TallyError(f"row {line}, column participant: not a code like I01")
    if not DATE.fullmatch(row["date"]):
        raise TallyError(f"row {line}, column date: not YYYY-MM-DD")
    check_code(line, "qualified", row["qualified"], YES_NO, required=True)
    check_code(line, "delegation", row["delegation"], DELEGATION)
    check_code(line, "pain_happened", row["pain_happened"], YES_NO)
    check_code(line, "would_pay_bucket", row["would_pay_bucket"], WOULD_PAY)
    check_code(line, "trusted_testnet_trial", row["trusted_testnet_trial"], TRIAL)
    codes = [c.strip() for c in row["pain_codes"].split(";") if c.strip()]
    for code in codes:
        check_code(line, "pain_codes", code, PAIN_CODES)
    row["pain_codes"] = list(dict.fromkeys(codes))
    return row


def read_rows(f):
    reader = csv.DictReader(f)
    if reader.fieldnames is None:
        raise TallyError("the sheet is empty")
    header = [c.strip() for c in reader.fieldnames]
    missing = [c for c in COLUMNS if c not in header]
    if missing:
        raise TallyError(f"missing column {', '.join(missing)}")
    extra = len([c for c in header if c not in COLUMNS])
    if extra:
        raise TallyError(f"{extra} column(s) outside the coding sheet — only {', '.join(COLUMNS)} are allowed")
    rows, seen = [], {}
    for line, raw in enumerate(reader, start=2):
        raw = {(k or "").strip(): v for k, v in raw.items()}
        if None in raw or not any((v or "").strip() for v in raw.values()):
            if None in raw:
                raise TallyError(f"row {line}: more cells than columns")
            continue
        row = parse_row(line, raw)
        if row["participant"] in seen:
            raise TallyError(f"row {line}, column participant: {row['participant']} already used in row "
                             f"{seen[row['participant']]}")
        seen[row["participant"]] = line
        rows.append(row)
    return rows


# --- rules --------------------------------------------------------------------

def pct(part, whole):
    return round(100.0 * part / whole, 1) if whole else None


def shares(values, options):
    """Count and share of every option over the answered values (blanks are not in n)."""
    answered = [v for v in values if v]
    return {"n": len(answered),
            "options": {o: {"count": answered.count(o), "share_pct": pct(answered.count(o), len(answered))}
                        for o in options}}


def tally(rows):
    qualified = [r for r in rows if r["qualified"] == "yes"]
    delegation = shares([r["delegation"] for r in qualified], DELEGATION)
    delegated = sum(delegation["options"][o]["count"] for o in DELEGATED)
    n = delegation["n"]
    # a pain counts only when it happened to the person (synthesis.md §2)
    counted = [r for r in qualified if r["pain_happened"] == "yes"]
    not_counted = sum(1 for r in qualified if r["pain_codes"] and r["pain_happened"] != "yes")
    return {
        "interviews": len(rows),
        "qualified": len(qualified),
        "target": TARGET,
        "small_sample": len(qualified) < TARGET,
        "delegation": {"n": n, "delegated": delegated, "share_pct": pct(delegated, n),
                       "answers": delegation["options"], "small_sample": n < TARGET},
        "pains": {"qualified": len(qualified), "pain_happened": len(counted), "not_counted": not_counted,
                  "codes": {c: {"count": sum(1 for r in counted if c in r["pain_codes"]),
                                "share_pct": pct(sum(1 for r in counted if c in r["pain_codes"]), len(qualified))}
                            for c in PAIN_CODES}},
        "would_pay": shares([r["would_pay_bucket"] for r in qualified], WOULD_PAY),
        "testnet_trial": shares([r["trusted_testnet_trial"] for r in qualified], TRIAL),
    }


# --- output -------------------------------------------------------------------

def _p(v):
    return "–" if v is None else f"{v:g} %"


def render_markdown(t):
    d, p = t["delegation"], t["pains"]
    out = ["# Interview tally", "",
           f"Qualified interviews: {t['qualified']} of ~{t['target']} (coded: {t['interviews']}).", ""]
    if t["small_sample"]:
        out += [f"Small sample: fewer than {t['target']} qualified — read the counts, not the shares.", ""]
    out += ["## Delegation (survey Q6's answers, qualified interviews)", "",
            "| Answer | Count | Share |", "|---|---|---|"]
    out += [f"| {a} | {v['count']} | {_p(v['share_pct'])} |" for a, v in d["answers"].items()]
    out += ["", f"Delegation share (yes still do + yes took it back): {_p(d['share_pct'])}, n = {d['n']}.", ""]
    out += ["## Pains (counted only when pain_happened is yes)", "",
            f"Share of the {p['qualified']} qualified interviews; {p['pain_happened']} said a pain happened to them.",
            "", "| Pain code | Count | Share |", "|---|---|---|"]
    out += [f"| {c} | {v['count']} | {_p(v['share_pct'])} |" for c, v in p["codes"].items()]
    if p["not_counted"]:
        out += ["", f"{p['not_counted']} qualified interview(s) named a pain that did not happen to them: not counted."]
    for title, key in (("Willingness to pay (no amounts)", "would_pay"), ("Trusted a testnet trial", "testnet_trial")):
        out += ["", f"## {title}", "", f"n = {t[key]['n']}", "", "| Answer | Count | Share |", "|---|---|---|"]
        out += [f"| {a} | {v['count']} | {_p(v['share_pct'])} |" for a, v in t[key]["options"].items()]
    return "\n".join(out) + "\n"


def main(argv=None):
    ap = argparse.ArgumentParser(description="Tally the coded interview sheet (delegation, pains, willingness to pay).")
    ap.add_argument("sheet", nargs="?", default=DEFAULT_CSV, help="interviews.csv (default: data/product/interviews/)")
    ap.add_argument("-json", action="store_true", help="print JSON instead of markdown")
    args = ap.parse_args(argv)
    try:
        with open(args.sheet, encoding="utf-8-sig", newline="") as f:
            t = tally(read_rows(f))
    except (TallyError, OSError, csv.Error) as e:
        print(f"interview_tally: {e}", file=sys.stderr)
        return 2
    sys.stdout.write(json.dumps(t, indent=2, ensure_ascii=False) + "\n" if args.json else render_markdown(t))
    return 0


if __name__ == "__main__":
    sys.exit(main())
