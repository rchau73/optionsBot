# Growth, activation, education and retention

## Activation

- Define the **aha moment** from data: the early action that best separates users who are retained at 30/90 days from those who churn (e.g. "connected an account and saw the first simulated result within 24 h"). Re-check it every quarter.
- Map the steps from sign-up to aha, and measure the drop-off and time at each step. Remove, defer or automate every step that is not needed for the aha.
- Use templates and smart defaults so a new user gets a working setup in minutes, then teach them to personalise it.
- Track **time-to-value** (median and p90) as a product KPI.

## Trial design (investing / trading products)

- A **paper or testnet trial** removes the fear of losing money. Make it realistic: real market data, fees, spreads and fills, and show losses as clearly as gains.
- Show in the trial what the paid product would have done, with the same honesty as live. Never cherry-pick the period or the strategy.
- The step from simulation to real money is a separate, conscious action: a checklist of risks in plain words, a suggested small starting size, and the possibility to go back.
- Lifecycle emails/notifications during the trial: day 0 welcome and first step, a nudge on the day the aha is missed, a mid-trial value summary, and a clear reminder before the trial ends with what happens next.

## In-product education

Novice investors stay when they understand what the product does for them and what the risks are.

- **Contextual tips**: one sentence, at the point of need, dismissible, never blocking. Explain jargon (delta, strike, margin, liquidation, drawdown) next to the number, with an example in currency.
- **Short how-to videos**: one task per video, 30–90 s, captioned (many watch muted), in the user's language, linked from the screen where the task happens. Show the real product UI, and update the video when the UI changes (track it with the release that changes the screen).
- **Risk education**: what can go wrong, how big it can get, what the product does about it (stops, limits) and what it does *not* protect against. Make this part of onboarding, not a footer.
- **Learning path**: a short checklist ("Set up → Run on paper → Understand your first report → Go live small") with progress shown. Completion is an activation input metric.
- **Measure the content**: views, completion rate, tip dismissal rate, and the effect on activation and on support contacts. Remove content that doesn't help.

### Spec of a tip or video

```
Title:            <task in the user's words: "How to read your weekly report">
Screen / trigger: <where and when it appears>
Audience:         <persona, experience level>
Learning goal:    <what the user can do afterwards>
Script (video):   <hook 5 s · steps · the one risk to remember · next step>
Length:           <30–90 s>   Languages: <pt-BR, en, …>
Success metric:   <completion %, task success after viewing, fewer tickets on the topic>
Owner / review:   <PM, design, compliance review of any claim>
```

## Engagement

- Build habit loops around real value (a weekly report that shows what the product did, an alert when something needs the user), not around streaks or confetti that push users to trade more.
- Notifications: each one has a job, a frequency cap and an opt-out. Measure the opt-out rate as a guardrail.
- Community and referral: referrals work when the user is proud of the result. Ask after a moment of value, never after a loss, and follow the marketing rules for financial products.

## Retention and churn

- **Dashboards**: logo and revenue churn monthly, cohort curves by sign-up month, plan and channel, NRR/GRR, and expansion and contraction.
- **Health score** per account: usage of the key features, logins, value delivered, support issues, payment status. Low scores trigger an action (in-app help, an email from a person, a call for high-value accounts).
- **Exit survey** (one question with categories + free text): too expensive · not enough value · missing feature · too complex · bad results / losses · switched to a competitor · temporary need · other. Code the free text weekly.
- **Involuntary churn**: retries spread over days, card updater services, reminders before the card expires, and alternative payment methods (Pix, bank transfer).
- **Save flow**: offer a pause, a downgrade or help, once, and never hide the cancel button.
- **Win-back**: contact churned users when the reason they gave has been fixed, with the evidence.
- **Performance-driven churn** in investing products: when markets hurt results, communicate early and honestly (what happened, what the product did, what it did not), before users find out alone. Silence during a drawdown drives churn.

## Churn review (weekly)

1. Numbers: churn this week vs the trend, by plan, cohort and reason.
2. The top 3 reasons, with quotes.
3. Accounts at risk (health score) and what is being done, by whom.
4. Actions from last week: done? effect?
5. New roadmap items or experiments, with an owner.
