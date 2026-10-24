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

## Fake-door result (`fakedoor.py`, #303)

Turns a run of the landing-page fake door (`docs/product/experiments/fake-door-landing.md`) into its §10 result, applying the §6 decision rule as written. The thresholds are constants copied from the brief (400 visits, 4 weeks, Success ≥ 10 % with lower bound ≥ 6 % and cycle holders ≥ half, Kill < 3 % with upper bound < 6 %, survey 30 %); `test_fakedoor.py` pins them — moving one is a new brief, not a code change.

```bash
python3 scripts/product/fakedoor.py data/product/fakedoor/run1
python3 scripts/product/fakedoor.py data/product/fakedoor/run1 --survey-share 0.34   # the survey's share (#247), a fraction
```

**Input** (in the run directory, which must be inside the gitignored `data/product/`; other names with `--signups`/`--visits`):

| File | Columns |
|---|---|
| `signups.csv` | `submitted_at`, `confirmed` (yes/no), `channel`, `country`, `first_bought` (a year, `before 2022` or `2022 or later`), `kept_most` (yes/no), `first_wanted`, `fair_pay` — one row per submission; blank segment/choice answers are "not answered" |
| `visits.csv` | `date` (YYYY-MM-DD), `channel`, `visits` |

Only these columns are read; any other — an `email` column — is ignored and never written. An unknown value in a yes/no or year column is refused with its line number.

**Output:** `result.md` and `result.json` beside the input — US sign-ups that got through first (a guardrail line telling the product owner to stop the run), then visits, confirmed sign-ups, the rate with its Wilson 95 % interval, per-channel rates with intervals, the cycle-holder share (confirmed sign-ups that bought before 2022 and kept most ÷ confirmed sign-ups that answered both), the secondary shares, the confirmation rate (confirmed ÷ submitted) and the outcome: **Not enough traffic** (< 400 visits, whatever the rate) · **Success** · **Kill** (with `--survey-share`: whether PRD §6's kill criterion is met) · **Iterate** (everything between, or a Success rate with cycle holders under half or none answering).
