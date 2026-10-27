# Comprehension test: tips, videos and the monitor (E11)

How E11's outcome is tested with real people (#338): *a non-technical user can read the monitor and a manager can set up a strategy using only the tips and videos.* Run it **before** any education UI is built, on the monitor as it is today, to get a baseline, and again after each E11 release. Product content only: no code, no monitor change, no new analytics event.

## 1. Who takes part

- **5 people per round.** Two segments, mixed in a round: **cycle holder** (new to options; the investor persona) and **manager** (not technical; sets up strategies for others).
- **Recruitment notes** say only the segment, how the person was reached (a channel, not a name) and the date. No name, email, phone number or employer. A participant is `P1`…`P5` in this round and nowhere else.
- **Recording and consent:** screen or voice recording and the consent text are **needs legal review** (`docs/legal/`, LGPD). Until counsel approves, take notes by hand, no recording, and tell the person the monitor shows testnet (practice) data.
- **Setup:** the monitor on testnet with a bot that holds at least one strangle, has one closed month and one skipped entry. No live account, no real amounts on screen. The facilitator reads the prompt aloud, then stays silent; a hint is logged (see section 4).

## 2. Tasks

Each task: the **prompt** read aloud, the **expected answer** (taken from the `short` and `risk_income` of the glossary entries named, `frontend/content/glossary.json`; a correct answer says the same thing in other words), the **glossary keys**, and the **time limit**. Tasks T1-T4 are the four questions E3 uses; T5 and T6 are for managers (segment `manager` only; cycle holders do T1-T4).

### T1 · What does the bot hold?

- **Prompt:** "Look at the summary. What does this bot hold right now?"
- **Expected answer:** It holds open strangles: a sold call above the price and a sold put below it, with the same expiry. Each sold option is a position the account holds now.
- **Glossary keys:** `strangle`, `position`
- **Time limit:** 2 min

### T2 · Is it making or losing money?

- **Prompt:** "Is this bot making or losing money? Tell me what you see and what is not final."
- **Expected answer:** Profit or loss after fees, in dollars or in coin. Realized P&L is locked in by closed legs. Unrealized P&L is for open legs: it can still change a lot, so a profit on paper is not yet income.
- **Glossary keys:** `p&l`, `realized p&l`, `unrealized p&l`
- **Time limit:** 2 min

### T3 · What is the worst case?

- **Prompt:** "What is the worst that could happen to this account in one bad day? Is it a limit?"
- **Expected answer:** The worst-case figure is a model's estimate of a one-day loss. A short strangle loses in a big move either way, and a stop-loss caps the loss on most moves, but a sudden gap can still go past it. The estimate is not a limit.
- **Glossary keys:** `strangle`, `stop-loss`, `worst month`
- **Time limit:** 2 min

### T4 · Why did it do its last trade?

- **Prompt:** "Find the bot's last trade. Why did it do that?"
- **Expected answer:** Every trade carries its reason, with the numbers of the rule that fired it, for example take-profit or stop-loss. A close buys a leg back and turns open P&L into realized P&L. A skipped entry is also written down, with its reason.
- **Glossary keys:** `reason`, `close`, `skipped`
- **Time limit:** 2 min

### T5 · What does this setting do? (manager)

- **Prompt:** "The setting `stop_loss_multiplier` is 2. If it went up to 3, what would change for risk and for income?"
- **Expected answer:** Up means fewer stop-outs but bigger losses each; down means more stop-outs and smaller losses each. (For comparison with `repair_cooldown_hours`: up is a longer pause after a loss, less income meanwhile.) The participant names both risk and income.
- **Glossary keys:** `stop_loss_multiplier`, `repair_cooldown_hours`
- **Time limit:** 3 min

### T6 · What will this manual close do? (manager)

- **Prompt:** "Open the Close confirmation for a leg. Before you confirm, tell me what it will do."
- **Expected answer:** It buys that leg back at market now. It removes that leg's risk and income, and pays the asking price and a fee. The result so far becomes realized. The side is then held like after a stop-loss. If it is the strangle's last leg, the slot is empty and the bot opens a new entry.
- **Glossary keys:** `manual close`, `realized p&l`, `stop hold`
- **Time limit:** 3 min

## 3. Scoring

For each task, the facilitator scores the answer without help:

| Score | Meaning |
|---|---|
| **0** | wrong, or no answer in the time limit, or the participant states a return or a forecast as fact |
| **1** | partly right: the main point is there, one needed point is missing or wrong (for example, "it is making money" without saying part of it is not final) |
| **2** | right: all the points of the expected answer, in the participant's words |

Score on the answer's meaning, not its wording. Note the time to the answer, to the second.

## 4. Self-service

For every task, log how the answer was reached, in one of three codes:

- `tip` — found with the tips or a video alone (the E11 UI, once it exists; before that, the glossary text on a printout);
- `hint` — the facilitator had to point to where to look or to explain a word;
- `none` — not answered.

**Self-service understanding** (the PRD's metric, §7) = tasks answered `tip` ÷ tasks asked, over the round. The facilitator never counts a `hint` as `tip`.

## 5. Pass bar

**Assumption, to confirm after the first round:**

- at least **80 %** of a participant's tasks scored **2**, in at least **4 of the 5** participants; and
- a **median of 5 minutes or less** for T1-T4 together, over the participants.

The first round sets the baseline; if everyone passes or no one does, the bar is wrong, and the product owner changes it before the second round. A pass is not a claim about returns, only about whether the screen is understood.

## 6. Where results go

- **Each round's raw results** (scores, times, codes, notes) are stored **only** under `data/product/comprehension/<round>/` (private, gitignored). Never in the repository, never with a name.
- **Only the aggregate** goes to the E11 epic issue: number of participants per segment, share of tasks scored 2 per task, the median time for T1-T4, the self-service share, and the pass bar result. No quote that could identify a person.
- A task that most participants scored under 2 becomes a story on the board (a tip, a video or a wording to fix), not an edit made during the round.

## 7. Running a round (checklist)

1. Check the setup (section 1) and print the prompts of the segment.
2. Welcome the person; say that the monitor shows practice data and that the test checks the screen, not them.
3. Read each prompt aloud; start the clock; stay silent. Log hints.
4. Score at once (section 3); write the time and the code (section 4).
5. Thank them; delete any note that names them; store the sheet under `data/product/comprehension/<round>/`.
6. After five people: aggregate (section 6) and post it on the E11 epic.
