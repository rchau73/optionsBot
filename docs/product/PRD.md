# optionsBot — Product Requirements

Version 0.1 · 2026-10-17 · owner: PM (product-manager skill) · status: **approved when a person merges the pull request that changes this file**

> Educational study of an automated trading system, not investment advice. Any offer to other people is gated on legal review (§11).

## 1. Summary

- **Who:** self-directed crypto investors who hold BTC or ETH and want income from it, but lack the time, tools or nerve to sell options by hand.
- **Problem:** selling volatility pays on average and ruins people in the bad week. Doing it well means watching the market 24/7, sizing for margin, rolling, cutting losses fast, and understanding Greeks. Most retail investors either don't try or try once and blow up.
- **Solution (hypothesis):** an automated, rules-based option-selling engine that runs on the investor's own exchange account, explains every action it takes, and makes the risk visible before the money is at stake.
- **Why now:** the engine exists and runs on testnet with a growing evidence base (journal, replays, stress tests). What is missing is the proof that it earns its risk, and the product around it.
- **Decision this version asks for:** approve the strategy (§2), the MVP direction (§6) and the epics (§9). Approving means E1, E2, E3, E5 and E6 are open for the PM agent to fill and rank. E4 stays parked until the legal gate and the evidence from E2 say otherwise.

## 2. Vision and strategy

**Vision (2–3 years):** the most trusted way for a retail crypto holder to earn option income, because it is the most honest one: every trade is explained, every risk is shown in money before it is taken, and the track record is public and net of fees.

**Strategy, in order:**
1. **Prove it, then sell it.** No offer to anyone before the engine has a track record we would show a sceptical investor (E2). Testnet proves mechanics, not profits (OptionStrategy.md §11.5), so the proof comes from replays on recorded mainnet books, stress tests and, later, small live capital.
2. **Win on trust and clarity, not on promised yield.** Competitors sell APY numbers. We sell understanding and control: the reason for every action, the worst case before every risk.
3. **Self-custody by design.** The engine trades on the investor's own exchange account through API keys with withdrawals disabled. We never hold customer money. This is the main product and legal lever (§11).
4. **Risk-free first experience.** The first contact is a strategy running on Deribit testnet: real market data, fake money (E4).

**What we will not do:** promise or advertise returns; custody funds; let novices raise risk without seeing the worst case; build features that push users to trade more often.

## 3. Customers

| Persona | Job to be done | Today they use | Pain |
|---|---|---|---|
| **The income holder** (primary hypothesis) | "When I hold BTC/ETH long term, I want it to earn income without selling it, so my stack grows in bad years too." | Exchange "earn" / dual-investment products, covered calls by hand, nothing | Opaque yields, no control over risk, or too complex to do by hand |
| **The active option trader** | "When I already sell options, I want the routine automated and risk-checked, so I can trade more books without watching screens." | Spreadsheets, exchange UI, scripts | Night-time moves, missed rolls, margin surprises |
| **The curious beginner** | "When I hear options pay income, I want to learn how without losing money, so I can decide if it's for me." | YouTube, courses, demo accounts | Jargon, fear, no safe place to practise with real market behaviour |

All three are **assumptions** to validate in E5. The first interviews should test whether the income holder exists in numbers and trusts automation with their account.

## 4. Market and competitors

To be built in E5. Categories to map: exchange-native structured yield (dual investment, earn), option vaults (DeFi), copy-trading and signal platforms, automation platforms with API keys, and option analytics tools. Sources, prices and dates go in `data/product/competitors/` (gitignored). This section keeps only the summary and the positioning gaps found.

## 5. Value proposition and positioning

**Positioning statement (draft, to test in E5):** for crypto holders who want income from their coins, optionsBot is an option-selling engine that runs on your own exchange account. Unlike yield products that hide the risk behind an APY, it shows the reason for every trade and the worst case before every risk, and it publishes its full track record, net of fees.

Proof points available today: the decision journal (every action with its reason and market context), P&L net of fees in coin and USD, kill switch, margin policy, and stress tests on recorded mainnet books.

## 6. Go-to-market MVP (hypothesis)

| | |
|---|---|
| Segment | the income holder with BTC or ETH on Deribit (or willing to open an account) |
| Smallest offer that proves value | a **testnet trial**: the engine runs on the user's Deribit testnet account and the monitor shows what it does and why, with education built in (E3, E4) |
| Before any build for third parties | discovery (E5): ~15 interviews, a landing page with a waiting list (fake door), a willingness-to-pay test |
| Pricing hypothesis | subscription by tier (number of strategies or coins, analytics, support), never a share of profits (regulatory and incentive risk). To be researched in E5 |
| Channels | content (the public track record and the agentic-development story), crypto option communities. No paid acquisition before payback is proven |
| Launch phases | private alpha (us, testnet) → private beta (5–10 invited users, testnet) → paid beta (small live, only after the legal gate) |
| Kill criteria | <30 % of interviewed income holders would let software trade their account, or E2 shows the engine doesn't beat simply holding the coin on a risk-adjusted basis |

## 7. Metrics

| Metric | Definition | Source | Baseline | Target |
|---|---|---|---|---|
| **North star (engine phase):** coin-denominated net return per unit of margin used | realised + unrealised P&L in coin, net of fees, ÷ average IM used, per 30 days | journal, `pnl_history.jsonl` | to measure (E2) | set after 30 days of baseline |
| Worst week | the worst 7-day P&L, in coin and % of equity | same | to measure | within the risk budget E2 defines |
| Margin utilisation | IM used ÷ IM limit, time-weighted | monitor `/api/account` | ETH ~2 % of 20 % (#86) | ≥ 60 % of the limit when allowed |
| Decisions explained | share of actions with a human-readable reason in the monitor | journal | ~100 % for closes | 100 % |
| **North star (product phase, later):** weekly active trial users with a strategy running | — | product analytics (E4) | — | set at beta |
| Guardrails | max drawdown, stop-loss count per week, MM peak, kill-switch events | journal, monitor | — | none may get worse after a release |

## 8. Roadmap

| Horizon | Outcome | Epics |
|---|---|---|
| **Now** | the engine uses its margin and earns its risk, and we can prove it | E1, E2, E6 |
| **Next** | we know who buys, why and at what price; a first user understands the monitor without help | E5, E3 |
| **Later** | a risk-free trial for invited users, then new markets | E4, then more coins (#117) |

## 9. Epics

### E1 · Strategy reliability and capital efficiency
<!-- epic: {"id": "E1", "status": "approved", "rank": 1, "rule_areas": ["rebalance", "sizing", "reconcile", "marketdata", "gateway"], "ui": false} -->
**Outcome:** each bot uses its approved margin (margin utilisation ≥ 60 % of the limit when entries are allowed) without unintended trades, and no restart or deploy changes the book by itself.
**Why:** ETH used ~2 % of a 20 % limit all week (#86), orders reached 9× a strike's open interest (#103), and a restart rebalances BTC in both directions (#123).
**Scope:** sizing to market liquidity; rebalance and top-up policy; reconciliation and account accounting; market data and connection robustness. Each change is backed by a replay or stress comparison in its PR.
**Delegated trading-rule areas:** rebalance, sizing, reconcile, marketdata, gateway. Changes to **entry, exits, repair, margin limits, GEX shedding, order handling or the kill switch** still need a person in Triage.
**Out of scope:** new strategies; new coins (E6 / later).
**Stories (seed):** #103 size by liquidity · #123 restart rebalance · #98 top-up fill policy · #86 idle margin.

### E2 · Proof of performance
<!-- epic: {"id": "E2", "status": "approved", "rank": 2, "rule_areas": [], "ui": true} -->
**Outcome:** a track record a sceptical investor accepts: net of fees, in coin and USD, against simply holding the coin, with drawdowns and the worst week shown, and the gap between replay and live measured.
**Why:** no product can be offered before this exists, and testnet P&L alone is not evidence (OptionStrategy.md §11.5).
**Scope:** an evidence report (replay + stress + live journal) refreshed weekly; a benchmark (hold the coin); account equity reconciled with bot P&L; a decision on each running trial (e.g. the GEX bias trial). Public figures only on testnet; live amounts stay private.
**Out of scope:** marketing claims (E5 decides what may be said, after legal review).
**Stories (seed):** #124 equity vs P&L · #88 bias trial review.

### E3 · Investor monitor: value at a glance, risk in plain words
<!-- epic: {"id": "E3", "status": "approved", "rank": 3, "rule_areas": [], "ui": true} -->
**Outcome:** a person new to options can answer, within 5 minutes of opening the monitor and without help: what does the bot hold, is it making or losing money, what is the worst case right now, and why did it do its last trade. Measured with 5 user tests.
**Why:** today the monitor is built for its developer: dense, accurate, full of jargon. It is also the surface where a trial user will feel the value.
**Scope:** an investor view (summary first, detail on demand); contextual tips for every jargon term; 3–5 short how-to videos (30–90 s); risk shown in money; honest P&L (losses as visible as gains). Prototype first (ux-designer), approved by a person before UI stories go to Ready.
**Out of scope:** accounts, logins and multi-user features (E4).

### E4 · Risk-free trial on testnet
<!-- epic: {"id": "E4", "status": "parked", "rank": 4, "rule_areas": [], "ui": true} -->
**Outcome:** an invited user connects their Deribit testnet account and has a strategy running, with the monitor explaining it, in under 15 minutes.
**Why:** the strongest acquisition lever for a fearful audience is trying it with fake money on real market behaviour.
**Parked until:** the legal gate (§11) is answered, E2 shows evidence worth showing, and E5 confirms demand. Needs per-user isolation, key handling (trade-only keys, encrypted at rest), onboarding and support.

### E5 · Discovery and go-to-market MVP
<!-- epic: {"id": "E5", "status": "approved", "rank": 5, "rule_areas": [], "ui": true} -->
**Outcome:** a go / no-go on the product with evidence: who the buyer is, the job they hire us for, what they would pay, and what we may legally offer.
**Scope:** ~15 interviews across the three personas; a competitor map (private); a landing page with a waiting list, positioning and pricing tested as a fake door; a legal-review brief (§11); the pricing proposal. Results update this PRD by pull request.
**Out of scope:** building the product for third parties before the go decision.

### E6 · Delivery platform: agents, CI and operations
<!-- epic: {"id": "E6", "status": "approved", "rank": 6, "rule_areas": [], "ui": false} -->
**Outcome:** the agentic loop turns approved work into deployed, verified changes with no human toil except the approvals, and every agent action is auditable.
**Scope:** the SME, PM, Dev, Reviewer and deploy agents; CI; deploy and health checks; agent metrics (issues opened → triaged → merged → verified fixed, review rounds, reverts).
**Out of scope:** trading-rule changes (E1 or Triage).

## 10. Design

Principles: summary first, detail on demand; every number with unit, sign and time; risk next to the action, in money; losses as visible as gains; education at the point of need. Prototypes are made with the ux-designer skill and linked here once a person approves them.

| Epic | Prototype | Status |
|---|---|---|
| E3 | — | needed |

## 11. Risks, compliance and open questions

- **Legal gate (needs legal review) — blocks E4 and any offer to third parties:** is running an automated strategy on a customer's own account, with their API key, a regulated activity (portfolio management, investment advice, a crypto-asset service) in the markets we target (Brazil: CVM; EU: MiCA/MiFID; US: SEC/CFTC)? What may marketing say about performance?
- **Exchange terms:** does Deribit allow third-party automation on customer accounts, and which jurisdictions does it serve?
- **Risk of ruin:** short options can lose far more than the premium. Every product surface shows the worst case; defaults are conservative; defined-risk structures (iron condors) are considered before any retail offer (OptionStrategy.md §11.3).
- **Key security:** trade-only API keys, no withdrawal rights, encrypted at rest, revocable by the user.
- **Open questions for the person approving this version:** (1) is the income holder the right primary persona? (2) is a self-custody subscription the right business direction, or is this a study with no commercial goal? (3) are the trading-rule areas delegated in E1 the right ones?

## 12. Decision log

| Date | Decision | Options | Reason | Decided by |
|---|---|---|---|---|
| 2026-10-17 | The PRD is approved by merging its pull request; epics carry the trading-rule areas the PM agent may send to Ready | label per epic; merge | one auditable gate, already used for code | human |
| 2026-10-17 | Prototypes are private claude.ai Artifacts, approved by the `prototype:approved` label on the epic | Next.js route; Artifact | fast to iterate, no code to maintain | human |
| 2026-10-17 | Product material that is sensitive stays in gitignored `data/product/` | all public; all private; split | the repo is public | human |
