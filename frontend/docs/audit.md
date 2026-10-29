# Audit

Part of the [monitor README](../README.md).

## Audit page (#308, #249)

`/admin/audit`, linked as **Account ▾ → Audit** for admins only (a manager or a user who opens it sees "Only an admin reads the audit trail" and no lines; the route answers them 403).
Filters: person (who acted: the line's `username`), action (the known list, `lib/audit.js` `AUDIT_ACTIONS`), from / to (UTC days, inclusive).
A table of time (BRT, `lib/time.js`), person, role, action, target or bot, and detail (every other field as stored, e.g. a close's `position_ids` and `status`, a rebalance's `dry_run`), newest first, 100 lines at a time with **Older** for the next page.
Read-only: nothing on the page or the route edits or deletes a line.

### Routes

#### `GET /api/admin/audit?user=&action=&from=&to=&before=&limit=`

**Does:** `{"off", "entries", "skipped", "next_before"}`: the lines as stored, newest first; `limit` 1–1000 (default 100); `next_before` = the cursor (a byte offset) of the next, older page, `null` at the end.
Reads `AUDIT_PATH` backwards in 64 KB chunks (`lib/auth/auditread.js`), never the whole file, and stops at a full page or at lines older than `from`.
A line that is not a JSON object is skipped and counted in `skipped` (per page), never a 500; a missing file is an empty list; an in-memory user database has no trail: `off: true`, which the page says

**Refusals:** 400 unknown action, a malformed day, `from` after `to`, a bad `limit` or `before` · 403 not admin
