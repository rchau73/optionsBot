#!/usr/bin/env python3
"""Unit economics of a pricing scenario (#235, epic E5).

Reads a scenario (YAML subset or JSON) of subscription tiers and prints, per tier
and blended by the expected mix: ARPA, gross margin, contribution margin, LTV,
LTV:CAC, CAC payback and a cohort's MRR after N months — each with a pessimistic
row (churn and conversion shifted by stated factors) and flags.

    python3 scripts/product/unit_economics.py scripts/product/example_scenario.yaml
    python3 scripts/product/unit_economics.py data/product/pricing/a.yaml -format json -out data/product/pricing/a.json

Standard library only. Real assumptions stay in the gitignored data/product/: -out
refuses any path outside it. The model prices a software subscription, never a
share of profits (PRD §6): performance or return inputs are refused, and a fee
that scales with capital is reported as "needs legal review".

Formulas (per month, in the scenario's currency):
    ARPA          = price × (1 − annual share × annual discount) + capital fee
    gross margin  = ARPA − infrastructure − exchange/API cost
    contribution  = gross margin − support − payment fees (% of ARPA + fixed)
    LTV           = contribution ÷ monthly churn
    CAC           = Σ channel share × channel CAC (or one number); pessimistic: ÷ conversion factor
    payback       = CAC ÷ contribution (months)
    MRR after N   = cohort × mix × ARPA × (1 − churn)^N
"""
import argparse
import json
import math
import os
import sys

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
PRIVATE_DIR = os.path.join(REPO, "data", "product")

LEGAL_WARNING = "needs legal review"
PAYBACK_MAX_MONTHS = 12
LTV_CAC_MIN = 3

TIER_KEYS = {
    "monthly_price", "annual_share_pct", "annual_discount_pct", "mix_pct", "monthly_churn_pct",
    "cost_to_serve", "payment_fee_pct", "payment_fee_fixed", "cac",
    "capital_fee_pct_per_year", "avg_capital",
}
COST_KEYS = {"infrastructure", "exchange_api", "support"}
TOP_KEYS = {"currency", "tiers", "cac", "gross_margin_target_pct", "cohort_size", "cohort_months", "pessimistic"}
PESSIMISTIC_KEYS = {"churn_factor", "conversion_factor"}
# Words that would make the price depend on trading results (PRD §6: no share of profits).
PERFORMANCE_WORDS = ("performance", "profit", "return", "yield", "pnl", "high_water")


class ScenarioError(ValueError):
    """The scenario cannot be priced as given."""


# --- input --------------------------------------------------------------------

def parse_scalar(text):
    text = text.strip()
    if text == "" or text in ("null", "~"):
        return None
    if len(text) >= 2 and text[0] == text[-1] and text[0] in "\"'":
        return text[1:-1]
    if text in ("true", "false"):
        return text == "true"
    try:
        return int(text)
    except ValueError:
        pass
    try:
        return float(text)
    except ValueError:
        return text


def strip_comment(line):
    """Drops a `#` comment that is outside quotes and starts the line or follows a space."""
    quote = None
    for i, ch in enumerate(line):
        if quote:
            if ch == quote:
                quote = None
        elif ch in "\"'":
            quote = ch
        elif ch == "#" and (i == 0 or line[i - 1] in " \t"):
            return line[:i]
    return line


def parse_yaml(text):
    """The YAML this tool needs: nested mappings by indentation, `key: value`, comments.
    No lists, anchors or multi-line values — tiers and channels are mappings by name."""
    root = {}
    stack = [(-1, root)]  # (indent, mapping)
    for num, raw in enumerate(text.splitlines(), 1):
        line = strip_comment(raw).rstrip()
        if not line.strip():
            continue
        if "\t" in line[: len(line) - len(line.lstrip())]:
            raise ScenarioError(f"line {num}: indent with spaces, not tabs")
        indent = len(line) - len(line.lstrip(" "))
        body = line.strip()
        if body.startswith("- "):
            raise ScenarioError(f"line {num}: lists are not supported; use a mapping by name")
        key, sep, value = body.partition(":")
        if not sep or not key.strip():
            raise ScenarioError(f"line {num}: expected `key: value`")
        while indent <= stack[-1][0]:
            stack.pop()
        parent = stack[-1][1]
        if parent is None:
            raise ScenarioError(f"line {num}: indented under a key that has a value")
        key = key.strip()
        if key in parent:
            raise ScenarioError(f"line {num}: duplicate key {key!r}")
        if value.strip() == "":
            child = {}
            parent[key] = child
            stack.append((indent, child))
        else:
            parent[key] = parse_scalar(value)
            stack.append((indent, None))
    return root


def load_scenario(path):
    with open(path, encoding="utf-8") as f:
        text = f.read()
    if path.endswith(".json"):
        try:
            return json.loads(text)
        except json.JSONDecodeError as e:
            raise ScenarioError(f"{path}: {e}") from e
    return parse_yaml(text)


# --- validation ---------------------------------------------------------------

def _check_keys(where, mapping, allowed):
    if not isinstance(mapping, dict):
        raise ScenarioError(f"{where}: expected a mapping")
    for key in mapping:
        low = key.lower()
        if any(w in low for w in PERFORMANCE_WORDS):
            raise ScenarioError(f"{where}.{key}: performance or return inputs are not allowed — "
                                "the model prices a software subscription, not a share of profits (PRD §6)")
        if key not in allowed:
            raise ScenarioError(f"{where}.{key}: unknown key (allowed: {', '.join(sorted(allowed))})")


def _num(where, value, lo=0.0, hi=math.inf, lo_open=False):
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ScenarioError(f"{where}: expected a number, got {value!r}")
    if value < lo or value > hi or (lo_open and value == lo):
        bound = f"> {lo}" if lo_open else f">= {lo}"
        raise ScenarioError(f"{where}: {value} out of range ({bound}" + (f", <= {hi})" if hi != math.inf else ")"))
    return float(value)


def blended_cac(where, cac):
    """One number, or channels {name: {cac, share_pct}} weighted by share."""
    if not isinstance(cac, dict):
        return _num(where, cac)
    if not cac:
        raise ScenarioError(f"{where}: no channels")
    total_share, weighted = 0.0, 0.0
    for name, ch in cac.items():
        _check_keys(f"{where}.{name}", ch, {"cac", "share_pct"})
        c = _num(f"{where}.{name}.cac", ch.get("cac"))
        s = _num(f"{where}.{name}.share_pct", ch.get("share_pct"), lo=0, hi=100)
        total_share += s
        weighted += s * c
    if total_share <= 0:
        raise ScenarioError(f"{where}: channel shares sum to 0")
    return weighted / total_share


def cost_parts(where, cost):
    """cost_to_serve as one number (all counted below gross margin) or {infrastructure, exchange_api, support}."""
    if cost is None:
        return 0.0, 0.0
    if not isinstance(cost, dict):
        return _num(where, cost), 0.0
    _check_keys(where, cost, COST_KEYS)
    cogs = sum(_num(f"{where}.{k}", cost.get(k, 0)) for k in ("infrastructure", "exchange_api"))
    return cogs, _num(f"{where}.support", cost.get("support", 0))


# --- model --------------------------------------------------------------------

def tier_economics(t, cac, churn, months, cohort):
    """One row. `t` holds validated numbers; churn is a monthly fraction > 0."""
    arpa = t["price"] * (1 - t["annual_share"] * t["annual_discount"]) + t["capital_fee"]
    gross = arpa - t["cogs"]
    fees = arpa * t["payment_fee_pct"] / 100 + t["payment_fee_fixed"]
    contribution = gross - t["support"] - fees
    ltv = contribution / churn
    return {
        "arpa": arpa,
        "gross_margin": gross,
        "gross_margin_pct": gross / arpa * 100 if arpa > 0 else None,
        "contribution_margin": contribution,
        "monthly_churn_pct": churn * 100,
        "cac": cac,
        "ltv": ltv,
        "ltv_cac": ltv / cac if cac > 0 else None,
        "payback_months": cac / contribution if contribution > 0 else None,
        "mrr_after_n": cohort * t["mix"] * arpa * (1 - churn) ** months,
    }


def flags_of(row, gm_target_pct):
    out = []
    if row["contribution_margin"] < 0:
        out.append("negative contribution margin")
    if row["payback_months"] is None:
        out.append("CAC never paid back")
    elif row["payback_months"] > PAYBACK_MAX_MONTHS:
        out.append(f"payback > {PAYBACK_MAX_MONTHS} months")
    if row["ltv_cac"] is not None and row["ltv_cac"] < LTV_CAC_MIN:
        out.append(f"LTV:CAC < {LTV_CAC_MIN}")
    if gm_target_pct is not None and row["gross_margin_pct"] is not None and row["gross_margin_pct"] < gm_target_pct:
        out.append(f"gross margin < target {gm_target_pct:g} %")
    return out


def blend(rows, weights, cac, months_cohort_mrr):
    """Mix-weighted ARPA, margins and LTV; ratios from the blended values."""
    def w(key):
        return sum(weights[n] * rows[n][key] for n in rows)
    contribution = w("contribution_margin")
    arpa = w("arpa")
    gross = w("gross_margin")
    ltv = w("ltv")
    return {
        "arpa": arpa,
        "gross_margin": gross,
        "gross_margin_pct": gross / arpa * 100 if arpa > 0 else None,
        "contribution_margin": contribution,
        "monthly_churn_pct": w("monthly_churn_pct"),
        "cac": cac,
        "ltv": ltv,
        "ltv_cac": ltv / cac if cac > 0 else None,
        "payback_months": cac / contribution if contribution > 0 else None,
        "mrr_after_n": months_cohort_mrr,
    }


def analyse(scenario):
    """Validates a scenario and returns the report as a dict (see the module docstring)."""
    _check_keys("scenario", scenario, TOP_KEYS)
    tiers = scenario.get("tiers")
    if not isinstance(tiers, dict) or not tiers:
        raise ScenarioError("scenario.tiers: at least one tier is needed")
    global_cac = blended_cac("scenario.cac", scenario["cac"]) if "cac" in scenario else None
    gm_target = scenario.get("gross_margin_target_pct")
    if gm_target is not None:
        gm_target = _num("scenario.gross_margin_target_pct", gm_target, hi=100)
    cohort = _num("scenario.cohort_size", scenario.get("cohort_size", 100), lo_open=True)
    months = int(_num("scenario.cohort_months", scenario.get("cohort_months", 12)))
    pess = scenario.get("pessimistic") or {}
    _check_keys("scenario.pessimistic", pess, PESSIMISTIC_KEYS)
    churn_factor = _num("scenario.pessimistic.churn_factor", pess.get("churn_factor", 1.5), lo=0, lo_open=True)
    conversion_factor = _num("scenario.pessimistic.conversion_factor", pess.get("conversion_factor", 0.7),
                             lo=0, hi=1, lo_open=True)

    parsed, warnings = {}, []
    for name, raw in tiers.items():
        where = f"tiers.{name}"
        _check_keys(where, raw, TIER_KEYS)
        cogs, support = cost_parts(f"{where}.cost_to_serve", raw.get("cost_to_serve"))
        capital_fee = 0.0
        if "capital_fee_pct_per_year" in raw:
            pct = _num(f"{where}.capital_fee_pct_per_year", raw["capital_fee_pct_per_year"], hi=100)
            capital = _num(f"{where}.avg_capital", raw.get("avg_capital"))
            capital_fee = pct / 100 * capital / 12
            warnings.append(f"{name}: fee scales with capital ({pct:g} % a year) — {LEGAL_WARNING} "
                            "(it can read as a management fee, PRD §6)")
        elif "avg_capital" in raw:
            raise ScenarioError(f"{where}.avg_capital: only used with capital_fee_pct_per_year")
        if "cac" in raw:
            cac = blended_cac(f"{where}.cac", raw["cac"])
        elif global_cac is not None:
            cac = global_cac
        else:
            raise ScenarioError(f"{where}: no CAC (set scenario.cac or the tier's cac)")
        parsed[name] = {
            "price": _num(f"{where}.monthly_price", raw.get("monthly_price")),
            "annual_share": _num(f"{where}.annual_share_pct", raw.get("annual_share_pct", 0), hi=100) / 100,
            "annual_discount": _num(f"{where}.annual_discount_pct", raw.get("annual_discount_pct", 0), hi=100) / 100,
            "mix": _num(f"{where}.mix_pct", raw.get("mix_pct", 100 if len(tiers) == 1 else None), hi=100),
            "churn": _num(f"{where}.monthly_churn_pct", raw.get("monthly_churn_pct"), lo=0, hi=100, lo_open=True) / 100,
            "cogs": cogs,
            "support": support,
            "payment_fee_pct": _num(f"{where}.payment_fee_pct", raw.get("payment_fee_pct", 0), hi=100),
            "payment_fee_fixed": _num(f"{where}.payment_fee_fixed", raw.get("payment_fee_fixed", 0)),
            "capital_fee": capital_fee,
            "cac": cac,
        }

    total_mix = sum(t["mix"] for t in parsed.values())
    if total_mix <= 0:
        raise ScenarioError("tiers: mix_pct sums to 0")
    weights = {n: t["mix"] / total_mix for n, t in parsed.items()}
    for t, wgt in zip(parsed.values(), weights.values()):
        t["mix"] = wgt

    def run(churn_mult, cac_div):
        rows = {n: tier_economics(t, t["cac"] / cac_div, min(1.0, t["churn"] * churn_mult), months, cohort)
                for n, t in parsed.items()}
        cac = sum(weights[n] * rows[n]["cac"] for n in rows)
        total_mrr = sum(r["mrr_after_n"] for r in rows.values())
        rows["blended"] = blend(rows, weights, cac, total_mrr)
        for r in rows.values():
            r["flags"] = flags_of(r, gm_target)
        return rows

    return {
        "currency": scenario.get("currency", ""),
        "cohort_size": cohort,
        "cohort_months": months,
        "mix": {n: w * 100 for n, w in weights.items()},
        "pessimistic_case": {"churn_factor": churn_factor, "conversion_factor": conversion_factor},
        "base": run(1.0, 1.0),
        "pessimistic": run(churn_factor, conversion_factor),
        "warnings": warnings,
    }


# --- output -------------------------------------------------------------------

def _fmt(v, digits=2):
    if v is None:
        return "—"
    return f"{v:,.{digits}f}"


def render_text(report):
    cur = report["currency"]
    unit = f" ({cur})" if cur else ""
    pc = report["pessimistic_case"]
    lines = [
        f"Unit economics per month{unit} — cohort of {report['cohort_size']:g}, MRR after {report['cohort_months']} months",
        f"Pessimistic: churn × {pc['churn_factor']:g}, conversion × {pc['conversion_factor']:g} (CAC ÷ {pc['conversion_factor']:g})",
        "",
    ]
    head = ["tier", "case", "mix %", "ARPA", "gross", "GM %", "contrib.", "churn %", "CAC", "LTV", "LTV:CAC",
            "payback mo", "MRR after N", "flags"]
    table = [head]
    for name in report["base"]:
        for case in ("base", "pessimistic"):
            r = report[case][name]
            mix = 100.0 if name == "blended" else report["mix"][name]
            table.append([name if case == "base" else "", case, _fmt(mix, 0), _fmt(r["arpa"]),
                          _fmt(r["gross_margin"]), _fmt(r["gross_margin_pct"], 1), _fmt(r["contribution_margin"]),
                          _fmt(r["monthly_churn_pct"], 1), _fmt(r["cac"]), _fmt(r["ltv"]), _fmt(r["ltv_cac"]),
                          _fmt(r["payback_months"], 1), _fmt(r["mrr_after_n"]), "; ".join(r["flags"]) or "ok"])
    widths = [max(len(row[i]) for row in table) for i in range(len(head))]
    for row in table:
        lines.append("  ".join(c.ljust(w) for c, w in zip(row, widths)).rstrip())
    if report["warnings"]:
        lines.append("")
        lines.extend(f"WARNING: {w}" for w in report["warnings"])
    return "\n".join(lines) + "\n"


def check_out_path(path, private_dir=PRIVATE_DIR):
    """Returns the absolute path if it lies inside data/product/, else raises."""
    target = os.path.realpath(os.path.abspath(path))
    root = os.path.realpath(private_dir)
    if os.path.commonpath([target, root]) != root or target == root:
        raise ScenarioError(f"-out {path}: refused — real assumptions and results stay in {root}/ (gitignored)")
    return target


def main(argv=None):
    ap = argparse.ArgumentParser(description="Unit economics of a pricing scenario (LTV, CAC payback, margins).")
    ap.add_argument("scenario", help="scenario file (.yaml or .json)")
    ap.add_argument("-format", choices=("text", "json"), default="text")
    ap.add_argument("-out", help="write the report here; must be inside data/product/")
    args = ap.parse_args(argv)
    try:
        out = check_out_path(args.out) if args.out else None
        report = analyse(load_scenario(args.scenario))
    except (ScenarioError, OSError) as e:
        print(f"unit_economics: {e}", file=sys.stderr)
        return 2
    text = json.dumps(report, indent=2) + "\n" if args.format == "json" else render_text(report)
    if out:
        os.makedirs(os.path.dirname(out), exist_ok=True)
        with open(out, "w", encoding="utf-8") as f:
            f.write(text)
        print(f"wrote {out}")
    else:
        sys.stdout.write(text)
    if out or args.format == "json":  # the text report already lists them on stdout
        for w in report["warnings"]:
            print(f"WARNING: {w}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
