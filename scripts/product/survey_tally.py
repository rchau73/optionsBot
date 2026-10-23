#!/usr/bin/env python3
"""Survey tally (#247, epic E5): the cycle-holder survey's answers → counts.

Reads the CSV export of the survey form (one row per anonymous ID, P01…, columns
named by the item IDs of docs/product/discovery/screener.md and survey.md) and
prints the numbers synthesis.md needs:

    qualified vs screened out (screener.md §3–§4), by segment
    count and share of every option of each choice question Q1–Q8
    NPS on N1 (promoters − detractors), skipped when Q1 = never tried
    fake-door outcomes Joined / Declined / Undecided (survey.md §5)
    delegation share = Q6 "yes, and still do" + "yes, and took it back",
        next to the PRD §6 kill threshold (< 30 %), with n and a small-sample note

    python3 scripts/product/survey_tally.py data/product/interviews/export.csv
    python3 scripts/product/survey_tally.py data/product/interviews/export.csv -map data/product/interviews/map.json
    python3 scripts/product/survey_tally.py data/product/interviews/export.csv -json -out data/product/interviews/tally.json

Standard library only. Answers are private: the free-text items (Q4, F2), the
F1 e-mail and any answer that matches no option are never printed — only how
many there were. -out refuses any path outside data/product/interviews/.

CSV columns: ID, S1–S8, Q1–Q8, N1, F1, F2 and F1_email (the e-mail left at F1).
A multi-choice answer (S4, Q8) lists its options separated by ";". Answers match
the option wording of the survey, ignoring case, quotes, dashes and spacing; a
longer answer that starts with an option counts as it ("yes, add me (leave an
e-mail)" → "yes, add me"). When the form tool names its columns differently,
-map points to a JSON object {"export column": "item ID"}.
"""
import argparse
import csv
import json
import os
import re
import sys

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
PRIVATE_DIR = os.path.join(REPO, "data", "product", "interviews")

KILL_THRESHOLD_PCT = 30  # PRD §6: < 30 % would let software trade their account → no-go
SMALL_SAMPLE = 15        # screener.md §5: ~15 qualified responses
NPS_MIN_N = 5            # synthesis.md §3: with fewer, read the scores, not an NPS

CYCLE_HOLDER = "cycle holder"
ACTIVE_TRADER = "active option trader"
SCREENED_OUT = "screened out"

# Options of every choice item, in the order of the docs (screener.md §2, survey.md §3–§5).
OPTIONS = {
    "S1": ["BTC", "ETH", "both", "neither"],
    "S2": ["before 2018", "2018-2021", "2022 or later"],
    "S3": ["kept them", "sold most", "bought more", "did not hold then"],
    "S4": ['exchange "earn" / savings', "dual investment", "covered calls or other options sold by hand",
           "lending", "staking", "DeFi vaults", "none"],
    "S5": ["never", "a few times", "monthly", "weekly or more"],
    "S6": ["yes, and tried something", "yes, only looked", "no"],
    "S7": ["yes", "no"],
    "S8": ["yes", "no"],
    "Q1": ['exchange "earn" / savings', "dual investment", "options sold by hand", "lending", "staking",
           "DeFi vault", "other", "never tried"],
    "Q2": ["in the last month", "1-6 months ago", "6-24 months ago", "longer ago"],
    "Q3": ["no, still using it", "yes, stopped", "yes, and moved to something else"],
    "Q5": ["the yield the product showed", "compared with just holding", "my own spreadsheet",
           "did not check", "other"],
    "Q6": ["yes, and still do", "yes, and took it back", "no"],
    "Q7": ["yes", "no", "never gave anyone access"],
    "Q8": ["fees or a share of the yield", "a tool subscription", "a course or group", "signals", "nothing"],
    "F1": ["yes, add me", "no", "not now"],
}
MULTI = {"S4", "Q8"}
CHOICE_QUESTIONS = ["Q1", "Q2", "Q3", "Q5", "Q6", "Q7", "Q8"]
FREE_TEXT = ["Q4", "F2"]
ITEMS = ["ID"] + [f"S{i}" for i in range(1, 9)] + [f"Q{i}" for i in range(1, 9)] + ["N1", "F1", "F2", "F1_email"]
DELEGATED = ("yes, and still do", "yes, and took it back")
OUTCOMES = ("Joined", "Declined", "Undecided")
UNRECOGNISED = "(unrecognised)"


class TallyError(ValueError):
    """The export cannot be tallied as given."""


# --- input --------------------------------------------------------------------

def norm(text):
    """Lower-case, straight quotes and hyphens, single spaces."""
    text = text.strip().lower()
    text = re.sub(r"[‘’“”\"']", "", text)
    text = re.sub(r"[‐-―]", "-", text)
    text = re.sub(r"\s*-\s*", "-", text)
    return re.sub(r"\s+", " ", text)


def match_option(item, answer):
    """The option an answer names, None when blank, UNRECOGNISED when it names none."""
    a = norm(answer)
    if not a:
        return None
    best = None
    for opt in OPTIONS[item]:
        n = norm(opt)
        if a == n:
            return opt
        # a longer answer that starts with the option at a word boundary
        if a.startswith(n) and not a[len(n)].isalnum() and (best is None or len(n) > len(norm(best))):
            best = opt
    return best or UNRECOGNISED


def answers_of(item, raw):
    """The options of one cell: a list for a multi-choice item, else a one-element list (empty when blank)."""
    parts = raw.split(";") if item in MULTI else [raw]
    out = []
    for p in parts:
        opt = match_option(item, p)
        if opt is not None and opt not in out:
            out.append(opt)
    return out


def load_map(path):
    if not path:
        return {}
    with open(path, encoding="utf-8") as f:
        mapping = json.load(f)
    if not isinstance(mapping, dict) or not all(isinstance(k, str) and isinstance(v, str) for k, v in mapping.items()):
        raise TallyError(f"-map {path}: must be a JSON object of export column → item ID")
    unknown = sorted(set(mapping.values()) - set(ITEMS))
    if unknown:
        raise TallyError(f"-map {path}: unknown item IDs {', '.join(unknown)}")
    return mapping


def read_rows(f, mapping=None):
    """Rows of the export as {item ID: raw text}; columns renamed by the mapping."""
    mapping = mapping or {}
    reader = csv.DictReader(f)
    if reader.fieldnames is None:
        raise TallyError("the export is empty")
    columns = {col: mapping.get(col.strip(), col.strip()) for col in reader.fieldnames}
    missing = [i for i in ("ID", "S1", "S5", "Q6", "F1") if i not in columns.values()]
    if missing:
        raise TallyError(f"the export has no column for {', '.join(missing)} (use -map to name them)")
    rows = []
    for raw in reader:
        row = {}
        for col, value in raw.items():
            if col in columns:
                row[columns[col]] = (value or "").strip()
        if any(row.values()):
            rows.append(row)
    return rows


# --- rules --------------------------------------------------------------------

def segment_of(row):
    """screener.md §3–§4: cycle holder, active option trader or screened out."""
    one = {item: (answers_of(item, row.get(item, "")) or [None])[0] for item in OPTIONS if item not in MULTI}
    s4 = answers_of("S4", row.get("S4", ""))
    if one["S1"] in (None, "neither", UNRECOGNISED):
        return SCREENED_OUT
    if one["S2"] == "2022 or later" or one["S3"] == "sold most":
        return SCREENED_OUT
    if one["S7"] != "no" or one["S8"] != "yes":
        return SCREENED_OUT
    if one["S5"] in ("monthly", "weekly or more"):
        return ACTIVE_TRADER
    earns = any(o in s4 for o in ('exchange "earn" / savings', "dual investment",
                                  "covered calls or other options sold by hand"))
    if (one["S2"] in ("before 2018", "2018-2021")
            and one["S3"] in ("kept them", "bought more")
            and one["S5"] in ("never", "a few times")
            and (earns or one["S6"] in ("yes, and tried something", "yes, only looked"))):
        return CYCLE_HOLDER
    return SCREENED_OUT


def fake_door(row):
    """survey.md §5: Joined only with an e-mail; "yes" without one is Undecided. None when F1 is blank."""
    f1 = (answers_of("F1", row.get("F1", "")) or [None])[0]
    if f1 is None or f1 == UNRECOGNISED:
        return None
    if f1 == "yes, add me":
        return "Joined" if row.get("F1_email", "").strip() else "Undecided"
    return "Declined" if f1 == "no" else "Undecided"


def nps_score(row):
    """N1 as 0–10, None when skipped (Q1 never tried or blank) or not a whole number in range."""
    q1 = (answers_of("Q1", row.get("Q1", "")) or [None])[0]
    if q1 in (None, "never tried"):
        return None
    try:
        score = float(row.get("N1", "").strip())
    except ValueError:
        return None
    return int(score) if score.is_integer() and 0 <= score <= 10 else None


def pct(part, whole):
    return round(100.0 * part / whole, 1) if whole else None


def nps_of(scores):
    n = len(scores)
    promoters = sum(1 for s in scores if s >= 9)
    detractors = sum(1 for s in scores if s <= 6)
    return {
        "n": n, "promoters": promoters, "passives": n - promoters - detractors, "detractors": detractors,
        "nps": round(100.0 * (promoters - detractors) / n, 1) if n else None,
        "scores": {str(s): scores.count(s) for s in range(11) if s in scores},
        "small_sample": n < NPS_MIN_N,
    }


def delegation_of(rows):
    answered = [r for r in rows if answers_of("Q6", r.get("Q6", "")) not in ([], [UNRECOGNISED])]
    yes = sum(1 for r in answered if answers_of("Q6", r["Q6"])[0] in DELEGATED)
    n = len(answered)
    share = pct(yes, n)
    return {
        "n": n, "delegated": yes, "unanswered": len(rows) - n, "share_pct": share,
        "threshold_pct": KILL_THRESHOLD_PCT,
        "below_threshold": share is not None and share < KILL_THRESHOLD_PCT,
        "small_sample": n < SMALL_SAMPLE,
    }


def tally(rows):
    seg = [segment_of(r) for r in rows]
    ids = [r.get("ID", "") for r in rows]
    qualified = [r for r, s in zip(rows, seg) if s != SCREENED_OUT]
    holders = [r for r, s in zip(rows, seg) if s == CYCLE_HOLDER]
    traders = [r for r, s in zip(rows, seg) if s == ACTIVE_TRADER]

    questions = {}
    for item in CHOICE_QUESTIONS:
        picked = [answers_of(item, r.get(item, "")) for r in qualified]
        answered = sum(1 for p in picked if p)
        counts = {}
        for opt in OPTIONS[item] + [UNRECOGNISED]:
            c = sum(1 for p in picked if opt in p)
            if opt != UNRECOGNISED or c:
                counts[opt] = {"count": c, "share_pct": pct(c, answered)}
        questions[item] = {"answered": answered, "multi": item in MULTI, "options": counts}

    def outcomes(group):
        got = [fake_door(r) for r in group]
        n = sum(1 for g in got if g)
        return {"n": n, **{o: {"count": got.count(o), "share_pct": pct(got.count(o), n)} for o in OUTCOMES}}

    return {
        "responses": len(rows),
        "qualified": len(qualified),
        "screened_out": len(rows) - len(qualified),
        "segments": {CYCLE_HOLDER: len(holders), ACTIVE_TRADER: len(traders)},
        "duplicate_ids": sorted(i for i in set(ids) if ids.count(i) > 1),
        "questions": questions,
        "nps": nps_of([s for s in (nps_score(r) for r in qualified) if s is not None]),
        "fake_door": {"qualified": outcomes(qualified), CYCLE_HOLDER: outcomes(holders),
                      ACTIVE_TRADER: outcomes(traders)},
        "delegation": {"qualified": delegation_of(qualified), CYCLE_HOLDER: delegation_of(holders)},
        "free_text_answers": {item: sum(1 for r in qualified if r.get(item, "").strip()) for item in FREE_TEXT},
    }


# --- output -------------------------------------------------------------------

def _p(v):
    return "–" if v is None else f"{v:g} %"


def _delegation_line(label, d):
    verdict = "below" if d["below_threshold"] else ("–" if d["share_pct"] is None else "at or above")
    return (f"| {label} | {d['delegated']} | {d['n']} | {_p(d['share_pct'])} | "
            f"< {d['threshold_pct']} % | {verdict} |")


def render_markdown(t):
    out = ["# Survey tally", ""]
    out += ["| Responses | Qualified | Cycle holders | Active option traders | Screened out |",
            "|---|---|---|---|---|",
            f"| {t['responses']} | {t['qualified']} | {t['segments'][CYCLE_HOLDER]} | "
            f"{t['segments'][ACTIVE_TRADER]} | {t['screened_out']} |", ""]
    if t["duplicate_ids"]:
        out += [f"WARNING: duplicate IDs {', '.join(t['duplicate_ids'])} — each response needs its own ID.", ""]

    out += ["## Choice questions (qualified responses)", ""]
    for item, q in t["questions"].items():
        note = " — several options per answer, shares do not add to 100 %" if q["multi"] else ""
        out += [f"### {item} (answered: {q['answered']}){note}", "", "| Option | Count | Share |", "|---|---|---|"]
        out += [f"| {opt} | {v['count']} | {_p(v['share_pct'])} |" for opt, v in q["options"].items()]
        out.append("")

    n = t["nps"]
    out += ["## NPS (N1, skipped when Q1 = never tried)", "",
            "| n | Promoters (9–10) | Passives (7–8) | Detractors (0–6) | NPS |", "|---|---|---|---|---|",
            f"| {n['n']} | {n['promoters']} | {n['passives']} | {n['detractors']} | "
            f"{'–' if n['nps'] is None else format(n['nps'], 'g')} |", ""]
    if n["small_sample"]:
        scores = ", ".join(f"{s}: {c}" for s, c in n["scores"].items()) or "none"
        out += [f"Small sample (n < {NPS_MIN_N}): read the scores, not the NPS — {scores}.", ""]

    out += ["## Fake door (F1)", "", "| Group | n | Joined | Declined | Undecided |", "|---|---|---|---|---|"]
    for group, f in t["fake_door"].items():
        cells = " | ".join(f"{f[o]['count']} ({_p(f[o]['share_pct'])})" for o in OUTCOMES)
        out.append(f"| {group} | {f['n']} | {cells} |")
    out += ["", "Undecided = \"not now\", or \"yes\" with no e-mail (counts as not joined).", ""]

    out += ["## Delegation share (Q6, the PRD §6 kill criterion)", "",
            "| Group | Delegated (yes, still / took back) | n | Share | Kill threshold | Share vs threshold |",
            "|---|---|---|---|---|---|"]
    for group, d in t["delegation"].items():
        out.append(_delegation_line(group, d))
    out.append("")
    for group, d in t["delegation"].items():
        if d["small_sample"]:
            out.append(f"Small sample: {group} n = {d['n']} < {SMALL_SAMPLE} — not enough for a verdict.")
        if d["unanswered"]:
            out.append(f"{group}: {d['unanswered']} left Q6 blank (not counted in n).")
    out.append("")

    out += ["## Free text (never printed here)", ""]
    out += [f"- {item}: {c} answers" for item, c in t["free_text_answers"].items()]
    return "\n".join(out) + "\n"


def check_out_path(path, private_dir=PRIVATE_DIR):
    """Returns the absolute path if it lies inside data/product/interviews/, else raises."""
    target = os.path.realpath(os.path.abspath(path))
    root = os.path.realpath(private_dir)
    if os.path.commonpath([target, root]) != root or target == root:
        raise TallyError(f"-out {path}: refused — survey results stay in {root}/ (gitignored)")
    return target


def main(argv=None):
    ap = argparse.ArgumentParser(description="Tally the cycle-holder survey export (counts, NPS, fake door, delegation).")
    ap.add_argument("export", help="CSV export of the form, in data/product/interviews/")
    ap.add_argument("-map", help="JSON object {export column: item ID} when the form names columns differently")
    ap.add_argument("-json", action="store_true", help="print JSON instead of markdown")
    ap.add_argument("-out", help="write the tally here; must be inside data/product/interviews/")
    args = ap.parse_args(argv)
    try:
        out = check_out_path(args.out) if args.out else None
        mapping = load_map(args.map)
        with open(args.export, encoding="utf-8-sig", newline="") as f:
            t = tally(read_rows(f, mapping))
    except (TallyError, OSError, json.JSONDecodeError, csv.Error) as e:
        print(f"survey_tally: {e}", file=sys.stderr)
        return 2
    text = json.dumps(t, indent=2, ensure_ascii=False) + "\n" if args.json else render_markdown(t)
    if out:
        os.makedirs(os.path.dirname(out), exist_ok=True)
        with open(out, "w", encoding="utf-8") as f:
            f.write(text)
        print(f"wrote {out}")
    else:
        sys.stdout.write(text)
    return 0


if __name__ == "__main__":
    sys.exit(main())
