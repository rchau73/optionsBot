# Sign-in

Part of the [monitor README](../README.md).

## Sign-in

People sign in at `/login` with a username and password; `proxy.js` sends every page without a session to `/login?next=…` and answers 401 on every `/api` route except `/api/auth/login`, `/api/auth/refresh` and `/api/auth/reset`; the `/reset` page (a reset link, below) opens without a session too.
The close and rebalance routes check the person again themselves (Roles, below).

### Users

SQLite (`sql.js`, WebAssembly — no native build in the image) in `data/monitor/auth.db` (`AUTH_DB_PATH`), a `monitor-auth` Docker volume per environment: stable and mvp have separate users.
Table `users`: username (3–64 of `a-z 0-9 . _ -`, case-insensitive), argon2id hash, role `admin`/`manager`/`user` (enforced: Roles, above), email, created, last sign-in

### Passwords

argon2id (`hash-wasm`; 19 MiB, 2 passes), at least 12 characters.
An unknown user is checked against a dummy hash, so it takes as long as a wrong password and gets the same 401 `invalid username or password`

### Access token

JWT HS256 (`jose`), 15 min, cookie `access_token_<env>` (`<env>` = `MONITOR_DEPLOYMENT`, so stable and mvp on one host keep separate sessions, #368; plain `access_token` without it; HttpOnly, SameSite=Lax, path `/`), signed with `AUTH_SECRET`; carries the user id, name, role and `sid` (its session)

### Refresh token

32 random bytes, only its SHA-256 stored, 14 days, cookie `refresh_token_<env>` on path `/api/auth`; reusable until logout (several tabs).
A password change, a role change or a deletion ends every session of that user

### Silent refresh

`lib/api/http.js` `apiFetch`: the monitor's own 401 (header `x-monitor-session: required`) → one refresh shared by every waiting request → one retry; when that fails the page goes to `/login` and comes back afterwards.
A bot's 401 passed through a proxy route is shown as an error, never a sign-out

### Password change

`POST /api/auth/password` `{"current", "next"}` (#212), the header's **Account ▾ → Change password** form (current, new, confirm; shows the 12-character rule before sending).
Live session required (re-read like a state-changing route; no session → 401, the service token never reaches it), same-origin JSON only.
`next`: at least 12 characters and not `current`, else 400 with the reason (`Too short: at least 12 characters`, …).
A wrong `current` → 401 `Current password is wrong` and counts as a failed sign-in of that username (Lockout, below: 5 lock it for sign-in and for this route, which then answers 429).
Success ends **every** session of the person, then opens a new one for this browser (new access and refresh cookies): other browsers' next refresh fails and they go to `/login`.
Their access token, being a stateless JWT, still reads the monitor for up to 15 min; their close, rebalance and password change are refused at once (the session is gone)

### Reset link

For a person who forgot their password, no e-mail needed (#307): an admin's **Reset link** on the Users page → `POST /api/admin/users/<id>/reset-link` → `{"url", "expires_at"}`, shown **once** with a copy button; the admin hands it over by any safe channel. 32 random bytes, only their SHA-256 stored (`reset_tokens` in `auth.db`: `token_hash`, `user_id`, `expires_at`, `created_by` = the admin, `created_at`, `used_at`), valid **30 min**, **single use**; a new link voids the person's older ones; deleting the person deletes their links; not for your own account (409).
The person opens `/reset?token=…` (public; `<meta name="referrer" content="no-referrer">`; the token is dropped from the address bar), types the new password twice and sends `POST /api/auth/reset` `{"token", "next"}` (public, same-origin JSON, under the per-IP sign-in limit: 429 with `Retry-After`).
`next` follows the 12-character rule (400 with the reason, the link stays usable); an unknown, used, expired or replaced link gets the one 400 `link expired or already used` and nothing changes.
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

one JSON line per sign-in, failure (reason, `locked`), lockout, rate limit, sign-out, first admin, user change (`user created`, `role changed`, `password reset` with `event: password_reset`, `reset link issued` (with `expires_at`), `signed out everywhere`, `user deleted`, each with `by`), reset by link (`password reset by link`, `event: password_reset`, `via: link`, the person, `by` = the admin who issued it, IP; a refused link is a WARN `reset by link refused` with the IP only), password change (`password changed`, `event: password_changed`, or `password change failed`) and refused action, with username and IP — never a password, hash, token or `AUTH_SECRET`

### Audit

`data/monitor/audit.jsonl` beside the user database (`AUDIT_PATH`; off for an in-memory database), append-only, owner-only: one JSON line `{time, action, username, role, …}` per `signed_in`, `sign_in_failed` (reason), `locked`, `signed_out`, `password_changed`, `password_change_failed` (reason `password` or `locked`), `user_created` (`to` = the role), `role_changed`, `password_reset` (an admin set another person's password, #306), `reset_link_issued` (with `expires_at`, #307), `user_deleted`, `signed_out_everywhere` (each with `target`; `username` and `role` are the admin's), `password_reset_by_link` (`username` and `role` are the person's, `by` the admin who issued the link, `ip`), the analytics retention lines `events_pruned`, `events_deleted`, `events_exported` and their `_failed` forms (counts only, never an event; see "Analytics"), `survey_answers_deleted` / `survey_answers_delete_failed` (counts only, never an answer), `analytics_consent_changed` (`consent` true or false), `survey_answered` (`survey_id` only, never the codes or text, #490), `nps_survey_changed` (`change`: shown, opt_out or opt_in, #490), `answers_survey_shown` (the four answers card was shown, nothing else, #498) and `manual_close` / `manual_rebalance` (bot, position ids or dry run, the bot's status and, for a close, what it filled).
Never a password, hash or token (nor a reset link).
Admins read it on the Audit page (below)

The first start with no users creates an admin from `ADMIN_USERNAME` / `ADMIN_PASSWORD` / `ADMIN_EMAIL`; later starts never touch existing users.
The server exits at start (`instrumentation-node.js`) when `AUTH_SECRET` is missing or shorter than 32 bytes, or the database cannot be opened.
