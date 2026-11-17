# Pricing proposal (E5)

**Status:** one decision page for the product owner (#762, epic E5). It sums up `pricing-options.md` (#236) and the unit-economics tool `scripts/product/unit_economics.py` (#235) and does not copy them: read those for the full trade-offs.
**The decision is the product owner's** (PRD §6, `decisions/2026-10-18-pricing-owner-decides.md`).

**No price is published here, and none is published before counsel answers L8.** This repository is public: the page holds no currency amount, no price and no fee stated as a share of capital.
The figures live in the gitignored `data/product/pricing/` (see §3).
Nothing here promises a return: the price, whatever it is, pays for **software** (the engine, the monitor, the explanations), never for a result.

How to read the marks: **evidence** is a fact the repository or a document already shows; **assumption** is a belief nobody has tested, and each one names the step that verifies it (`Verify:`).

## 1. Value metric

The value metric is the unit the price grows with. Candidates, each scored 1 (poor) to 3 (good):

| Candidate | Easy to understand | Grows with the customer's success | Hard to game |
|---|---|---|---|
| **Flat subscription** (one price per tier) | 3 (**evidence**: nothing to explain) | 1 (**evidence**: the same price for a small and a large account) | 3 (**evidence**: there is nothing to move) |
| **Per connected account** | 3 (**assumption**) Verify: interview question on how many exchange accounts a holder would connect (#178) | 2 (**assumption**) Verify: the same interview answers; a second account is read as trust | 2 (**assumption**) Verify: ask what holders do with several accounts; accounts can be merged on the exchange |
| **Capital automated** | 2 (**assumption**) Verify: interviews ask whether holders expect the price to follow the size of their stack | 3 (**evidence**: the fee grows with the capital by construction) | 1 (**evidence**: capital can be moved off before a billing date) |

Legal exposure is the other axis: flat and per account count software, capital automated **needs legal review** (L8).
Strategies running and seats are scored in `pricing-options.md` §1; a share of profits is excluded by PRD §6.

## 2. Tier shapes

Good, better and best by persona (PRD §3). Each tier has one or two fences and no price.

| Tier | Persona | Fences | Depends on |
|---|---|---|---|
| **Good** | curious beginner | testnet only, no live key; education and the monitor | E4 trial, parked behind the legal gate (L6); named testers only today |
| **Better** | cycle holder | one connected account; the default strategy and its settings | legal gate L1–L5 before it may be live; the interviews (#178) |
| **Best** | active trader | several accounts or coins; manager role for parameter changes after a comparison backtest | L10 and L24 for the manager role; E8 for more exchanges; E9 for the comparison |

Alternative shape: **per connected account** (one price per account, every feature included, the beginner tier stays testnet only).
A shape by **capital band** is listed only to be refused for now: its band edges are a disguised capital fee, **needs legal review** (L8 with L1, L4, L5), and it is not shown on the fake door.

## 3. Unit-economics method

The owner supplies these inputs; each is an **assumption** until measured.

| Input | Status | Verify |
|---|---|---|
| Contribution margin per tier (price less cost to serve, support, payment fees) | **assumption** | Verify: take infrastructure, exchange and API cost from the running environments' bills; support from the ticket log |
| CAC payback by channel (content and communities only, PRD §6) | **assumption** | Verify: the fake door's visits and sign-ups per channel (`experiments/fake-door-landing.md`) |
| LTV:CAC | **assumption** | Verify: derived from the two rows above and churn; recompute with each new measurement |
| NRR and GRR | **assumption** | Verify: no cohort exists before a paid beta; until then treat as unknown, never as a target |

Run: `python3 scripts/product/unit_economics.py data/product/pricing/<scenario>.yaml --scenario`.
It writes `data/product/pricing/scenarios.md` with the base case and the **pessimistic-conversion** case (churn and conversion shifted by stated factors), every input labelled assumption.
It writes nothing outside `data/product/`, which is gitignored, and it refuses performance or return inputs.

## 4. Churn levers

Each lever is checked against the no-dark-patterns rule of `pricing-options.md` §5: it says when it ends, what it costs after, and cancelling is as easy as signing up (one button, same page, no call or retention screen).

| Lever | No dark patterns check | Status |
|---|---|---|
| **Pause** (no trading, monitor and history kept) | passes: the customer chooses the end date, it cancels with the same one button | **assumption** Verify: interview question on stopping yield products in bull runs |
| **Downgrade** (move to a lower tier, one step) | passes if it is as easy as upgrading and the new price is shown before it applies | **assumption** Verify: interview question on what made them leave a crypto product before |
| **Annual plan** (cheaper per month, shown next to monthly, never pre-selected) | passes: reminder with the renewal price a month before; refund rule stated at purchase (counsel: consumer law) | **assumption** Verify: interview question on a year's commitment for a lower monthly price |
| **Dunning** (failed payments: a notice, a retry and a grace period) | passes only if the notice says the amount, the date and how to cancel, and never threatens or hides the cancel button | **assumption** Verify: counsel reads the notice text before first use |

## 5. Legal flags

- **L8: needs legal review.** A fee as a share of capital or of profits reinforces the portfolio-management reading; a flat subscription per account is the cleanest (`docs/legal/preliminary-answers.md`).
- **Any fee that scales with capital: needs legal review**, including capital bands.
- **No profit share**, in any form (PRD §6).
- **No price is published before counsel answers L8** (and L9, a price on a waiting list).

## 6. Decision requested

The owner decides which value metric and tier shape go into the next interviews and the later price test. Options:

| Option | For it | Against it |
|---|---|---|
| A. Flat tiers by persona | simplest to explain; fences match what each persona needs | a large holder pays as a small one |
| B. Per connected account | one sentence to explain; grows with E8 | one account today, so little growth before E8 |
| C. Capital automated | grows with the customer's success | **needs legal review** (L8); capital can be moved to game it |

**PM recommendation:** A, with an annual plan, the joining price kept while the customer stays, and pause; B as the fallback if the interviews show customers think in accounts.
Do not test C before counsel answers L8. No option takes a share of profits.

This section is posted as a comment on the E5 epic.
If a kill criterion of `go-no-go.md` is met, there is nothing to price.
