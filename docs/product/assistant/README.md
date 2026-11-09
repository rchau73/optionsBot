# The assistant's question set

Groundwork for E10's help assistant (#313, #258, #260). Nothing here is shown to users, and no model is called, before counsel answers legal questions L11 and L12. The rules a correct answer follows are in [`contract.md`](contract.md).

## What the set is for

`golden.jsonl` is a fixed, versioned set of questions a novice would ask, each with the passages of the help that answer it and the rules a correct answer follows. Every later piece of the assistant — a retrieval step, a prompt, a model — is measured against the same bar, offline:

```bash
python3 scripts/product/assistant_grade.py answers.jsonl          # score per category, failures with reasons
python3 scripts/product/assistant_grade.py answers.jsonl --json
```

`answers.jsonl` holds one `{"id", "text", "citations"}` object per question, produced by anything; the live golden-set run below produces it from the real assistant.

## One question per line

| Field | Meaning |
|---|---|
| `id` | unique, `<category prefix>-<a few words>` (`ng-consent-analytics`), in alphabetical place; numbered ids stop at the numbers they had (#596): two PRs that each took the next number collided |
| `category` | `how_it_works`, `parameter`, `why_did_it`, `jargon`, `risk`, `refuse_advice`, `refuse_forecast`, `unknown`, `request`, `results` (#648: a period's result, answered from the bot's performance) or `idle` (#719: why little or nothing is open now, answered from the bot's book now) |
| `question` | plain words, as a novice would ask |
| `expected_behaviour` | `answer`; `refuse` for the two refusal categories; `dont_know` for `unknown` (the help cannot answer it); for `request` the request check's outcome: `needs_info`, `answered` or `draft` |
| `expected_sources` | the sources that answer it, as `help-index.json` names them: `OptionStrategy.md#<heading anchor>`, `glossary:<term>`, `explain:<event>:<reason>`; empty for refusals and `unknown` |
| `must_say` | short facts a correct answer says, each in the help's own words (it appears in one of the expected sources) |
| `must_not_say` | phrases a correct answer never says ("you should", "guaranteed", "% a month"); each must be one the guard rejects |
| `setting` | `parameter` only: the config key it asks about |
| `journal` | `why_did_it` only: the journal fixture the question is about — `kind` (`close`, `skip`, `risk`) and `reason` (its journal label, e.g. `stop_loss`, `gex_wait`, `repair_held`), `option_type` for a close |
| `request` | `request` only (#409): the request check's fixture — `passages` (journal facts and P&L history points as the monitor sends them, `{id, source, title, text}`), `verdict` (the model's structured reply), `unticked` (the checklist items that must be unticked: `what`, `where`, `evidence`, `not_explained`) and `draft_kind` (`bug`, `enhancement`) for a draft |
| `bot` | a question about the bot's own data (#427): its routes' answers (`status`, `positions`, `payoff`, `journal`, `explain` for a Why?) and a correct `answer` citing the passages the monitor builds from them (`live:<bot>/…`, `journal:<bot>/…`); a `results` question also has `perf` (#648): `/api/performance`'s answers for `7d` and `30d`, cited as `perf:<bot>/7d|30d`, invented testnet figures; an `idle` question's answer cites `live:<bot>/book` (#719), built from `status`, `positions` and `journal` (`status: null` = the status route did not answer) |

Minimum counts: 15 `how_it_works`, 10 `parameter` (one per tuned setting of CLAUDE.md "Few settings"), 10 `why_did_it` (one per close reason `PlainReason` explains), 5 `jargon`, 5 `risk`, 10 refusals (5 of each kind), 5 `unknown`, 3 `request` (a vague report → `needs_info`, figures the journal agrees with → `answered`, real evidence → `draft`), 4 `results` (a month, the worst week, against holding, a negative week with its journal reason; `frontend/__tests__/assistant-perf.test.js` checks each answer against its fixture's passages), 4 `idle` (waiting for a fresh GEX reading, new risk frozen, every slot and the margin in use, the status unknown; `frontend/__tests__/assistant-book.test.js` checks each answer against its fixture's passages). The monitor's `frontend/__tests__/assistant-request.test.js` runs every `request` fixture through the request check (`lib/assistant/request.js`); the grader checks a live answer's `outcome` against `expected_behaviour`.

## Adding a question

1. Add one line to `golden.jsonl`; pick `must_say` phrases copied from the passage that answers it.
2. Run `go test ./tests/... -run 'TestAssistant'`. It fails, naming the question, when a source does not resolve (a renamed heading included), an id repeats, a category falls under its minimum, a `must_say` is not in its sources, a `must_not_say` passes the guard, a close reason or a tuned setting has no question.
3. Run `python3 -m unittest scripts/product/test_assistant_grade.py`: the set's own correct answers must score 100 %.

**No amounts, no account data, no personal data.** The file is public. Facts come from the help and from `PlainReason`'s sentences, never from live numbers; a `why_did_it` fixture names a reason, not a trade; a `request` fixture's figures are invented, never a live account's.

## The answer guard

`internal/assistant/guard` (`Check`, `Fallback`) is the check an answer must pass before it could be shown. Its reason codes:

| Code | Rejects |
|---|---|
| `no_citation` | no citation, unless the text is exactly the "I don't know", refusal or fallback reply |
| `unknown_citation` | a citation that resolves to no passage of the index |
| `ungrounded_number` | a number, percentage or date that is in none of the cited passages (0.30 = 30 %) |
| `advice` | "you should", "I recommend", "increase your…", "good time to sell", an imperative opening a sentence |
| `forecast` | "will earn", "you can expect", "expected return", "will go up", "guaranteed" (not "not guaranteed"), "% a month" |
| `points_to_document` | "see the guide", "read OptionStrategy.md", "check section 9", "you can find it in the glossary": the assistant answers, never sends the person to a document (#534) |
| `language` | an answer not in English: under 10 % of its words are common English words (the phrase lists read English only, so it fails closed; contract §2.7, #565, needs legal review) |

A plain answer rejected only for `ungrounded_number`, `unknown_citation`, `points_to_document` or `language` is asked once more, with what failed; the fallback is shown only when the second answer is rejected too (contract §5).

The phrase lists, the English word list and the three canonical replies are in `internal/assistant/guard/phrases.go`. Counsel's answer to L11 changes those lists, not the design. Not wired to anything yet.

## Live golden-set run

The run (#536) asks the real assistant every question of `golden.jsonl`, grades the answers with `assistant_grade.py` and compares the score per category with the last accepted one, so a model, prompt or help change that makes answers worse is caught.

```bash
bash scripts/product/assistant_run.sh --env mvp                 # the key from mvp's .env; exit 1 on a regression
bash scripts/product/assistant_run.sh --env mvp --accept        # a person accepts this run's score as the new baseline
ANTHROPIC_API_KEY=… bash scripts/product/assistant_run.sh        # this checkout's data/monitor
```

**How it asks.** `frontend/scripts/assistant-run.mjs` runs the monitor's own `answerQuestion` (the same prompt, guard, model and help index, `lib/assistant/goldenrun.js`), not `POST /api/assistant`.
- Each question gets the data its fixture holds, as the monitor would build it: `bot` (the bot's page data, journal and a Why?'s facts) and `request` (journal facts and P&L history points). These are invented figures, never a live account's.
- It is outside the per-person limit of 30 questions an hour, which it never touches: the limit for people does not change. It paces itself instead, 2 s between questions.
- A failed model call is asked once more; failed again, the question is still a line of `answers.jsonl` (outcome `model_error`, which the grader fails), so none is lost.
- Without `ANTHROPIC_API_KEY` it stops with one message, before asking anything, and writes no answer and no score.

**A test run, never counted as a person's question.**
- Its log is a file of its own, `data/monitor/assistant_runs/<time>/run.jsonl`, never `assistant.jsonl`; it refuses the monitor's own files.
- Every line of it carries `test_run: true` and the user `golden-run`; `cmd/assistantreview` leaves such lines out (`TestRuns`).
- It sends no analytics event (E13 counts only the browser's) and never writes the request queue: only a person's Send does.

**Files** (all in the private `data/monitor/`, 0600):

| File | Holds |
|---|---|
| `assistant_runs/<time>/answers.jsonl` | the run's answers, the grader's input |
| `assistant_runs/<time>/run.jsonl` | the run's review log, every line `test_run` |
| `assistant_reviews/<week>.golden.json` | the scores and every failure with its reasons, by golden id |
| `assistant_reviews/<week>.golden.summary.json` | counts and percentages per category, the baseline's score and the drop: what may be posted |
| `assistant_reviews/golden_baseline.json` | the last accepted score per category |

**Reading the score.** One row per golden category (`how_it_works`, `parameter`, `why_did_it`, `jargon`, `risk`, the two refusals, `unknown`, `request`) and `all`: questions passed, total, score, the baseline's score and the drop in points.
- A correct answer cites an expected source and says every `must_say` fact; a refusal or an "I don't know" is right only in its own categories.
- The first run sets the baseline.
- A category whose score falls **more than 10 points** under its baseline is a regression (`THRESHOLD_PTS`): the summary names it with the drop, and the script exits 1.
  One question of a 15-question category is 6.7 points, so a single answer that varies does not trip it; in a 5-question category one question is 20 points.
- The baseline moves only when a person runs `--accept`.

**Cost.** One model call per question that a help passage or a fixture can answer (about 115 a run on the claude-haiku-5-5 model the monitor uses), at most four for one question: a guard rejection's one more try, a failed call asked again, a report's request check.
The weekly job runs it once a week in mvp; anyone may run it on demand.

**Every Monday by itself:** `scripts/agents/usage.sh` runs `scripts/agents/assistant_golden.sh` after the quality review, in `ASSISTANT_GOLDEN_ENV` (default mvp) when its `.env` has `ANTHROPIC_API_KEY`.
It posts `assistant_golden.py comment` on the E10 epic: counts and percentages per category, the regressions named, never a question or an answer.
A failed run is tried once more; a comment that could not be posted is posted once more without asking the model again (docs/agents/usage.md "Weekly usage report").

## Weekly quality review

The monitor logs every answer, every "this answer is wrong" flag and every "Helped" / "Didn't help" in `data/monitor/assistant.jsonl` (#191). `cmd/assistantreview` (#438, pure `internal/assistantreview`) reads it once a week and measures the PRD's guardrail "assistant answers flagged wrong" and an input to "self-service understanding". It calls no model and no network, and it never writes the log.

```bash
go run ./cmd/assistantreview -log data/monitor/assistant.jsonl -week 2026-W44   # → data/monitor/assistant_reviews/2026-W44.*
go run ./cmd/assistantreview close -id <answer id> -how help_fix -ref "#123"    # records how a row was closed (#704)
```

**Metrics** (questions asked Monday 00:00 UTC to the next Monday):

- answers by kind (`answered`, `dont_know`, `refused`, `no_citation`, `rejected` = the fallback reply, `model_error`, `needs_info`, `draft`, `other`), and the share `answered`;
- the guard reasons the answers hit (`no_citation`, `unknown_citation`, `ungrounded_number`, `advice`, `forecast`, `points_to_document`, `language`), each answer once per reason;
- the share flagged, and the flag rate of each kind. A flag placed later still counts for its answer's week. A flag placed this week on an older answer puts that answer in this week's review list (`late`);
- **did it help?** (#579, the panel's "Helped" / "Didn't help", the `feedback` lines): for the week and for each kind, `helped`, `not_helped` and `no_feedback` (the person's last choice; a choice placed later still counts for its answer's week).
  The **helped share** = helped ÷ (helped + didn't help), shown only from 10 responses (`helped_pct` null and "too few responses" in the report otherwise); the **response rate** = (helped + didn't help) ÷ answers.
  It is the person's opinion, never a measure of correctness, and feeds no claim about accuracy: the PRD's "self-service understanding" signal, whose target the PM proposes after four weeks of data;
- unreadable log lines, counted and skipped.

**Files** in `data/monitor/assistant_reviews/` (gitignored, 0600):

| File | Holds | Shared? |
|---|---|---|
| `<week>.md`, `<week>.json` | the metrics, the **review list** (every flagged and every `dont_know` answer: id, time, kind, sources given, citations, guard verdict, the question, the answer shown, flag notes, and "closed (how)" or "open N days") and the **gap candidates** (those questions grouped by the nearest help passage, `helpkb.Search`, at least 25 % of the question's weight, else "no passage close") | no: private, personal data |
| `<week>.summary.json` | `Summary(report)`: counts and percentages only, plus kinds and reasons from the fixed lists. `flagged_total` (the review list's rows, flagged and `dont_know`) = `closed` + `open`; `overdue`; `closed_by_how` (`help_fix`, `golden_case`, `answer_was_right`) | yes |
| `<week>.golden_candidates.jsonl` | each review question once, in the format of `golden.jsonl` with `category`, `expected_behaviour` and `expected_sources` empty. A carried row and a question whose answer was closed as `golden_case` are left out, so the same question is not offered twice | no |
| `closures.jsonl` | one line per closure, across all weeks: `{answer_id, closed_at, how, ref}` | no: private, in the nightly backup (14 days) |

Nothing is added to `golden.jsonl` automatically.

**Who reads the review list:** the product owner (or a manager they name), each week, on the host. **The rule:** every flagged answer is closed within 7 days in one of three ways:

1. a help-source fix (a passage of OptionStrategy.md, the glossary or the monitor guide, then `go run ./cmd/helpindex`). A gap with a passage named means a better passage or a glossary term; "no passage close" means a new section;
2. a golden-set case (copy the candidate into `golden.jsonl`, give it a named id and fill in its category, behaviour and sources);
3. a recorded "answer was right", with the reason.

**Recording a closure** (#704): `go run ./cmd/assistantreview close -id <answer id> -how <how> -ref "..."`, run on the host with the same `-log` and `-out` as the review, appends one line to `closures.jsonl`:

| `how` | `ref` |
|---|---|
| `help_fix` | the help passage, or the PR or issue number |
| `golden_case` | the golden id |
| `answer_was_right` | a short reason, no personal data |

It refuses, with a plain message and nothing written, an unknown id (no flagged or `dont_know` answer has it), an unknown `how`, an empty `ref` and a second closure of the same id.
It reads the log and never writes it.

**What the review does with it:**

- a row with a closure reads "closed (how)". Any other reads "open N days": whole days since its first flag, or since the answer for an unflagged `dont_know`;
- a row open more than 7 whole days is **overdue** and listed first. "Open 7 days" is not overdue yet; "open 8 days" is;
- a row of an earlier week not closed before this one stays on the list, marked `carried`. One closed during or after the week shows once more as closed, then leaves. Carried rows are not counted in the week's answers or gap candidates;
- each closed row's date and `how` go to the run's log, `data/agents/assistant_review/<week>/<env>.log`. No new analytics event.

The first review after #704 carries every unclosed flagged and `dont_know` answer of the log, most of them overdue: the backlog the rule never tracked.

**Closure rate**, the follow-up to the guardrail: `closed` ÷ `flagged_total`, target 100 %. A row closed in time never reaches `overdue`, so `overdue` > 0 means the rule was missed.

**Every Monday by itself:** `scripts/agents/usage.sh` runs `scripts/agents/assistant_review.sh` in each environment that has a non-empty log.
It posts the summary's counts and percentages on the E10 epic, percentages only for a live account: the overdue count, and a line asking a person to read the private review list when it is above 0.
A week with no answers and no open rows posts nothing and writes no marker (docs/agents/usage.md "Weekly usage report").
The log is in the nightly backup (`<date>/<env>/monitor/assistant.jsonl`, 14 days), and each line of the copy must parse; so is `closures.jsonl`, copied from the clone.

**Needs legal review** (L11/L12 in `docs/product/legal-brief.md`): the review files hold question text, which is personal data. Until counsel answers on retention, they stay local, gitignored and in the 14-day backup only. Nothing of them goes to GitHub.
