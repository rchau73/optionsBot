# frontend

Folder notes for `CLAUDE.md`; UI/UX documentation lives in `frontend/README.md` (setup, structure, index) and one file per area under `frontend/docs/` (#462).

- **Docs by area** (#462): a new page, route or rule goes in its area's file, one sentence per line; a route is a heading of its own, never a table row, so two new routes land in different places.
  - `scripts/docs_nothing_lost.py <old> <new files…>` lists the sentences of an old document missing from the files it was split into (`--links` also checks the links).

- **Monitor pages and the menu** (product owner, 2026-10-27):
  - every change that adds, renames or removes a monitor page updates the menu in the same PR (`frontend/lib/menu.js`, #385), or names the page in the menu test's allow-list with its reason;
  - the test fails on a page that is in neither.
  It also updates the user-facing guide, `frontend/content/monitor-guide.md` (#418): one `##` section named as the menu item; `tests/monitor_guide_test.go` fails on a menu page with no section.
  Then re-run `go test ./tests -run Help -args -update-help` (the guide is an assistant source).

- **The Overview's four answers** (#489, epic E3): `__tests__/overview-four-answers.test.jsx` renders the desktop Overview on four fixtures (profit, loss, nothing held, stale)
  and fails when a redesign drops one answer: the positions per strangle, the month net of fees in coin and USD (a loss as prominent as a gain),
  the worst case in money, and the last trade's one-click Why? (`LastTrade`, from `/api/summary`'s `last_action`).
