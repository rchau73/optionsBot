# Launch plan: private alpha → private beta → paid beta (E5)

> **Needs legal review: nothing here is offered to anyone outside the team until counsel has answered in writing.**
> Not legal advice, not an offer, no promise of returns.

**Status:** draft (#624) for the product owner's approval.
It writes down the three launch phases of PRD §6, which named them and held no plan.
Every number marked **(assumption)** is a target for the product owner to confirm or change; none is evidence.
No price, revenue figure, name or survey answer is in this file: those stay in the gitignored `data/product/` (`data/product/launch/` for the launch log, `data/product/interviews/` for the survey).

## 1. What this page is, and is not

- It is the one page to read when the go / no-go decision comes: what must be ready, what we measure and how we step back.
- The decision is the product owner's, on the whole evidence (`go-no-go.md`).
- The kill criteria are PRD §6's and are quoted in section 7, never changed here: changing them is a PRD pull request.
- Roles, trading rules and config do not change because of a launch phase.

## 2. Segment and ICP (all phases)

- **ICP:** the cycle holder (PRD §3): holds BTC or ETH through the cycle, wants recurring income without selling the coins, is not technical.
- **Segment (PRD §6, quoted):** "the cycle holder with BTC or ETH on Deribit (or willing to open an account); other exchanges after E8".
- Secondary personas (the active option trader, the curious beginner) may join the beta, but no metric or decision here is read on them alone.
- How many cycle holders exist, what they would pay and whether they let software trade their account are still assumptions (`go-no-go.md` §7).

## 3. The sources every gate points to

| Source | What it holds | Where |
|---|---|---|
| Go / no-go rows 1–4 | survey delegation share, fake-door outcome, E2 risk-adjusted test, legal gate L1–L6 and L9 | `go-no-go.md` §2, run `python3 scripts/product/go_no_go.py` |
| Legal questions L1–L24 | what counsel must answer, what each answer unlocks, the cheapest fallback | `legal-brief.md` §3 and §4 |
| E2 scoreboard | does the engine beat holding the coin, risk-adjusted | `go-no-go.md` §3, `internal/evidence` `ScoreWeeks` (every weekly evidence report carries the `scoreboard` block) |
| Event plan | the events and questions Q1–Q8 the product metrics read | `analytics/event-plan.md` §1 and §2 |
| PRD metrics and guardrails | north stars, retention, self-service, guardrails | `PRD.md` §7 |
| Landing copy | the only draft wording about the offer | `landing-copy-draft.md` |

## 4. Readiness checklist

Each line is ready, or the phase does not start.
"Owner" is a role; the person is named in no file here.

| Area | Ready when | Needed in | Source or ticket | Owner |
|---|---|---|---|---|
| Support path | one mailbox or form is named, a person answers within the time promised to testers, and each request is coded (question, bug, complaint, request) | beta, paid beta | assistant request triage `assistant/request-triage.md` | product owner |
| Docs and in-product help (E10, E11) | the monitor guide covers every page a tester sees, the tips and videos exist for them, the assistant answers from the guide and refuses advice, and its golden questions pass | alpha: guide and assistant; beta: tips and videos | `assistant/contract.md`, `education/README.md`, `frontend/content/monitor-guide.md` | product owner |
| The monitor's four answers (E3) | a new tester can say what the account holds, whether it is making or losing, the worst case and why the last trade happened, within 5 minutes without help | alpha: tried by us; beta: by testers | event plan Q4, event `first_answers_seen` | product owner |
| Roles and audit (E7) | admin, manager and user roles hold as in PRD §3, a user cannot change a strategy or move money, and every state-changing action is in the audit log | beta, paid beta | PRD §3 and §9 E7, `docs/legal/audit-and-records.md` | product owner |
| Analytics consent (E13) | analytics is off until the person says yes, the yes is stored before any event is sent, and no amount or secret travels | beta, paid beta | event plan §3–§5, L13, L15, L16 | product owner |
| Status and monitoring (E6) | the owner is told when a bot, the feed or the monitor is down, the kill switch is rehearsed, and a rollback to an older release tag has been tried | alpha | PRD §9 E6, `scripts/promote.sh`, `docs/operations.md` | product owner |
| Legal texts counsel must approve first | terms of use, risk-disclosure acknowledgement, privacy policy, the tester agreement ("testnet only, no real money") and any text a user sees; **needs legal review** | beta: testnet texts; paid beta: all | L6, L7, L13, L19, L20, `docs/legal/README.md` | product owner with counsel |
| Keys | testers use trade-only keys on their own testnet account, or the customer runs the software and keeps the key | beta, paid beta | L17, L21, L22 | product owner with counsel |

## 5. The phases

For every metric: **definition**, **source**, **owner**.
A metric with no event yet says which event is missing, and is read by hand until it exists.
All targets are **(assumption)** until the product owner confirms them.
Figures from testnet, a replay or a stress test are always labelled so, with the risk beside them; a result on testnet is not a market result.

### 5.1 Phase 1: private alpha (us, testnet)

**Segment:** the team and the product owner, on our own testnet accounts.
No outside person sees the product, so no legal step is needed.

**Entry gate**
- The engine runs on testnet with the monitor on each environment (`docs/operations.md`).
- The readiness rows marked alpha in section 4 are ready.
- The scorecard runs end to end (`go_no_go.py`); its rows may still be `missing`.

**Exit gate (to private beta)**
- Go / no-go rows 1 and 3 are read and neither fails (PRD §6 kill criteria, quoted in section 7); row 2 is Success or the product owner decides in writing to go ahead without it.
- Row 4: counsel answered L1–L6 and L9 in writing, and L6 allows invited users on testnet. **Needs legal review (L6, L23).**
- The product owner approves the beta gates and targets of this page.

| When | Metric | Definition | Source | Owner | Target (assumption) |
|---|---|---|---|---|---|
| Day 1 | Four answers, first try | each of us answers the four questions of E3 from the monitor alone | event plan Q4, `first_answers_seen`; by hand until a session is seen | product owner | all of us, under 5 minutes |
| Day 1 | Decisions explained | share of actions with a reason in the monitor | PRD §7, journal | product owner | 100 % |
| Week 1 | Defects found | open defects tagged from alpha use, by severity | the GitHub board | product owner | no open severity-1 |
| Week 1 | Help resolved | share of "did this help?" answered yes | `help_resolved` | product owner | read, no target yet |
| Month 1 | E2 reading | the scoreboard status of the stable bot | `go_no_go.py` row 3, weekly evidence reports | product owner | status read each week; the 12-week window may still be open |
| Month 1 | Worst week | worst 7-day P&L in coin and % of equity, from testnet runs | `internal/perf` `Summarize` | product owner | within the risk budget E2 defines |

**Rollback:** stop inviting; the bots keep running on testnet and nothing outside the team is affected.
If a release misbehaves, promote an older tag with `scripts/promote.sh` (rollback = an older tag, `docs/operations.md`).
**Kill criterion:** PRD §6 (section 7).

### 5.2 Phase 2: private beta (5–10 invited users, testnet)

**Segment:** 5–10 invited cycle holders, each on their own Deribit testnet account, under a written "testnet only, no real money" agreement.
Invitations go by hand, from the people who answered the survey and agreed to be contacted (`data/product/interviews/`).

**Entry gate**
- Phase 1 exit gate met.
- **Needs legal review: L6, L17, L19, L20, L21, L23.** Counsel's written answer allows invited users on testnet, in the countries chosen, with the terms and risk text counsel approved.
  Until that answer is recorded (as in `docs/legal/counsel-engagement.md`), no invitation is sent; the fallback is us and named testers under the written agreement (L6 fallback).
- **Needs legal review: L13–L16** answered before analytics or surveys run for outside users; until then none are on for them (L15 fallback).
- **Needs legal review: L11, L12** answered before testers use the assistant.
- Every beta row of section 4 is ready.

**Exit gate (to paid beta)**
- Go / no-go has no `missing` row and no kill criterion met; the overall line is "no kill criterion met" (`go-no-go.md` §4).
- Row 3 meets over the full 12-week window, or the product owner decides in writing to go ahead and why.
- The month-1 beta targets below are met, and the guardrails of section 6 did not get worse.
- **Needs legal review: L1–L5, L7, L8, L19, L20, L22**: counsel's written answers say a paid beta with small live capital may be offered, to whom, under which wording.
- The pricing is decided by the product owner after L8 (`pricing-options.md`); the number is not in this file.

| When | Metric | Definition | Source | Owner | Target (assumption) |
|---|---|---|---|---|---|
| Day 1 | Tester connected | invited testers who reach a first monitor view | `session_started`, `screen_viewed` (needs the tester's yes to analytics, L15) | product owner | at least 70 % of invited |
| Day 1 | Four answers | testers who see the KPI strip, positions, worst case and the last reason in the first session | `first_answers_seen` | product owner | at least 70 % |
| Week 1 | Active testers | testers with at least one visit in the week while a strategy ran (the PRD §7 product north star, in a week) | `session_started` | product owner | at least 60 % of invited |
| Week 1 | Self-service | share of help questions answered by tips, videos or the assistant without a person | `assistant_answered`, `help_resolved`; requests to a person from the triage log | product owner | set at beta (PRD §7) |
| Week 1 | Understanding | testers who say the worst case in their own words | the comprehension test `education/comprehension-test.md`; no event, by hand | product owner | at least 80 % |
| Month 1 | Retention | testers still active in week 4 | event plan Q2, cohort retention | product owner | at least 60 % of week 1 |
| Month 1 | NPS and reason | monthly NPS with its coded reason | `survey_answered`, `surveys.md` | product owner | read, no target on n < 10 |
| Month 1 | Delegation | testers who say they would let software trade their live account | the follow-up survey (`discovery/survey.md`), by hand; no event | product owner | at least 30 %, the PRD §6 threshold |
| Month 1 | Time to answer | median time from a tester's request to the first reply | support triage log; **event missing**: requests are not an event yet | product owner | under 1 working day |

**Rollback**
1. Stop new invitations.
2. Ask each tester to close the session; their testnet account is theirs and holds no real money.
3. Switch off what reaches them (the assistant, surveys, analytics) before touching the engine.
4. Delete or return their data as counsel's answer on L14 and the privacy text say.
5. Fall back to phase 1.

**Kill criterion:** PRD §6 (section 7).

### 5.3 Phase 3: paid beta (small live capital)

**Segment:** the same cycle holders, a small number, with small live capital on their own Deribit account and a trade-only key.
The capital limit and the fee are the product owner's, in `data/product/`.
**This phase starts only after the legal gate**: nothing live, and no fee, ships before counsel's written answer.

**Entry gate**
- Phase 2 exit gate met.
- **Needs legal review: L1–L5** answered in writing and allowing the live service in the chosen markets, or the licence model of the fallback column.
- **Needs legal review: L7** on every performance figure shown; **L8** on the fee; **L19, L20** on suitability and loss wording; **L21, L22** on keys and breach duties; **L18** on exchange changes.
- Roles and audit hold with real accounts (E7); the kill switch is rehearsed on the live account's setup with the tester.
- The product owner approves the capital limit and the fee in writing.

**Exit gate (to wider availability)**
- A post-launch review (product-manager template) at 2 and 6 weeks, with metric against target, guardrails and the decision: scale, iterate or roll back.
- Anything wider than the paid beta is a new plan and a new legal question, not part of this one.

| When | Metric | Definition | Source | Owner | Target (assumption) |
|---|---|---|---|---|---|
| Day 1 | Accounts connected | paid beta accounts with a strategy running | monitor, `session_started`; account count by hand | product owner | all invited |
| Day 1 | Guardrails clean | kill-switch events, stop-losses and the MM peak on the new accounts | journal, `internal/perf` | product owner | none worse than the existing bots |
| Week 1 | Margin used | IM used ÷ IM limit, time-weighted over the time entries are allowed | `/api/status` `utilisation_7d` (PRD §7) | product owner | at least 60 % when allowed (PRD §7) |
| Week 1 | Worst week | worst 7-day P&L in coin and % of equity; **a live result, not a forecast** | `internal/perf` `Summarize` | product owner | within the risk budget E2 defines |
| Month 1 | Net return per margin | coin-denominated net return per unit of margin used, net of fees (PRD §7 engine north star) | journal, `pnl_history.jsonl` | product owner | set after 30 days of baseline (PRD §7) |
| Month 1 | Retention | accounts still active after 30 days | event plan Q2; revenue churn **event missing**: cancellation reasons are not an event yet (`surveys.md` exit survey) | product owner | curve flattens by month 3 (PRD §7) |
| Month 1 | Complaints and refunds | count and coded reason | support triage log; no event | product owner | none unresolved |

**Rollback**
1. Stop new accounts.
2. Use the kill switch on every affected account (cancel orders, flatten, stay idle), then confirm each is flat on the exchange.
3. Tell the testers, in the words counsel approved, that the service is paused and that they can close everything on the exchange at any time (L18 fallback).
4. Revoke the trade-only keys with the customer, and delete or keep their data as L14 requires.
5. Refund what was paid, if a fee was charged, as the terms say.
6. Fall back to phase 2 or 1.

**Kill criterion:** PRD §6 (section 7), and any guardrail of section 6 getting worse after a release stops the rollout.

## 6. Guardrails (all phases)

The PRD §7 guardrails, quoted: "max drawdown, stop-loss count per week, MM peak, kill-switch events; for the product: complaints, refunds, assistant answers flagged wrong".
"none may get worse after a release" is the PRD's target; a phase does not widen while one is worse.

| Guardrail | Definition | Source | Owner | Missing |
|---|---|---|---|---|
| Max drawdown | worst fall from peak, in coin and % of equity | `internal/perf` | product owner | — |
| Stop-loss count per week | stops the bot made in the week | journal | product owner | — |
| MM peak | highest maintenance margin ÷ equity | monitor, journal | product owner | — |
| Kill-switch events | times the kill switch ran | journal | product owner | — |
| Complaints | requests coded "complaint" by the support path | triage log | product owner | no event; counted by hand |
| Refunds | refunds made (paid beta only) | by hand, in `data/product/` | product owner | no event |
| Assistant answers flagged wrong | flags per 100 answers | `assistant_flag` ÷ `assistant_answered` | product owner | — |
| Stale or failed data | pages shown stale or failed under a tester | `data_stale_shown` | product owner | — |

One flagged-wrong answer about advice, a promise or a number is read by a person the same day and, if confirmed, the assistant is switched off for outside users until fixed.

## 7. Kill criteria (PRD §6, quoted)

> <30 % of interviewed cycle holders would let software trade their account, or E2 shows the engine doesn't beat simply holding the coin on a risk-adjusted basis

Quoted, not set: `scripts/product/test_go_no_go.py` fails if the PRD's wording moves alone.
If either is met in any phase, the phase stops, its rollback runs and nothing further is invited.

## 8. Positioning and messaging

**PRD §5 draft (quoted, still to test in E5):**

> for long-term crypto holders who want monthly income from their coins, optionsBot is an option-selling strategy that runs on your own exchange accounts.
> Unlike yield products that hide the risk behind an APY, it shows the reason for every trade and the worst case before every risk, it never touches your funds, and it publishes its full track record, net of fees.

- The wording of the offer is `landing-copy-draft.md`: C1–C9 and V1–V3 are quoted from there, each risk beside its benefit, and each with its L-numbers.
- **Needs legal review (L1, L5, L7, L9, L19):** none of it goes online, into an invitation or a post before counsel's written answer and the product owner's approval to publish (#234).
- No return, yield, APY or other performance figure in any invitation or message.
  A figure is shown only if L7 allows it, labelled testnet, replay, stress test or live, with "past results are not a forecast" and the risk beside it.
- Testnet is always called testnet, with "no real money" beside it.
- Options can lose more than the premium collected; this sentence goes wherever the benefit goes.
- The track record is the stable bot's weekly evidence, net of fees; its backtests and stress tests are labelled as such.

## 9. What only a person can do

| What | Who | Ticket | Unblocks |
|---|---|---|---|
| Confirm or change the phase gates and the numeric targets marked assumption | Product owner | #624 | every phase |
| Send the survey and record the tally in `data/product/interviews/` | Product owner | #178 | kill criterion 1, the beta's invitation list, delegation target |
| Choose the waiting-list tool, approve the page and publish it after counsel's review | Product owner | #234 | demand signal, first invitations |
| Get counsel's written answers: L1–L6 and L9 first, then L7, L8, L11–L24 | Product owner with counsel | #179 | beta entry, paid beta entry |
| Approve the terms, risk text, privacy policy and tester agreement | Counsel, then product owner | #179 | beta entry |
| Decide the fee and the live capital limit, after L8 | Product owner | #236 | paid beta entry |
| Do the competitor research in `data/product/competitors/` | Product owner | #304 | positioning |
| Name and staff the support path | Product owner | #624 | beta entry |
| Invite the testers and keep the list in `data/product/` | Product owner | #624 | beta |
| Decide go, no-go, scale or roll back at each exit gate | Product owner | #305 | every phase |

Open state is checked on the board, not here.

## 10. Keeping this page true

- Update the gates and targets when the product owner changes them, with the date and the decision file under `decisions/`.
- A metric that gets its event moves from "missing" to the event's name, and `analytics/event-plan.md` is the source.
- No price, revenue figure, name or survey answer goes here, only the path under `data/product/`.
- A change to the kill criteria is a PRD pull request, never an edit here.
