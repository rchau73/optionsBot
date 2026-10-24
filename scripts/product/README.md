# Product scripts

Tools for the product work of epic E5 (`docs/product/PRD.md` §6). Standard library only; no Go code, nothing that trades.

## Competitor map (`competitors.py`, #304)

Checks the private competitor entries — one YAML per competitor in the gitignored `data/product/competitors/` — and prints the positioning gaps. Method, categories and fields: `docs/product/competitors/README.md`.

```bash
python3 scripts/product/competitors.py                     # check data/product/competitors/*.yaml
python3 scripts/product/competitors.py scripts/product/example_competitor.yaml --today 2026-10-23
python3 scripts/product/competitors.py --summary           # per category, safe to paste into PRD §4
```

- **Refused (exit 1):** a fact without `source_url` or `seen_on`, a date that is not `YYYY-MM-DD` or is after today, an unknown or missing field, a category outside PRD §4's five, a yes/partly/no field with another value. Each error names the file and the field; nothing is summarised.
- **Stale (warning, exit 0):** a fact seen more than 90 days before today (`--today` sets it; default today UTC), listed as `STALE: <file>: <field>: …`.
- **`--summary`:** per category, the number of competitors, how many show the risk beside the yield, and each PRD §5 claim as open or contested, plus the stale-fact count. Counts only — no name, price, URL or path.

`example_competitor.yaml` is fictional and lives here, outside `data/`, for the tests.

## Unit economics (`unit_economics.py`, #235)

Turns a pricing scenario — tiers, prices, expected mix, churn, CAC, cost to serve — into the numbers a pricing proposal needs, per tier and blended by the mix, each with a **pessimistic** row:

| Figure | Formula (per month) |
|---|---|
| ARPA | price × (1 − annual-plan share × annual discount) + capital fee |
| Gross margin | ARPA − infrastructure − exchange/API cost (and its % of ARPA) |
| Contribution margin | gross margin − support − payment fees (% of ARPA + fixed) |
| LTV | contribution margin ÷ monthly logo churn |
| LTV:CAC, CAC payback | LTV ÷ CAC; CAC ÷ contribution margin (months) |
| MRR after N months | cohort × mix × ARPA × (1 − churn)^N |

Blended ARPA, margins and LTV are the mix-weighted values (the mix is normalised); blended LTV:CAC and payback come from those. CAC is one number or channels weighted by `share_pct`; a tier may set its own. The pessimistic row multiplies churn by `churn_factor` (default 1.5) and conversion by `conversion_factor` (default 0.7, so CAC ÷ 0.7).

**Flags:** payback > 12 months, LTV:CAC < 3, negative contribution margin (CAC never paid back), gross margin under `gross_margin_target_pct`.

```bash
python3 scripts/product/unit_economics.py scripts/product/example_scenario.yaml
python3 scripts/product/unit_economics.py data/product/pricing/v1.yaml -format json -out data/product/pricing/v1.json
python3 -m unittest discover -s scripts/product -p 'test_*.py'
```

**Input:** `.json`, or a small YAML subset (nested mappings, `key: value`, `#` comments; tiers and channels are mappings by name, no lists). Every key is listed in `example_scenario.yaml`; an unknown key is refused.

**Private data stays private.** `example_scenario.yaml` holds fake round numbers marked `# placeholder, not a price`. Real assumptions and results go in the gitignored `data/product/`; `-out` refuses any path outside it.

**A subscription, not a share of profits** (PRD §6): performance or return inputs (`performance…`, `profit…`, `return…`, `yield…`, `pnl…`) are refused. A tier priced as a percentage of capital (`capital_fee_pct_per_year` with `avg_capital`) is computed but printed with the warning **needs legal review** — it can read as a management fee.
