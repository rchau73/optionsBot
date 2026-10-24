# Product scripts

Tools for the product work of epic E5 (`docs/product/PRD.md` §6). Standard library only; no Go code, nothing that trades.

## Survey tally (`survey_tally.py`, #247)

Turns the cycle-holder survey's CSV export (`docs/product/discovery/survey.md`, `screener.md`) into the counts `synthesis.md` needs, the same way every time:

| Figure | Rule |
|---|---|
| Qualified / screened out | `screener.md` §3–§4: cycle holder, active option trader, or screened out |
| Choice questions | count and share of each option of Q1–Q3, Q5–Q8 over the qualified responses that answered it (Q8 several per answer) |
| NPS | N1 over qualified responses whose Q1 is not "never tried": % 9–10 − % 0–6; under 5 scores it shows the scores |
| Fake door | F1 → Joined (yes + e-mail), Declined (no), Undecided (not now, or yes with no e-mail) |
| Delegation share | Q6 "yes, and still do" + "yes, and took it back" ÷ qualified responses that answered Q6, for all qualified and cycle holders only, next to the PRD §6 kill threshold (< 30 %); n < 15 carries a small-sample note |

```bash
python3 scripts/product/survey_tally.py data/product/interviews/export.csv
python3 scripts/product/survey_tally.py data/product/interviews/export.csv -json -out data/product/interviews/tally.json
```

**Input:** one row per anonymous ID; columns `ID`, `S1`–`S8`, `Q1`–`Q8`, `N1`, `F1`, `F2`, `F1_email`. A multi-choice cell (S4, Q8) separates options with `;`. Answers match the docs' option wording ignoring case, quotes, dashes and spacing (an answer that starts with an option counts as it). If the form tool names its columns differently, `-map map.json` gives `{"export column": "item ID"}`, e.g. `{"Respondent": "ID", "Do you hold BTC or ETH today?": "S1"}`.

**Private data stays private.** Free text (Q4, F2), the F1 e-mail and answers that match no option are never printed — only how many there were. `-out` refuses any path outside `data/product/interviews/` (gitignored). The tests build a synthetic CSV inside the test; no real answer is in the repository.

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
