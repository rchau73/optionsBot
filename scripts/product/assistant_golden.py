#!/usr/bin/env python3
"""Score a live golden-set run against its baseline (#536).

frontend/scripts/assistant-run.mjs asks the real assistant every question of
docs/product/assistant/golden.jsonl and writes answers.jsonl; this grades it
with assistant_grade.py and compares the score per category with the last
accepted one:

    python3 scripts/product/assistant_golden.py score answers.jsonl --out-dir data/monitor/assistant_reviews --week 2026-W44
    python3 scripts/product/assistant_golden.py score answers.jsonl --out-dir … --week … --accept   # a person accepts this run's score
    python3 scripts/product/assistant_golden.py comment data/monitor/assistant_reviews/2026-W44.golden.summary.json

score writes, in the private --out-dir:
  <week>.golden.json          the grader's scores and every failure with its reasons (golden ids, no answer text)
  <week>.golden.summary.json  counts and percentages per category, the baseline's and the drop: what may be posted
  golden_baseline.json        the last accepted score per category; the first run writes it
A category whose score falls more than THRESHOLD_PTS percentage points under
its baseline is a regression. Exit 0 = no regression (or the baseline was just
set or accepted), 1 = a regression, 2 = bad input.
comment prints the E10 epic comment from a summary: counts and percentages
only, category names from the grader's fixed list.
"""

import argparse
import datetime
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import assistant_grade as ag  # noqa: E402

# A drop of more than this many points in a category's score is a
# regression: one question of a 15-question category is 6.7 points (noise of
# one answer), two are 13; in a 5-question category one question is 20.
THRESHOLD_PTS = 10
BASELINE = "golden_baseline.json"
HEADER = "🤖 **Assistant golden-set run**"


def pct(passed, total):
    return round(100 * passed / total, 1) if total else 0.0


def compare(scores, baseline, threshold=THRESHOLD_PTS):
    """Per category {category, passed, total, pct, baseline_pct, drop_pts,
    regression}, in the grader's order; a category the baseline lacks has no
    drop."""
    rows = []
    for cat, s in scores.items():
        p = pct(s["passed"], s["total"])
        base = (baseline or {}).get(cat)
        base_pct = base.get("pct") if isinstance(base, dict) else None
        drop = round(base_pct - p, 1) if isinstance(base_pct, (int, float)) else None
        rows.append({"category": cat, "passed": s["passed"], "total": s["total"], "pct": p,
                     "baseline_pct": base_pct, "drop_pts": drop,
                     "regression": drop is not None and drop > threshold})
    return rows


def baseline_of(scores, week):
    return {"week": week, "set_at": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
            "categories": {cat: {"passed": s["passed"], "total": s["total"], "pct": pct(s["passed"], s["total"])}
                           for cat, s in scores.items()}}


def write_private(path, data):
    os.makedirs(os.path.dirname(path), mode=0o700, exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, indent=2, ensure_ascii=False)
        f.write("\n")
    os.chmod(path, 0o600)


def load_baseline(path):
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
    except FileNotFoundError:
        return None
    except (OSError, json.JSONDecodeError) as e:
        raise ag.InputError(f"{path}: {e}") from e
    if not isinstance(data, dict) or not isinstance(data.get("categories"), dict):
        raise ag.InputError(f"{path}: a baseline with \"categories\" is required")
    return data


def score(answers_path, out_dir, week, golden=ag.GOLDEN, index=ag.INDEX, accept=False, threshold=THRESHOLD_PTS):
    """Grades a run, writes its files and returns the summary."""
    answers = ag.load_jsonl(answers_path)
    results = ag.grade(ag.load_jsonl(golden), answers, ag.load_index(index))
    scores = ag.scores(results)
    base_path = os.path.join(out_dir, BASELINE)
    baseline = load_baseline(base_path)
    rows = compare(scores, baseline["categories"] if baseline else None, threshold)
    status = "compared"
    if baseline is None or accept:
        status = "baseline_set" if baseline is None else "baseline_accepted"
        write_private(base_path, baseline_of(scores, week))
    summary = {
        "week": week,
        "status": status,
        "baseline_week": baseline.get("week") if baseline else week,
        "threshold_pts": threshold,
        "questions": len(results),
        "model_errors": sum(1 for a in answers if a.get("outcome") == "model_error"),
        "categories": rows,
        "regressions": [r["category"] for r in rows if r["regression"]] if status == "compared" else [],
    }
    write_private(os.path.join(out_dir, f"{week}.golden.json"),
                  {"week": week, "scores": scores, "failures": [r for r in results if not r["passed"]]})
    write_private(os.path.join(out_dir, f"{week}.golden.summary.json"), summary)
    return summary


def fmt_pct(v):
    return "—" if v is None else f"{v:g} %"


def report(summary):
    lines = [f"{'category':<16} {'passed':>6} {'total':>6} {'score':>8} {'baseline':>9} {'drop':>6}"]
    for r in summary["categories"]:
        drop = "—" if r["drop_pts"] is None else f"{r['drop_pts']:g}"
        lines.append(f"{r['category']:<16} {r['passed']:>6} {r['total']:>6} {fmt_pct(r['pct']):>8} "
                     f"{fmt_pct(r['baseline_pct']):>9} {drop:>6}" + ("  REGRESSION" if r["regression"] else ""))
    lines.append(regression_line(summary))
    return "\n".join(lines)


def regression_line(summary):
    if summary["status"] == "baseline_set":
        return "First run: this score is the baseline."
    if summary["status"] == "baseline_accepted":
        return "This score was accepted as the new baseline."
    by = {r["category"]: r for r in summary["categories"]}
    if not summary["regressions"]:
        return f"No category fell more than {summary['threshold_pts']} points under its baseline ({summary['baseline_week']})."
    return "Regression: " + ", ".join(
        f"{c} {fmt_pct(by[c]['pct'])} (baseline {fmt_pct(by[c]['baseline_pct'])}, −{by[c]['drop_pts']:g} pts)"
        for c in summary["regressions"]) + "."


def comment(summary):
    """The E10 epic comment: counts and percentages per category only. The
    categories come from the grader's fixed list; nothing else is read."""
    known = set(ag.CATEGORIES) | {"all"}
    out = [f"{HEADER} — week {summary['week']} (`scripts/agents/assistant_golden.sh`, #536)", "",
           "Every question of `docs/product/assistant/golden.jsonl` asked to the real assistant on its fixtures, "
           "graded by `scripts/product/assistant_grade.py` (docs/product/assistant/README.md \"Live golden-set run\"). "
           "Counts and percentages only: the answers stay in the private run files.", "",
           "| category | passed | total | score | baseline | drop (pts) |", "|---|---|---|---|---|---|"]
    for r in summary["categories"]:
        if r["category"] not in known:
            continue
        drop = "—" if r["drop_pts"] is None else f"{r['drop_pts']:g}"
        name = f"**{r['category']}** ⚠️" if r["regression"] else r["category"]
        out.append(f"| {name} | {int(r['passed'])} | {int(r['total'])} | {fmt_pct(r['pct'])} | {fmt_pct(r['baseline_pct'])} | {drop} |")
    out += ["", regression_line({**summary, "regressions": [c for c in summary["regressions"] if c in known]}),
            f"Model errors: {int(summary['model_errors'])} of {int(summary['questions'])} questions."]
    return "\n".join(out) + "\n"


def main(argv=None):
    p = argparse.ArgumentParser(description="Score a live golden-set run against its baseline.")
    sub = p.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("score", help="grade answers.jsonl, write the run's files, compare with the baseline")
    s.add_argument("answers")
    s.add_argument("--out-dir", required=True, help="the private review folder (data/monitor/assistant_reviews)")
    s.add_argument("--week", required=True, help="ISO week the run belongs to, e.g. 2026-W44")
    s.add_argument("--golden", default=ag.GOLDEN)
    s.add_argument("--index", default=ag.INDEX)
    s.add_argument("--accept", action="store_true", help="make this run's score the baseline (a person's decision)")
    s.add_argument("--json", action="store_true", help="print the summary as JSON")
    c = sub.add_parser("comment", help="print the E10 epic comment of a run's summary")
    c.add_argument("summary")
    args = p.parse_args(argv)
    try:
        if args.cmd == "comment":
            with open(args.summary, encoding="utf-8") as f:
                sys.stdout.write(comment(json.load(f)))
            return 0
        summary = score(args.answers, args.out_dir, args.week, args.golden, args.index, args.accept)
    except (ag.InputError, OSError, json.JSONDecodeError, KeyError) as e:
        print(f"assistant_golden: {e}", file=sys.stderr)
        return 2
    print(json.dumps(summary, indent=2) if args.json else report(summary))
    return 1 if summary["regressions"] else 0


if __name__ == "__main__":
    sys.exit(main())
