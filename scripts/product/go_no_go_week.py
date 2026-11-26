#!/usr/bin/env python3
"""The weekly go / no-go reading (#738): run the scorecard, keep a dated history, say when it changed.

Runs scripts/product/go_no_go.py -json (its calculation is not copied), appends one line to
data/product/go_no_go_history.jsonl (date, overall line, each row's status, value and source), and,
when a row changed status since the last line or the overall line became "kill criterion met" or
"no kill criterion met", writes the issue to open (title and body files) for the product owner.
Percentages and ratios only: a value that carries a coin or USD amount is replaced by "—", and a
source path is reduced to a repo-relative or environment-relative name (the repository is public).
The alert recommends nothing beyond PRD §6's own kill rule. Offline and tested; scheduled by
scripts/agents/go_no_go.sh. Rows and thresholds: docs/product/go-no-go.md.

    python3 scripts/product/go_no_go_week.py --evidence-root ~/dev/go/optionsBot-env/stable \\
        --bot btc --alert-dir /tmp/alert
"""
import argparse
import glob
import json
import os
import re
import subprocess
import sys
from datetime import datetime, timezone

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(os.path.dirname(HERE))
PRIVATE = os.path.join(REPO, "data", "product")
HISTORY = os.path.join(PRIVATE, "go_no_go_history.jsonl")

MISSING = "missing"
INCOMPLETE = "incomplete"  # the overall line before any reading: every row missing
ALERT_OVERALL = ("kill criterion met", "no kill criterion met")
# A coin or USD amount: a currency sign or word beside a figure. Shares and ratios never match.
AMOUNT = re.compile(r"\$|\bUSD\b|\bUSDC\b|\b(?:BTC|ETH)\b|₿|Ξ", re.IGNORECASE)
MARKER = "<!-- agent:pm -->"
HEADER = "🧭 **PM agent**"


def clean_value(text):
    """The text, or "—" when it carries an amount."""
    text = "" if text is None else str(text)
    return "—" if AMOUNT.search(text) else text


def source_name(path, evidence_root):
    """A path the public repository may show: repo- or environment-relative, never absolute."""
    if not path or path == "—":
        return "—"
    path = str(path)
    for base, label in ((evidence_root, "<env clone>"), (REPO, "")):
        if base and path.startswith(os.path.abspath(base) + os.sep):
            rel = os.path.relpath(path, base)
            return (label + "/" + rel) if label else rel
    return path if not os.path.isabs(path) else os.path.basename(path)


def reading(card, today, evidence_root):
    """One history line from the scorecard's JSON: no note, no amount, no absolute path."""
    rows = [{"input": r["input"], "status": r["status"], "value": clean_value(r["value"]),
             "threshold": clean_value(r["threshold"]),
             "source": clean_value(source_name(r["source"], evidence_root)),
             "date": r.get("date", "—")} for r in card["rows"]]
    return {"date": today, "overall": card["overall"], "rows": rows}


def last_reading(path):
    """The newest well-formed line of the history, or None."""
    try:
        with open(path, encoding="utf-8") as f:
            lines = [line for line in f.read().splitlines() if line.strip()]
    except OSError:
        return None
    for line in reversed(lines):
        try:
            data = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(data, dict) and "rows" in data:
            return data
    return None


def changes(old, new):
    """What differs: (row changes, overall change or None). No old reading counts as every row missing."""
    before = {r["input"]: r["status"] for r in (old or {}).get("rows", [])}
    moved = [(r, before.get(r["input"], MISSING)) for r in new["rows"] if before.get(r["input"], MISSING) != r["status"]]
    old_overall = (old or {}).get("overall", INCOMPLETE)
    overall = (old_overall, new["overall"]) if old_overall != new["overall"] else None
    return moved, overall


def alert(old, new):
    """(title, body) of the issue to open, or None when nothing changed that needs a person."""
    moved, overall = changes(old, new)
    to_verdict = overall is not None and overall[1] in ALERT_OVERALL
    if not moved and not to_verdict:
        return None
    old_overall = overall[0] if overall else new["overall"]
    lines = [f"{HEADER} — the weekly go / no-go reading of {new['date']} differs from the last one"
             f" ({(old or {}).get('date', 'none yet')}).", ""]
    for r, was in moved:
        lines.append(f"- **{r['input']}**: {was} → **{r['status']}** — {r['value']} (threshold: {r['threshold']});"
                     f" source `{r['source']}`, dated {r['date']}")
    lines.append(f"- **Overall**: {old_overall} → **{new['overall']}**")
    lines += ["", "PRD §6's own kill rule is the only rule applied; the go / no-go decision is the product owner's."
              " Scorecard, rows and history: `docs/product/go-no-go.md`, `data/product/go_no_go_history.jsonl`."
              " Shares and ratios only.", "", MARKER]
    title = f"[E5] Go / no-go reading {new['date']}: " + (new["overall"] if to_verdict else "a row changed status")
    return title, "\n".join(lines)


def newest(pattern):
    found = glob.glob(pattern, recursive=True)
    return max(found, key=os.path.getmtime) if found else None


def scorecard(evidence_root, bot, private=PRIVATE):
    """go_no_go.py -json on the newest tally and fake-door result under data/product/."""
    cmd = [sys.executable, os.path.join(HERE, "go_no_go.py"), "-json", "--evidence-root", evidence_root, "--bot", bot]
    tally = newest(os.path.join(private, "**", "tally.json"))
    if tally:
        cmd += ["--tally", tally]
    fakedoor = newest(os.path.join(private, "fakedoor", "*", "result.json"))
    # an absent file is passed as a path that does not exist: the row reads "missing", never assumed
    cmd += ["--fakedoor", fakedoor or os.path.join(private, "fakedoor", "none", "result.json")]
    out = subprocess.run(cmd, capture_output=True, text=True, check=True)
    return json.loads(out.stdout)


def run(card, history, today, evidence_root, alert_dir=None):
    """Append the reading; write alert_dir/title.txt and body.md when it needs a person. True if it did."""
    new = reading(card, today, evidence_root)
    found = alert(last_reading(history), new)
    os.makedirs(os.path.dirname(history), exist_ok=True)
    with open(history, "a", encoding="utf-8") as f:
        f.write(json.dumps(new, ensure_ascii=False) + "\n")
    if found and alert_dir:
        os.makedirs(alert_dir, exist_ok=True)
        with open(os.path.join(alert_dir, "title.txt"), "w", encoding="utf-8") as f:
            f.write(found[0])
        with open(os.path.join(alert_dir, "body.md"), "w", encoding="utf-8") as f:
            f.write(found[1] + "\n")
    return found is not None


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--evidence-root", required=True, help="the stable clone holding data/evidence/")
    ap.add_argument("--bot", default="btc")
    ap.add_argument("--history", default=HISTORY)
    ap.add_argument("--alert-dir", default=None, help="where title.txt and body.md go when an issue is due")
    args = ap.parse_args(argv)
    card = scorecard(args.evidence_root, args.bot, os.path.join(os.path.dirname(args.history)))
    today = datetime.now(timezone.utc).date().isoformat()
    print("alert" if run(card, args.history, today, args.evidence_root, args.alert_dir) else "no alert")
    return 0


if __name__ == "__main__":
    sys.exit(main())
