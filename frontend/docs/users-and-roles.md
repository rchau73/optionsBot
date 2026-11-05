# Users and roles

Part of the [monitor README](../README.md).
Who may do what, and the Users page that manages them.

## Roles (#171, PRD §3)

- See the monitor
  - admin: ✓
  - manager: ✓
  - user: ✓
- Manual close, manual rebalance (and its dry run)
  - admin: ✓
  - manager: ✓
  - user: — (403)
- Switch the futures hedge experiment on or off (Account page, `POST /api/bots/<bot>/hedge`, #657; testnet only, the bot refuses it live)
  - admin: ✓
  - manager: — (403)
  - user: — (403)
- Read the strategy-change lab's runs, check a proposed setting value and request a comparison run (`/api/lab/runs`, `POST /api/lab/validate`, `POST /api/lab/requests`; nothing is applied)
  - admin: ✓
  - manager: ✓
  - user: — (403)
- Create, edit and delete users; reset a password or issue a reset link (Users page, `/api/admin/users`)
  - admin: ✓
  - manager: — (403)
  - user: — (403)
- See your own sessions and sign out your other browsers (Account ▾ → Active sessions, `/api/auth/sessions`, #584); never another person's
  - admin: ✓
  - manager: ✓
  - user: ✓
- Read the audit trail and check its hash chain (Audit page, `/api/admin/audit`, `/api/admin/audit/verify`, #619)
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
Read-only routes that only some roles may call are in `ROLE_GUARDED_READS` of the same file and called as every caller too: `GET /api/admin/audit/verify` (admins only, #619), `GET /api/auth/sessions`, `GET /api/lab/runs` and `GET /api/lab/runs/<id>`.
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

#### `GET /api/auth/sessions` (#584)

**Does:** `{"sessions": [{id, browser, created_at, last_used_at, current}]}`: the signed-in person's own live sessions, last used first, `current: true` on the one asking (`AuthService.listSessions`); never a token, hash or address.
Any role; the session is re-read like a state-changing route (`requireRole`), and the role matrix calls it as every caller (`ROLE_GUARDED_READS`)

**Refusals:** 401 no session, an ended one or the service token

#### `DELETE /api/auth/sessions/<id>` `{}` (#584)

**Does:** `endSession`: deletes one of the person's own other sessions (its next refresh fails); audited `session_ended` with `target_session` (the id's first 8 characters).
Any role; in the role matrix (`STATE_CHANGING_ROUTES`)

**Refusals:** 404 someone else's or an unknown id (nothing changes) · 409 the session in use ("use Sign out")

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
`requests` (#641) are the run requests (`POST /api/lab/requests`) whose report is not in `runs` yet, newest first, 50 at most: `{id, requested_at, requester, underlying, settings, status, updated_at, run_id, reason}`.
`status` is `queued`, `running`, `failed` (with `reason`), `expired` (queued over a day ago; shown so before the runner marks the file) or `done` while its report cannot be read yet; a done request whose report is listed appears only in `runs`.
Manager or admin (`ROLE_GUARDED_READS` in `lib/auth/routeRoles.js`, called as every caller by the role-matrix test); read-only, applies nothing.

**Refusals:** 401 no session · 403 role user

#### `GET /api/lab/runs/<id>` (#539)

**Does:** that run's list row, `report` (the `report.json` content, its `disclaimer` included) and `analysis`.
`analysis` is "withheld pending legal review", and `analysis.json` is not read, while `L10_ANALYSIS_GATE_OPEN` in `lib/lab.js` is false: a constant, no setting, pinned to false by `__tests__/lab-route.test.js` until counsel's answer to legal question L10 is recorded in `docs/legal/preliminary-answers.md`.

**Refusals:** 404 an id that is not a folder time (`../secrets`, a slash, …; the disk is not touched) or no run · 500 unreadable or malformed JSON (a log line with the id, never the content) · 401/403 as above

#### `POST /api/lab/requests` `{"settings": [{"key", "value"}], "underlying"?}` (#641)

**Does:** asks for a comparison run of the shipped config of that underlying (BTC by default) against the same config with these values (`lib/labrequests.js`).
Each value is checked like `POST /api/lab/validate` (a key with a list index, `dte_delta_matrix[0].dte`, as its catalogue key; `[]` means every element); one refused value refuses the whole request, with the reason, and nothing is written.
A valid request becomes `COMPARE_DIR/requests/<id>.json` (mode 0600; id = UTC time and a random suffix): `id`, `requester`, `requested_at`, `underlying`, `settings`, `status: "queued"`, `updated_at` — no secret, no amount.
201 with that record and `queued: true`.
The runner (`scripts/lab_runner.sh` → `cmd/labrun`, one at a time) runs `cmd/compare -set … -neighbours <the first number>` over the last 7 whole UTC days and writes `running`, then `done` with `run_id` or `failed` with a short `reason`; a request still queued after a day becomes `expired`.
Manager or admin (`STATE_CHANGING_ROUTES`); the audit line `lab_request` names the person, the request id, the underlying and the keys, never the values.
It writes no config and applies no setting.

**Refusals** (every refusal answers `queued: false`):
- 400 no settings, more than 5, a key listed twice or not a setting key, a value outside its range or a cross rule (`error` "<key>: <reason>", `reasons`), a fixed key or an unknown underlying.
- 404 an unknown key · 429 three requests of yours already queued or running · 403 cross-origin.
- 413 a body over 4 KB · 415 not JSON · 500 the file could not be written · 401/403 as above.

#### `POST /api/lab/validate` `{"key", "value", "underlying"?}` (#586)

**Does:** says whether a proposed value may be tried and who must approve a new default, from `content/lab-settings.json` (`lib/labvalidate.js`); it writes and applies nothing.
That file is generated by `go run ./cmd/paramdocs` from the Go catalogue (`config.Tunables()`) and the shipped configs (`config_btc.yaml` for BTC, the default, and `config_eth.yaml` for ETH); `tests/lab_settings_test.go` fails when it is stale and loads every cross-rule bound in it through `config.Load`.
Answer: `key`, `title`, `value`, `underlying`, `config` (the file), `valid`, `reasons` (plain words; `[]` when valid), `range` (`kind`, `min`, `max`, `step`, `unit`, `values` for a choice) and `cross_rules`.
It also carries `core_policy`, `policy`, `policy_note` ("changing the default needs the product owner", null when not core policy) and `tip` (`/api/help/tips`'s answer for the key, or null).
Last come `current` (the shipped config's value; a list for a `[]` key) and `applied` ("nothing was changed").
The setting's own range, step and kind are checked first, then the cross rules against the rest of that config (drift at most 75 % of every entry delta, exit above every entry delta, slot days to expiry above `rollout_dte`; a list key's value is checked as each element's).
Rules that depend on the whole list (the margin bands' "the lowest band starts at 0" and "no two bands start at the same percentile") are listed but not checked for one value: the answer says `cross_rules_checked: false` with `cross_rules_note`, and `Validate` checks them when the run's config loads.
Manager or admin (`STATE_CHANGING_ROUTES`, called as every caller by the role-matrix test, though it changes nothing); the audit line `lab_validate` names the key, the underlying and `valid`, never the value.

**Refusals:** 400 no key or no value, malformed JSON, an unknown underlying, or a key fixed in code (`fixed.go`: "not tunable: it is fixed in code (<value>)") · 404 an unknown key · 403 cross-origin · 413 a body over 2 KB · 415 not JSON · 401/403 as above

#### `DELETE /api/admin/users/<id>` `{}`

**Does:** `deleteUser` (and the person's analytics events, "Analytics" below) → `{"events_removed"}` or `{"events_error"}`

**Refusals:** 404 · 409 yourself or the last admin

### Notes on the routes

All state-changing ones are same-origin JSON only (403 cross-origin, 415 without `application/json`); an ended session is 401.
Every change is one audit line and one JSON log line naming the admin and the person — never a password.
`last_sign_in_at` is a column of `users`, added to an older `auth.db` at start and set by each successful sign-in.
