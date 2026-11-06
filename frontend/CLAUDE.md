# frontend

Folder notes for `CLAUDE.md`; UI/UX documentation lives in `frontend/README.md` (setup, structure, index) and one file per area under `frontend/docs/` (#462).

- **Docs by area** (#462): a new page, route or rule goes in its area's file, one sentence per line; a route is a heading of its own, never a table row, so two new routes land in different places.
  - `scripts/docs_nothing_lost.py <old> <new files…>` lists the sentences of an old document missing from the files it was split into (`--links` also checks the links).

- **Monitor pages and the menu** (product owner, 2026-10-27):
  - every change that adds, renames or removes a monitor page updates the menu in the same PR (`frontend/lib/menu.js`, #385), or names the page in the menu test's allow-list with its reason;
  - the test fails on a page that is in neither.
  It also updates the user-facing guide, `frontend/content/monitor-guide.md` (#418): one `##` section named as the menu item; `tests/monitor_guide_test.go` fails on a menu page with no section.
  Then re-run `go test ./tests -run Help -args -update-help` (the guide is an assistant source).

- **The team's cards in the Ask panel** (#446, #498): `NpsCard.jsx` and `AnswersCard.jsx`, one at a time (NPS first, never both in one tab);
  their due rules share `surveyNotEligibleReason` (`lib/analytics/nps.js`) and are decided on the server from the user's row, so a new card reuses it rather than a second copy.

- **Did this help?** (#579): `components/HelpfulButtons.jsx` under an `answered` reply posts `POST /api/assistant/feedback` (a `feedback` line in `assistant.jsonl`, `frontend/docs/assistant.md` "Routes");
  `cmd/assistantreview` reports `helped`, `not_helped`, `no_feedback`, `helped_pct` (from 10 responses) and `response_pct` per kind — the person's opinion, never a measure of correctness.

- **The Overview's four answers** (#489, epic E3): `__tests__/overview-four-answers.test.jsx` renders the desktop Overview on four fixtures (profit, loss, nothing held, stale)
  and fails when a redesign drops one answer: the positions per strangle, the month net of fees in coin and USD (a loss as prominent as a gain),
  the worst case in money, and the last trade's one-click Why? (`LastTrade`, from `/api/summary`'s `last_action`).
- **The hedge's lines in plain words** (#679, epic E3): `lib/hedgeWords.js` has one sentence per `orders.Hedge*` change and `shed_hedged`, for the Activity page and "Since you last looked";
  a new hedge change needs its sentence there (`__tests__/hedge-words.test.jsx` reads the Go constants), besides the bot's own in `internal/api/explain.go`.
- **Losses as visible as gains** (#582, epic E3): `__tests__/loss-visibility.test.jsx` renders Income, Details (pro), the phone overview, Worst case and Positions on the shared fixtures of `test/resultFixtures.js` (profit, loss, nothing held)
  and fails, naming the page, on a loss without its minus sign and unit, smaller or lighter than the gain, folded away, or a 0 shown for nothing held; a new page that shows a result gets its row in `PAGES`.
