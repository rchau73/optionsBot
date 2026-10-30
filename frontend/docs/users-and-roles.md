# Users and roles

Part of the [monitor README](../README.md). Who may do what, and the Users page that manages them.

## Roles (#171, PRD §3)

- See the monitor
  - admin: ✓
  - manager: ✓
  - user: ✓
- Manual close, manual rebalance (and its dry run)
  - admin: ✓
  - manager: ✓
  - user: — (403)
- Create, edit and delete users; reset a password or issue a reset link (Users page, `/api/admin/users`)
  - admin: ✓
  - manager: — (403)
  - user: — (403)
- Read the audit trail (Audit page, `/api/admin/audit`)
  - admin: ✓
  - manager: — (403)
  - user: — (403)
- Read the weekly usage report (Analytics page, `/api/admin/analytics`)
  - admin: ✓
  - manager: — (403)
  - user: — (403)

`admin` satisfies `manager` satisfies `user` (`lib/auth/roles.js`).

### The role matrix (#490)

Every route that changes state is one line of `lib/auth/routeRoles.js` (`STATE_CHANGING_ROUTES`): the roles that may call it (`anonymous` for sign-in, refresh and the reset link) and the audit action an allowed call writes, or why it writes none.
`__tests__/role-matrix.test.js` calls each route through `proxy.js` and its handler as admin, manager, user, with no session, with the read-only `MONITOR_API_TOKEN` and with an expired session.
A signed-in person of the wrong role gets 403, the others 401, and nothing is written; an allowed call leaves its audit line with the person's username and the time.
A route file exporting `POST`, `PUT`, `PATCH` or `DELETE`, or a non-GET route of the bot's API, that the table does not list fails the test by name: a new state-changing route needs its line first.
Checked on the server, in the route handlers — not by hiding buttons (a user still sees them; the route answers 403 `manager role required` and the bot receives nothing).
**State-changing routes re-read the person from the database** (`requireRole` in `lib/auth/server.js` → `AuthService.actingUser`): the user must still exist, their **current** role must allow the action (else 403), and the access token's session (`sid`, its refresh token's SHA-256) must still be live (else 401).
So a demoted manager is refused at once, and a deleted user or one signed out everywhere too — not 15 minutes later.
Read routes trust the token.
An access token from before #171 has no `sid`: its first close answers 401, the page refreshes into a new token and retries.

The route sends the person to the bot with the request — `"actor": {"user": "bob", "role": "manager"}` beside `BOT_ADMIN_TOKEN` — and the bot logs and journals it (the close's `actor` and `detail` "manual close from the monitor by bob (manager)", `rebalance_manual`'s `actor`).
User management is in `AuthService` (`createUser`, `changeRole`, `resetPassword`, `deleteUser`, `signOutEverywhere`: never zero admins, never delete yourself, every change ends that user's sessions), behind the Users page below.

## Users page (#306)

`/admin/users`, linked as **Account ▾ → Users** for admins only — the link is a convenience, the routes decide: a manager or a user gets 403 from every one of them (`requireRole(admin)`, the person re-read from the database like close and rebalance).
The page lists every person (username, role, email, created, last sign-in) and has an **Add user** form; each row changes the role, resets the password, signs the person out everywhere or deletes them, each after a confirmation that says what follows.
Your own row cannot be deleted or reset (use **Change password**).

### Routes

#### `GET /api/admin/users`

**Does:** `{"users": [{id, username, role, email, created_at, last_sign_in_at}]}` (ISO times, `null` = never); never a hash

**Refusals:** 403 not admin

#### `POST /api/admin/users` `{"username", "password", "role", "email"?}`

**Does:** `AuthService.createUser` → 201 `{"user"}`; audited `user_created`

**Refusals:** 400 username rule (3–64 of `a-z 0-9 . _ -`), password under 12 characters, unknown role, malformed email · 409 username taken

#### `PATCH /api/admin/users/<id>` `{"role"}`

**Does:** `changeRole`; the user's sessions end; audited `role_changed`

**Refusals:** 400 unknown role · 404 · 409 the last admin

#### `POST /api/admin/users/<id>/password` `{"password"}`

**Does:** `resetPassword`: sets it and ends every session of that user (each browser's next request that re-reads the session, and its refresh, answer 401); audited `password_reset`

**Refusals:** 400 under 12 characters · 404 · 409 yourself

#### `POST /api/admin/users/<id>/reset-link` `{}`

**Does:** `issueResetLink` → `{"url", "expires_at"}` once (Reset link, above); audited `reset_link_issued`, never with the token

**Refusals:** 404 · 409 yourself

#### `POST /api/admin/users/<id>/signout` `{}`

**Does:** `signOutEverywhere`; audited `signed_out_everywhere`

**Refusals:** 404

#### `GET /api/compare` (#302)

**Does:** `{"reports": [{id, underlying, base, proposed, from, to, ran: {backtest, replay, stress}}]}` from `COMPARE_DIR/<id>/report.json`: newest first, 50 at most, file names only (never paths), a folder that is not a report time or whose report cannot be read skipped (WARN with the ids); missing or empty dir → empty list.
Manager or admin; every read logged with the person; read-only

**Refusals:** 401 no session · 403 role user

#### `GET /api/compare/<id>` (#302)

**Does:** that `report.json` exactly as stored, `disclaimer` included.
`<id>` is a folder time (`20261012T093000Z`)

**Refusals:** 400 any other id (`..`, a slash, …; the disk is not touched) · 404 no report · 500 unreadable or malformed JSON (a log line with the id, never the content) · 401/403 as above

#### `GET /api/lab/runs` (#539)

**Does:** `{"runs": [{id, time, underlying, label, changed, backtest, replay, stress_ran, windows, cliffs}]}` from the same `COMPARE_DIR/<id>/report.json` as `/api/compare`, newest first, 50 at most (`lib/lab.js`).
`label` is "backtested, not a forecast"; `changed` is the report's `diffs` (key, base, proposed).
`backtest` and `replay` are `{base, proposed}`, each `{return_pct, worst_week_pct, max_drawdown_pct, fees_pct}`: percentages only, drawdown and fees as % of the run's starting equity, so the risk sits beside the return; null when the source did not run or the config's run failed.
`windows` is the proposal's wins per window (`-windows`), `cliffs` the number of cliffs (`-neighbours`); null without them.
Manager or admin (`ROLE_GUARDED_READS` in `lib/auth/routeRoles.js`, called as every caller by the role-matrix test); read-only, applies nothing.

**Refusals:** 401 no session · 403 role user

#### `GET /api/lab/runs/<id>` (#539)

**Does:** that run's list row, `report` (the `report.json` content, its `disclaimer` included) and `analysis`.
`analysis` is "withheld pending legal review", and `analysis.json` is not read, while `L10_ANALYSIS_GATE_OPEN` in `lib/lab.js` is false: a constant, no setting, pinned to false by `__tests__/lab-route.test.js` until counsel's answer to legal question L10 is recorded in `docs/legal/preliminary-answers.md`.

**Refusals:** 404 an id that is not a folder time (`../secrets`, a slash, …; the disk is not touched) or no run · 500 unreadable or malformed JSON (a log line with the id, never the content) · 401/403 as above

#### `DELETE /api/admin/users/<id>` `{}`

**Does:** `deleteUser` (and the person's analytics events, "Analytics" below) → `{"events_removed"}` or `{"events_error"}`

**Refusals:** 404 · 409 yourself or the last admin

### Notes on the routes

All state-changing ones are same-origin JSON only (403 cross-origin, 415 without `application/json`); an ended session is 401.
Every change is one audit line and one JSON log line naming the admin and the person — never a password.
`last_sign_in_at` is a column of `users`, added to an older `auth.db` at start and set by each successful sign-in.
