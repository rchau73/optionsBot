# Landing-page copy — first draft (E5)

> **Needs legal review: not to be published.** No sentence of this file goes online, into a post or to anyone outside the team before counsel has reviewed it against `docs/product/legal-brief.md` and the product owner has approved publishing (#234). Not legal advice.

**Status:** draft (#234), built on PRD §5's positioning. The experiment it serves is `docs/product/experiments/fake-door-landing.md`; the ux-designer prototype lays it out.

**Rules this copy keeps** (`tests/landing_copy_test.go` checks them):

- No return, yield, APY or performance figure of any kind — not testnet's, not a replay's, not a stress test's (preliminary answer to L7). No number in the copy at all, except the age limit of the consent line.
- Every benefit has its risk beside it, in the same row and at the same size: the prototype shows both in the same type style, never the risk in small print.
- Testnet is always called testnet, with "no real money" beside it; the page never calls it a market result.
- Every element carries, in its "Legal" column, the legal-brief questions (L-numbers) it raises.
- No price, no checkout, no payment field (L9).

English only for now; the PT-BR copy is written after counsel has approved this wording (Brazil first, `docs/legal/`).

## 1. Page

| ID | Element | Copy | Legal |
|---|---|---|---|
| C1 | Headline | Sell options on the coins you already hold — and see every risk before it is taken. | L1, L7, L19 |
| C2 | Sub-head | We are building software that would sell options on your BTC or ETH from your own exchange account, aiming for monthly income without selling your coins. It can lose money, sometimes more than it collected. Nothing is available yet. | L1, L7, L9, L19 |
| C3 | Not-available notice (top of the page) | This service is not offered yet. This list records your interest so we can tell you when and where it becomes available; it is not an offer, there is no price and we accept no money. Availability is subject to regulatory approval. | L9, L23 |
| C4 | Trial line | The first step would be a trial on the exchange's testnet: test money, no real money, real market behaviour. Results on testnet are not market results. | L6, L7, L9 |
| C5 | Call to action (button) | Join the waiting list | L9 |
| C6 | After sign-up | Thank you — you are on the list. Nothing is available yet, and we will never ask you for money or for your exchange keys through this list. We will write only when a trial exists where you live. | L9, L13 |
| C7 | US exclusion (shown instead of the form for "United States") | Sorry — this list is not open to people living in the United States. | L5, L17 |
| C8 | Consent line (the form's tick box) | I want to join the waiting list. I agree that optionsBot keeps my e-mail, my country and my optional answers to tell me when and where a trial becomes available, and to count interest in it. I can leave or ask to delete my data at any time. I am 18 or older and I do not live in the United States. | L13, L15, L5 |
| C9 | Footer | Software, not investment advice. Options can lose more than the premium collected. No performance is promised or shown on this page. | L1, L7, L19, L20 |

## 2. Value points

PRD §5's three proof points, each with its risk at the same size.

| ID | Benefit | Risk beside it | Legal |
|---|---|---|---|
| V1 | **A reason for every trade.** Every order the software places is written down with the rule that triggered it and the market at that moment, in plain words. | A reason is not a guarantee: the rules are fixed in advance and can be wrong for the market that comes. | L1, L4, L11 |
| V2 | **The worst case before every risk.** Before it sells, you see what a large overnight move could cost, in money. | The worst case is a model estimate at today's prices: bigger moves happen and the real loss can be larger. It is not a limit. | L7, L19 |
| V3 | **It never touches your funds.** It would trade through a key you create on your own exchange account that can place orders but cannot withdraw; deposits and withdrawals stay with you on the exchange. | A trading key still trades: the software's orders can lose money on your account, and an exchange outage can leave positions open. You can revoke the key on the exchange at any time. | L21, L17, L18, L20 |

## 3. Open wording questions for counsel

- C1/C2: does "monthly income", even as "aiming for", read as a promise of returns (L7)? Fallback: drop "income" and describe the mechanics only.
- C2: does "software that would sell options from your own account" already describe portfolio management or advice (L1–L5)? Fallback: the licence model's wording (the customer runs the software).
- C4: may a testnet trial be mentioned to the public at all before L6 is answered? Fallback: remove C4 and describe the waiting list only.
- V2: is a model worst case on a marketing page a hypothetical-performance figure (L7)? It carries no number on this page.
