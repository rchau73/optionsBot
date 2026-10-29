# frontend

Folder notes for `CLAUDE.md`; UI/UX documentation lives in `frontend/README.md`.

- **Monitor pages and the menu** (product owner, 2026-10-27):
  - every change that adds, renames or removes a monitor page updates the menu in the same PR (`frontend/lib/menu.js`, #385), or names the page in the menu test's allow-list with its reason;
  - the test fails on a page that is in neither.
  It also updates the user-facing guide, `frontend/content/monitor-guide.md` (#418): one `##` section named as the menu item; `tests/monitor_guide_test.go` fails on a menu page with no section.
  Then re-run `go test ./tests -run Help -args -update-help` (the guide is an assistant source).
