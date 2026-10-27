# Pricing options (E5)

**Status:** options for the product owner to decide (#236, epic E5). Prepared **before** the interviews (#178) and the fake door (`experiments/fake-door-landing.md`) report, so their questions are specific. **The decision is the product owner's**, after those results and counsel's answer to L8.

**No figures here.** This repository is public: no price, revenue, CAC, churn rate, competitor price or survey answer is written in this file. Prices, research and the unit-economics scenarios go in the gitignored `data/product/pricing/` (run them with `scripts/product/unit_economics.py`, #235). This file names the choices, their trade-offs, their legal flags and the data each one still needs.

**Principles** (PRD §6, quoted): fair to the customer, rewards staying (annual plans, loyalty), low churn; **no share of profits** (regulatory and incentive risk). Any fee that scales with capital is flagged **needs legal review** — it can read as a management fee (`legal-brief.md` L8; `docs/legal/preliminary-answers.md` L8: a fee as a share of capital or of profits reinforces the portfolio-management reading and does not fit a pure software licence; a flat subscription per account is the cleanest).

**What any price is for.** Until counsel answers the legal gate (L1–L6, L9), the only thing that may carry a price is **software** — the engine, the monitor, the explanations — never a managed investment service, a return or a result (`preliminary-answers.md` L9, overall position).

## 1. Value metric candidates

The value metric is the unit the price grows with. Scored 1 (poor) to 3 (good) on the four questions the ticket asks; legal exposure is scored the other way (3 = least exposure).

| Metric | Easy to understand | Grows with the customer's success | Hard to game | Legal exposure | Notes |
|---|---|---|---|---|---|
| **Flat subscription** (no metric: one price per tier) | 3 | 1 | 3 | 3 — the cleanest under a licence (L8 preliminary) | Pays the same whether the account is small or large; the tiers do the fencing |
| **Strategies running** (strategy monitors, e.g. one per coin) | 2 | 2 — a customer who trusts it adds a coin | 2 — one strategy can be run on a bigger account | 3 — counts software instances, not money | Matches how the product is built (one bot per coin, PRD §2); "strategy" needs a plain-words definition |
| **Connected exchange accounts** | 3 | 2 — a second account is a sign of trust | 2 — accounts can be merged on the exchange | 3 — counts connections, not money (L8 fallback names it) | Natural once E8 adds exchanges; today one exchange only |
| **Capital automated** (fee scales with the capital the engine trades) | 2 | 3 | 1 — capital can be moved off the account before a billing date | **needs legal review** (L8; L1, L4, L5): a fee scaling with capital reads as a management fee | Strongest link to value, highest regulatory risk; also an incentive to push more capital in |
| **Seats / roles** (users, managers per account, PRD §3 roles) | 3 | 1 — a holder rarely adds people | 2 — logins can be shared | 3 | Fits families or advisers later (L24 decides whether advisers may be managers) |
| **Share of profits** | — | — | — | **excluded** by PRD §6 (no share of profits; a fee scaling with profits needs legal review, L8) | Listed only so nobody proposes it again without changing the PRD |

**Reading:** the metrics that count software (strategies, accounts, seats) carry the least legal exposure; capital tracks value best but is the one L8 asks about. The interviews must tell us whether customers think of the product as "a tool I run" (count software) or "income on my stack" (they will expect a price linked to size).

## 2. Tier shapes

Each shape is built from the PRD §3 personas; each tier has one or two **fences** (what separates it from the next) and no price. Three shapes to compare; the product owner picks one, or none.

### Shape A — by persona, flat subscription

| Tier | For | Fences | Legal flag |
|---|---|---|---|
| **Learn** | the curious beginner | testnet only (no live key), education and the monitor | low (L6: testnet, named testers) |
| **Hold** | the cycle holder | one connected account, one or two coins, the default strategy and its settings, monthly income view | flat; L1–L5 decide whether it may be live |
| **Trade** | the active option trader | several accounts or coins; manager role for parameter changes after a comparison backtest (E9) | flat; L10, L24 for the manager role |

Simple to explain; the fences are what each persona needs, not an artificial limit.

### Shape B — per connected account

One price per connected exchange account, every feature included; Learn stays a testnet-only entry. Easiest to explain ("one account, one price"); a large holder pays the same as a small one, which favours the cycle holder with a big stack. Legal flag: low (L8 fallback).

### Shape C — by capital band

Tiers by bands of capital automated (no percentage, a flat price per band). Grows with the customer; the band boundaries are a disguised capital fee. Legal flag: **needs legal review** (L8, with L1, L4, L5 — a fee that scales with capital, even in bands, may read as a management fee). Not to be shown on the fake door before counsel answers L8 and L9.

## 3. The entry point: the testnet trial

The PRD's smallest offer is a **testnet trial** (PRD §6): the engine runs on the customer's own Deribit **testnet** account, with no real money. **E4 (risk-free trial) is parked behind the legal gate** (PRD §1 and §11, L6), so the trial is stated as **later**: today it is open only to the team and named testers under the written testnet agreement (`docs/legal/drafts/acordo-teste-testnet.md`). Every shape above starts with it; it converts into a paid tier only when a live offer is allowed.

## 4. Loyalty and annual plans

PRD §6 asks to reward staying:

- **Annual plan:** paid yearly, cheaper per month than monthly (the discount is a figure, in `data/product/pricing/`). Shown next to the monthly price, never pre-selected.
- **Loyalty:** the price a customer joined at is kept while they stay (no surprise increases), and a price change is announced ahead of the renewal with the way to cancel in the same message.
- **Pause instead of cancel:** a customer may pause the engine (no trading) and keep the monitor and their history, at a lower or zero fee — a cycle holder who wants to stop selling options in a bull run should not have to leave.

## 5. Trial and plan rules (no dark patterns)

Every rule below states **when it ends, what it costs after, and that cancelling is as easy as signing up**: the same number of steps, on the same page, with no call, chat or retention screen in between. No card is asked for a free trial; nothing renews into a paid plan without the customer choosing it.

### Testnet trial

- **Ends:** on the date shown at sign-up and on the monitor's header from the first day; a reminder a few days before.
- **After it ends:** nothing is charged. The trial stops and the customer chooses a plan or leaves; no card was asked for, so nothing renews by itself.
- **Cancelling:** one button in the monitor, as easy as signing up; the testnet account and its key stay the customer's.

### Monthly plan

- **Ends:** at the end of each paid month, unless renewed; the renewal date is on the billing page.
- **After it ends:** it renews at the price the customer agreed to, shown before each renewal; a price change is announced before the renewal it applies to.
- **Cancelling:** one button on the billing page, as easy as signing up; the plan runs to the end of the paid month. The engine stops opening positions; open positions stay on the customer's exchange account and the customer decides what to do with them (L18, L20).

### Annual plan

- **Ends:** at the end of the paid year; a reminder with the renewal price a month before.
- **After it ends:** it renews for a year at the price shown in the reminder, or the customer moves to monthly or leaves.
- **Cancelling:** one button on the billing page, as easy as signing up; the refund rule for the unused months is stated at purchase (a figure and a legal text, for counsel: consumer law, CDC in Brazil).

### Pause

- **Ends:** when the customer resumes, or at the date they chose when pausing.
- **After it ends:** the plan resumes at its price, announced in the resume reminder.
- **Cancelling:** a paused plan cancels with the same one button, as easy as signing up.

## 6. What the interviews and the fake door must answer

The questions are mapped to `discovery/interview-guide.md` (#178) and the fake door's choice questions (`experiments/fake-door-landing.md` §4, `fair_pay`); new wording goes through those documents.

| Option | Interview (#178) must answer | Fake door must answer |
|---|---|---|
| Flat subscription (shape A) | what they pay today for yield products or tools, and how they think of it (a fee on yield, a tool subscription); does a beginner tier make sense to a holder | share choosing *a fixed monthly subscription* in the "fair way to pay" question (`fair_pay`) |
| Per connected account (shape B) | how many exchange accounts they hold and would connect | share choosing *a fee per connected exchange account* (`fair_pay`) |
| Capital bands (shape C, **needs legal review**, L8) | whether they expect the price to depend on the size of their stack; at which size they would stop delegating | not shown before L8, L9 |
| Annual plan, loyalty | would they commit for a year for a lower monthly price; what made them leave a crypto product before | — |
| Pause | do they stop yield products in bull runs; would they pause rather than cancel | — |
| Testnet trial | would they try it, how long they need to judge it | share choosing *a free trial on the exchange's testnet* (`first_wanted`) |

## 7. Calculator inputs

The unit-economics story of this epic (`scripts/product/unit_economics.py`, its keys in `example_scenario.yaml`) needs, per shape, these inputs — all figures, all in `data/product/pricing/`:

- **Per tier:** price, expected mix, monthly logo churn, infrastructure and exchange/API cost to serve, support cost, payment fees.
- **Plans:** annual-plan share and its discount.
- **Acquisition** (content and communities only, PRD §6): CAC by channel, trial-to-paid conversion.
- **Shape C only:** average capital per band (`avg_capital`); the calculator prints it with **needs legal review**.
- **Not an input:** any performance, profit or return figure — the calculator refuses them (no share of profits).

## 8. Decision for the product owner

**Decision to take:** which value metric and tier shape the pricing proposal carries into the next interviews, a later fake-door run that tests price points (its own brief, after counsel answers L8 and L9) and, later still, a paid beta (only after the legal gate).

**Options:**

| Option | For it | Against it | Legal |
|---|---|---|---|
| A. Flat tiers by persona | simplest to explain; fences match what each persona needs | a large holder pays as a small one | low (L8 preliminary) |
| B. Per connected account | one sentence to explain; grows with E8 | one account today, so little growth until E8 | low (L8 fallback) |
| C. Capital bands | grows with the customer's success | reads as a management fee; capital can be moved to game it | **needs legal review** (L8, with L1, L4, L5) |

**Recommendation:** shape **A** (flat tiers by persona), with an annual plan, price kept while the customer stays and a pause option; keep **B** as the fallback if the interviews show customers think in accounts. Do not test **C** on the fake door before counsel answers L8 and L9. No option takes a share of profits.

**Still missing:**

- the interviews (#178): what cycle holders pay today and how they think of the price (a tool, or a fee on yield);
- the fake door's `fair_pay` and `first_wanted` shares, with enough traffic for a verdict (brief §5);
- counsel's answers to L8 (capital-scaled fees) and L9 (a price on a waiting list), and the legal gate L1–L6;
- the unit-economics run of each shape in `data/product/pricing/` (cost to serve, CAC by channel);
- the go / no-go scorecard (`go-no-go.md`): if a kill criterion is met, there is nothing to price.
