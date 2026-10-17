# Discovery and experiments

## Discovery methods

- **Customer interviews** (The Mom Test): ask about the last time they did the job, what they used, what it cost them, what they tried that failed. Never pitch, never ask "would you use…?". Run 5–8 per segment before deciding, keep notes in a shared place, and tag each insight with the segment and its date.
- **Jobs-to-be-done**: "When <situation>, I want to <motivation>, so I can <outcome>." Capture the functional, emotional (fear of losing money!) and social jobs.
- **Opportunity solution tree** (Teresa Torres): desired outcome → opportunities (customer needs, pains) → solutions → assumption tests. Keep it current, and compare at least three solutions per opportunity.
- **Assumption mapping**: list the assumptions behind a solution across value, usability, viability and feasibility. Plot them by importance × evidence and test the important ones with the least evidence first.
- **Cheap tests before building**: fake-door button, landing page with waiting list, concierge (do it by hand for 5 customers), Wizard of Oz, clickable prototype, pre-sales or letters of intent.
- **Quantitative**: funnel and cohort analysis, session recordings and heatmaps, support ticket tagging, search logs, surveys (keep them short and behaviour-based).
- **Continuous discovery**: at least one customer conversation per week for the product trio (PM, design, engineering).

## When to A/B test, and when not

A/B test when: there is enough traffic to reach the sample size in ≤ 2–4 weeks, the change is reversible and the metric responds within the test window.

Don't A/B test when: traffic is too low (use qualitative tests, sequential cohorts or a before/after with care), the change is a large strategic bet (test the assumption, not the variant), or the change has legal or safety effects (risk disclosures are not A/B tested *down*).

## Experiment rigor

1. **Hypothesis**: "We believe <change> for <segment> will <move metric> by <MDE> because <evidence>."
2. **Primary metric** (one), **secondary metrics**, **guardrails** (must not worsen: refunds, complaints, support contacts, user losses / risk metrics, unsubscribes).
3. **Sample size** from the baseline rate, the minimum detectable effect, α = 0.05 and power = 0.8. Example: baseline conversion 5 %, MDE 1 pp (relative 20 %) → about 8,000 users per arm. State the duration and run at least one full weekly cycle (crypto: weekends behave differently).
4. **Randomisation unit**: the user (not the session), so each user sees one variant consistently. Check the **sample ratio** (SRM, χ² test) before reading results.
5. **No peeking**: decide the duration in advance, or use a sequential method (e.g. always-valid p-values, Bayesian with a pre-registered decision rule).
6. **Read the result**: effect size with a confidence interval, not just "significant". Check the main segments (new vs returning, device, country) for consistency, but don't fish for a segment that happens to win.
7. **Novelty and primacy effects**: watch the trend over the test period. Prefer long-term metrics (retention at 30 days) for big UX changes, and use holdouts for features where the effect accumulates.
8. **Decide and document**: ship / iterate / kill, and record the result in the experiment log even when it failed. Failed tests are learning, and a log keeps the team from repeating them.

## Prioritisation

- **RICE** = Reach × Impact × Confidence / Effort. **ICE** = Impact × Confidence × Ease for quick experiments.
- Mark the confidence honestly: 100 % = data, 80 % = strong qualitative evidence, 50 % = intuition.
- Balance the roadmap: core improvements (retention, quality), growth bets (acquisition, monetization) and a few bold bets with explicit learning goals.
- Use a **Now / Next / Later** roadmap (outcomes, not dates) for the long term, and commit dates only for the "Now".
