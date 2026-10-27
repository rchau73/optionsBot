# Go / no-go scorecard (E5)

**Status:** the decision inputs of epic E5, side by side (#305). `scripts/product/go_no_go.py` reads them and prints one scorecard. **The go / no-go decision is the product owner's**, on the whole evidence; the scorecard recommends nothing beyond PRD §6's own kill rule.

## 1. The kill criteria (PRD §6, quoted)

> <30 % of interviewed cycle holders would let software trade their account, or E2 shows the engine doesn't beat simply holding the coin on a risk-adjusted basis

They are quoted, not set, here: changing them is a PRD pull request. `scripts/product/test_go_no_go.py` fails if the PRD's wording or the script's 30 % moves alone.

## 2. Rows

| # | Input | Source | Threshold | meets / fails / missing |
|---|---|---|---|---|
| 1 | **Survey:** share of cycle holders who would let software trade their account (Q6: "yes, and still do" + "yes, and took it back") | `survey_tally.py -json` (#247) → `data/product/interviews/tally.json`, `delegation["cycle holder"].share_pct` | ≥ 30 % | fails under 30 % (**PRD §6 kill**); missing with no file or no cycle holder answering Q6. A sample under 15 is noted, not hidden |
| 2 | **Fake door** outcome | `fakedoor.py` (#303) → `data/product/fakedoor/<run>/result.json`, the newest run unless `--fakedoor` | Success (`experiments/fake-door-landing.md` §6) | meets on Success; fails on Kill or Iterate; missing when not run or Not enough traffic. Not a PRD kill input |
| 3 | **E2:** does the engine beat holding the coin, risk-adjusted | `cmd/evidence` weekly reports → `<clone>/data/evidence/<ISO week>/<bot>.json`, `hold.return_usd_pct` and `hold.hold_return_usd_pct` (weeks where both are known); worst `live.perf.MaxDrawdownPct` shown beside | §3 | fails when the engine's ratio is not above hold's (**PRD §6 kill**); missing under the window (`not enough weeks`) or with no spread |
| 4 | **Legal gate:** L1–L6 and L9 | `docs/legal/preliminary-answers.md`: a question's line carries `**Counsel (date):**` once counsel answers | every one answered by counsel | meets when all seven are answered (read the answers: the script cannot judge them); missing while any is preliminary. Never fails on its own |

Every row prints its value, threshold, status, source file and date (the file's date; for E2 the latest week). A missing input is `missing`, never assumed.

## 3. E2: the risk-adjusted test

- **Window:** the last **12 weekly reports** (a quarter) of the bot whose hold-the-coin and strategy returns are both known. Fewer → `not enough weeks`. The product owner may change the window here and in `E2_MIN_WEEKS` together.
- **Measure:** each week's return in USD of the engine (`return_usd_pct`: equity plus net P&L at the end spot against equity at the start spot) and of holding the coin (`hold_return_usd_pct`); the risk-adjusted figure of each is its weekly Sharpe ratio — mean ÷ standard deviation of the weekly returns, no risk-free rate.
- **Reading:** meets when the engine's ratio is above hold's; fails otherwise. A series with no spread has no ratio → missing.
- **Which bot:** `--evidence-root` (the environment clone, default this checkout) and `--bot` (default `btc`). The track record is stable's.

## 4. Overall line

1. **kill criterion met** — row 1 or row 3 fails (PRD §6), naming the failing input and its source file. Printed whatever the other rows say: either criterion alone is the PRD's kill.
2. **incomplete** — no kill, and any row is missing. Never "go".
3. **no kill criterion met** — every input is in and none kills; the decision is the product owner's.

## 5. Percentages only

A bot may run live, so the scorecard prints shares, ratios and percentages only — never a coin or USD amount, even when the evidence JSON carries them.

## 6. Run

```bash
python3 scripts/product/go_no_go.py
python3 scripts/product/go_no_go.py --tally data/product/interviews/tally.json \
    --fakedoor data/product/fakedoor/run1/result.json --evidence-root ~/dev/go/optionsBot-env/stable --bot btc
python3 scripts/product/go_no_go.py -json
```

Related: the survey's verdict section `discovery/synthesis.md` §4; pricing options `pricing-options.md` (nothing to price if a kill criterion is met).
