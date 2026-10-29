# Assistant

Part of the [monitor README](../README.md).

## Assistant (#191, E10)

A help assistant behind the sign-in, with its panel on every signed-in page (#408).
It answers from the published help, the bot's live page data and the user's own bot journal, cites them, says "I don't know" otherwise, and never advises or forecasts — the answer contract is `docs/product/assistant/contract.md` (draft, needs legal review, L11/L12).

- **Sources** (`content/help-index.json`, built by `go run ./cmd/helpindex`): OptionStrategy.md, the glossary, the plain-words catalogue, `docs/parameters.md` (`parameters.md#<anchor>`) and the user-facing `content/monitor-guide.md` (`monitor-guide.md#<anchor>`, #418: one `##` section per menu page, named as its menu item).
  A new or renamed page updates the guide in the same PR; `tests/monitor_guide_test.go` fails otherwise.
- **Flow** (`lib/assistant/service.js`): the model gets three kinds of passage.
  - The question's best `HELP_PASSAGES` (6) passages of `content/help-index.json` (`lib/assistant/search.js`, the BM25 of `internal/helpkb`).
  - With `bot`, that bot's journal facts of the last `JOURNAL_DAYS` (7) from its `/api/journal?days=7`.
    - #417: the journal is read back from `orders.log`, with no `pnl` lines, and the bot collapses repeats.
    - It is kept per bot for `JOURNAL_CACHE_MS` (60 s) in the monitor's memory, so there is one read a minute, not one per question.
    - A failed read is not cached.
    - It sends every open, close and fill of the week.
    - Then it sends the other facts the question matches best (the same BM25 over each one's sentence and rule word) and the decisions within 30 min of a trade.
    - All of it stays within `JOURNAL_PASSAGES` (60) passages and `JOURNAL_TOKENS` (~6,000) tokens (`selectJournal`).
    - Each is a passage `journal:<bot>/<seq>` holding its plain-words sentence.
    - A collapsed one reads "×42, 20 Oct 08:00 → 21 Oct 14:30: <plain>".
    - A fill or a close is followed by the market its line recorded (#427): "Market recorded at 21 Oct 14:30 UTC: spot 84,498, DVOL 37.2, IV percentile 15 %, regime POSITIVE/PINNING."
  - The live page data, below.

  **The sources are the help, the live page data and the journal (7 days + the open positions' whole history)** (#427).
  The read is `/api/journal?days=7&held=1`, which adds every older event of each instrument the bot holds now, however old.
  `selectJournal` sends the history of the held legs the question names first.
  `namedInstruments` matches by instrument, by strike ("75.000", "75,000", "75k") or by side, else every held leg.
  Their trades come oldest first, so the entry is there however old.
  Then come the week's other trades, then those legs' other events.

  **Live page data** (`livePassages`, `botLive` in `lib/assistant/server.js`): with `bot`, the bot's `/api/status`, `/api/positions` and `/api/strangles/payoff` are read together.
  They are kept `LIVE_CACHE_MS` (10 s) per bot.
  They become these passages:
  - `live:<bot>/market` holds spot, DVOL, IV percentile, gamma regime and its confirmation, trend, flip and shed band, IM % of margin balance and the limit.
  - One `live:<bot>/leg/<instrument>` per open leg holds side, strike, slot, expiry and days left, qty, entry price and mark.
    It also holds P&L in coin and % of premium as the Positions page shows them, delta, OTM %, the stop-loss mark and its modelled spot level, the delta-exit line, and why a missing leg is held.

  The text is plain, from the routes' own fields, nothing recomputed, and a missing figure reads "unknown".
  All of them are sent.
  A full book of 8 legs + the market stays under `LIVE_TOKENS` (1,500).
  With the help and the journal a question is ≲ 10k input tokens.
  Without the payoff the exit lines read "unknown"; a bot that does not answer leaves the help and the journal (WARN log, not cached).
  All of it goes to the model (`lib/assistant/model.js`, Claude through the Messages API, `claude-haiku-5-5` unless `ASSISTANT_MODEL`; answer as JSON `{answer, citations}`).
  When no passage matches and no journal was asked for, the reply is "I don't know" with no model call.
  Then the guard (`lib/assistant/guard.js`, the twin of `internal/assistant/guard`) checks the answer: **no citation → "I don't know"**; an unknown citation, a number in no cited passage, advice or a forecast → the fallback reply.
  A bot that does not answer leaves the help alone as the source (WARN log).
  **A mixed question** (facts and "how do I avoid it?", #427): the system prompt has the model answer the factual part with citations and decline only the advice part with the refusal sentence; a cited answer holding the refusal is `answered`, "I don't know" followed by the refusal is canonical (`dont_know`, no citation needed).
- **Twins of the Go code:**
  - `lib/assistant/phrases.js` copies `internal/assistant/guard/phrases.go`.
    `__tests__/assistant-parity.test.js` fails when they differ: change both.
  - `__tests__/assistant-guard.test.js` runs the Go guard's cases and the golden set on the JS guard.
  - `__tests__/assistant-search.test.js` scores the JS search on `tests/testdata/helpkb_questions.yaml` against `helpkb_baseline.json`.
- **Review log:** `data/monitor/assistant.jsonl` beside `auth.db` (`ASSISTANT_LOG_PATH`; off for an in-memory database), append-only, owner-only.
  One `"kind": "answer"` line per question — `id`, `ts`, `user`, `role`, `bot`, `question`, `sources` (passage ids given to the model), `model`, `model_answer` (the model's own text), `citations`, `verdict` (the guard's findings), `outcome`, `answer` (what was shown), `flag: false` — and one `"kind": "flag"` line per flag (`id`, `ts`, `user`, `role`, `note`); the answer line is never rewritten.
  An answer that cannot be logged is not shown (500).
  The monitor's stdout gets one line per question with the id and outcome only.

**Requests** (#409, the prototype's "Vague report", "Report that isn't a bug", "Bug with evidence" and "Enhancement" conversations):
A question that reads as a report runs the **request check** instead.
It reads as a report when `lib/assistant/request.js` `looksLikeReport` finds "bug", "wrong", "broken", "should" or "can you add"…, or when the model sets its `report` flag on a plain answer.
"should I/we" does not count, because it is the refusal's.
With `bot`, that bot's last day of P&L history joins the help and the journal.
It is read with `GET /api/pnl/history?range=1d`, at most 24 points are kept (the two around the largest step first), and they become passages `pnl:<bot>/<time>`.
The model returns a structured verdict (`checkRequest`).
It has a kind (`bug`/`enhancement`/`none`), what, where, evidence (each item citing one source), steps, explained_by, a title, and expected or who/why/example.
The server checks it (`checkRequest`, pure).
- **what** and **where** must be there.
- **evidence** is an item cited from a given passage whose numbers that passage holds (the guard's `ungrounded_number`, a journal time read as "09:12"), or two reproducible steps.
  One ungrounded item rejects the draft.
- **not explained** means no given passage in `explained_by`.
Outcomes: `needs_info` (the checklist, no draft), `answered` (the model's answer through the guard, with the checklist; an enhancement may still come with a draft), `draft` (the card, citing its evidence); kind `none` is answered as any other question.
A report with nothing to check it against gets the checklist without a model call.
The draft is logged with its answer in `assistant.jsonl` (`request: {checklist, draft}`, every AI output is reviewable) and is **never queued without the person's click**.
Send appends to `data/monitor/requests.jsonl` (`ASSISTANT_REQUESTS_PATH`; off for an in-memory database): `id`, `ts`, `user`, `role`, `environment` (`MONITOR_DEPLOYMENT`), `kind`, `fields` (as the person edited them), `citations` and `journal_refs` (taken from the logged draft, never from the browser), `answer_id`.
The review writes `data/monitor/requests_status.jsonl` (`{id, status, note?, issue_url?, ts}`, status `in_review` \| `filed` \| `fixed` \| `not_a_bug` \| `duplicate` \| `needs_info`; the latest line of an id wins; an `issue_url` that is not https is dropped).
Both files are gitignored, never copied into the repository (it is public) and in the nightly backup (`scripts/agents/backup_data.sh`, `<date>/<env>/monitor/`).
Filing a public issue is the next story's.

**The panel** (#408, the E10 prototype the product owner approved on #161): `components/AssistantProvider.jsx` sits in the root layout for a signed-in person, so the panel stays open across page changes and the conversation lasts for this browser tab only (the review log keeps the rest).
**Ask** (`components/AskButton.jsx`, #419) is a floating chat button fixed at the bottom-right of every signed-in page (a chat-bubble icon, "Ask" beside it from 640 px; rendered once by the provider, asking about the page's `?bot=`); it is dimmed with the reason as its tooltip while `GET /api/assistant` (or a question) answers 503.
The panel (`components/AssistantPanel.jsx`, pure helpers in `lib/assistantPanel.js`) is a 400 px `complementary` region on the right from `sm`, a full-screen `dialog` with a focus trap on a phone; × or Esc closes it and focus returns to the opener.
The header says "Assistant" with the chip "AI · answers from the docs and your journal".
There are two tabs, Ask and **My requests**.
- **My requests** (`components/MyRequests.jsx`) lists id, kind, title and status, with the reviewer's note.
  The status is In review, Filed (with a link to the public issue), Fixed, Not a bug, Duplicate or Needs info.
  It is read when the tab opens.
- Under a report there is the **request check** list (`components/RequestChecklist.jsx`, ✓/✗ per item).
- For a draft there is the **draft card** (`components/RequestDraftCard.jsx`).
  It shows a kind chip, "Draft request · you send it", the fields and the private-queue notice.
  The buttons are **Send for review**, **Edit** (every field editable) and **Discard**.
  After sending it says "Sent as R-0012.
Follow it in My requests."
  The wording and sizes are in `lib/assistantRequests.js`.

The message log is `role="log"` with `aria-live="polite"`.
- Empty, it shows three example questions.
- While thinking, it shows a skeleton.
- Each answer is shown as written (`pre-line`) with its **source chips**:
  - an OptionStrategy.md or other document section on the repository (new tab);
  - a glossary term's own source section;
  - a journal entry's trade row in Details (pro) (`#trade-<bot>-<seq>`);
  - the plain-words catalogue on Activity.
- Each answer also has 👍, **Flag** (→ "Flagged") and a foot line.
  The foot line is "Answered from the cited sources", "Facts from your journal" or "Advice and forecasts are refused".
- "I don't know" links How it works.
- A refusal offers "How does sizing work?" and "Open Worst case".
- A 429 shows "30 questions an hour: next at HH:MM".
- Another failure shows Retry.
The question box takes 500 characters (Enter sends, Shift+Enter a new line), under it "Answers can be wrong: check the linked source.
Not investment advice." and "n of 30 questions left this hour".
**Why?** (`components/WhyButton.jsx`) on each row of the Details trade log and on each open or close of the Activity page asks "Why did <BOT> <open|close> the <put|call> <instrument> on <time>?" with `bot` and the row's journal `seq`.
Analytics: `assistant_open` / `assistant_ask` (`entry`: `menu` \| `why`), `assistant_answered` (`answer`: `answered` \| `dont_know` \| `refused` \| `fallback`, from the route's `outcome` by `answerOf` in `lib/analytics/screens.js` — `no_citation` is `dont_know`, `rejected` is `fallback`; a 429, 503 or error sends none; #424) and `assistant_flag` — never the question, the answer or its id.

### Routes

#### `GET /api/assistant`

**Does:** `{"enabled": true, "remaining", "per_hour"}`: the questions the signed-in person has left this hour (counts nothing; the panel's Ask button)

**Refusals:** 401 no session · 503 `{"enabled": false, "error"}` while off

#### `POST /api/assistant` `{"question", "bot"?, "seq"?}`

**Does:** `{"id", "answer", "citations": [{id, source, title}], "outcome", "ai_generated": true, "notice", "remaining"}`.
- `outcome` is `answered` | `dont_know` | `refused` | `no_citation` | `rejected`.
  For a report it is `needs_info` | `draft`, with `checklist` (and `draft`, below).
- `citations` are only for `answered` and `draft`.
- `notice` is the AI label (avisos.md's fixed notice), to show with every answer.
- `remaining` is the questions left this hour.

With `seq` (a journal sequence number, the panel's Why?), the bot's `GET /api/explain?seq=` facts — the event's plain sentence, rule, detail and recorded market, and its related events — are passages too (`journal:<bot>/<seq>`); an event no longer in the bot's memory leaves the latest journal and the help (WARN log)

**Refusals:** 401 no session · 403 cross-origin · 415 not JSON · 400 no question, over 500 characters, `bot` not a string, `seq` not a positive integer or without `bot` · 404 unknown bot · 413 body over 2 KB · 429 over `QUESTIONS_PER_HOUR` (30) per person, with `Retry-After` and `{"retry_after", "remaining": 0}` (in memory) · 502 the model failed (logged) · 503 no `ANTHROPIC_API_KEY`

#### `POST /api/assistant/flag` `{"id", "note"?}`

**Does:** 204: a flag line for that answer

**Refusals:** 401 · 403 cross-origin, or someone else's answer (the person who asked, a manager or an admin may flag) · 400 no id, a note over 500 characters · 404 unknown id

#### `POST /api/assistant/requests` `{"answer_id", "kind": "bug"|"enhancement", "fields"}`

**Does:** 201 `{"id": "R-NNNN", "status": "in_review"}`: one line in the private queue — only from the draft card's Send

**Refusals:** 401 · 403 cross-origin · 415 · 400 bad kind, no title or what, over 10 evidence items or steps, evidence with a source the draft did not cite · 404 not the person's own draft · 409 this draft was already sent · 413 body over 16 KB · 429 over `REQUESTS_PER_DAY` (10) per person a UTC day (counted from the file) · 503 no queue file

#### `GET /api/assistant/requests`

**Does:** `{"requests": [{id, ts, kind, title, status, note, issue_url}], "per_day"}`: the person's own requests, newest first

**Refusals:** 401 · 503 no queue file
