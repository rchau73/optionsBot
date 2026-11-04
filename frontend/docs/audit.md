# Audit

Part of the [monitor README](../README.md).

## Audit page (#308, #249)

`/admin/audit`, linked as **Account ▾ → Audit** for admins only (a manager or a user who opens it sees "Only an admin reads the audit trail" and no lines; the route answers them 403).
Filters: person (who acted: the line's `username`), action (the known list, `lib/audit.js` `AUDIT_ACTIONS`), from / to (UTC days, inclusive).
A table of time (BRT, `lib/time.js`), person, role, action, target or bot, and detail (every other field as stored, e.g. a close's `position_ids` and `status`, a rebalance's `dry_run`), newest first, 100 lines at a time with **Older** for the next page.
Read-only: nothing on the page or the route edits or deletes a line.

### Chain line (#619)

Under the filters, `components/AuditChainStatus.jsx` (composed by the page into `AuditTrail`'s `chain` slot) says "Chain intact — N lines checked." or "Chain broken at line N (time): why. M lines before it checked intact.", with **Check again**.
Why, in words (`lib/audit.js` `auditChainSummary`): "a line before it was removed, reordered or edited", "this line was edited", "this line is not a readable audit line" or "this line has no link to the line before"; a break in an archive names its file.
Legacy lines (written before #619) are named: "N legacy lines before <time> are not covered."
While it checks: "Checking the audit chain…" (Check again disabled); an error: "The chain could not be checked: <reason>"; an in-memory trail shows no chain line (the trail says it is off).
A broken chain hides no line: the table below lists every line as before.
The chain's own fields, `prev` and `line_hash`, are left out of the detail column.

### Hash chain (#619)

Every line written by `fileAudit` (`lib/auth/audit.js`) ends with `prev` (SHA-256 of the previous line's exact bytes, `""` for the first line of the trail) and `line_hash` (SHA-256 of the line itself up to `,"line_hash"`), built by `sealLine` (`lib/auth/auditchain.js`).
`prev` shows a removed, inserted or reordered line at the line after it; `line_hash` shows an edited line at that line, including the last one.
The append is one synchronous step (read the last line, maybe rotate, write), so two actions of the monitor never take the same `prev`, even in the same millisecond.
A last line cut short (no newline) is left as it is; the next line starts on its own line, and verification reports the cut line as damaged.
Lines without `prev` before the first chained line are legacy: counted, not checked; the first chained line links to the last legacy one.

### Rotation (#619)

When `audit.jsonl` has reached `AUDIT_MAX_BYTES` (`.env`, a platform setting; default 20 MB), the next audited action renames it to `audit.<UTC time>.jsonl` beside it (`audit.20261104T031433123Z.jsonl`, owner-only, never over an existing archive) and writes its line to a new `audit.jsonl` whose `prev` is the old file's last line's hash.
Logged `audit trail rotated` with both paths; a bad `AUDIT_MAX_BYTES` is logged and the default used.
The app never deletes an archive.

### Routes

#### `GET /api/admin/audit?user=&action=&from=&to=&before=&limit=`

**Does:** `{"off", "entries", "skipped", "next_before"}`: the lines as stored, newest first; `limit` 1–1000 (default 100); `next_before` = the cursor of the next, older page, `null` at the end.
The cursor is a byte offset into the archives (oldest first) and the current file read as one sequence, so rotation between two pages never moves it.
Reads the current file, then the archives newest first, backwards in 64 KB chunks (`lib/auth/auditread.js`), never a whole file, and stops at a full page or at lines older than `from`.
A line that is not a JSON object is skipped and counted in `skipped` (per page), never a 500; a missing file is an empty list; an in-memory user database has no trail: `off: true`, which the page says

**Refusals:** 400 unknown action, a malformed day, `from` after `to`, a bad `limit` or `before` · 403 not admin

#### `GET /api/admin/audit/verify` (#619)

**Does:** `{"off", "ok", "checked", "legacy", "legacy_before", "first_break"}`: the hash chain of the archives (oldest first) and the current file, read forwards in 64 KB chunks and checked line by line (`verifyAudit` → `verifyChain`), stopping at the first break.
`checked` = chained lines found intact; `legacy` = lines before the chain; `legacy_before` = the time of the first chained line when there are legacy lines; `first_break` = `null` or `{file, archive, line (1-based in that file), time, reason}` with reason `link`, `edited`, `damaged` or `unchained`.
A missing trail is `ok` with nothing checked; an in-memory user database: `off: true`.
Read-only; one log line, `audit chain verified` (INFO when intact, WARN when broken), with the admin's username and the result.
Admins only, in `ROLE_GUARDED_READS` (`lib/auth/routeRoles.js`), so the role matrix calls it as every caller.

**Refusals:** 403 a manager or a user · 401 no session, an expired one or the service token — before any file is opened
