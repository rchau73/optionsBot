#!/usr/bin/env python3
"""Comprehension-round scorer (#718, epic E11): a results table → pass/fail per tip and the rewrite list.

Follows docs/product/education/comprehension-test.md. Reads a CSV or JSON table, one row
per participant and tip key:

    participant  P01, P02… (a code, never a name)
    key          a tip key of docs/product/education/tips.json or a glossary term
    answer       correct | partly | wrong
    seconds      optional, whole seconds to the answer
    round        round id, e.g. 2026-R1

    python3 scripts/product/comprehension_score.py data/product/comprehension/results.csv
    python3 scripts/product/comprehension_score.py results.json --round 2026-R2 -json

The latest round in the file (ids sorted as text) is scored, or --round; the round just
before it, when the file has one, gives the change in share correct. Output: score.md and
score.json under data/product/comprehension/<round>/ (gitignored); --out-dir refuses any
other place. Standard library only. A column the reader does not know (a name, an e-mail,
free text) stops it, naming the column, before anything is written.
"""
import argparse
import csv
import json
import os
import re
import sys

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
PRIVATE_DIR = os.path.join(REPO, "data", "product", "comprehension")
TIPS_PATH = os.path.join(REPO, "docs", "product", "education", "tips.json")
GLOSSARY_PATH = os.path.join(REPO, "frontend", "content", "glossary.json")

# Assumption of comprehension-test.md §5, to confirm after the first round: a tip passes when
# at least this share of the people asked answered it correctly. Changing the bar is this one line.
PASS_BAR_PCT = 80
MIN_PARTICIPANTS = 5  # comprehension-test.md §1: a round is 5 people; fewer is "not enough people"

PASS, FAIL, TOO_FEW = "pass", "fail", "not enough people"
ANSWERS = ("correct", "partly", "wrong")
COLUMNS = ("participant", "key", "answer", "seconds", "round")
REQUIRED = ("participant", "key", "answer", "round")
CODE = re.compile(r"^P\d{2,}$")  # same participant-code rule as the survey tally (P01…)
FLAT_PCT = 0.05  # a change smaller than this is flat


class ScoreError(ValueError):
    """The table cannot be scored as given."""


# --- input --------------------------------------------------------------------

def known_keys(tips_path=TIPS_PATH, glossary_path=GLOSSARY_PATH):
    """{key: video id or None}: every tip key, then every glossary term (no video)."""
    with open(tips_path, encoding="utf-8") as f:
        tips = json.load(f)
    with open(glossary_path, encoding="utf-8") as f:
        glossary = json.load(f)
    keys = {term: None for term in glossary}
    keys.update({key: tip.get("video") for key, tip in tips.items()})
    return keys


def read_table(path):
    """The raw rows as a list of {column: text}; JSON is a list of objects."""
    if path.lower().endswith(".json"):
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
        if not isinstance(data, list) or not all(isinstance(r, dict) for r in data):
            raise ScoreError(f"{path}: a JSON table is a list of row objects")
        return [{k: "" if v is None else str(v) for k, v in r.items()} for r in data]
    with open(path, encoding="utf-8-sig", newline="") as f:
        reader = csv.DictReader(f)
        if reader.fieldnames is None:
            raise ScoreError(f"{path}: the table is empty")
        unknown_cols = [c for c in reader.fieldnames if c is None or c.strip() not in COLUMNS]
        if unknown_cols:
            raise ScoreError(_unknown_columns(unknown_cols))
        return [{c.strip(): (v or "").strip() for c, v in r.items() if c is not None} for r in reader]


def _unknown_columns(cols):
    named = ", ".join(repr(c) for c in cols if c is not None) or "an unnamed extra column"
    return (f"unknown column {named}: only {', '.join(COLUMNS)} are read "
            "(no names, e-mails or free text) — nothing was written")


def validate(rows, keys):
    """Checks every row; returns clean rows {participant, key, answer, seconds, round}."""
    seen_cols = {c for r in rows for c in r}
    unknown_cols = sorted(seen_cols - set(COLUMNS))
    if unknown_cols:
        raise ScoreError(_unknown_columns(unknown_cols))
    if not rows:
        raise ScoreError("the table has no rows")
    clean, seen = [], set()
    for n, r in enumerate(rows, 1):
        r = {c: r.get(c, "").strip() for c in COLUMNS}
        for col in REQUIRED:
            if not r[col]:
                raise ScoreError(f"row {n}: column {col!r} is empty")
        if not CODE.match(r["participant"]):
            raise ScoreError(f"row {n}: participant must be a code like P01, not {r['participant']!r}")
        if r["key"] not in keys:
            raise ScoreError(f"row {n}: unknown key {r['key']!r}: not in tips.json or the glossary")
        answer = r["answer"].lower()
        if answer not in ANSWERS:
            raise ScoreError(f"row {n}: answer must be one of {', '.join(ANSWERS)}, not {r['answer']!r}")
        seconds = None
        if r["seconds"]:
            if not r["seconds"].isdigit():
                raise ScoreError(f"row {n}: seconds must be a whole number, not {r['seconds']!r}")
            seconds = int(r["seconds"])
        ident = (r["round"], r["participant"], r["key"])
        if ident in seen:
            raise ScoreError(f"row {n}: {r['participant']} answered {r['key']!r} twice in round {r['round']}")
        seen.add(ident)
        clean.append({"participant": r["participant"], "key": r["key"], "answer": answer,
                      "seconds": seconds, "round": r["round"]})
    return clean


# --- rules --------------------------------------------------------------------

def pct(part, whole):
    return round(100.0 * part / whole, 1) if whole else None


def score_round(rows, keys, bar_pct=PASS_BAR_PCT, min_people=MIN_PARTICIPANTS):
    """Per tip key: participants, answer counts, share correct and wrong, result and video id."""
    result = {}
    for key in sorted({r["key"] for r in rows}):
        got = [r["answer"] for r in rows if r["key"] == key]
        n = len(got)
        share = pct(got.count("correct"), n)
        if n < min_people:
            status = TOO_FEW
        else:
            status = PASS if share >= bar_pct else FAIL
        result[key] = {"participants": n, "correct": got.count("correct"), "partly": got.count("partly"),
                       "wrong": got.count("wrong"), "share_correct_pct": share,
                       "share_wrong_pct": pct(got.count("wrong"), n), "result": status, "video": keys[key]}
    return result


def change_of(now, before):
    """up / down / flat against the earlier round's share correct; None when it did not ask the tip."""
    if before is None:
        return None
    delta = round(now - before, 1)
    return {"previous_pct": before, "delta_pts": delta,
            "direction": "flat" if abs(delta) < FLAT_PCT else ("up" if delta > 0 else "down")}


def rewrite_list(scored):
    """Failing tips, the highest share wrong first (then by key)."""
    failing = [(key, s) for key, s in scored.items() if s["result"] == FAIL]
    failing.sort(key=lambda ks: (-ks[1]["share_wrong_pct"], ks[0]))
    return [{"key": key, "share_wrong_pct": s["share_wrong_pct"], "video": s["video"]} for key, s in failing]


def build_report(rows, keys, round_id=None, bar_pct=PASS_BAR_PCT):
    rounds = sorted({r["round"] for r in rows})
    round_id = round_id or rounds[-1]
    if round_id not in rounds:
        raise ScoreError(f"round {round_id!r} is not in the table (rounds: {', '.join(rounds)})")
    earlier = [r for r in rounds if r < round_id]
    previous_id = earlier[-1] if earlier else None
    scored = score_round([r for r in rows if r["round"] == round_id], keys, bar_pct)
    previous = score_round([r for r in rows if r["round"] == previous_id], keys, bar_pct) if previous_id else {}
    for key, s in scored.items():
        before = previous.get(key, {}).get("share_correct_pct")
        s["change"] = change_of(s["share_correct_pct"], before) if previous_id else None
    counted = [s for s in scored.values() if s["result"] != TOO_FEW]
    return {
        "round": round_id,
        "previous_round": previous_id,
        "pass_bar_pct": bar_pct,
        "min_participants": MIN_PARTICIPANTS,
        "tips": scored,
        "passed": sum(1 for s in counted if s["result"] == PASS),
        "failed": sum(1 for s in counted if s["result"] == FAIL),
        "not_enough_people": len(scored) - len(counted),
        "rewrite": rewrite_list(scored),
    }


# --- output -------------------------------------------------------------------

def _p(v):
    return "–" if v is None else f"{v:g} %"


def _change(c):
    if c is None:
        return "–"
    return f"{c['direction']} ({c['delta_pts']:+g} pts from {_p(c['previous_pct'])})"


def render_markdown(rep):
    out = [f"# Comprehension round {rep['round']}", "",
           f"Pass bar: {rep['pass_bar_pct']} % correct (an assumption to confirm after the first round); "
           f"under {rep['min_participants']} people a tip is \"{TOO_FEW}\" and counts as neither pass nor fail.", ""]
    out += [f"Passed {rep['passed']} · failed {rep['failed']} · {TOO_FEW} {rep['not_enough_people']}", ""]
    head = "| Tip | People | Correct | Result |"
    rule = "|---|---|---|---|"
    if rep["previous_round"]:
        head += f" Change since {rep['previous_round']} |"
        rule += "---|"
    out += [head, rule]
    for key, s in rep["tips"].items():
        line = f"| {key} | {s['participants']} | {_p(s['share_correct_pct'])} | {s['result']} |"
        if rep["previous_round"]:
            line += f" {_change(s['change'])} |"
        out.append(line)
    out += ["", "## Tips to rewrite (most wrong first)", ""]
    if rep["rewrite"]:
        out += ["| Tip | Wrong | Video |", "|---|---|---|"]
        out += [f"| {r['key']} | {_p(r['share_wrong_pct'])} | {r['video'] or '–'} |" for r in rep["rewrite"]]
    else:
        out.append("None.")
    return "\n".join(out) + "\n"


def check_out_dir(path, private_dir=PRIVATE_DIR):
    """The absolute path if it lies inside data/product/comprehension/, else raises."""
    target = os.path.realpath(os.path.abspath(path))
    root = os.path.realpath(private_dir)
    if os.path.commonpath([target, root]) != root:
        raise ScoreError(f"--out-dir {path}: refused — round results stay in {root}/ (gitignored)")
    return target


def main(argv=None):
    ap = argparse.ArgumentParser(description="Score a comprehension round (pass/fail per tip, the rewrite list).")
    ap.add_argument("table", help="CSV or JSON results table")
    ap.add_argument("--round", help="round id to score (default: the latest in the table)")
    ap.add_argument("--out-dir", help="where score.md and score.json go (default data/product/comprehension/<round>)")
    ap.add_argument("-json", action="store_true", help="print JSON instead of markdown")
    args = ap.parse_args(argv)
    try:
        keys = known_keys()
        rep = build_report(validate(read_table(args.table), keys), keys, args.round)
        out_dir = check_out_dir(args.out_dir or os.path.join(PRIVATE_DIR, rep["round"]))
    except (ScoreError, OSError, json.JSONDecodeError, csv.Error) as e:
        print(f"comprehension_score: {e}", file=sys.stderr)
        return 2
    os.makedirs(out_dir, exist_ok=True)
    md, js = render_markdown(rep), json.dumps(rep, indent=2, ensure_ascii=False) + "\n"
    for name, text in (("score.md", md), ("score.json", js)):
        with open(os.path.join(out_dir, name), "w", encoding="utf-8") as f:
            f.write(text)
    sys.stdout.write(js if args.json else md)
    print(f"wrote {out_dir}/score.md and score.json", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
