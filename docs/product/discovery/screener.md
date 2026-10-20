# Screener — cycle-holder survey (E5)

**Status:** ready to use (#178). Part of the E5 discovery kit, one written form: `consent.md` (the first screen) → `screener.md` (who counts) → `survey.md` (the questions) → `synthesis.md` (what we learned). No calls and no recordings.

**Where answers go:** every screener answer and any contact detail stays in `data/product/interviews/` (gitignored, never committed, never pasted into an issue or a pull request). This file is public: it holds no names, no contacts and no amounts.

## 1. Who we want

Enough responses for ~15 qualified people who hold BTC or ETH for the long term — the PRD's **cycle holder** (§3) — plus a few active option traders to compare against. We are learning what they *did*, so we want people who have a recent, real story about earning (or trying to earn) on their coins.

A **cycle holder** has held BTC or ETH through at least one full market cycle: they held through at least one bear market (for example 2022) without selling most of it, and they hold today.

## 2. Questions

The form's first questions, in this order. Every answer is a choice from the list (no amounts: we never ask how much someone holds).

| ID | Question | Options |
|---|---|---|
| S1 | Do you hold BTC or ETH today? | BTC · ETH · both · neither |
| S2 | When did you first buy the coin you hold longest? | before 2018 · 2018–2021 · 2022 or later |
| S3 | During the last big fall in price (for example 2022), what did you do with most of your coins? | kept them · sold most · bought more · did not hold then |
| S4 | In the last two years, which of these have you used on coins you hold? (all that apply) | exchange "earn" / savings · dual investment · covered calls or other options sold by hand · lending · staking · DeFi vaults · none |
| S5 | In the last two years, how often have you traded options yourself? | never · a few times · monthly · weekly or more |
| S6 | Have you looked for a way to get a regular monthly income from your coins without selling them? | yes, and tried something · yes, only looked · no |
| S7 | Do you work for an exchange, a crypto fund, a trading-software company, or as a journalist? | yes · no |
| S8 | Are you 18 or older? | yes · no |

## 3. Who qualifies

| Segment | Qualifies when |
|---|---|
| **Cycle holder** (primary) | S1 ≠ neither · S2 before 2022 · S3 = kept them or bought more · S5 = never or a few times · **and** at least one of: S4 lists earn, dual investment or covered calls; S6 = yes |
| **Active option trader** (comparison) | S1 ≠ neither · S5 = monthly or weekly or more |

## 4. Who does not qualify

- S1 = neither, or S2 = 2022 or later (no full cycle yet), or S3 = sold most.
- S7 = yes (conflict of interest or a story we cannot use).
- S8 = no.
- Anyone on the team, or a close friend or relative of someone on the team (don't send them the link).

Whoever does not qualify sees a thank-you and stops; their screener answers are not used in the synthesis.

## 5. Quotas

| Group | Quota |
|---|---|
| Qualified responses in total | ~15 |
| Cycle holders | **≥ 10** (the kill criterion in `synthesis.md` needs at least 10) |
| Active option traders | **≤ 5** |
| Within cycle holders: used earn, dual investment or covered calls (S4) | ≥ 3 |
| Within cycle holders: want income but never tried (S4 = none or staking only, S6 = yes) | ≥ 3 |
| Within cycle holders: not from a crypto-option or trading community | ≥ 3 (avoid hearing only the most technical voices) |

Keep the form open until the cycle-holder quotas are full; active option traders beyond 5 are reported but not counted. Share a separate link per channel (community, referral, waiting list), so the synthesis can tell a channel's bias apart from a real pattern.
