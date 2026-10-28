# optionsBot — Product Requirements

Version 0.2 · 2026-10-18 · owner: PM (product-manager skill) · status: **approved when a person merges the pull request that changes this file**

> optionsBot is becoming a product (product owner on #149). Nothing here is investment advice or a promise of returns. Any offer to other people is gated on legal review (§11).

## 1. Summary

- **Who:** long-term BTC/ETH holders who keep their coins positioned through the 4–5-year market cycle and want sustainable, recurring monthly income from them. They follow the institutional money; they are not day traders and are often not technical.
- **Problem:** selling volatility pays on average and ruins people in the bad week. Doing it well means watching the market 24/7, sizing for margin, rolling, cutting losses fast and understanding Greeks. Most holders either don't try or try once and blow up.
- **Solution:** an automated, rules-based option-selling platform that places orders on the customer's own exchange accounts, explains every action it takes and makes the risk visible before the money is at stake. Customers keep their money on their exchanges; the platform never moves funds.
- **Why now:** the engine exists and runs on testnet with a growing evidence base (journal, replays, stress tests). What is missing is the proof that it earns its risk, and the product around it: accounts and roles, several exchanges, education, analytics.
- **Decision this version asks for:** approve the product direction (§2), the primary persona (§3), the agents' line on trading rules (§9 E1, §12) and seven new epics (E7–E13). Approving opens every epic except E4, which stays parked behind the legal gate.

## 2. Vision and strategy

**Vision (2–3 years):** the most trusted way for a long-term crypto holder to earn recurring income from the coins they already hold, because it is the most honest one: every trade is explained, every risk is shown in money before it is taken, the track record is net of fees, and the customer never hands over custody. One platform, the customer's choice of exchange.

**Strategy, in order:**
1. **Prove it, then sell it.** No offer to anyone before the engine has a track record we would show a sceptical investor (E2). Testnet proves mechanics, not profits (OptionStrategy.md §11.5), so the proof comes from replays on recorded mainnet books, stress tests and, later, small live capital.
2. **Win on trust and clarity, not on promised yield.** Competitors sell APY numbers. We sell understanding and control: the reason for every action, the worst case before every risk.
3. **The customer keeps the money.** The platform is an investment strategy that places buy and sell orders on the customer's exchange accounts through trade-only API keys. Deposits, transfers and withdrawals happen only on the exchange, never through us (product owner on #149, 2026-10-18). This is the main product and legal lever (§11).
4. **Know the customer's pain better than anyone.** Continuous discovery, surveys, analytics and cohort reports (E5, E13) decide what we build; retention, not acquisition, is the first growth lever.
5. **Exchange-agnostic.** Deribit is the MVP; Bybit, OKX, Binance, Hyperliquid and others follow behind one seamless connection experience (E8).
6. **Built for non-technical investors.** Tips, short videos, an AI assistant and a responsive web UI make the monitor and the strategy settings understandable on a phone (E10–E12).

**What we will not do:** promise or advertise returns; custody, move or withdraw funds; let novices raise risk without seeing the worst case; build features that push users to trade more often; make cancelling harder than signing up.

## 3. Customers

| Persona | Job to be done | Today they use | Pain |
|---|---|---|---|
| **The cycle holder** (primary, product owner on #149) | "While my BTC/ETH sits through the cycle, I want it to earn recurring monthly income without selling it, so my stack grows in bad years too." | Exchange "earn" / dual-investment products, covered calls by hand, nothing | Opaque yields, no control over risk, too complex to do by hand, not technical |
| **The active option trader** (secondary) | "When I already sell options, I want the routine automated and risk-checked, so I can trade more books without watching screens." | Spreadsheets, exchange UI, scripts | Night-time moves, missed rolls, margin surprises |
| **The curious beginner** (secondary) | "When I hear options pay income, I want to learn how without losing money, so I can decide if it's for me." | YouTube, courses, demo accounts | Jargon, fear, no safe place to practise with real market behaviour |

The cycle holder is confirmed as the target by the product owner. How many there are, what they pay and whether they let software trade their account are still **assumptions** to validate in E5.

### Roles in the platform

| Role | May | May not |
|---|---|---|
| **Admin** | everything | — |
| **Manager** | adjust strategy parameters (after a comparison backtest, E9), close legs manually, everything a user may | — |
| **User** (the money owner) | monitor the dashboard, suggest enhancements, ask questions (E10) | change strategies or parameters; deposit, transfer or withdraw through the platform (that happens only on their exchange) |

Changes to a **core trading policy** (margin limits, exits, entry, repair, GEX shedding, order handling, kill switch) need the product owner's approval before they become the product's default, whoever proposes them (§12).

## 4. Market and competitors

To be built in E5. Categories to map: exchange-native structured yield (dual investment, earn), option vaults (DeFi), copy-trading and signal platforms, automation platforms with API keys, and option analytics tools. Sources, prices and dates go in `data/product/competitors/` (gitignored). This section keeps only the summary and the positioning gaps found.

## 5. Value proposition and positioning

**Positioning statement (draft, to test in E5):** for long-term crypto holders who want monthly income from their coins, optionsBot is an option-selling strategy that runs on your own exchange accounts. Unlike yield products that hide the risk behind an APY, it shows the reason for every trade and the worst case before every risk, it never touches your funds, and it publishes its full track record, net of fees.

Proof points available today: the decision journal (every action with its reason and market context), P&L net of fees in coin and USD, kill switch, margin policy, and stress tests on recorded mainnet books.

## 6. Go-to-market MVP (hypothesis)

| | |
|---|---|
| Segment | the cycle holder with BTC or ETH on Deribit (or willing to open an account); other exchanges after E8 |
| Smallest offer that proves value | a **testnet trial**: the engine runs on the user's Deribit testnet account and the monitor shows what it does and why, with education built in (E3, E4, E11) |
| Before any build for third parties | discovery (E5): ~15 interviews, a landing page with a waiting list (fake door), a willingness-to-pay test |
| Pricing hypothesis | to be proposed in E5 and decided by the product owner. Options to research: subscription tiers, per connected account, by capital automated. Principles: fair to the customer, rewards staying (annual plans, loyalty), low churn; no share of profits (regulatory and incentive risk). Any fee that scales with capital is flagged **needs legal review** (it can read as a management fee). Research and figures stay in `data/product/` |
| Channels | content (the public track record and the agentic-development story), crypto option and long-term-holder communities. No paid acquisition before payback is proven |
| Launch phases | private alpha (us, testnet) → private beta (5–10 invited users, testnet) → paid beta (small live, only after the legal gate) |
| Kill criteria | <30 % of interviewed cycle holders would let software trade their account, or E2 shows the engine doesn't beat simply holding the coin on a risk-adjusted basis |

## 7. Metrics

| Metric | Definition | Source | Baseline | Target |
|---|---|---|---|---|
| **North star (engine phase):** coin-denominated net return per unit of margin used | realised + unrealised P&L in coin, net of fees, ÷ average IM used, per 30 days | journal, `pnl_history.jsonl` | to measure (E2) | set after 30 days of baseline |
| Worst week | the worst 7-day P&L, in coin and % of equity | `pnl_history.jsonl` via `internal/perf` (`Summarize`: also net return in coin/USD, hold-the-coin return over the same window, max drawdown, missing days) | to measure | within the risk budget E2 defines |
| Margin utilisation | IM used ÷ IM limit, time-weighted over the time entries are allowed | `pnl_history.jsonl` margin fields → `/api/status` `utilisation_7d`/`utilisation_30d` (#174) | ETH ~2 % of 20 % (#86) | ≥ 60 % of the limit when allowed |
| Decisions explained | share of actions with a human-readable reason in the monitor | journal | ~100 % for closes | 100 % |
| **North star (product phase):** monthly active customer accounts with a strategy running | accounts with ≥ 1 running strategy and ≥ 1 user visit in the month | product analytics (E13) | — | set at beta |
| Retention | monthly cohort retention of customer accounts; logo and revenue churn with a coded reason for every cancellation | product analytics, exit survey (E13) | — | curve flattens by month 3 |
| Self-service understanding | share of help questions answered by tips, videos or the assistant without a person | assistant and support logs (E10, E11) | — | set at beta |
| Guardrails | max drawdown, stop-loss count per week, MM peak, kill-switch events; for the product: complaints, refunds, assistant answers flagged wrong | journal, monitor, support; max drawdown from `internal/perf` | — | none may get worse after a release |

## 8. Roadmap

| Horizon | Outcome | Epics |
|---|---|---|
| **Now** | the engine uses its margin and earns its risk, and we can prove it; we measure how the monitor is used | E1, E2, E6, E13 |
| **Next** | we know who buys, why and at what price; a first user understands the monitor without help, on a phone too; people sign in with their role | E3, E5, E7, E11, E12 |
| **Later** | several customers, accounts and exchanges; managers test changes with AI analysis; an AI assistant answers questions; a risk-free trial for invited users | E8, E9, E10, E4, then more coins (#117) |

## 9. Epics

### E1 · Strategy reliability and capital efficiency
<!-- epic: {"id": "E1", "status": "approved", "rank": 1, "rule_areas": ["rebalance", "sizing", "reconcile", "marketdata", "gateway"], "ui": false} -->
**Outcome:** each bot uses its approved margin (margin utilisation ≥ 60 % of the limit when entries are allowed) without unintended trades, and no restart or deploy changes the book by itself.
**Why:** ETH used ~2 % of a 20 % limit all week (#86), orders reached 9× a strike's open interest (#103), and a restart rebalances BTC in both directions (#123).
**Scope:** sizing to market liquidity; rebalance and top-up policy; reconciliation and account accounting; market data and connection robustness. Each change is backed by a replay or stress comparison in its PR.
**Delegated trading-rule areas (kept, product owner on #149):** rebalance, sizing, reconcile, marketdata, gateway. Changes to **entry, exits, repair, margin limits, GEX shedding, order handling or the kill switch** are core policies: agents may investigate, backtest and propose them, and the product owner approves before they ship.
**Out of scope:** new strategies; new coins (later).
**Stories (seed):** #103 size by liquidity · #123 restart rebalance · #98 top-up fill policy · #86 idle margin.

### E2 · Proof of performance
<!-- epic: {"id": "E2", "status": "approved", "rank": 2, "rule_areas": [], "ui": true} -->
**Outcome:** a track record a sceptical investor accepts: net of fees, in coin and USD, against simply holding the coin, with drawdowns and the worst week shown, and the gap between replay and live measured.
**Why:** no product can be offered before this exists, and testnet P&L alone is not evidence (OptionStrategy.md §11.5).
**Scope:** an evidence report (replay + stress + live journal) refreshed weekly; a benchmark (hold the coin); account equity reconciled with bot P&L; a decision on each running trial (e.g. the GEX bias trial); more recorded history to improve backtests and replays. Public figures only on testnet; live amounts stay private.
**Out of scope:** marketing claims (E5 decides what may be said, after legal review).
**Stories (seed):** #124 equity vs P&L · #88 bias trial review.

### E3 · Investor monitor: value at a glance, risk in plain words
<!-- epic: {"id": "E3", "status": "approved", "rank": 3, "rule_areas": [], "ui": true} -->
**Outcome:** a person new to options can answer, within 5 minutes of opening the monitor and without help: what does the bot hold, is it making or losing money, what is the worst case right now, and why did it do its last trade. Measured with 5 user tests.
**Why:** today the monitor is built for its developer: dense, accurate, full of jargon. It is also the surface where a customer feels the value.
**Scope:** an investor view (summary first, detail on demand); income shown per month, in coin and USD, net of fees; risk shown in money; honest P&L (losses as visible as gains). Tips and videos come from E11. Prototype first (ux-designer), approved by a person before UI stories go to Ready.
**Out of scope:** logins and roles (E7); several accounts or exchanges (E8).

### E4 · Risk-free trial on testnet
<!-- epic: {"id": "E4", "status": "parked", "rank": 13, "rule_areas": [], "ui": true} -->
**Outcome:** an invited user connects their Deribit testnet account and has a strategy running, with the monitor explaining it, in under 15 minutes.
**Why:** the strongest acquisition lever for a fearful audience is trying it with fake money on real market behaviour.
**Parked until:** the legal gate (§11) is answered, E2 shows evidence worth showing, and E5 confirms demand. Builds on E7 (accounts) and E8 (per-customer accounts and key handling).

### E5 · Discovery and go-to-market MVP
<!-- epic: {"id": "E5", "status": "approved", "rank": 4, "rule_areas": [], "ui": true} -->
**Outcome:** a go / no-go on the product with evidence: who the buyer is, the pains they hire us for, what they would pay, and what we may legally offer.
**Scope:** ~15 interviews, cycle holders first; a competitor map (private); a landing page with a waiting list, positioning and pricing tested as a fake door; a legal-review brief (§11); a pricing proposal (tiers, value metric, unit economics, churn levers) for the product owner to decide. Results update this PRD by pull request.
**Out of scope:** building the product for third parties before the go decision.

### E6 · Delivery platform: agents, CI and operations
<!-- epic: {"id": "E6", "status": "approved", "rank": 5, "rule_areas": [], "ui": false} -->
**Outcome:** the agentic loop turns approved work into deployed, verified changes with no human toil except the approvals, and every agent action is auditable.
**Scope:** the SME, PM, Dev, Reviewer and deploy agents; CI; deploy and health checks; agent metrics (issues opened → triaged → merged → verified fixed, review rounds, reverts).
**Out of scope:** trading-rule changes (E1 or Triage).

### E7 · Accounts, roles and security
<!-- epic: {"id": "E7", "status": "approved", "rank": 7, "rule_areas": [], "ui": true} -->
**Outcome:** every person signs in and sees and does only what their role allows (§3 Roles): admin, manager, user. Every state-changing action is attributed to a person and audited.
**Why:** today one shared admin token guards the manual close and rebalance; a product with money owners and managers needs people, not tokens (product owner on #149).
**Scope:** username/password sign-in with secure basics (hashed passwords, session expiry, lockout after failed attempts, password reset); the three roles enforced on the server, not only in the UI; audit log of who did what; manual close and parameter changes for managers and admins only.
**Out of scope:** deposits, transfers or withdrawals (they stay on the exchange); SSO and 2FA (later, once the basics ship).
**Stories (seed):** sign-in and sessions · role-based access on the API · audit log per person.

### E8 · Multi-customer, multi-account, multi-exchange
<!-- epic: {"id": "E8", "status": "approved", "rank": 10, "rule_areas": ["gateway", "marketdata"], "ui": true} -->
**Outcome:** one platform serves several customers, each with one or more exchange accounts, and adding an exchange does not change the strategy. Deribit first; Bybit, OKX, Binance and Hyperliquid next.
**Why:** customers hold their coins where they already trade; the product owner wants the integration seamless (#149).
**Scope:** an exchange adapter boundary behind today's gateway (connection, market data, orders, positions, margin) with Deribit as the first adapter; per-customer and per-account isolation (one account's bot never touches another's orders — today two bots on one account fight); trade-only API keys, encrypted at rest, revocable, with withdrawal rights refused; a guided "connect your exchange" flow; a feasibility study per exchange (options market, margin model, simulation API, rate limits). Order handling and reconcile changes needed by a new exchange go to a person.
**Out of scope:** onboarding third-party customers before the legal gate (§11); custody.
**Stories (seed):** exchange adapter interface (Deribit) · per-account isolation design · exchange feasibility briefs.

### E9 · Strategy-change lab with AI analysis
<!-- epic: {"id": "E9", "status": "approved", "rank": 11, "rule_areas": [], "ui": true} -->
**Outcome:** before a manager changes a strategy parameter, they see a backtest of the current and the proposed settings side by side, and an AI analysis of why the change helps or hurts, where it is fragile and what to try next.
**Why:** managers may not be technical; a parameter change without evidence is a bet with the customers' money (product owner on #149).
**Scope:** a comparison run (backtest, replay of recorded books, stress) for current vs proposed parameters; return, worst week, drawdown and fees side by side; an AI-written analysis grounded in the run's numbers; results labelled as backtested, with risks shown next to them; the change applies only after the manager confirms, and is audited (E7). A change to a core policy's default still needs the product owner (§3).
**Out of scope:** the AI changing parameters by itself.

### E10 · AI help assistant
<!-- epic: {"id": "E10", "status": "approved", "rank": 12, "rule_areas": [], "ui": true} -->
**Outcome:** a user gets a correct, plain-words answer to "how does it work / what does this parameter do / why did the bot do that" inside the product, without a person.
**Why:** the persona is not technical; questions are the main barrier to trust (product owner on #149).
**Scope:** a chat assistant whose source of truth is `OptionStrategy.md` and the product's help content, plus the user's own journal for "why did it do that"; it cites its source; it says "I don't know" rather than guess; it never gives personal investment advice or predicts returns; wrong answers can be flagged and are reviewed. **Bug and enhancement requests** (product owner, 2026-10-27): the assistant may draft a request when the conversation points to a bug (the product does something other than its docs or the journal say) or an enhancement. It drafts only a request that passes its filter: what happened or what is wanted, where (page, bot, time), the data or example behind it (journal facts, numbers, a screen, steps to repeat), and why the docs do not already answer it. A vague request gets questions back, never a draft; a question the help answers gets the answer. The person reviews the draft and sends it with their own click. Sent requests go to a **private queue** in the monitor, never straight to GitHub (the repository is public): the PM agent reads the queue, drops duplicates and requests without evidence, and opens a public issue only with amounts, account details and personal data removed, linked back to the request for the person who sent it.
**Out of scope:** the assistant placing or changing anything, or filing a request the person did not send.

### E11 · In-product education
<!-- epic: {"id": "E11", "status": "approved", "rank": 8, "rule_areas": [], "ui": true} -->
**Outcome:** a non-technical user can read the monitor and a manager can set up a strategy using only the tips and videos in the product. Measured with user tests and the share of help questions they resolve.
**Why:** the persona may struggle to read the monitor and to set strategy parameters (product owner on #149).
**Scope:** a contextual tip for every jargon term and every parameter (what it is, how it changes risk and income); 30–90 s captioned how-to videos, one task each, linked where the task happens; empty states that explain what will appear; risk explained before any commitment.
**Content so far (stories #337, #338, #250, #339):** video storyboards `docs/product/education/video-storyboards.md`, the comprehension-test protocol `docs/product/education/comprehension-test.md` (pass bar is an assumption to confirm after the first round) and the empty-state inventory `docs/product/education/empty-states.md`; the glossary and empty-state texts are checked for sentence length and undefined jargon. Videos are not recorded yet.
**Out of scope:** courses and long-form content (marketing, E5).

### E12 · Responsive web for phones
<!-- epic: {"id": "E12", "status": "approved", "rank": 9, "rule_areas": [], "ui": true} -->
**Outcome:** every monitor and settings screen works on a phone (from 360 px wide) with the same information and actions as on desktop.
**Why:** customers check their income and risk from their phones; a native app is not planned yet (product owner on #149).
**Scope:** responsive layouts for every screen; touch-friendly controls; readable charts and tables on small screens; accessibility basics (contrast, font sizes, screen readers).
**Out of scope:** native iOS/Android apps; push notifications.

### E13 · Product analytics and customer insight
<!-- epic: {"id": "E13", "status": "approved", "rank": 6, "rule_areas": [], "ui": true} -->
**Outcome:** we know how people use the product and why they stay or leave: weekly engagement, cohort retention and churn reasons are reported without manual work.
**Why:** excelling at understanding customer pains is how we keep them (product owner on #149).
**Scope:** an event plan and instrumentation of the monitor (screens, actions, help used); dashboards and cohort reports on engagement and retention; in-product surveys (NPS, short "why" questions) and an exit survey; alerts when engagement or a key flow drops; privacy by design (consent, no secrets or amounts in events, data protection review).
**Out of scope:** selling or sharing user data; tracking outside the product.

## 10. Design

Principles: summary first, detail on demand; every number with unit, sign and time; risk next to the action, in money; losses as visible as gains; education at the point of need; works on a phone. Prototypes are made with the ux-designer skill and linked here once a person approves them.

| Epic | Prototype | Status |
|---|---|---|
| E3 | — | needed (designer starts now that the persona is confirmed) |
| E7, E8, E9, E10, E11, E12 | — | needed before their UI stories go to Ready |

## 11. Risks, compliance and open questions

- **Legal gate (needs legal review) — blocks E4 and any offer to third parties:** is running an automated strategy on a customer's own account, with their API key, a regulated activity (portfolio management, investment advice, a crypto-asset service) in the markets we target (Brazil: CVM; EU: MiCA/MiFID; US: SEC/CFTC)? What may marketing say about performance? Does a fee that scales with capital change the answer?
- **AI features (needs legal review):** the assistant (E10) and the strategy analysis (E9) must not be personal investment advice or imply expected returns; their outputs are logged and reviewable.
- **Data protection (needs legal review):** analytics, surveys and accounts (E7, E13) process personal data under LGPD/GDPR: consent, retention, the right to delete.
- **Exchange terms:** does each exchange allow third-party automation on customer accounts, and which jurisdictions does it serve? Checked per exchange in E8.
- **Risk of ruin:** short options can lose far more than the premium. Every product surface shows the worst case; defaults are conservative; defined-risk structures (iron condors) are considered before any retail offer (OptionStrategy.md §11.3).
- **Key security:** trade-only API keys, no withdrawal rights, encrypted at rest, revocable by the user; passwords hashed; roles enforced on the server (E7, E8).
- **Scope risk:** thirteen epics are more than a team runs at once. Ranks and the roadmap set the order: Now epics first, Later epics only once their prerequisites (E2 evidence, E7 accounts) exist.
- **Open questions:** (1) does a manager's parameter change apply to one customer account or to every account running that strategy? (2) who are managers — our staff only, or also customers' advisers? Both are needed before E9's stories go to Ready.

## 12. Decision log

| Date | Decision | Options | Reason | Decided by |
|---|---|---|---|---|
| 2026-10-17 | The PRD is approved by merging its pull request; epics carry the trading-rule areas the PM agent may send to Ready | label per epic; merge | one auditable gate, already used for code | human |
| 2026-10-17 | Prototypes are private claude.ai Artifacts, approved by the `prototype:approved` label on the epic | Next.js route; Artifact | fast to iterate, no code to maintain | human |
| 2026-10-17 | Product material that is sensitive stays in gitignored `data/product/` | all public; all private; split | the repo is public | human |
| 2026-10-18 | Primary persona: the long-term cycle holder seeking recurring monthly income | income holder; only the owner | confirmed target | product owner on #149 |
| 2026-10-18 | optionsBot is a product, not a study; the repo stays public and the PRD stays shareable, sensitive material in `data/product/` | product; study | commercial goal | product owner on #149 |
| 2026-10-18 | Agents may investigate, backtest, A/B test, record data, fix bugs and change UI/UX alone; core trading policies change only with the product owner's approval; E1's delegated areas are kept | keep; widen; narrow | solid foundation fast, core risk with a person | product owner on #149 |
| 2026-10-18 | Roles admin / manager / user; the platform never handles deposits, transfers or withdrawals — those stay on the customer's exchange | — | the platform only places orders | product owner on #149 |
| 2026-10-18 | New epics E7–E13 (accounts and roles, multi-exchange, strategy-change lab, AI assistant, education, responsive web, analytics) | all; some | requested by the product owner | product owner on #149 |
| 2026-10-18 | Pricing: the PM proposes in E5 (fair, low churn, no profit share); the product owner decides | — | monetization is a product decision with legal limits | product owner on #149 |
| 2026-10-27 | E10: the assistant drafts bug/enhancement requests that pass its evidence filter; the person sends them; they go to a private queue the PM agent triages into redacted public issues | file to GitHub directly; private queue | the repository is public; requests must carry data or examples | product owner in chat, recorded on #161 |
