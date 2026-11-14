# Futures-hedge trial: decision brief (hedge vs shed)

Written before the result, so the trial ends with a decision and not an opinion (#780, epic E2: "a decision on each running trial"; modelled on `docs/direction-trial.md`, #324).
**This brief only proposes.**
The futures hedge (#657) replaces the GEX shed, a core strategy policy: a verdict goes to the product owner in a ticket marked `agent:needs-human`.
The weekly report's verdict never changes the hedge mode (`data/hedge_mode.json`), a setting, a config or the hedge's rules in `internal/hedge`.

Every threshold below is marked **assumption** until a person confirms it.

## Hypothesis

While the hedge is on, the GEX shed closes nothing and the perpetual holds the coin delta instead (CLAUDE.md "Futures hedge experiment").
The hypothesis under test: **keeping the threatened legs and hedging them with the perpetual is worth more than buying them back, net of fees, with no worse a tail.**

## Setup

One testnet bot at a time, switched by an admin from the monitor's futures hedge panel.
Only one approach trades the account: the other is followed on paper in the benchmark's ledger (`hedge.Ledger`: the real perpetual, the paper hedge, and each shed decision's counterfactual leg).
So the comparison is shadow-based, not a controlled test.

## The evidence: the weekly report's hedge section

`go run ./cmd/evidence -env mvp -week <YYYY-Www>` → "Futures hedge trial" (`internal/evidence/hedgetrial.go`, #678).
Every figure runs from the ledger's start to when it was read, through `hedge.Summary`, never recomputed.
Coin on testnet; a live account shows days and counts only and gets no verdict.

| Figure | Row in the section | Role |
|---|---|---|
| Advantage **A** | "Advantage: hedge P&L + counterfactual legs" (`advantage`) | decides |
| Fees **F** | "Fees (real + paper), already in the P&L" (`fees`) | the noise band |
| Hedge P&L net of fees | "Hedge P&L, net of fees" (`hedge_pnl`) | context: already in A |
| Worst day **W** | "Worst day of shed decisions" (`worst_day.value`): the ledger keeps no daily hedge P&L | tail guard |
| Hourly cap | "Real hedge orders an hour, mean (cap 12)" and "Hedges that waited for the hourly cap" (`hedge_rate_limited` lines of the journal) | blocks a proposal |
| Unavailable | "Episodes with the hedge unavailable" (`hedge_unavailable` lines of the journal, one per episode) | stop guard |
| Days **D** | "Days measured" beside every figure | minimum span |

## Minimum span

**7 days** since the ledger began (`HedgeTrialMinDays`); under it the section shows no figure and the verdict is `needs more days`, with the days missing.
**Assumption**: a week holds at least one GEX shed episode on a normal market.

A proposal needs **84 days** (`HedgeProposeMinDays` = `SampleMinDays`, #772): under it every figure reads "too few to conclude".
**Assumption**, revisited with the sample floors in 2027-W02.

## Outcomes

Checked in this order; the first that holds is the verdict (`hedgeVerdict`).

1. **`needs more days`**: D < 7.
2. **`stop`**: A < −F (the hedge did worse than the shed by more than its fees), **or** more than **1** `hedge_unavailable` episode a day on average (`HedgeUnavailablePerDayMax`, **assumption**: past it the hedge cannot be relied on to hold the delta).
3. **`keep running`**, no difference: |A| ≤ F.
   **Assumption**: a result inside its own fees decides nothing.
4. **`keep running`**, mixed: A > F but W < −½ A (one day of shed decisions cost more than half the advantage, `HedgeWorstDayShareMax`, **assumption**).
5. **`keep running`**: A > F and the tail guard holds, but the journal's hedge events were not read, a hedge waited for the 12 orders/h cap, or D < 84.
6. **`propose as a core-policy change`**: everything above passed.

No verdict without figures: not started, the mode file not read, no spot to mark the ledger at, or a live account.

## What happens with a verdict

1. The weekly report prints it as one line in the hedge section, with the figures it used.
2. `stop` or `propose as a core-policy change`: whoever reads the report files one ticket marked `agent:needs-human` with the verdict and its figures (`evidence.sh` does not file it yet).
   The product owner decides whether to switch the hedge off, keep it on paper, or make it the policy.
3. Switching the hedge on or off is done by a person from the monitor; making it the policy is a PR a person merges.
   Neither is ever done by an agent or by the report.
