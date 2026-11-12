# Sign-in

Part of the [monitor README](../README.md).
What each table, the audit file and the logs keep about a person, and for how long: [Personal data](personal-data.md).

## Sign-in

People sign in at `/login` with a username and password; `proxy.js` sends every page without a session to `/login?next=…` and answers 401 on every `/api` route except `/api/auth/login`, `/api/auth/refresh` and `/api/auth/reset`; the `/reset` page (a reset link, below) opens without a session too.
The close and rebalance routes check the person again themselves (Roles, below).

### Users

SQLite (`sql.js`, WebAssembly — no native build in the image) in `data/monitor/auth.db` (`AUTH_DB_PATH`), a `monitor-auth` Docker volume per environment: stable and mvp have separate users.
Table `users`: username (3–64 of `a-z 0-9 . _ -`, case-insensitive), argon2id hash, role `admin`/`manager`/`user` (enforced: Roles, above), email, created, last sign-in

### Passwords

argon2id (`hash-wasm`; 19 MiB, 2 passes), at least 12 characters.
Creating a user, changing a password and both resets also pass `guessablePasswordProblem` (`lib/auth/password.js`, #680), the one definition of the rule, which answers 400 with a reason in plain words:
- `too common`: on the bundled list of about 1,000 common passwords (`lib/auth/commonPasswords.js`; public data, not personal data, and no call to an outside service), ignoring case, look-alikes (`p@ssw0rd`) and digits or symbols before or after (`password2026!`);
- `contains your username`: the username inside the password, in any case.
The list's source: the words at the top of Mark Burnett's public-domain list of the 10,000 most common passwords, typed in rather than downloaded, plus long keyboard and digit runs.
Stored passwords are not checked (no forced change), and neither is the first admin's `ADMIN_PASSWORD`, which keeps the length rule alone.
A refused password is neither logged nor audited.
An unknown user is checked against a dummy hash, so it takes as long as a wrong password and gets the same 401 `invalid username or password`

### Access token

JWT HS256 (`jose`), 15 min, cookie `access_token_<env>` (`<env>` = `MONITOR_DEPLOYMENT`, so stable and mvp on one host keep separate sessions, #368; plain `access_token` without it; HttpOnly, SameSite=Lax, path `/`), signed with `AUTH_SECRET`; carries the user id, name, role and `sid` (its session)

### Refresh token

32 random bytes, only its SHA-256 stored, 14 days, cookie `refresh_token_<env>` on path `/api/auth`; reusable until logout (several tabs).
Each session row (`refresh_tokens`) also keeps a random public id (16 hex characters, the only id a person sees), the browser name read from the User-Agent at sign-in (`lib/auth/browser.js`, e.g. `Chrome on Windows`; the header itself is not stored) and when it was last refreshed (written at most once a minute), #584.
A password change, a role change or a deletion ends every session of that user

### Silent refresh

`lib/api/http.js` `apiFetch`: the monitor's own 401 (header `x-monitor-session: required`) → one refresh shared by every waiting request → one retry; when that fails the page goes to `/login` and comes back afterwards.
A bot's 401 passed through a proxy route is shown as an error, never a sign-out

### Password change

`POST /api/auth/password` `{"current", "next"}` (#212), the header's **Account ▾ → Change password** form (current, new, confirm; shows the 12-character rule before sending).
Live session required (re-read like a state-changing route; no session → 401, the service token never reaches it), same-origin JSON only.
`next`: at least 12 characters, not `current` and not guessable (Passwords, above), else 400 with the reason (`Too short: at least 12 characters`, `too common`, …).
A wrong `current` → 401 `Current password is wrong` and counts as a failed sign-in of that username (Lockout, below: 5 lock it for sign-in and for this route, which then answers 429).
Success ends **every** session of the person, then opens a new one for this browser (new access and refresh cookies): other browsers' next refresh fails and they go to `/login`.
Their access token, being a stateless JWT, still reads the monitor for up to 15 min; their close, rebalance and password change are refused at once (the session is gone)

### Deleting your own account

`DELETE /api/auth/account` (body `{}`, #543), the header's **Account ▾ → Delete my account** dialog (`components/DeleteAccount.jsx`).
Live session required (re-read like a state-changing route), same-origin JSON only; any role.
The last admin is refused with 409 and the reason (make another person an admin first); nothing changes.
Otherwise `AuthService.deleteOwnAccount` runs the admin deletion's steps: the user row with its consent, every session, reset links and notification state, then the analytics events and the survey answers.
The answer is `{"events_removed" or "events_error", "survey_answers_removed" or "survey_answers_error"}` and clears both cookies; the page goes to `/login`.
Audited as `account_deleted` (the person's own `username` and `role`), followed by `events_deleted` and `survey_answers_deleted` with `reason: account_deleted`; logged `account deleted by its owner`.
Exchange accounts and funds are not the monitor's and are untouched.
The dialog's exit question is in [Analytics](analytics.md).

### Active sessions

The header's **Account ▾ → Active sessions** dialog (`components/ActiveSessions.jsx`, #584) lists where the person is signed in and signs out the other browsers, without changing the password.
`GET /api/auth/sessions` and `DELETE /api/auth/sessions/<id>`: both need a live session, re-read like a state-changing route; any role ([API routes](api-routes.md)).
A row shows the browser name, when it signed in and when it was last used, and **This browser** on the current one; never a token, hash or address.
Signing out another browser deletes its session row: its close, rebalance and other changes are refused at once, and its next refresh fails, so it goes to `/login` within 15 min (its access token, a stateless JWT, still reads pages until then); the dialog says so.
The session in use is refused with 409 (use **Sign out**); someone else's or an unknown id gets 404 and nothing changes.
Audited as `session_ended` (`username`, `role`, `target_session`: the first 8 characters of the public id); logged `session ended`.
No analytics event is sent.

### Reset link

For a person who forgot their password, no e-mail is needed (#307).
An admin's **Reset link** on the Users page calls `POST /api/admin/users/<id>/reset-link` and gets `{"url", "expires_at"}`.
The link is shown **once** with a copy button, and the admin hands it over by any safe channel.
The token is 32 random bytes, and only its SHA-256 is stored.
It lives in `reset_tokens` in `auth.db`: `token_hash`, `user_id`, `expires_at`, `created_by` (the admin), `created_at`, `used_at`.
It is valid **30 min** and **single use**.
A new link voids the person's older ones.
Deleting the person deletes their links.
It is not available for your own account (409).
The person opens `/reset?token=…` (public; `<meta name="referrer" content="no-referrer">`; the token is dropped from the address bar), types the new password twice and sends `POST /api/auth/reset` `{"token", "next"}` (public, same-origin JSON, under the per-IP sign-in limit: 429 with `Retry-After`).
`next` follows the 12-character rule and the guessable-password rule (400 with the reason, the link stays usable: it is read, not claimed, to learn whose username to check); an unknown, used, expired or replaced link gets the one 400 `link expired or already used` and nothing changes.
Success sets the password, marks the link used and ends **every** session of the person — they then sign in at `/login`; lockout is untouched.
The admin's direct **Reset password** stays as it was

### Lockout

5 failed sign-ins for a username within 15 min lock it until 15 min after the last one (same 401); 20 sign-ins a minute per IP, then 429 with `Retry-After`.
The username failures and locks are kept per environment in `auth.db` (`login_failures`, `login_locks`; failures older than 15 min are deleted at the next failure), so a restart or deploy of the monitor does not clear a lockout; the per-IP limit is short-lived and stays in memory (a restart forgets it)

### Secure cookies

`Secure` except on plain http to `localhost`/`127.0.0.1` (the compose port), where a browser would drop them.
Opened from another machine over plain http, sign-in cannot work: put TLS in front first

### Service token

`MONITOR_API_TOKEN` (≥ 32 chars) as `Authorization: Bearer …` reads `GET /api/bots/*` without a session — the deploy health check, `promote.sh`, the SME agent and `stress_inputs.sh` use it (`env_monitor_token` in `scripts/env_lib.sh`).
Never a page, a close or a rebalance

### Logs

One JSON line for each of these, with username and IP:
- a sign-in, a failure (reason, `locked`), a lockout, a rate limit, a sign-out and the first admin;
- a user change: `user created`, `role changed`, `password reset` (with `event: password_reset`), `reset link issued` (with `expires_at`), `signed out everywhere` and `user deleted`, each with `by`;
- a reset by link: `password reset by link`, with `event: password_reset`, `via: link`, the person, `by` (the admin who issued it) and the IP;
- a refused link: a WARN `reset by link refused` with the IP only;
- a password change: `password changed` (with `event: password_changed`) or `password change failed`;
- another session signed out by its owner: `session ended` (with `event: session_ended` and `target_session`, #584);
- a person's own data downloaded: `my data exported` with the counts per section (#750);
- a refused action.

Never a password, hash, token or `AUTH_SECRET`

### Audit

`data/monitor/audit.jsonl` sits beside the user database (`AUDIT_PATH`; off for an in-memory database).
It is append-only and owner-only.
Each line is JSON, `{time, action, username, role, …}`, one per action:
- `signed_in`, `sign_in_failed` (reason), `locked` and `signed_out`;
- `password_changed` and `password_change_failed` (reason `password` or `locked`);
- `user_created` (`to` = the role) and `role_changed`;
- `password_reset` (an admin set another person's password, #306);
- `reset_link_issued` (with `expires_at`, #307);
- `user_deleted` and `signed_out_everywhere`, each with `target`, where `username` and `role` are the admin's;
- `account_deleted` (a person deleted their own account, #543), where `username` and `role` are the person's;
- `session_ended` (a person signed out one of their other sessions, #584), with `target_session` (a short id, never the token or its hash);
- `my_data_exported` (a person downloaded their own data, #750), with `counts` per section, never the content;
- `password_reset_by_link`, where `username` and `role` are the person's, `by` is the admin who issued the link, and `ip` is set;
- the analytics retention lines `events_pruned`, `events_deleted`, `events_exported` and their `_failed` forms (counts only, never an event; see "Analytics");
- `survey_answers_deleted` and `survey_answers_delete_failed` (counts only, never an answer);
- `analytics_consent_changed` (`consent` true or false);
- `survey_answered` (`survey_id` only, never the codes or text, #490);
- `nps_survey_changed` (`change`: shown, opt_out or opt_in, #490);
- `answers_survey_shown` (the four answers card was shown, nothing else, #498);
- `manual_close` and `manual_rebalance` (bot, position ids or dry run, the bot's status and, for a close, what it filled).
- `audit_exported` (an admin downloaded the trail as CSV, #702: `filters`, `lines`, `cut`, `skipped` and `complete`, never an exported line);
- `futures_hedge_switched` (bot, `enabled` true or false and the bot's status; #657, admins only);
- `parameter_change_previewed` (#682, a lab apply preview was read: `run_id`, `underlying`, `config`, `changes` with each `key`, `current` and `proposed`, `needs_product_owner`, `stale` and `line`, the same in plain words; never the confirm token);
- `parameter_change_recorded` (#741, a manager confirmed a lab run's change and it was recorded, nothing applied: `run_id`, `underlying`, `config`, `changes` with each `key`, `old` and `new`, `token_id` (a hash prefix, never the token), `applied: false` and `line`).
Every line ends with two fields of the hash chain (#619, `lib/auth/auditchain.js`): `prev`, the SHA-256 of the previous line's exact bytes (`""` for the first line of the trail), and `line_hash`, the SHA-256 of the line itself up to `,"line_hash"`.
Both are hashes of audit lines, never of a password or a token; the Audit page leaves them out of the detail column and the export out of the file.
Never a password, hash or token (nor a reset link).
Admins read it on the Audit page (below)

The first start with no users creates an admin from `ADMIN_USERNAME` / `ADMIN_PASSWORD` / `ADMIN_EMAIL`; later starts never touch existing users.
The server exits at start (`instrumentation-node.js`) when `AUTH_SECRET` is missing or shorter than 32 bytes, or the database cannot be opened.

### Security digest

An admin hears about password guessing and odd account changes from a weekly digest and a same-day alert (#717), not by reading the audit trail.
Both come from the agents' host (`scripts/agents/security.sh`, `go run ./cmd/security`, pure rules in `internal/security`), as Mac notifications; the monitor has no new page or route for them.
They read the audit trail (above) and its archives, never write it, and hold counts only: never a password, token, reset link, IP address or a person's name.
They read the trail rather than `login_failures`, because failures there are deleted after 15 minutes and a weekly count needs the history.
The weekly digest of the last ISO week (Monday to Sunday, UTC), per environment:
- the audit chain first: "Audit chain intact — N lines checked." or "Audit chain BROKEN: <file, line and reason>", which opens the digest before any count;
- for failed sign-ins, lockouts, failed password changes, passwords set by an admin, reset links issued, passwords reset with a link, role changes, users created, users deleted by an admin, accounts deleted by their owner, sessions ended and signed out everywhere: the count this week and the week before;
- usernames locked out this week, and distinct sources (IP addresses) signing in this week, as counts;
- the date of the last change of the admin role (any line that gave or took it, the first-admin setup included);
- "No data yet" when the trail has no line.
It is one notification per environment ("Security 2026-W45: chain intact; 12 failed sign-ins (3 the week before), 2 lockouts (0), 0 role changes (0).") and the "Security" section of that week's usage report.
The alert rules, fixed values in `internal/security/alerts.go`, not settings:
- at least 10 failed sign-ins for one username within any hour: one alert per username and UTC day, with the most in any hour;
- 3 or more lockouts in one UTC day: one alert per day;
- any role change to admin outside the first-admin setup (a `role_changed` to admin, or an admin creating another admin; the first start's own admin is the setup): one alert per change, with its date and time.
The check runs at most every 30 minutes over the last 48 hours of lines, so an alert reaches the admin the same day.
Its text states the count and the rule that fired and says the Audit page names it; the IDs of alerts raised are kept (with a hash of the username, never the name), so one pattern never alerts twice.
