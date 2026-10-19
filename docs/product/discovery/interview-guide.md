# Interview guide — cycle holders (E5)

**Status:** ready to use (#178). 45 minutes, one interviewer and, if possible, one note-taker. Jobs-to-be-done style: we ask what people **did**, the last time, in detail — never what they *would* do or *think* of an idea. Past behaviour predicts; opinions about the future flatter.

**Where notes go:** recordings, notes, names and contacts stay only in `data/product/interviews/` (gitignored, never committed, never in an issue or a pull request). Fill the per-interview sheet of `synthesis.md` there within 24 hours of the call. This file is public: no names, no amounts.

## 1. Rules for the interviewer

- **Ask about the last time.** "Tell me about the last time you…", then "what happened next?". If they generalise ("I usually…"), bring them back to one real occasion.
- **No leading questions.** Never suggest the answer ("was it too complicated?") or give options they did not mention. Silence is fine; let them fill it.
- **No pitching.** Don't mention our product, the bot, strategies, returns or features before part G. If they ask what we're building, say "I'll tell you at the end — first I'd like to hear about you."
- **No advice.** If they ask what they should do with their coins, say we can't advise and move on.
- **No amounts.** Don't ask how much they hold or earned. If they say an amount, note "amount mentioned" instead of the number.
- **Write down quotes word for word.** A quote with its interview ID (`P01`) is the evidence the synthesis needs.

## 2. Timing

| Part | Minutes | Goal |
|---|---|---|
| A. Welcome and consent | 3 | read `consent.md`, get the "yes", start recording |
| B. Their story as a holder | 5 | how they hold and what they did in the last cycle |
| C. The last time they tried to earn on holdings | 10 | one real occasion, from trigger to today |
| D. What they used and what went wrong | 8 | tools, the worst moment, why they stopped |
| E. What they have let software or others do on their account | 7 | past delegation: the evidence for the kill criterion |
| F. What they pay today | 5 | fees, subscriptions, tools, courses |
| G. Fake door | 4 | a neutral description and an offer to join a waiting list |
| H. Close | 3 | thanks, referrals, stop recording |

Total: 45 minutes. If time runs short, shorten B and F, never C, E or G.

## 3. Questions

Every question asks about something that already happened. The probes after each answer are always the same: "What happened next?", "Why then?", "How did that feel?", "What did you do about it?".

| ID | Part | Question |
|---|---|---|
| Q1 | B | Tell me about how you came to hold BTC or ETH — when did you first buy, and what have you done with it since? |
| Q2 | B | Walk me through the last big fall in price: what did you do with your coins, week by week? |
| Q3 | C | Tell me about the last time you tried to earn something on coins you hold. When was it, and what made you try then? |
| Q4 | C | What did you use — which exchange, product, app or method — and how did you choose it? |
| Q5 | C | Walk me through how it went, from the day you started to the day you stopped, or to today. |
| Q6 | D | What was the worst moment? What happened, and what did you do? |
| Q7 | D | If you stopped: what was the last thing that happened before you stopped? |
| Q8 | D | How did you check whether it was worth it — what did you look at, and how often did you look? |
| Q9 | E | Have you ever given an app, a bot, an exchange product or another person permission to trade on one of your accounts? Tell me about that time. |
| Q10 | E | What did you allow it to do, and what did you keep for yourself? Have you ever taken that permission back — what happened? |
| Q11 | E | Tell me about a time a tool or another person did something on your account you did not expect. What did you do? |
| Q12 | F | What did you pay for the product you told me about — fees, a subscription, a share of the yield? How did you find out what it cost? |
| Q13 | F | In the last year, what else have you paid for to manage your coins — tools, courses, groups, signals? |
| Q14 | H | Who have you talked to about earning on your coins, and whose advice did you follow? |
| Q15 | H | Is there anything about the last time you tried this that I did not ask about and should have? |

## 4. Fake door (part G, always last before the close)

Read this aloud word for word, then stop talking. Don't add benefits, numbers or answers about returns.

> We are exploring a service that would sell options on your coins to aim for monthly income. It would place orders on your own exchange account through an API key that can trade but cannot withdraw, so your coins never leave your exchange. It would show the reason for every trade and the worst case before taking a risk, and it would start with a free trial on the exchange's test environment, with no real money. It can lose money. Nothing exists yet. There is a waiting list for the trial: do you want to join it now?

Record exactly one outcome in the per-interview sheet:

- **Joined** — they gave a contact for the waiting list now (separate consent, see `consent.md`).
- **Declined** — they said no.
- **Undecided** — "maybe", "send me more", "let me think". Counts as not joined.

Then ask only: "What made you decide that?" and write down the answer word for word. It is the strongest quote of the interview, whichever way it went.

## 5. After the call

1. Stop the recording; store it and the notes in `data/product/interviews/` under the interview ID.
2. Fill the per-interview sheet in `synthesis.md` (a copy in `data/product/interviews/`) within 24 hours.
3. Delete the recording within 90 days, once the notes are written (`consent.md`).
