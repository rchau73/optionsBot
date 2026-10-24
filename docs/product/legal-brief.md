# Legal-review brief (E5)

**Status:** questions for counsel, before any offer to third parties (#179, PRD §11). Based on PRD v0.2 (2026-10-18) and `OptionStrategy.md`.

> **Not legal advice.** This file states what the product does and lists the questions a lawyer must answer. It contains no answers, and nothing in it is a legal opinion. Counsel's names, quotes and fees go in the gitignored `data/product/`, never here (the repository is public).

**How to read it:** §1 states the facts counsel can quote against. §2 maps every risk in PRD §11 to its questions. §3 lists the questions — each names the jurisdiction, the epic it gates and the cheapest fallback if the answer is "regulated" or "not allowed". §4 says what each answer unlocks. `tests/legal_brief_test.go` checks that every PRD §11 risk has a question and every question names an epic and a fallback.

## 1. Product facts

These describe the product as planned in the PRD; where today's build differs, it says so.

| # | Fact |
|---|---|
| F1 | **What it is.** Software that runs one rules-based options strategy: it sells short strangles (an out-of-the-money call and put) on BTC and ETH options on Deribit, and buys them back by fixed rules (stop-loss at 2× premium, time roll, delta exit, take-profit, margin limits). `OptionStrategy.md` §3–§9 describe every rule. |
| F2 | **Custody: none.** The customer's coins stay on the customer's own exchange account. Deposits, transfers and withdrawals happen only on the exchange, never through the platform (PRD §2 strategy 3, §12 2026-10-18). |
| F3 | **Keys.** The customer creates an API key on their own exchange account and gives it to the platform. The key is trade-only: on Deribit, trading scope with no wallet (withdrawal) scope. Planned: keys encrypted at rest, revocable by the customer at any time on the exchange (PRD E8, §11 "Key security"). Today only our own accounts are connected (two Deribit **testnet** accounts; live only by an explicit opt-in on our own capital). |
| F4 | **What the software decides, without asking the customer trade by trade:** which options to sell (expiry, strike by delta), how many (sized to a margin limit set by volatility and market regime), when to buy them back (the exit rules), when to stop selling one side (market-regime filters), and emergency reductions when maintenance margin gets high. Every decision is written to a journal with its reason and the market at that moment. |
| F5 | **What the customer decides:** whether to connect an account and with which key; how much capital sits on that account (deposited on the exchange); when to stop (revoke the key, or stop the strategy). Under the planned roles (PRD §3) a **user** (the money owner) only monitors and asks questions; a **manager** may change strategy parameters and close legs by hand; an **admin** may do everything. Who managers are — our staff, or also customers' advisers — is open (PRD §11 open question 2). |
| F6 | **Where it runs.** Today on our own machines, for our own accounts. The planned product runs it for customers (hosted by us). A fallback in §3 is a licence where the customer runs it themselves. |
| F7 | **Performance figures.** Public figures exist only for testnet (fake money, thin prices — `OptionStrategy.md` §11.5: testnet proves mechanics, not profits). Replays on recorded mainnet order books and stress tests are hypothetical results. Live amounts stay private. The product promises no returns (PRD §2). |
| F8 | **Fees under consideration** (PRD §6, nothing decided): subscription tiers, a fee per connected account, or a fee by capital automated. **No share of profits.** A fee that scales with capital is flagged for legal review. |
| F9 | **AI features (planned).** E9: for a manager, an AI-written analysis of a backtest comparing current and proposed parameters. E10: a help assistant that answers from `OptionStrategy.md`, the product's help and the user's own journal ("why did the bot do that"), cites its source, and is told never to give personal investment advice or predict returns. Neither places or changes anything. |
| F10 | **Personal data (planned).** Accounts (username, hashed password, role), an audit log of who did what (E7), the customer's trade journal, encrypted API keys (E8), product analytics events that carry no amounts, sizes, prices or secrets (`docs/product/analytics/event-plan.md`), and surveys including an exit survey (E13). |
| F11 | **Target markets** for the first offer: Brazil, the EU and the US are named in PRD §11; none is decided. Deribit's own terms decide which customers may hold an account there. |

## 2. PRD §11 risks and their questions

| Risk (PRD §11) | Questions |
|---|---|
| Legal gate | L1–L9 |
| AI features | L10–L12 |
| Data protection | L13–L16 |
| Exchange terms | L17–L18 |
| Risk of ruin | L19–L20 |
| Key security | L21–L22 |
| Scope risk | L23 |
| Open questions | L24 |

## 3. Questions for counsel

"Gates" names the epic whose stories wait for the answer. "Fallback" is the cheapest way to go ahead if the answer is "regulated" or "not allowed" — still for counsel to confirm.

| ID | Risk | Where | Question | Gates | Fallback |
|---|---|---|---|---|---|
| L1 | Legal gate | Brazil (CVM) | Is running this strategy on a customer's own account, with discretion trade by trade (F4) and no custody (F2), portfolio management (administração de carteiras) or investment advice (consultoria) under CVM rules — and are BTC/ETH options on a foreign exchange "valores mobiliários" for this purpose? | E4, E5 | Software licence: the customer runs the software on their own machine with their own key and settings; we never hold keys or operate it. |
| L2 | Legal gate | Brazil (BCB) | Is it a virtual-asset service under Law 14.478/2022 and the Central Bank's rules for virtual-asset service providers, given we never hold or transfer assets? | E4, E5 | Same licence model as L1; no hosted service for Brazilian residents until authorised. |
| L3 | Legal gate | EU (MiCA) | Is it "portfolio management on crypto-assets" or "advice on crypto-assets" under MiCA, or are options on BTC/ETH outside MiCA because they are derivatives? | E4, E5 | Software licence (L1); no EU customers for the hosted service until authorised. |
| L4 | Legal gate | EU (MiFID II) | Are Deribit's BTC/ETH options financial instruments under MiFID II, making the hosted service portfolio management needing investment-firm authorisation? Does executing through the customer's key and the customer's account (not ours) change that? | E4, E5 | Software licence; or partner with an authorised firm that operates the service while we supply the software. |
| L5 | Legal gate | US (SEC/CFTC) | Is the hosted service commodity trading advice (CTA) under the CEA because BTC/ETH options are commodity options, investment advice under the Advisers Act, or neither? Does any registration exemption fit a small number of customers? Deribit's terms may exclude US persons (L17). | E4, E5 | Exclude US persons from the hosted service and the waiting list; licence only if counsel confirms that is not advice. |
| L6 | Legal gate | All | Does the answer to L1–L5 change between the testnet trial (E4: fake money, testnet accounts) and live trading? May we run the testnet trial for invited users before any authorisation? | E4 | Keep the trial to us and named friends-and-family testers under a written "testnet only, no real money" agreement. |
| L7 | Legal gate | All | What may marketing and the landing page say about performance: may we show testnet figures, replay or stress results (hypothetical), or our own live results? Which labels and disclaimers are required (hypothetical, past performance, net of fees)? | E5, E2 | Show no performance figures; describe only the mechanics, the rules and the risks, and show the worst case. |
| L8 | Legal gate | All | Does a fee that scales with capital automated (F8) read as a management fee and change the answers to L1–L5, compared with a flat subscription or a fee per connected account? | E5 | Flat subscription or per-account fee only; no fee linked to capital or results. |
| L9 | Legal gate | All | May we run a waiting list and a fake-door pricing test (E5) before we know whether the service may be offered — what wording keeps it from being an offer or solicitation? | E5 | A waiting list that collects interest only, with no price, no performance and a "not available yet" notice. |
| L10 | AI features | All | Is an AI analysis of a backtest comparing parameters (E9), shown to a manager, investment advice — and does it matter whether the manager is our staff or the customer's adviser (L24)? | E9 | Show only the numbers of the comparison run (return, worst week, drawdown, fees), with no AI-written recommendation. |
| L11 | AI features | All | Is a help assistant that explains the strategy and the user's own trades (E10) personal advice in any target market? What must it never say, and what disclaimer and logging are required? | E10 | Answers only from the published help and the journal's recorded reasons, with no forward-looking or "should I" answers; or no assistant, only searchable help. |
| L12 | AI features | EU | Does the EU AI Act place either feature in a regulated category (e.g. a transparency obligation for a chatbot), and what does that require? | E9, E10 | Label every AI output as AI-generated and keep every output logged and reviewable (PRD §11). |
| L13 | Data protection | Brazil (LGPD), EU (GDPR) | What is the lawful basis for each kind of personal data in F10 (accounts, audit log, journal, keys, analytics, surveys), and which need consent? | E7, E13 | Collect only what the service needs to run; analytics and surveys opt-in only. |
| L14 | Data protection | Brazil (LGPD), EU (GDPR) | How long may we keep the trade journal and the audit log after a customer leaves, given the right to delete versus a possible duty to keep records of trading activity? | E7, E13 | Keep only what a record-keeping duty requires, deleted at the end of that period; delete everything else on request. |
| L15 | Data protection | Brazil (LGPD), EU (GDPR) | Is first-party product analytics without amounts or secrets (`event-plan.md`) allowed without a cookie-consent banner? Which hosting locations and processors need transfer safeguards? | E13 | No analytics until reviewed; server-side, first-party counting of screens only, no identifiers. |
| L16 | Data protection | All | Do exit surveys and in-product surveys need separate consent, and may answers be linked to an account? | E13 | Anonymous surveys, not linked to an account. |
| L17 | Exchange terms | All | Do Deribit's terms (and later Bybit's, OKX's, Binance's, Hyperliquid's) allow a third party to trade a customer's account through an API key? Which customer residences do they exclude, and must we check residence ourselves? | E4, E8 | Serve only customers the exchange itself accepts, and require them to confirm the exchange's terms allow it before connecting. |
| L18 | Exchange terms | All | If an exchange changes its terms or bans automation on a customer's account, who is liable for positions left open, and what must our customer terms say? | E8 | Customer terms state the exchange's rules prevail and the customer may close everything on the exchange at any time. |
| L19 | Risk of ruin | All | Short options can lose far more than the premium. Which suitability or appropriateness checks and risk disclosures are required before a retail customer connects (e.g. MiFID appropriateness, CVM suitability)? Must defined-risk structures be the default for retail? | E4, E5 | Professional or experienced investors only, with a written risk disclosure and the worst case shown before connecting; defined-risk structures before any retail offer (`OptionStrategy.md` §11.3). |
| L20 | Risk of ruin | All | What must the customer terms say about losses, including a loss larger than the premium, an exchange outage, or the software failing — and may we limit our liability for them? | E4, E5 | Licence terms where the customer operates the software and accepts its rules as written. |
| L21 | Key security | All | Does holding a customer's trade-only API key (no withdrawal right) count as control or custody of their assets under any target regime? | E8, E4 | The customer runs the software and keeps the key; we never see it. |
| L22 | Key security | Brazil (LGPD), EU (GDPR) | What breach-notification and security duties apply if keys or accounts are compromised, and what minimum security must the terms promise? | E7, E8 | Keys never stored by us (licence model); or encrypted keys with a breach plan counsel approves. |
| L23 | Scope risk | All | Which epics may be built and used internally (our own accounts, our own capital, testnet) before any authorisation, and which need the answers above before even a private beta with invited users? | E4, E5, E13 | Build everything for internal use only; no third party until the answers are in. |
| L24 | Open questions | All | If managers may be customers' own advisers rather than our staff (PRD §11 open question 2), or one manager's change applies to many customers' accounts (open question 1), does that make us or them the regulated party? | E9, E7 | Managers are our staff only, and each parameter change applies to one account with that customer's confirmation. |

## 4. What each answer unlocks

| Epic | Waits for | Decision unlocked |
|---|---|---|
| E4 · Risk-free trial | L1–L6, L17, L19–L21, L23 | whether invited users may connect a testnet account, and in which countries |
| E5 · Discovery and go-to-market | L1–L9, L19, L20, L23 | which markets to target; what the landing page may say; which pricing models (L8) the proposal may include; whether a paid beta is possible |
| E2 · Proof of performance | L7 | which figures may be published, and with which labels |
| E7 · Accounts and roles | L13, L14, L22, L24 | what account data we keep and for how long; who may be a manager |
| E8 · Multi-exchange | L17, L18, L21, L22 | which exchanges and residences to support; whether we may hold keys |
| E9 · Strategy-change lab | L10, L12, L24 | whether the AI analysis may be shown, and to whom |
| E10 · AI assistant | L11, L12 | what the assistant may answer, and its required disclaimers |
| E13 · Analytics | L13–L16, L23 | which events and surveys may be collected, with what consent |

## 5. What we hand counsel

- This file, PRD §1–3, §6 and §11, and `OptionStrategy.md` (the rules in plain words, §8 risk controls, §11 lessons).
- A walk-through of the monitor on testnet: the journal, the reasons for each trade, the kill switch.
- The planned API-key scopes per exchange and how a key is revoked.
- `docs/product/analytics/event-plan.md` (what analytics may and may not carry).

Answers go back into the PRD by pull request (§11, and the decision log §12); counsel's written opinion stays in `data/product/`.
