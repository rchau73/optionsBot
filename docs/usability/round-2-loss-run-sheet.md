# E3 usability round 2 (loss round): run sheet

One page to run the loss round of the investor monitor test (#881) and turn its failures into issue drafts the same day.
The full kit is [e3-investor-monitor-test.md](e3-investor-monitor-test.md): this sheet follows it step by step and changes none of its rules.
It is modelled on the baseline round's [run sheet](round-1-run-sheet.md).
The loss round tests how a loss reads: today's monitor on a losing demo scenario.

## 1. Who sees which scenario

Each participant sees **one** scenario only (kit section 1):

| Participant | Scenario | Command |
|---|---|---|
| P1 | `mixed` | `scripts/usability_round.sh mixed` |
| P2 | `drawdown` | type `drawdown` in the script's terminal |
| P3 | `mixed` | type `mixed` in the script's terminal |
| P4 | `drawdown` | type `drawdown` in the script's terminal |
| P5 | `mixed` | type `mixed` in the script's terminal |

- **5 new people**, never someone from the baseline round: whoever has seen the monitor once is no longer new.
- Same screener as the baseline round (kit section 2): they must say *yes* to "Do you currently hold bitcoin (BTC) or ether (ETH)?" and *no* to "Have you ever sold an option (a call or a put)?".
  Never ask how much.
  Not: anyone who works on this project, has seen this monitor, or trades options professionally.
- Codes **P1 to P5**, in the order they come. P5 takes the phone layout.
- The list of who is who stays with you, outside this repository, and is deleted after the round.
- Nothing is recorded (screen, audio or video): written notes only, unless the consent text has had legal review first.

## 2. Setup (15 minutes before the first session)

From the repository root, with Go, Node.js 24+ and a terminal:

```bash
scripts/usability_round.sh mixed
```

1. Open <http://localhost:3000> and sign in as `moderator` with the password the script printed, **before** the participant sits down.
2. P1 to P4: a browser window of **1440 × 900**, zoom 100 %, scrolled to the top. P5: Chrome DevTools device toolbar, **iPhone 12 Pro (390 × 844)**.
3. Between participants, type the next participant's scenario from the table in the script's terminal and press Enter (the demo restarts on it), then reload the page.
4. Keep the kit's [section 6b, the losing scenarios' answer key](e3-investor-monitor-test.md#6b-answer-key-losing-scenarios) open on your side, out of the participant's sight.

## 3. Pre-flight checklist (before every participant)

- [ ] The header shows **DEMO · scenario mixed** (or **drawdown**), the one the table gives this participant.
- [ ] The four sections are present: *What it holds*, *Income per month*, *Worst case now* and *Last action* (on the phone, the phone overview).
- [ ] You are signed in as `moderator` and the page is reloaded.
- [ ] The script printed a pre-flight line and no line saying the demo did not answer. If it did, press Ctrl-C and start again.
- [ ] The timer is at zero and the answer key is out of sight.

## 4. The script and the timer

Read the moderator script and run the 5-minute timer exactly as written in the kit's [section 4](e3-investor-monitor-test.md#4-moderator-script).
It is not copied here, so it cannot drift from the kit.
Then ask the kit's three after-task questions (section 5), answer the participant's questions and close the tab.
In a loss scenario a **sign** error (a loss read as a gain) is the main thing to watch.

## 5. What to record per participant

Right after each session, from your notes and the answer key only, fill in one row of the kit's results table (section 8):

- the **code** (P1 to P5), the layout and the scenario: never a name, an e-mail, a phone number, an employer or anything the person says about their own money;
- per question: success, partial or fail, and the time of the final answer (mm:ss);
- the errors as question and type, e.g. `Q2 sign` (sign, unit, coin, time);
- the wording when wrong, in lower case, as in the baseline round.

## 6. Scoring and where the findings go

Copy the filled-in table, with no other notes, to the private, gitignored folder `data/usability/e3/loss/results.md`.
Score it with the key of [section 6b](e3-investor-monitor-test.md#6b-answer-key-losing-scenarios) and the rules of section 7, the same day:

```bash
python3 scripts/product/score_usability.py data/usability/e3/loss/results.md --findings
```

It writes `summary.md`, `findings.md` and one `issue-draft-Q<n>.md` per failing question beside the table.
The findings go to **Triage issue drafts**: read each draft and file it on the board yourself.
Nothing goes straight to GitHub, and the findings table (participant codes and wording) stays in the private folder.
