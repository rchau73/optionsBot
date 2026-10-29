# E3 usability test: can a new investor read the monitor?

A ready-to-run test of E3's outcome (PRD, epic E3): *a person new to options can answer, within 5 minutes of opening the monitor and without help, what the bot holds, whether it is making or losing money, what the worst case is right now, and why it did its last trade — measured with 5 user tests.*

The kit is meant to be followed by a moderator who has never seen the bot. It uses the demo data only: no real account, no real amounts. `tests/usability_kit_test.go` starts the demo with the flags below and checks every expected answer in the answer key against `/api/status`, `/api/positions`, `/api/events` and `/api/summary`, so the key stays true when the demo changes.

**What a person does, not the agent:** recruits the 5 participants, moderates the sessions and approves the consent wording. If anything is recorded (screen, audio or video), the consent text **needs legal review first** (data protection, see `docs/legal/`). Without that review: no recording, written notes only.

## 1. When to run it

| Round | What is tested | When |
|---|---|---|
| Baseline | today's monitor (the main branch as it is) | now, before any E3 UI story |
| Prototype | the E3 investor-view prototype (ux-designer skill) | after the prototype is approved, before the UI stories go to Ready |
| Release | the shipped investor view | after the E3 UI stories are deployed |

Use the same screener, script, tasks, answer key and scoring in all three rounds, so the results compare. Use 5 new participants each round (someone who has seen the monitor once is no longer new). If the demo changes between rounds, update the answer key in the same pull request (the test fails until you do).

## 2. Screener: who takes part

Recruit **5** people. Each must answer *yes* to both:

1. "Do you currently hold bitcoin (BTC) or ether (ETH)?" — yes / no. **Never ask how much.**
2. "Have you ever sold an option (a call or a put)?" — must be **no**.

Also exclude anyone who works on this project, has seen this monitor, or trades options professionally. Buying options in the past is fine; selling them is not.

Give each participant a code, **P1 to P5**. Use only the code in every note. Names and contact details stay with the recruiter, outside this repository, and are deleted after the round.

## 3. Setup (moderator, 15 minutes before the first session)

You need Go, Node.js 24+ and a terminal, from the repository root.

```bash
# Terminal 1 — the demo bot API: simulated BTC strategy, no exchange connection
go run ./cmd/monitor-demo

# Terminal 2 — the monitor, showing only the demo BTC bot
cd frontend
npm ci
# A fresh, throw-away user database for this round: the monitor creates the
# first admin only when its user database is empty, so a reused one would
# reject the new password. Nothing is written to .env.local (yours is kept;
# these variables take precedence over it).
export AUTH_DB_PATH="$(mktemp -d)/auth.db"
export AUTH_SECRET="$(openssl rand -base64 48)"
export ADMIN_USERNAME=moderator
export ADMIN_PASSWORD="$(openssl rand -base64 18)"
echo "$ADMIN_PASSWORD"                # the password to sign in with
BOT_APIS=btc=http://127.0.0.1:8081 npm run dev
```

1. Open <http://localhost:3000> and sign in as `moderator` with the password printed above. Sign in **before** the participant sits down: signing in is not part of the test. Run terminal 2's commands again, in a new terminal, at the start of every round (and whenever terminal 2 was closed): each run gets its own empty user database, so the printed password always works.
2. **Desktop run:** a browser window of **1440 × 900** (Chrome: DevTools → device toolbar → Responsive → 1440 × 900, then close DevTools). Zoom 100 %. Scroll to the top.
3. **Phone run (E12):** one of the 5 sessions — P5 — uses a phone layout: Chrome DevTools device toolbar, **iPhone 12 Pro (390 × 844)**, or a real phone on the same network. Write "mobile" as its layout in the results table.
4. Check the page shows the **BTC · investor summary** block with four parts: *What it holds*, *Income per month*, *Worst case now*, *Last action*. If it says "Summary unavailable", restart terminal 1. At phone width (under 640 px) the page is the phone overview instead (#320): the month's result as the headline and four cards (*What it holds*, *Making or losing*, *Worst case right now*, *Last action*); the full monitor is under the menu's *Details*.
5. Restart `go run ./cmd/monitor-demo` (terminal 1) between participants, so each one starts from the same demo state. Reload the page after the restart.

The demo moves on its own: spot follows a random walk, marks follow spot, and now and then a leg is bought back and sold again. That is expected; the answer key says which answers stay fixed and which you read off the screen at the moment the participant answers.

The answer key is for the demo **without flags**. To test how losses read, `go run ./cmd/monitor-demo -scenario mixed` (a losing month among winning ones) or `-scenario drawdown` (an open loss near its stop, a worst case large next to the month's income) serve the same API with those figures (#203; `frontend/README.md` "Without a bot (demo data)"); the header then shows DEMO · scenario <name>. The answer key's fixed answers do not hold for them.

## 4. Moderator script

Read the text in quotes word for word. Do not add to it.

**Before the tasks (2 minutes, timer not running):**

> "Thank you for helping. We are testing this page, not you: if something is hard to find, that tells us what to fix. There are no wrong answers. This page shows a program that trades automatically with made-up money. I will ask you four questions about it, one at a time. Please think aloud: say what you look at and what you think it means. You can scroll and click anything. I cannot help or answer questions during the tasks — I will answer them at the end. You have 5 minutes for all four questions. Say 'I don't know' whenever you want to move on."

**Start the 5-minute timer** when the participant first sees the page. Ask the four questions **in this order**, each as soon as the previous one is answered or given up. Write down the time (mm:ss since the start) of each final answer.

| # | Ask exactly | What it measures |
|---|---|---|
| Q1 | "What does the program hold right now?" | what the bot holds |
| Q2 | "Is it making money or losing money?" | making or losing money |
| Q3 | "What is the worst that could happen right now?" | the worst case now |
| Q4 | "What was the last thing it did, and why?" | why it did its last trade |

If the participant asks what you mean: "Whatever it means to you." If they stop talking: "What are you looking at now?" Nothing else.

At 5:00 say "Thank you, time is up" and stop, even mid-question. Unanswered questions are a **fail**.

**The moderator must not:**

- say or point at any word on the page: *strangle, call, put, option, premium, realised, unrealised, worst case, model, last action, income, summary, short, sell, buy back*;
- point, scroll, click or look at a part of the screen to hint;
- say whether an answer is right, nod, or say "good", "exactly", "almost", "are you sure?";
- rephrase a question, explain a term, or ask a follow-up during the 5 minutes;
- mention the bot's name, the strategy, BTC or money amounts.

## 5. After the tasks (5 minutes, open-ended)

Ask these three, in this order. They ask about what happened, never "would you…?".

1. "Which number on the page did you trust least just now, and what made you doubt it?"
2. "Where on the page did you get stuck or confused? Walk me through what happened."
3. "Which words on the page did you skip or guess at?"

Then answer the participant's questions, thank them and close the browser tab.

## 6. Answer key (demo data)

`go run ./cmd/monitor-demo` with no flags: BTC, simulated, testnet (demo). **Fixed** answers are the same in every session. **Read it off the screen** means: write down the value the page shows at the moment the participant answers, and score against that.

### Q1 — what does it hold

Fixed facts (*What it holds*, and the **Open positions** table):

- 3 strangles = 6 options: **3 calls and 3 puts, all sold** (side SELL — the program owes them, it does not own them).
- Each option is **0.3 BTC**; together **1.80 BTC sold** (the USD value moves with the price).
- Three expiries, about **25, 45 and 60 days** away; the nearest is in 24–25 days.
- Strikes (above / below the price when it started at 100,000): 110,000 / 90,000 (≈ 25 days), 114,000 / 87,000 (≈ 45 days), 116,000 / 84,000 (≈ 60 days).

| Score | The answer… |
|---|---|
| Success | says it has **sold** (or "is short", "owes", "wrote") options on BTC, and gives either the count (3 pairs, 6 options, 3 calls + 3 puts) or the size (1.8 BTC, 0.3 each) |
| Partial | gets the count or size right but not that they are sold (e.g. "it owns 1.8 BTC", "it bought 6 options"), or says "options" with no count or size |
| Fail | says it holds coins, cash or nothing; or no answer within the time |

### Q2 — making or losing money

Fixed facts (*Income per month (realised, net of fees)*):

- Six months of closes. Each **full past month is positive** (BTC and USD, green, with a + sign): the program has been **making money**.
- *Open positions now (not realised)* is a separate line; its sign moves with the price. **Read it off the screen.**

| Score | The answer… |
|---|---|
| Success | says it is **making money**, based on the monthly income (or the KPI strip's realised / total P&L); naming the open-position line as "not yet final" is fine |
| Partial | says "making money" without being able to point to a figure, or reads the right figure with the wrong unit (BTC as dollars, or the reverse) |
| Fail | says it is losing money (e.g. from a negative open-position line alone, or a red stop-loss close), or cannot say |

### Q3 — the worst case right now

Fixed facts (*Worst case now (one day)*):

- Two lines: "If BTC falls …%" and "If BTC rises …%", each a **loss** (− sign) in BTC, USD and % of equity. The move is about 7–9 % (3 daily σ at the demo's DVOL, which starts at 52 and drifts). **Read the amounts off the screen**; note the larger loss.
- The block says **"This is a model, not a quote"**.

| Score | The answer… |
|---|---|
| Success | gives a loss amount for a big one-day move (either direction) in BTC, USD or % of equity, as a loss, and shows it is an estimate (e.g. "about", "if the price moves", "the model says") |
| Partial | gives the right figure but as a gain, or in the wrong unit, or as a certain outcome ("it will lose…"); or says only "it loses if the price moves a lot" with no figure |
| Fail | names another number (the stop-loss mark, margin, equity, a past loss) as the worst case, says "it can lose everything" or "nothing", or cannot say |

### Q4 — the last thing it did, and why

The *Last action* block shows a plain sentence, a time, "sold" or "bought back", a size and an instrument. **Read the sentence and the time off the screen when the participant answers** (it changes every half-minute or so). In the demo it is one of:

| Action | Plain sentence on screen (call or put) |
|---|---|
| Sold (most of the time) | "Sold the call to open a new strangle and collect its premium (entry)" |
| Bought back | "Bought back the put: most of its premium was already earned (take-profit). The put is sold again to complete the strangle" |
| Bought back | "Bought back the call: little premium was left to earn; it is sold again nearer the price (delta drift)" |
| Bought back | "Bought back the put: it lost twice its premium (stop-loss). It was bought back to limit the loss; the put will not be sold again on that side until its cooldown ends" |
| Bought back | "Bought back the call: only few days to expiry left, so the strangle closes before expiry (time roll). The slot then opens a new strangle on a later expiry" |

| Score | The answer… |
|---|---|
| Success | says **what** it did (sold / bought back a call or put) **and why**, in their own words matching the sentence shown (e.g. "it sold a call to earn the fee"; "it bought back a put because it had already made most of the money") |
| Partial | says what but not why, or why but not what |
| Fail | describes a different event (an order sent, a skipped entry, a trend change in the Activity feed), the wrong side (sold vs bought back), or cannot say |

### Errors to count (any question)

Count each, even when the question is still scored a success:

- **Sign:** a gain read as a loss, or the reverse.
- **Unit:** BTC read as USD, a % as an amount, or the reverse.
- **Coin:** a figure of another coin read as BTC (e.g. the ETH or USDC line in *Account & collateral*).
- **Time:** a monthly figure read as total, or "since the journal began" as this month.

## 7. Scoring and pass mark

Per participant and question: **success / partial / fail**, the time of the final answer (mm:ss from the start), and the errors.

- A participant **passes** when all four questions are a **success** and the fourth answer came before 5:00.
- **The round passes when at least 4 of the 5 participants pass.**

Partial counts as not passed. Score right after each session, from your notes and this key only.

## 8. Templates

Copy these into the private notes folder `data/usability/e3/<round>/` (for example `data/usability/e3/baseline/`). It is gitignored: nothing about participants goes into the public repository. Use participant codes only; never write a name, e-mail, phone number, employer, location, or anything the participant says about their own holdings or money. Quotes are kept only after removing anything that identifies the person.

<!-- templates: begin -->

### Results table

Round: baseline / prototype / release · Date (YYYY-MM-DD) · Monitor version (commit)

| Code | Layout | Q1 | Q1 time | Q2 | Q2 time | Q3 | Q3 time | Q4 | Q4 time | Errors (sign / unit / coin / time) | Passed |
|---|---|---|---|---|---|---|---|---|---|---|---|
| P1 | desktop | | | | | | | | | | |
| P2 | desktop | | | | | | | | | | |
| P3 | desktop | | | | | | | | | | |
| P4 | desktop | | | | | | | | | | |
| P5 | mobile | | | | | | | | | | |

Participants passed: _ of 5 · Round passed (≥ 4 of 5): yes / no

### Synthesis

- **Result:** _ of 5 passed; success per question: Q1 _/5, Q2 _/5, Q3 _/5, Q4 _/5; median time to the fourth answer: _.
- **Top confusions** (what, where on the page, how many participants):
  1.
  2.
  3.
- **Jargon that blocked people** (the word, the question it blocked, how many participants):
  -
- **Errors seen** (sign / unit / coin / time, and where):
  -
- **Trust** (the number trusted least and why, from the first after-task question):
  -
- **Quotes** (anonymised, by participant code):
  - P_: "…"
- **What to change before the next round** (one line each, the page area it concerns):
  -

<!-- templates: end -->

Only the synthesis, with codes and anonymised quotes, may be shared outside the private folder — for example in the E3 epic's comments.
