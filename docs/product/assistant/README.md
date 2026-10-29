# The assistant's question set

Groundwork for E10's help assistant (#313, #258, #260). Nothing here is shown to users, and no model is called, before counsel answers legal questions L11 and L12. The rules a correct answer follows are in [`contract.md`](contract.md).

## What the set is for

`golden.jsonl` is a fixed, versioned set of questions a novice would ask, each with the passages of the help that answer it and the rules a correct answer follows. Every later piece of the assistant — a retrieval step, a prompt, a model — is measured against the same bar, offline:

```bash
python3 scripts/product/assistant_grade.py answers.jsonl          # score per category, failures with reasons
python3 scripts/product/assistant_grade.py answers.jsonl --json
```

`answers.jsonl` holds one `{"id", "text", "citations"}` object per question, produced by anything.

## One question per line

| Field | Meaning |
|---|---|
| `id` | unique, `<category prefix>-NN` |
| `category` | `how_it_works`, `parameter`, `why_did_it`, `jargon`, `risk`, `refuse_advice`, `refuse_forecast`, `unknown` or `request` |
| `question` | plain words, as a novice would ask |
| `expected_behaviour` | `answer`; `refuse` for the two refusal categories; `dont_know` for `unknown` (the help cannot answer it); for `request` the request check's outcome: `needs_info`, `answered` or `draft` |
| `expected_sources` | the sources that answer it, as `help-index.json` names them: `OptionStrategy.md#<heading anchor>`, `glossary:<term>`, `explain:<event>:<reason>`; empty for refusals and `unknown` |
| `must_say` | short facts a correct answer says, each in the help's own words (it appears in one of the expected sources) |
| `must_not_say` | phrases a correct answer never says ("you should", "guaranteed", "% a month"); each must be one the guard rejects |
| `setting` | `parameter` only: the config key it asks about |
| `journal` | `why_did_it` only: the journal fixture the question is about — `kind` (`close`, `skip`, `risk`) and `reason` (its journal label, e.g. `stop_loss`, `gex_wait`, `repair_held`), `option_type` for a close |
| `request` | `request` only (#409): the request check's fixture — `passages` (journal facts and P&L history points as the monitor sends them, `{id, source, title, text}`), `verdict` (the model's structured reply), `unticked` (the checklist items that must be unticked: `what`, `where`, `evidence`, `not_explained`) and `draft_kind` (`bug`, `enhancement`) for a draft |

Minimum counts: 15 `how_it_works`, 10 `parameter` (one per tuned setting of CLAUDE.md "Few settings"), 10 `why_did_it` (one per close reason `PlainReason` explains), 5 `jargon`, 5 `risk`, 10 refusals (5 of each kind), 5 `unknown`, 3 `request` (a vague report → `needs_info`, figures the journal agrees with → `answered`, real evidence → `draft`). The monitor's `frontend/__tests__/assistant-request.test.js` runs every `request` fixture through the request check (`lib/assistant/request.js`); the grader checks a live answer's `outcome` against `expected_behaviour`.

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

The phrase lists and the three canonical replies are in `internal/assistant/guard/phrases.go`. Counsel's answer to L11 changes those lists, not the design. Not wired to anything yet.

## Weekly quality review

The monitor logs every answer and every "this answer is wrong" flag in `data/monitor/assistant.jsonl` (#191). `cmd/assistantreview` (#438, pure `internal/assistantreview`) reads it once a week and measures the PRD's guardrail "assistant answers flagged wrong" and an input to "self-service understanding". It calls no model and no network, and it never writes the log.

```bash
go run ./cmd/assistantreview -log data/monitor/assistant.jsonl -week 2026-W44   # → data/monitor/assistant_reviews/2026-W44.*
```

**Metrics** (questions asked Monday 00:00 UTC to the next Monday):

- answers by kind (`answered`, `dont_know`, `refused`, `no_citation`, `rejected` = the fallback reply, `model_error`, `needs_info`, `draft`, `other`), and the share `answered`;
- the guard reasons the answers hit (`no_citation`, `unknown_citation`, `ungrounded_number`, `advice`, `forecast`), each answer once per reason;
- the share flagged, and the flag rate of each kind. A flag placed later still counts for its answer's week. A flag placed this week on an older answer puts that answer in this week's review list (`late`);
- unreadable log lines, counted and skipped.

**Files** in `data/monitor/assistant_reviews/` (gitignored, 0600):

| File | Holds | Shared? |
|---|---|---|
| `<week>.md`, `<week>.json` | the metrics, the **review list** (every flagged and every `dont_know` answer: id, time, kind, sources given, citations, guard verdict, the question, the answer shown, flag notes) and the **gap candidates** (those questions grouped by the nearest help passage, `helpkb.Search`, at least 25 % of the question's weight, else "no passage close") | no: private, personal data |
| `<week>.summary.json` | `Summary(report)`: counts and percentages only, plus kinds and reasons from the fixed lists | yes |
| `<week>.golden_candidates.jsonl` | each review question once, in the format of `golden.jsonl` with `category`, `expected_behaviour` and `expected_sources` empty | no |

Nothing is added to `golden.jsonl` automatically.

**Who reads the review list:** the product owner (or a manager they name), each week, on the host. **The rule:** every flagged answer is closed within 7 days in one of three ways:

1. a help-source fix (a passage of OptionStrategy.md, the glossary or the monitor guide, then `go run ./cmd/helpindex`). A gap with a passage named means a better passage or a glossary term; "no passage close" means a new section;
2. a golden-set case (copy the candidate into `golden.jsonl` and fill in its category, behaviour and sources);
3. a recorded "answer was right", with the reason.

**Every Monday by itself:** `scripts/agents/usage.sh` runs `scripts/agents/assistant_review.sh` in each environment that has a non-empty log. It posts the summary's counts and percentages on the E10 epic, percentages only for a live account. A week with no answers posts nothing and writes no marker (docs/agents/usage.md "Weekly usage report"). The log is in the nightly backup (`<date>/<env>/monitor/assistant.jsonl`, 14 days), and each line of the copy must parse.

**Needs legal review** (L11/L12 in `docs/product/legal-brief.md`): the review files hold question text, which is personal data. Until counsel answers on retention, they stay local, gitignored and in the 14-day backup only. Nothing of them goes to GitHub.
