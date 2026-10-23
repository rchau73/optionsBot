# Experiment: landing page with a waiting list (fake door)

**Status:** brief, designed before anything is built (#234, E5). **Nothing goes online** before counsel has reviewed the page (`docs/product/legal-brief.md` L5, L7, L9, L13; `docs/legal/preliminary-answers.md`) and the product owner has chosen the waiting-list tool and approved publishing. The copy is in `docs/product/landing-copy-draft.md`; the ux-designer prototype comes after this brief.

**Where data goes:** sign-ups and every export stay with the chosen waiting-list tool and in the gitignored `data/product/waitlist/`, never in the repository, an issue or a pull request. Public documents (this brief's result, the PRD) carry only counts and shares. Competitor and price research stays in `data/product/`.

## 1. Hypothesis

We believe a page that explains the mechanics plainly — the reason for every trade, the worst case before every risk, orders placed on the visitor's own exchange account through a key that cannot withdraw, and a trial on the exchange's **testnet** with no real money — will turn **at least 10 % of cycle-holder visits** into waiting-list sign-ups, because cycle holders tell us (PRD §3) that today's yield products are opaque and too complex to run by hand, and the page answers exactly that without promising any return.

What it decides: whether there is enough demand among cycle holders to keep building toward the testnet trial (E4) and a pricing proposal (E5), measured by an action (leaving an e-mail), not an opinion.

## 2. Segment and channels

| | |
|---|---|
| Segment | the **cycle holder** (PRD §3; same definition as `docs/product/discovery/screener.md` §1): holds BTC or ETH today, bought before 2022, kept most of it through the 2022 fall |
| Channels | PRD §6 only: content (the public testnet track record and the agentic-development story) and posts in crypto-option and long-term-holder communities. **No paid acquisition**, no bought lists, no cold e-mail |
| Excluded | US persons (preliminary answer to L5: excluded everywhere, waiting list included); anyone under 18; the team and their close friends and relatives (they skew the rate) |
| Channel tag | each post links with a channel tag in the URL (e.g. `?c=forum-a`), so the rate can be cut per channel; the tag names a channel, never a person |

## 3. Design

- **One page, one variant** (no A/B split): the traffic we can reach without paid acquisition is too small to split. Copy changes between runs are a new run with its own brief result, never mixed.
- **The page:** headline, sub-head, three value points each with its risk beside it at the same size, the "not available yet" notice, and the sign-up form (`landing-copy-draft.md`).
- **The form:** e-mail (required) · consent tick (required, unticked by default) · residence (required: country, with "United States" leading to the exclusion message and no sign-up) · two optional segment questions · two optional choice questions (§4 secondary metrics).
- **The honest end state.** After sign-up the visitor reads, at once and in full: *the service is not available yet, this list records interest only, it is not an offer, there is no price and we accept no money* (the waiting-list notice of `docs/legal/drafts/avisos.md`, L9). There is **no fake checkout**, no price, no payment form and no field for card, bank, wallet or exchange-key details — at no step.
- **Counting visits:** server-side, first-party, page loads only, no cookie and no stored IP (the L15 fallback). A "visit" is one page load. **Assumption:** page loads ≈ visitors for this traffic; verify by comparing with the click counts the channels themselves show (where they do) in the first week, and note the ratio in the result.

## 4. Metrics

| Metric | Definition | Source |
|---|---|---|
| **Primary:** visit → sign-up rate | confirmed sign-ups ÷ visits, over the whole run and per channel tag | waiting-list tool (sign-ups), server page-load count (visits) |
| Cycle-holder share of sign-ups | sign-ups answering "before 2022" **and** "kept most" ÷ sign-ups that answered both | the two optional segment questions |
| Secondary: first thing wanted | share choosing each option of "What would you want first?" — *a free trial on the exchange's testnet, no real money* · *watching a demo account's monitor* · *just keep me informed* | optional choice question |
| Secondary: fair way to pay | share choosing each option of "If it is ever offered, which way of paying would feel fair?" — *a fixed monthly subscription* · *a fee per connected exchange account* · *not sure* — **no amounts** (preliminary answer to L9: no price for an investment service; L8: no fee linked to capital or results, so that model is not offered) | optional choice question |
| Confirmation rate | confirmed ÷ submitted (double opt-in) | waiting-list tool |

Price points are **not** tested in this run: they wait for counsel's answer to L9 (and L8). A later run may test them with its own brief.

## 5. Sample and duration

- **Sample: at least 400 visits.** Sized so the primary rate is known to about ±3 percentage points at the 10 % success threshold (95 % interval: n = 1.96² × 0.10 × 0.90 ÷ 0.03² ≈ 385). It also separates the kill threshold (3 %) from the success threshold (10 %) with margin: telling 3 % from 10 % one-sided at 5 % significance and 80 % power needs about 60 visits, so 400 is driven by the precision, not the test. Report the rate with its Wilson 95 % interval.
- **Duration: at least 2 weeks, at most 4** — two full weekly cycles so weekday and weekend readers both count; stop at 4 weeks whatever the count.
- **Assumption:** our unpaid channels reach 400 visits in 4 weeks. Verify after week 1: if the first week brought fewer than 100 visits, the run will not reach the sample — say so, keep it running to 4 weeks, and treat the reach itself as a finding (§6 "Not enough traffic").
- Thresholds are fixed **now**, before any visit, and never moved during the run.

## 6. Decision rule

Thresholds sit beside PRD §6's kill criteria: "< 30 % of interviewed cycle holders would let software trade their account" (measured by the survey's fake door, `docs/product/discovery/synthesis.md` §4) and "E2 shows the engine doesn't beat simply holding the coin". The page measures the same willingness by an action from strangers; the survey measures it from screened cycle holders. The verdict reads both.

| Outcome | When | Decision it leads to |
|---|---|---|
| **Success** | rate ≥ 10 % (lower bound of the interval ≥ 6 %) and cycle holders ≥ half of the sign-ups that answered | keep E5 going: write the pricing proposal for the product owner; propose the testnet-trial path (E4) once the legal gate and E2 allow it; reuse the copy for the next run |
| **Iterate** | rate between 3 % and 10 %, or success with cycle holders under half | change one thing (copy or channel, chosen from the per-channel cuts and the F2-style reasons from the survey) and run again with a new brief result; at most two iterations, then decide go / no-go on the evidence there is |
| **Kill** | rate < 3 % (upper bound of the interval < 6 %) | recommend **no-go on the hosted product** to the product owner, and with the survey: if the survey share is also < 30 %, PRD §6's kill criterion is met → no-go. If the survey share is ≥ 30 %, the page — not the demand — is the suspect: one rewrite, then decide |
| **Not enough traffic** | fewer than 400 visits after 4 weeks | no verdict on demand; report the reach per channel and the rate with its (wide) interval; the product owner decides whether to extend, add channels (still unpaid) or stop |

The product owner takes the decision; this brief only says what each outcome recommends.

## 7. Guardrails

Stop the run at once, and tell the product owner, if any of these happens:

- any message or post reads the page as an offer, a promise of returns or advice (preliminary answer to L9; risk register R13);
- a complaint to a regulator, a platform or a community moderator about the page;
- a community removes a post as spam or advertising (we follow each community's rules on self-promotion first);
- a sign-up from the United States gets through the residence check;
- a field other than those in §8 starts collecting data.

Watched during the run: unsubscribe and spam-report rates of the confirmation e-mail (stop if spam reports exceed 1 % of e-mails sent), and confirmation rate (below half of submissions means the confirmation e-mail is failing, not the demand).

## 8. Personal data (LGPD · GDPR)

The waiting list collects personal data, so it follows `docs/legal/data-protection.md` (§2 row "Marketing email, waiting list": consent, kept until withdrawn; §4 consent records kept while consent lasts + 5 years; §5 rights; §9 transfers).

**Data collected:**

| Item | Required | Why | Kept |
|---|---|---|---|
| E-mail | yes | the only way to tell the person when and where a trial exists | until they leave the list |
| Consent: the tick, the consent text version, date and time | yes | proof of consent | while consent lasts + 5 years |
| Country of residence | yes | exclude US persons (L5) and later tell each person only about what is available where they live | with the e-mail |
| Segment answers (when they first bought; kept through 2022) | no | the cycle-holder share | with the e-mail, or anonymised at the end of the run |
| Choice answers (§4) | no | secondary metrics | same |
| Channel tag of the visit | — | the per-channel rate | with the sign-up |

**Not collected:** name, phone, amounts held, exchange or account details, API keys, IP address, cookies or any tracking identifier, payment details of any kind.

**Consent text needed** (draft, needs legal review — wording builds on the waiting-list notice in `docs/legal/drafts/avisos.md` and the survey consent in `docs/product/discovery/consent.md`; a PT-BR version is needed for Brazil):

> ☐ I want to join the waiting list. I agree that optionsBot keeps my e-mail, my country and my optional answers to tell me when and where a trial becomes available, and to count interest in it. No other use, no sharing for marketing, no sale. I can leave the list or ask to see or delete my data at any time through the link in every e-mail. I am 18 or older and I do not live in the United States.

**Open data-protection questions** (for counsel; legal-brief L13, L15, L9):

1. Is consent the right basis for the waiting list under LGPD and GDPR, and is one unticked box enough, or must "tell me about a trial" and "count interest" be separate?
2. Where may the list be stored — which waiting-list tools or providers are acceptable as processors, and what transfer safeguards they need if they are outside Brazil or the EEA (`data-protection.md` §9)?
3. Retention: "until withdrawn" for a product that may never launch — should the list expire (e.g. delete everyone after a fixed period with no launch), and how long are consent records kept?
4. Is server-side page-load counting without cookies or IPs (§3) allowed without a cookie banner (L15)?
5. Does asking the country of residence to exclude US persons need its own notice, and is a self-declaration enough?
6. Who is the named controller and the contact for requests on the page (`data-protection.md` §1 `[FILL]`)?
7. Does the double-opt-in confirmation e-mail count as marketing, and what must it say?

**The tool or provider is the product owner's decision**, after counsel answers questions 1–2.

## 9. Risks and compliance

- **Read as an offer** (L9, R13): mitigated by the "not available yet" notice, no price, no money, no checkout.
- **Performance claims** (L7): the page shows no return, yield, APY or performance figure, not even testnet's; it describes mechanics and risks only.
- **Exclusions** (L5, L17): US persons excluded; the page names Deribit's testnet only as the place of a future trial.
- **Risk of ruin** (L19): every benefit carries its risk beside it at the same size; "it can lose money, more than the premium received" is on the page.
- **Before publishing:** counsel's review of `landing-copy-draft.md` and §8, the product owner's approval, and the ux-designer prototype approved by a person.

## 10. Result (filled after the run)

Visits · sign-ups · rate with its Wilson 95 % interval · per-channel rates · cycle-holder share · secondary shares · confirmation rate · guardrail events · the survey's kill-criterion share beside it · outcome (§6) · decision taken by the product owner · what we learned.
