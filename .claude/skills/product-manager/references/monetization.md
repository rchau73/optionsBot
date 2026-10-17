# Monetization playbook

Read this before you propose or review any price, plan, tier, trial or fee.

## 1. Choose the model

| Model | Fits when | Watch out for |
|---|---|---|
| **Flat subscription** | one clear persona, simple value | leaves money on the table with heavy users; no expansion revenue |
| **Tiered (good-better-best)** | several segments with different needs and willingness to pay | too many tiers (> 4) confuse; tiers built from features instead of personas |
| **Per seat** | value grows with the number of users (B2B teams) | discourages adding users; weak for single-user prosumer products |
| **Usage-based** | value is proportional to consumption (API calls, transactions, orders) | unpredictable bills scare customers, so add commitments, caps and alerts |
| **Value-based / % of assets (AUM)** | value scales with the money managed (wealth, automation of capital) | regulated in many places (advisory/management fees); revenue falls with the market |
| **Performance fee** | aligned with customer outcome | often restricted by regulators for retail customers; creates an incentive to take risk; high-water mark needed. **Needs legal review** |
| **Transaction / take rate** | payments, marketplaces, brokerage | race to zero; a "free" offer paid by payment for order flow carries regulatory and trust risk |
| **Hybrid** (platform fee + usage) | predictable base plus upside | harder to explain, so the pricing page must be one screen |
| **Freemium** | low marginal cost, viral or network effects, large top of funnel | free users who never convert; the free tier must be useful but bounded |

## 2. The value metric

Questions to choose it:
1. Does it grow when the customer gets more value?
2. Can the customer predict their bill?
3. Is it easy to understand on the pricing page in under 5 seconds?
4. Is it hard to game (shared logins, splitting accounts)?
5. Can we measure it reliably and cheaply?

Examples for an automated-investing product: number of strategies running, capital under automation (bands, not %), number of exchange accounts connected, advanced analytics/backtests, alerts and reports.

## 3. Designing tiers

- Start from 2–4 **personas** (e.g. *Curious beginner*, *Active retail trader*, *Pro / small fund*, *B2B / white label*). Per persona: job, alternatives, willingness to pay, the features they need, the ones they don't care about.
- Map features into: **table stakes** (in every tier), **differentiators** (where tiers separate), **add-ons** (valuable to a minority only, sold separately).
- Use **fences** to separate tiers: usage limits, advanced features, number of accounts, history depth, support level, SLAs, compliance/reporting exports, API access.
- Anchor with the top tier and make the middle one the obvious choice (the "decoy" effect works, but every tier must be honestly worth its price).
- Annual plans: 15–20 % discount for paying upfront improves cash and retention. Show the monthly equivalent.
- Localise prices by market (PPP) where you sell internationally. Brazil vs US vs EU can differ 2–3×.

## 4. Free tier, free trial, reverse trial

| Option | Use when | Key decisions |
|---|---|---|
| **Free trial (opt-in, no card)** | value is visible within days; you want volume | length = time-to-value × 2 (often 7–14 days); conversion is lower but trust higher |
| **Free trial (card required)** | high-intent traffic, you need qualified leads | higher conversion, lower volume; reminder **before** the first charge is mandatory (and legally required in several jurisdictions) |
| **Reverse trial** | you have a freemium tier but want users to taste premium | full premium for 14 days, then drop to free with a clear view of what they lose |
| **Freemium** | low marginal cost and a big market | the free tier solves a real but limited job; the upgrade trigger is a moment of need |
| **Paper / testnet trial** (investing) | users fear losing money; value takes time to show | simulated results must be realistic (fees, slippage, fills). Never present paper results as a promise. Show the move to real money as a separate, conscious step with risk disclosure |

Measure: trial start rate, activation within the trial, trial-to-paid conversion, and 90-day retention of trial converts (good conversion with bad retention means the trial oversold).

## 5. Pricing research

- **Interviews**: ask what they pay today for the alternative (money, time, losses), what the problem costs them, and who decides the purchase.
- **Van Westendorp** (too cheap / cheap / expensive / too expensive) gives an acceptable range. Needs ~100+ answers per segment.
- **Gabor-Granger**: purchase likelihood at given prices, which gives a demand curve and the revenue-maximising price.
- **Conjoint / MaxDiff**: which features drive willingness to pay, which is how you package tiers.
- **Live tests**: A/B test prices only with enough traffic and a clean split (new visitors only, consistent per user, legal check on price discrimination). Otherwise run sequential cohort tests or test on the packaging rather than the price.
- **Competitor benchmarks**: dated and with sources, and treated as an input, not as the answer. Pricing at the competitor's level says "we are the same".

## 6. Unit economics checks (per tier and per channel)

```
ARPA                    = MRR / paying accounts
Gross margin            = (revenue − COGS: infra, data feeds, exchange/API costs, payment fees, support) / revenue   → SaaS target ≥ 70–80 %
CAC                     = sales + marketing spend / new paying customers (per channel)
CAC payback (months)    = CAC / (ARPA × gross margin)                → < 12 months prosumer/SMB, < 18–24 enterprise
LTV                     = ARPA × gross margin / monthly revenue churn
LTV : CAC               ≥ 3
NRR                     = (start MRR + expansion − contraction − churn) / start MRR   → > 100 % is the goal
GRR                     = (start MRR − contraction − churn) / start MRR               → > 85–90 %
Burn multiple           = net burn / net new ARR                     → < 2 is healthy, < 1 is great
Rule of 40              = growth % + profit margin %                 → ≥ 40 for scaled SaaS
```

Every pricing proposal includes a scenario table (pessimistic / base / optimistic) for conversion, tier mix and churn, with the resulting MRR at 6 and 12 months and the payback. Say which inputs are assumptions.

## 7. Changing prices

- Grandfather existing customers for a period (or for good) and tell them early and clearly why.
- Raise prices together with visible new value when you can.
- Test on new customers first, then measure churn and downgrades of the cohorts.
- Never change a price silently. In financial products in particular, trust is the asset you sell.

## 8. Discount governance

- Written rules: who may give which discount, for how long, and why. Track discounted MRR separately.
- Prefer extra value (longer trial, onboarding session, extra usage) over cutting the price.
- Promotions have an end date and are measured on the retention of the cohort they bring.

## 9. Fintech-specific notes

- Fees tied to assets or performance can make the product a regulated activity. **Needs legal review** before any such model is proposed to customers.
- Show the total cost: subscription + exchange fees + spreads + funding, so the customer can compare honestly.
- Payment methods by market (Pix in Brazil, SEPA in the EU, cards, crypto) affect conversion and involuntary churn.
- Taxes (VAT/ISS/sales tax) and invoicing requirements differ by country. Include them in the price you show where the law requires it.
