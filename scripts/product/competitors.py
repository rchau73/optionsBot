#!/usr/bin/env python3
"""Competitor map checker (#304, epic E5).

Validates the competitor entries (one YAML per competitor, private, in the
gitignored data/product/competitors/) and prints the positioning gaps.

    python3 scripts/product/competitors.py                      # check data/product/competitors/*.yaml
    python3 scripts/product/competitors.py scripts/product/example_competitor.yaml
    python3 scripts/product/competitors.py --summary            # per category, safe to paste into the PRD

Every fact carries `source_url` and `seen_on` (YYYY-MM-DD): an entry with a fact
lacking either is refused (exit 1, naming the file and the field). A fact seen
more than 90 days ago is listed as stale (a warning, exit 0). `--summary` prints
counts only — no competitor name, price, URL or file path — so it may go into
PRD §4 by pull request. Method and fields: docs/product/competitors/README.md.

Standard library only; the YAML subset is unit_economics.py's (nested mappings,
no lists: strengths, tiers and the like are mappings by name).
"""
import argparse
import datetime
import glob
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
from unit_economics import ScenarioError, parse_yaml  # noqa: E402

REPO = os.path.dirname(os.path.dirname(HERE))
PRIVATE_DIR = os.path.join(REPO, "data", "product", "competitors")
STALE_DAYS = 90

# PRD §4's categories, in its order.
CATEGORIES = {
    "exchange_yield": "Exchange-native structured yield",
    "defi_vault": "DeFi option vaults",
    "copy_trading": "Copy-trading and signal platforms",
    "api_automation": "API-key automation platforms",
    "option_analytics": "Option analytics tools",
}
RISK_VALUES = ("yes", "partly", "no")
# The claims of PRD §5's positioning statement, checked against each competitor.
POSITIONING = {
    "explains_trades": "shows the reason for every trade",
    "worst_case_before_risk": "shows the worst case before every risk",
    "non_custodial": "never touches the customer's funds (own exchange account)",
    "public_track_record": "publishes its full track record, net of fees",
}
SOURCE_KEYS = ("source_url", "seen_on")
TOP_KEYS = {"name", "category", "segment", "pricing", "positioning", "strengths", "weaknesses",
            "recent_moves", "praise", "complaints", "risk_shown", "against_positioning"}
REQUIRED = ("name", "category", "segment", "pricing", "positioning", "risk_shown", "against_positioning")
LISTS = ("strengths", "weaknesses", "recent_moves", "praise", "complaints")  # mappings by name of facts


class Checker:
    """Collects errors (refuse) and stale facts (warn) of one entry."""

    def __init__(self, path, today):
        self.path = path
        self.today = today
        self.errors = []
        self.stale = []

    def error(self, field, msg):
        self.errors.append(f"{self.path}: {field}: {msg}")

    def fact(self, field, value, keys):
        """A mapping holding `keys` plus source_url and seen_on; returns it or None."""
        if not isinstance(value, dict):
            self.error(field, "expected a mapping with " + ", ".join(keys + SOURCE_KEYS))
            return None
        for k in sorted(set(value) - set(keys) - set(SOURCE_KEYS)):
            self.error(f"{field}.{k}", "unknown field")
        for k in keys:
            if value.get(k) in (None, ""):
                self.error(f"{field}.{k}", "missing")
        url = value.get("source_url")
        if not isinstance(url, str) or not url.strip():
            self.error(f"{field}.source_url", "missing — every fact needs its source")
        seen = value.get("seen_on")
        if seen in (None, ""):
            self.error(f"{field}.seen_on", "missing — every fact needs the date it was seen (YYYY-MM-DD)")
        else:
            try:
                day = datetime.date.fromisoformat(str(seen))
            except ValueError:
                self.error(f"{field}.seen_on", f"{seen!r} is not a date (YYYY-MM-DD)")
            else:
                age = (self.today - day).days
                if age < 0:
                    self.error(f"{field}.seen_on", f"{seen} is after today ({self.today})")
                elif age > STALE_DAYS:
                    self.stale.append(f"{self.path}: {field}: seen_on {seen} is {age} days old (> {STALE_DAYS})")
        return value


def check_entry(path, entry, today):
    """Validates one entry; returns (checker, normalised record for the summary)."""
    c = Checker(path, today)
    if not isinstance(entry, dict):
        c.error("(file)", "expected a mapping")
        return c, None
    for k in sorted(set(entry) - TOP_KEYS):
        c.error(k, "unknown field")
    for k in REQUIRED:
        if k not in entry:
            c.error(k, "missing")
    category = entry.get("category")
    if "category" in entry and category not in CATEGORIES:
        c.error("category", f"{category!r} is not one of {', '.join(CATEGORIES)}")
    for k in ("name", "segment"):
        if k in entry and (not isinstance(entry[k], str) or not entry[k].strip()):
            c.error(k, "expected text")

    if "pricing" in entry:
        pricing = c.fact("pricing", entry["pricing"], ("value_metric", "tiers"))
        tiers = (pricing or {}).get("tiers")
        if pricing is not None and tiers not in (None, "") and not isinstance(tiers, dict):
            c.error("pricing.tiers", "expected a mapping of tiers by name")
        elif isinstance(tiers, dict):
            for name, tier in tiers.items():
                t = c.fact(f"pricing.tiers.{name}", tier, ("price", "currency", "period"))
                if t is not None and t.get("price") is not None and not isinstance(t.get("price"), (int, float)):
                    c.error(f"pricing.tiers.{name}.price", "expected a number")
    if "positioning" in entry:
        c.fact("positioning", entry["positioning"], ("text",))
    for k in LISTS:
        if k not in entry:
            continue
        if not isinstance(entry[k], dict):
            c.error(k, "expected a mapping of facts by name")
            continue
        for name, item in entry[k].items():
            c.fact(f"{k}.{name}", item, ("text",))

    risk = None
    if "risk_shown" in entry:
        r = c.fact("risk_shown", entry["risk_shown"], ("value",))
        if r is not None and r.get("value") not in (None, ""):
            if r["value"] in RISK_VALUES:
                risk = r["value"]
            else:
                c.error("risk_shown.value", f"{r['value']!r} is not one of {'/'.join(RISK_VALUES)}")
    claims = {}
    if "against_positioning" in entry:
        ap = entry["against_positioning"]
        if not isinstance(ap, dict):
            c.error("against_positioning", "expected a mapping of " + ", ".join(POSITIONING))
        else:
            for k in sorted(set(ap) - set(POSITIONING)):
                c.error(f"against_positioning.{k}", "unknown field")
            for k in POSITIONING:
                field = f"against_positioning.{k}"
                if k not in ap:
                    c.error(field, "missing")
                    continue
                f = c.fact(field, ap[k], ("value",))
                if f is not None and f.get("value") not in (None, ""):
                    if f["value"] in RISK_VALUES:
                        claims[k] = f["value"]
                    else:
                        c.error(f"{field}.value", f"{f['value']!r} is not one of {'/'.join(RISK_VALUES)}")
    return c, {"category": category, "risk_shown": risk, "claims": claims, "stale": len(c.stale)}


def load(path):
    with open(path, encoding="utf-8") as f:
        return parse_yaml(f.read())


def entry_paths(args):
    paths = []
    for p in args or [PRIVATE_DIR]:
        if os.path.isdir(p):
            paths.extend(sorted(glob.glob(os.path.join(p, "*.yaml"))))
        else:
            paths.append(p)
    return paths


def summarise(records):
    """Per category: competitors, risk beside the yield, and each positioning claim:
    `open` when no competitor of the category makes it, else how many do (yes/partly).
    Counts only — never a name, price, URL or path."""
    lines = ["Competitor map summary (counts only; sources and prices in the private entries)", ""]
    for slug, title in CATEGORIES.items():
        rows = [r for r in records if r["category"] == slug]
        lines.append(f"## {title}")
        if not rows:
            lines.extend(["not mapped yet", ""])
            continue
        risk = {v: sum(1 for r in rows if r["risk_shown"] == v) for v in RISK_VALUES}
        lines.append(f"competitors: {len(rows)}")
        lines.append(f"risk shown beside the yield: yes {risk['yes']}, partly {risk['partly']}, no {risk['no']}")
        lines.append("against our positioning:")
        for key, claim in POSITIONING.items():
            yes = sum(1 for r in rows if r["claims"].get(key) == "yes")
            partly = sum(1 for r in rows if r["claims"].get(key) == "partly")
            state = "open (none does)" if yes + partly == 0 else f"contested: yes {yes}, partly {partly} of {len(rows)}"
            lines.append(f"- {claim}: {state}")
        stale = sum(r["stale"] for r in rows)
        if stale:
            lines.append(f"stale facts (> {STALE_DAYS} days): {stale}")
        lines.append("")
    return "\n".join(lines)


def main(argv=None):
    ap = argparse.ArgumentParser(description="Check the competitor entries; --summary prints the positioning gaps.")
    ap.add_argument("paths", nargs="*", help=f"entries or directories (default {PRIVATE_DIR})")
    ap.add_argument("--summary", action="store_true", help="per-category counts, safe to publish")
    ap.add_argument("--today", help="YYYY-MM-DD (default: today, UTC) — the date stale facts are measured from")
    args = ap.parse_args(argv)
    try:
        today = datetime.date.fromisoformat(args.today) if args.today else datetime.datetime.now(datetime.timezone.utc).date()
    except ValueError:
        print(f"competitors: --today {args.today!r} is not a date (YYYY-MM-DD)", file=sys.stderr)
        return 2
    paths = entry_paths(args.paths)
    if not paths:
        print("competitors: no entries found (one YAML per competitor in data/product/competitors/)", file=sys.stderr)
        return 2

    errors, stale, records = [], [], []
    for path in paths:
        try:
            entry = load(path)
        except (ScenarioError, OSError) as e:
            errors.append(f"{path}: {e}")
            continue
        c, record = check_entry(path, entry, today)
        errors.extend(c.errors)
        stale.extend(c.stale)
        if record is not None:
            records.append(record)

    for line in errors:
        print(f"ERROR: {line}", file=sys.stderr)
    if errors:
        print(f"competitors: {len(errors)} error(s) in {len(paths)} entr{'y' if len(paths) == 1 else 'ies'}; nothing summarised",
              file=sys.stderr)
        return 1
    if args.summary:
        # Stale facts are counted per category in the summary; their paths stay off stdout.
        print(summarise(records))
        return 0
    for line in stale:
        print(f"STALE: {line}")
    print(f"{len(paths)} entr{'y' if len(paths) == 1 else 'ies'} ok, {len(stale)} stale fact(s)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
