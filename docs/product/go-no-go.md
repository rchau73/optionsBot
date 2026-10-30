# Go / no-go scorecard (E5)

**Status:** the decision inputs of epic E5, side by side (#305).
`scripts/product/go_no_go.py` reads them and prints one scorecard.
**The go / no-go decision is the product owner's**, on the whole evidence; the scorecard recommends nothing beyond PRD §6's own kill rule.
Sections 7–10 are the reading of 2026-10-29 (#491): what is known, what is assumed, what is missing and what only a person can do.

## 1. The kill criteria (PRD §6, quoted)

> <30 % of interviewed cycle holders would let software trade their account, or E2 shows the engine doesn't beat simply holding the coin on a risk-adjusted basis

They are quoted, not set, here: changing them is a PRD pull request.
`scripts/product/test_go_no_go.py` fails if the PRD's wording or the script's 30 % moves alone.

## 2. Rows

| # | Input | Source | Threshold | meets / fails / missing |
|---|---|---|---|---|
| 1 | **Survey:** share of cycle holders who would let software trade their account (Q6: "yes, and still do" + "yes, and took it back") | `survey_tally.py -json` (#247) → `data/product/interviews/tally.json`, `delegation["cycle holder"].share_pct` | ≥ 30 % | fails under 30 % (**PRD §6 kill**); missing with no file or no cycle holder answering Q6. A sample under 15 is noted, not hidden |
| 2 | **Fake door** outcome | `fakedoor.py` (#303) → `data/product/fakedoor/<run>/result.json`, the newest run unless `--fakedoor` | Success (`experiments/fake-door-landing.md` §6) | meets on Success; fails on Kill or Iterate; missing when not run or Not enough traffic. Not a PRD kill input |
| 3 | **E2:** does the engine beat holding the coin, risk-adjusted | `cmd/evidence` weekly reports → `<clone>/data/evidence/<ISO week>/<bot>.json`, `hold.return_usd_pct` and `hold.hold_return_usd_pct` (weeks where both are known); worst `live.perf.MaxDrawdownPct` shown beside | §3 | fails when the engine's ratio is not above hold's (**PRD §6 kill**); missing under the window (`not enough weeks`) or with no spread |
| 4 | **Legal gate:** L1–L6 and L9 | `docs/legal/preliminary-answers.md`: a question's line carries `**Counsel (date):**` once counsel answers | every one answered by counsel | meets when all seven are answered (read the answers: the script cannot judge them); missing while any is preliminary. Never fails on its own |

Every row prints its value, threshold, status, source file and date (the file's date; for E2 the latest week).
A missing input is `missing`, never assumed.

## 3. E2: the risk-adjusted test

- **Window:** the last **12 weekly reports** (a quarter) of the bot whose hold-the-coin and strategy returns are both known.
  Fewer → `not enough weeks`.
  The product owner may change the window here and in `E2_MIN_WEEKS` together.
- **Measure:** each week's return in USD of the engine (`return_usd_pct`: equity plus net P&L at the end spot against equity at the start spot) and of holding the coin (`hold_return_usd_pct`); the risk-adjusted figure of each is its weekly Sharpe ratio — mean ÷ standard deviation of the weekly returns, no risk-free rate.
- **Reading:** meets when the engine's ratio is above hold's; fails otherwise.
  A series with no spread has no ratio → missing.
- **Which bot:** `--evidence-root` (the environment clone, default this checkout) and `--bot` (default `btc`).
  The track record is stable's.

## 4. Overall line

1. **kill criterion met** — row 1 or row 3 fails (PRD §6), naming the failing input and its source file.
   Printed whatever the other rows say: either criterion alone is the PRD's kill.
2. **incomplete** — no kill, and any row is missing.
   Never "go".
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

## 7. Each go-to-market criterion: evidence or assumption

PRD §6 and epic E5 ask four things: who the buyer is, which pain they hire us for, what they would pay and what we may legally offer.
Each is **evidence** (a dated source that exists) or an **assumption** (with the way to verify it).
Anything that holds a name, a price or a revenue figure is in the gitignored `data/product/`; this page only points to it.
The file `data/product/` does not exist in a fresh checkout, so "no data" below means no answer has been collected there yet.

| Criterion | Status | Evidence and date | Gap and how to verify |
|---|---|---|---|
| **Buyer:** the cycle holder (PRD §3) | Evidence for the choice, **assumption** for the size | The product owner confirmed the persona (`decisions/2026-10-18-primary-persona.md`, 2026-10-18). Screener `discovery/screener.md` defines who qualifies (#178) | How many cycle holders there are is an assumption. Verify: send the survey (`discovery/survey.md`) and read the screener's qualified count in the tally (`survey_tally.py`, #247) |
| **Pain:** opaque yield, no control of risk, too complex by hand (PRD §3) | **Assumption** | Written from the product owner's own experience and the persona table (PRD v0.2, 2026-10-18) | Verify: the survey's Q4 and F2 answers, coded in `discovery/synthesis.md` §2 (a pain counts when it happened to the respondent). No answers yet |
| **Delegation:** would let software trade their account | **No data** | Survey and tally tool ready (#178, #247); the scorecard row 1 reads the result | Verify: a tally with at least 15 qualified cycle holders. Below 15 the share is noted, not trusted |
| **Willingness to pay** | **Assumption** | Options and trade-offs only: `pricing-options.md` (#236, prepared before any data); unit-economics tool `unit_economics.py` (#235) | No customer answer yet. Verify: the survey's price questions and the fake door (below). The decision is the product owner's, after counsel answers L8 |
| **Demand signal:** a waiting list | **No data** | Experiment brief `experiments/fake-door-landing.md` (#234) and copy draft `landing-copy-draft.md`; result reader `fakedoor.py` (#303) | The page is not online. Verify: publish it after counsel clears L5, L7, L9 and L13, then read the result. Scorecard row 2 |
| **Competitors and positioning gaps** (PRD §4) | **No data** | The method and checker exist (`competitors/README.md`, #304) | The research goes in `data/product/competitors/` and has not been done. Verify: one entry per competitor with its source and date; `competitors.py --summary` feeds PRD §4 |
| **What we may legally offer** | **Assumption** (preliminary, not counsel's) | Questions L1–L24 in `legal-brief.md` (#179); the legal counsel skill's preliminary answers in `docs/legal/preliminary-answers.md` (#228), written as a draft for a lawyer | No `Counsel (date):` line yet, so no question is answered by counsel. Verify: counsel's written answers, then record them as in `docs/legal/counsel-engagement.md`. Until then: internal use and testnet only |
| **Engine beats holding the coin** (E2) | **Evidence**, read by the scorecard | Weekly evidence reports (`internal/evidence`), scorecard row 3, window of section 3 | Needs the full 12-week window. Run `python3 scripts/product/go_no_go.py` for today's reading |

## 8. Kill criteria and the current reading

| Kill criterion (PRD §6) | Current reading | First step |
|---|---|---|
| Fewer than 30 % of interviewed cycle holders would let software trade their account | **No data** (no tally file in `data/product/interviews/`) | Send the survey; run `survey_tally.py -json`; rerun the scorecard |
| E2 shows the engine does not beat holding the coin on a risk-adjusted basis | Not read here: run the scorecard against the stable clone (`--evidence-root`), which holds the track record | Let the weekly reports reach the 12-week window; read row 3 |

Two gates are not kill criteria but stop an offer: the legal gate (row 4, PRD §11) and the fake-door outcome (row 2).
The overall line of the scorecard stays `incomplete` until every row has an input (section 4).

## 9. What only a person can do

| What | Who | Ticket | Unblocks |
|---|---|---|---|
| Send the survey to cycle holders and export the answers into `data/product/interviews/` | Product owner | #178 | Kill criterion 1, buyer, pain, willingness to pay |
| Choose the waiting-list tool, approve the page and publish it after counsel's review | Product owner | #234 | Demand signal, price test of software |
| Get counsel's written answers to L1–L6 and L9 first, then the rest of L1–L24 | Product owner with counsel | #179 | Legal gate, fake door, any offer to third parties |
| Do the competitor research and save it in `data/product/competitors/` | Product owner | #304 | PRD §4 and positioning |
| Decide the pricing, after the survey, the fake door and counsel's answer on L8 | Product owner | #236 | Pricing hypothesis of PRD §6 |

Open state is checked on the board, not here: if a ticket above is closed and its work is not done, it needs a new request with `agent:pm` and `agent:needs-human`.

## 10. Keeping this page true

- Update sections 7 and 8 when a source changes: a tally, a fake-door result, counsel's answer or a new scorecard reading, each with its date.
- No name, price, revenue figure or survey answer goes here, only the path under `data/product/` where it lives.
- A change to the kill criteria is a PRD pull request, never an edit here.
