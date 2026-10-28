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
