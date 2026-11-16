# E3 usability round 1: run sheet

One page to run the first round of the investor monitor test (#810) and turn its failures into issue drafts the same day.
The full kit is [e3-investor-monitor-test.md](e3-investor-monitor-test.md): this sheet follows it step by step and changes none of its rules.
Round 1 is the kit's **baseline** round: today's monitor on the demo without flags.

## 1. Who to invite

- **5 people**, each one session.
- First the cycle holders met in the E5 interviews (PRD, epic E5): people who hold BTC or ETH through the cycle.
- Each must say *yes* to "Do you currently hold bitcoin (BTC) or ether (ETH)?" and *no* to "Have you ever sold an option (a call or a put)?". Never ask how much.
- Not: anyone who works on this project, has seen this monitor, or trades options professionally.
- Give each person a code, **P1 to P5**, in the order they come. P5 takes the phone layout.
- The list of who is who stays with you, outside this repository, and is deleted after the round.
- Nothing is recorded (screen, audio or video): written notes only, unless the consent text has had legal review first.

## 2. Setup (15 minutes before the first session)

From the repository root, with Go, Node.js 24+ and a terminal:

```bash
scripts/usability_round.sh
```

1. Open <http://localhost:3000> and sign in as `moderator` with the password the script printed, before the participant sits down.
2. P1 to P4: a browser window of **1440 × 900**, zoom 100 %, scrolled to the top. P5: Chrome DevTools device toolbar, **iPhone 12 Pro (390 × 844)**.
3. The page shows the **BTC · investor summary** with *What it holds*, *Income per month*, *Worst case now* and *Last action* (on the phone, the phone overview). If it says "Summary unavailable", press Enter in the script's terminal.
4. Between participants press Enter in the script's terminal (the demo restarts) and reload the page.
5. Keep the kit's section 6 (answer key) open on your side, out of the participant's sight.

## 3. The script to read aloud

Read the text in quotes word for word. Do not add to it.

> "Thank you for helping. We are testing this page, not you: if something is hard to find, that tells us what to fix. There are no wrong answers. This page shows a program that trades automatically with made-up money. I will ask you four questions about it, one at a time. Please think aloud: say what you look at and what you think it means. You can scroll and click anything. I cannot help or answer questions during the tasks — I will answer them at the end. You have 5 minutes for all four questions. Say 'I don't know' whenever you want to move on."

Start the 5-minute timer when the participant first sees the page, then ask, in this order, each as soon as the previous one is answered or given up:

1. "What does the program hold right now?"
2. "Is it making money or losing money?"
3. "What is the worst that could happen right now?"
4. "What was the last thing it did, and why?"

## 4. No help during the 5 minutes

- If asked what you mean: "Whatever it means to you." If they stop talking: "What are you looking at now?" Nothing else.
- Do not point, scroll, click, nod, say whether an answer is right, rephrase a question or explain a word on the page (the kit's section 4 lists them).
- At 5:00 say "Thank you, time is up" and stop, even mid-question. An unanswered question is a **fail**.
- Then ask the kit's three after-task questions (section 5), answer the participant's questions and close the tab.

## 5. What to record per participant

Right after each session, from your notes and the answer key only, fill in one row of the kit's results table (section 8):

- the **code** (P1 to P5) and the layout: never a name, an e-mail, a phone number, an employer or anything the person says about their own coins or money;
- per question: success, partial or fail, and the time of the final answer (mm:ss from the start);
- the errors as question and type, e.g. `Q2 sign, Q3 unit` (sign, unit, coin, time);
- **Wording when wrong:** in lower case, what the person said when an answer was wrong, one part per question, e.g. `Q3: the margin one is the worst; Q2: it is losing because it is red`.
  No figures with a currency, no names: the scorer refuses the file and names the column and the row.

## 6. Where the results go

Copy the filled-in table, with no other notes, to the private, gitignored folder (everything under `data/` is ignored):

```bash
data/usability/e3/baseline/results.md
```

Then, still the same day:

```bash
python3 scripts/product/score_usability.py data/usability/e3/baseline/results.md --findings
```

It writes, beside the table, `summary.md` (pass or fail, with the synthesis to fill in), `findings.md` (one row per question fewer than 4 of 5 answered with a success: the scenario, how many failed, the screen, and the wording) and `issue-draft-Q<n>.md` per failing question.
Each draft has an `[E3]` title, the problem, the evidence as counts only, Gherkin acceptance criteria and "Depends on: none".
The script sends nothing anywhere: read each draft, then file it on the board yourself (the first line is the title).
The findings table holds participant codes and their wording: it stays in the private folder; only the drafts and the synthesis are shared.
