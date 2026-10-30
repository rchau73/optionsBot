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

## Assistant answer grader (`assistant_grade.py`, #313)

Grades a JSONL file of help-assistant answers — `{"id", "text", "citations"}` per line, from any source; no model is called — against the golden question set `docs/product/assistant/golden.jsonl` and the help index `frontend/content/help-index.json` (epic E10). Rules: `docs/product/assistant/contract.md`; the set's fields: `docs/product/assistant/README.md`.

```bash
python3 scripts/product/assistant_grade.py answers.jsonl          # passed / total per category, then each failure
python3 scripts/product/assistant_grade.py answers.jsonl --json
```

- **Answer questions:** every citation exists in the index (an unknown one is named), one is an expected source, every `must_say` fact is there, and the answer neither refuses nor says "I don't know".
- **Refusals** (`refuse_advice`, `refuse_forecast`): a refusal is given (the contract's reply, or "I can't recommend / predict…"), with no number with % or a currency.
- **Unknown:** "I don't know".
- **Every question:** none of its `must_not_say` phrases.

Exit 0 when every question passes, 1 when one fails, 2 on bad input (unreadable JSON, an answer for an unknown id, two answers for one id).

## Go / no-go scorecard (`go_no_go.py`, #305)

Puts E5's decision inputs side by side — rows, thresholds and the E2 window in `docs/product/go-no-go.md`:

| Row | Source | Threshold |
|---|---|---|
| Survey: cycle holders who would delegate | `survey_tally.py -json` output (`--tally`, default `data/product/interviews/tally.json`) | ≥ 30 % (PRD §6 kill if under) |
| Fake door outcome | `fakedoor.py`'s `result.json` (`--fakedoor`, default the newest under `data/product/fakedoor/`) | Success |
| E2: engine vs holding the coin | weekly evidence reports `<--evidence-root>/data/evidence/<week>/<--bot>.json` | engine's weekly Sharpe > hold's over the last 12 weeks |
| Legal gate L1–L6, L9 | `docs/legal/preliminary-answers.md` (`--legal`) | all answered by counsel |

```bash
python3 scripts/product/go_no_go.py
python3 scripts/product/go_no_go.py --evidence-root ~/dev/go/optionsBot-env/stable --bot btc -json
```

Each row: value, threshold, status (`meets` / `fails` / `missing`), source file and date. A missing input stays `missing`. **Overall:** `kill criterion met` when the survey or E2 row fails (PRD §6), else `incomplete` while any row is missing, else `no kill criterion met` — the decision is the product owner's. Percentages and ratios only: no coin or USD amount is printed, even when the evidence JSON has them.
