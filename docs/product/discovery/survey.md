# Survey — cycle holders (E5)

**Status:** ready to use (#178). A short written form of about 6 minutes, filled in by the participant alone. There are no calls and no recordings (the product owner on #178: anything more is off-putting). We ask what people **did**, the last time, never what they *would* do or *think* of an idea: past behaviour predicts, opinions about the future flatter. The only exceptions are the standard NPS score (N1), which rates a product they have already used, and the fake door (F1), which asks for an action now, not an opinion.

**Where answers go:** the form's answers, the contacts left at F1 and every export stay only in `data/product/interviews/` (gitignored, never committed, never pasted into an issue, a pull request or a shared drive without access control). The folder keeps the name the ticket gave it. This file is public: no names, no amounts.

## 1. Rules for the form

- **One form, one link.** The data-use note (`consent.md` §1) is the first screen, then the screener (`screener.md`), then this survey. Whoever the screener does not qualify sees a thank-you and stops.
- **Short.** At most 20 items, at most 2 free-text answers and both optional. Every other answer is a choice from a list.
  The price block (P1–P4, §3b) is one extra screen of four numbers, outside that count.
- **No leading wording.** Questions never suggest an answer or name our product before the fake door. Every list has an "other" or "none" option.
- **No amounts about money they hold.** No question asks how much they hold, earned or paid; the only amounts asked are the four monthly prices of a hypothetical software subscription (§3b), and they stay in the private tally. If a free-text answer gives an amount, leave it out of anything shared.
- **No contact unless they join.** The form asks for an e-mail only at F1, and only if they choose to join the waiting list.

## 2. Form outline

| Part | Minutes | Items |
|---|---|---|
| A. Data-use note and screener | 2 | `consent.md` §1, S1–S8 |
| B. The last time they tried to earn on holdings | 2 | Q1–Q5 |
| C. Delegation, cost, price and score | 2 | Q6–Q8, P1–P4, N1 |
| D. Fake door | 1 | F1–F2 |

Total: about 7 minutes. If the form must be cut, cut Q5 and Q8, never Q6, N1 or the fake door.

## 3. Questions

Every question asks about something that already happened (the price block, §3b, is the one exception). Q4 is the only free-text question before the fake door.

| ID | Part | Question | Answer |
|---|---|---|---|
| Q1 | B | The last time you tried to earn something on coins you hold, what did you use? | exchange "earn" / savings · dual investment · options sold by hand · lending · staking · DeFi vault · other · never tried (skip to Q6) |
| Q2 | B | When did you last use it? | in the last month · 1–6 months ago · 6–24 months ago · longer ago |
| Q3 | B | Have you stopped using it? | no, still using it · yes, stopped · yes, and moved to something else |
| Q4 | B | What happened that made you stop, or nearly stop? | free text, optional |
| Q5 | B | How did you check whether it was worth it? | the yield the product showed · compared with just holding · my own spreadsheet · did not check · other |
| Q6 | C | Have you ever given an app, a bot, an exchange product or another person permission to trade on one of your accounts? | yes, and still do · yes, and took it back · no |
| Q7 | C | Have you ever seen a tool or a person do something on your account that you did not expect? | yes · no · never gave anyone access |
| Q8 | C | In the last year, what have you paid for to manage or earn on your coins? (all that apply) | fees or a share of the yield · a tool subscription · a course or group · signals · nothing |

### 3b. Price questions (P1–P4, after Q8)

Four numbers in the survey's local currency, shown together after Q8 so Q1–Q8 keep their wording and order.
They are the Van Westendorp questions about a **software subscription per month** (a tool, never a fee on capital).
Show this line with every question: *This is research. It is not an offer or a price.*
No question promises a return or names a profit.

| ID | Question | Answer |
|---|---|---|
| P1 | At what monthly price would a software subscription for managing options on your own coins be so cheap that you would doubt its quality? | a number per month |
| P2 | At what monthly price would it be a bargain, good value for the money? | a number per month |
| P3 | At what monthly price would it start to feel expensive, but you would still consider it? | a number per month |
| P4 | At what monthly price would it be too expensive to consider? | a number per month |

The answers must rise in this order: P1 < P2 < P3 < P4.
`survey_tally.py` leaves out a row that breaks it (counted as `inconsistent`) and reads the rest for cycle holders only.
The amounts and the range stay in `data/product/interviews/tally.json`; nothing here, in an issue or in a comment may state one.
Before the form is sent, counsel reads this wording (L5, L7; `docs/product/legal-brief.md`); sending stays a person's action (#178).

## 4. Score (N1)

The standard Net Promoter Score, on the product named at Q1; skipped when Q1 = never tried.

> N1. How likely are you to recommend the product you used most recently (your answer to Q1) to a friend? 0 = not at all likely, 10 = extremely likely.

It tells us how satisfied cycle holders are with what they use today, which is what a new product has to beat.

## 5. Fake door (part D, always last)

Show this text as it is, with nothing added: no benefits, numbers or returns.

> We are exploring a service that would sell options on your coins to aim for monthly income. It would place orders on your own exchange account through an API key that can trade but cannot withdraw, so your coins never leave your exchange. It would show the reason for every trade and the worst case before taking a risk, and it would start with a free trial on the exchange's test environment, with no real money. It can lose money. Nothing exists yet.

- **F1.** There is a waiting list for the trial. Do you want to join it now? — *yes, add me (leave an e-mail)* · *no* · *not now*
- **F2.** What made you decide that? — free text, optional

Each response has exactly one outcome:

- **Joined** — chose "yes, add me" and left an e-mail (separate consent, see `consent.md`).
- **Declined** — chose "no".
- **Undecided** — chose "not now", or "yes" with no e-mail. Counts as not joined.

F2 is the strongest quote of the response, whichever way it went.

## 6. After the survey closes

1. Export the answers to `data/product/interviews/`, give each response an anonymous ID (`P01`, `P02`…) and delete the answers from the form tool (`consent.md` §2).
2. Tally the export with `python3 scripts/product/survey_tally.py data/product/interviews/<export>.csv` (counts, NPS, fake door, delegation share, price-range status; `scripts/product/README.md`), then fill `synthesis.md` (a copy in `data/product/interviews/`).
3. Copy the F1 e-mails to the waiting list and delete them from the export.
