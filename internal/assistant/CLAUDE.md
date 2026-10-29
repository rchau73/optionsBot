# internal/assistant

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

```bash
go run ./cmd/assistantreview -log data/monitor/assistant.jsonl -week 2026-W44   # assistant quality review (#438): answers by kind, guard reasons, flag rates, the review list of flagged and dont_know answers, gap candidates by nearest help passage (helpkb.Search) → data/monitor/assistant_reviews/<week>.md|json (private: question text) + .summary.json (counts/% only, posted on E10 by usage.sh → assistant_review.sh) + .golden_candidates.jsonl (golden.jsonl untouched); no model, no network (docs/product/assistant/README.md "Weekly quality review")
```

- **`internal/assistant/guard`** — the E10 answer guard (#260), pure;
  - the monitor's `POST /api/assistant` (#191, `frontend/lib/assistant/`;
    - its panel on every signed-in page — Ask in the menu bar, Why? on trade rows sending the journal `seq` whose `/api/explain` facts become passages,
    - #408: Claude on the best help passages + the bot's journal events +
      (#427) its live page data as `live:<bot>/market|leg/<instrument>` passages from `/api/status`, `/api/positions`, `/api/strangles/payoff` (cached 10 s)
    - — a mixed question gets its facts answered and only the advice declined with `Refusal`, `ANTHROPIC_API_KEY` in the monitor's `.env` or 503,
      Q&A logged in `data/monitor/assistant.jsonl`, `POST /api/assistant/flag`, 30 questions/h per person;
    - #409: a question that reads as a report runs the request check — `lib/assistant/request.js`, the model's structured verdict checked server-side for what/where/evidence/not explained,
      evidence numbers by the guard's `ungrounded_number`, the bot's P&L history as `pnl:<bot>/<time>` passages → `needs_info` | `answered` | `draft`;
    - only the person's Send puts a draft in the private queue `data/monitor/requests.jsonl`, `POST|GET /api/assistant/requests`, 10 a day,
      merged with `requests_status.jsonl`, both gitignored and in the nightly backup, never GitHub; golden `request` cases)
  - runs its JavaScript twin `guard.js` (with `search.js`, the twin of `helpkb.Search`)
    — `frontend/__tests__/assistant-*.test.js` keep phrases, guard cases, golden set and search baseline equal to the Go side, so a change here changes both:
  - `Check(Answer{Text, Citations}, passages)` rejects with `no_citation` (unless the text is exactly a canonical reply: `DontKnow`, `Refusal`, `FallbackReply`), `unknown_citation`,
    `ungrounded_number` (a number/percentage/date in no cited passage; 0.30 = 30 %), `advice`, `forecast`,
    `points_to_document` ("see the guide": the assistant answers, never points to a document, #534);
  - `Fallback` is the safe reply, naming no document;
    the monitor asks the model once more (with what failed) before it, for an answer rejected only for `ungrounded_number`, `unknown_citation` or `points_to_document` (`retryFeedback`).
  Phrase lists and replies live in `phrases.go` only (counsel's L11 answer changes a list).
  The answer contract is `docs/product/assistant/contract.md` (needs legal review);
  - the golden set `docs/product/assistant/golden.jsonl` (#313, #258: categories with minimum counts, expected sources, `must_say`/`must_not_say`, refuse/dont_know)
    is checked by `tests/assistant_questions_test.go` (sources resolve to OptionStrategy.md headings or index passages, one question per tuned setting and per close reason `PlainReason` explains)
    and graded offline by `scripts/product/assistant_grade.py` (answers JSONL → score per category);
  - `tests/assistant_guard_test.go` keeps the replies equal in the guard, the contract and the grader and checks every `must_not_say` is rejected.
