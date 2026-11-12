# Personal data of the accounts system

Part of the [monitor README](../README.md); the sign-in behaviour is in [Sign-in](sign-in.md), the audit file in [Audit](audit.md).
This is an inventory for counsel (PRD §11, data protection; questions L13, L14 and L22 of [the legal brief](../../docs/product/legal-brief.md)): facts, not legal advice.
It documents what the code does today and changes nothing.
Analytics events and surveys are inventoried by E13 in [the event plan](../../docs/product/analytics/event-plan.md) and [the surveys plan](../../docs/product/analytics/surveys.md); here only the tables that hold them in `auth.db` are listed.
No value, hash or username appears in this file: field names and rules only.
A retention marked **proposal** is a choice nobody has decided; the numbers without it are what the code does today.
`tests/personal_data_inventory_test.go` fails when a column of `auth.db` has no row below, or a row names a column that is gone.

All tables are in `data/monitor/auth.db` (one per environment, a Docker volume), readable by the monitor process and by whoever has the host or the volume; the admin CLI reads a copy.
No page or route returns a hash.

## `users`

One row per person.
Retention of the row: until the person (or an admin) deletes the account; **proposal**: no inactivity expiry.

| Column | Purpose | Retention |
| --- | --- | --- |
| `id` | Key that links the person's other rows | with the row |
| `username` | Sign-in name and what the audit and logs show | with the row |
| `password_hash` | argon2id hash to check the password; never the password | with the row; replaced at a password change |
| `role` | admin, manager or user: what the person may do | with the row |
| `email` | Contact address; optional; not used to send anything today | with the row; **proposal**: drop it if it stays unused |
| `created_at` | When the account was made | with the row |
| `updated_at` | Last change of the row | with the row |
| `analytics_consent` | The person's yes or no to analytics (empty = not asked) | with the row; deleted with it |
| `last_sign_in_at` | Shown to admins on the Users page | with the row; overwritten at each sign-in |
| `sign_in_count` | Decides when the survey card is due | with the row |
| `nps_last_shown_at` | Spacing of the monthly survey card | with the row |
| `nps_opted_out` | The person's "don't ask me again" | with the row |
| `answers_last_shown_at` | Spacing of the four-answers card | with the row |

## `refresh_tokens`

One row per signed-in browser (a session).
Readable by the person (browser name and times, through Active sessions) and never as a token.

| Column | Purpose | Retention |
| --- | --- | --- |
| `token_hash` | SHA-256 of the refresh token, to recognise it; the token is not stored | until sign-out, password change, role change, deletion or expiry |
| `user_id` | Whose session it is | same |
| `expires_at` | End of the 14 days | the expired row is removed at the next sign-in by anyone |
| `created_at` | When the browser signed in | same as the row |
| `public_id` | Random id the person sees in Active sessions | same as the row |
| `browser` | Browser name read from the User-Agent at sign-in; the header itself is not stored | same as the row |
| `last_used_at` | Last refresh, written at most once a minute | same as the row |

## `reset_tokens`

One row per reset link an admin issued; the link itself is shown once and not stored.

| Column | Purpose | Retention |
| --- | --- | --- |
| `token_hash` | SHA-256 of the link's token | removed when the same person gets a new link or any link is expired, and when the person is deleted |
| `user_id` | Whom the link is for | same |
| `expires_at` | End of the 30 minutes | same |
| `created_by` | Username of the admin who issued it | same; the audit line `reset_link_issued` also names the admin |
| `created_at` | When it was issued | same |
| `used_at` | When it was used (empty = not yet) | same |

## `login_failures` and `login_locks`

Kept in `auth.db` so a restart does not clear a lockout.
They hold a username as typed after normalising, which may be a name that is not an account.
The IP is not stored here; the per-IP limit lives in memory and a restart forgets it.

| Column | Purpose | Retention |
| --- | --- | --- |
| `login_failures.username` | Count failed sign-ins per username | removed after 15 min at the next failure, and at a successful sign-in |
| `login_failures.at` | When each failure happened | same |
| `login_locks.username` | Which username is locked | removed once the lock has ended, at the next failure |
| `login_locks.until` | When the lock ends | same |

## `survey_answers`

Answers to in-product surveys, kept beside the user; the content of E13's surveys is in its plan.
Never sent in an event or a log line.

| Column | Purpose | Retention |
| --- | --- | --- |
| `id` | Key of the answer | with the row |
| `user_id` | Whose answer it is (so it can be deleted with the person) | with the row; **proposal**: unlink answers after a period (question L16) |
| `survey_id` | Which survey | with the row |
| `codes` | The allow-listed coded answers | with the row |
| `free_text` | Optional free text, which may hold anything the person types | with the row |
| `created_at` | When it was given | with the row |

## `notification_reads` and `notification_settings`

State of the notification bell, per person.

| Column | Purpose | Retention |
| --- | --- | --- |
| `notification_reads.user_id` | Whose read mark | removed with the person |
| `notification_reads.item_id` | Which item was read | pruned after 90 days, like the items |
| `notification_reads.read_at` | When it was read | same |
| `notification_settings.user_id` | Whose setting | removed with the person |
| `notification_settings.kind` | Which kind of item | removed with the person |
| `notification_settings.enabled` | On or off | removed with the person |

## `account_links`

Which exchange accounts a manager may act on (#760): an admin sets them on the Users page; a manager with no row acts on every account of this deployment.
The account ids are the registry's opaque ids (`data/accounts_status.json`, #655): no key, money, position or name of an account holder.
Not in "Download my data" yet; an admin sees them on the Users page.

| Column | Purpose | Retention |
| --- | --- | --- |
| `user_id` | Which manager the link limits | removed when an admin removes the link, and with the person |
| `account_id` | The account the manager may act on | same |
| `created_by` | Username of the admin who added the link | same; the audit line `account_link_added` also names the admin |
| `created_at` | When the link was added | same |

## `audit.jsonl`

`data/monitor/audit.jsonl` beside the database: one JSON line per action with the time, the action, the username and role of the actor and fields of the action; append-only, owner-only, hash-chained (`prev`, `line_hash`, hashes of lines only).
Admins read it on the Audit page; nothing edits or deletes a line.
An admin, and nobody else, may also take a copy: **Export CSV** on that page (#702, [Audit](audit.md)) downloads the lines of the filters applied, at most 50,000.
The file is a copy of personal data that leaves the monitor: usernames, roles, actions and the fields of each line.
Once downloaded it is the admin's to keep safe, and this inventory's retention, deletion and hash chain no longer reach it.
Each export leaves an `audit_exported` line with the admin, the filters (which can name the person looked up) and the count, never the lines.
Never a password, hash, token or reset link; the survey and analytics lines carry counts and ids, never an answer or an event.
`ip` is set only on `password_reset_by_link`.

Retention: the file rotates to `audit.<UTC time>.jsonl` at `AUDIT_MAX_BYTES` (default 20 MB, #619) and **the app never deletes an archive**, so lines stay as long as the host keeps the files.
**Proposal**: a retention period for archives, decided with counsel (L14).

Actions recorded (the same list as `AUDIT_ACTIONS` in `lib/audit.js`):
- sign-in and sessions: `signed_in`, `sign_in_failed`, `locked`, `signed_out`, `signed_out_everywhere`, `session_ended`;
- passwords: `password_changed`, `password_change_failed`, `password_reset`, `reset_link_issued`, `password_reset_by_link`;
- users: `user_created`, `role_changed`, `user_deleted`, `account_deleted`;
- manager–account links (#760): `account_link_added`, `account_link_removed` (the admin, the manager as `target` and the `account_id`);
- trading actions from the monitor: `manual_close`, `manual_rebalance`, `futures_hedge_switched`;
- the trail itself: `audit_exported`;
- a person's own data: `my_data_exported` (#750: who downloaded their file and the count per section, never the content);
- the lab:
  - `parameter_change_previewed` (#682: which manager read what a confirm would change, with the account, the run and both values, as evidence of who saw it; never the confirm token; kept as every other line of this file);
  - `parameter_change_recorded` (#741: which manager confirmed a change, with the account, the run, both values and the token's hash prefix; the same username and role are in that run's record, `data/compare/applied/<id>.json`, kept with the run);
- analytics and surveys: `events_pruned`, `events_prune_failed`, `events_deleted`, `events_delete_failed`, `events_exported`, `analytics_consent_changed`, `survey_answers_deleted`, `survey_answers_delete_failed`, `survey_answered`, `nps_survey_changed`, `answers_survey_shown`.

## The security digest and alerts

The weekly security digest and the same-day alerts (#717, [Sign-in](sign-in.md) "Security digest") are built on the agents' host from a copy of the audit trail, which is removed once read.
They hold counts only: no username, IP address, password, token or reset link; a locked-out username appears only on the Audit page, for an admin.

| Store | Purpose | Retention |
| --- | --- | --- |
| Digest files, `data/agents/security/<week>/<env>/digest.md` and `.json` | The week's counts, the chain check and the date of the last admin-role change; the md is also the usage report's "Security" section | as long as the host keeps the agents' state; nothing deletes them today |
| Alert IDs raised, `data/agents/security/<env>.alerts` | So one pattern never alerts twice: the rule, the UTC day or time and, for failed sign-ins and a promotion, a 12-character SHA-256 prefix of the username (a pseudonym: whoever holds the user list can match it), never the name | same; grows by one line per alert |
| Notifications | The digest's one-line headline and each alert's text, shown on the Mac that runs the agents | the Mac's notification centre |

## The monitor's JSON logs

One line on stdout per sign-in, failure, lockout, rate limit, sign-out, user change, reset, password change and refused action, with the username and the IP; the list is in [Sign-in](sign-in.md) "Logs".
Never a password, hash, token or `AUTH_SECRET`.
Retention is the Docker log driver's setting on the host, not the app's; **proposal**: set a rotation and a maximum age in compose, decided with counsel (L14).
Deleting an account does not edit the logs.

## The service token

`MONITOR_API_TOKEN` is a secret of the environment, not a person's data: it is in the `.env` of the host and the scripts that read it, with no name attached.
It reads the bots' data without a session and never a page, a close or a rebalance.

## What "download my data" holds

Every signed-in person, of any role, can download what this inventory says the monitor keeps about them (#750, the right of access): Account ▾ → **Download my data**, `GET /api/auth/my-data` ([Users and roles](users-and-roles.md)).
The file is one JSON document, built by `lib/auth/mydata.js` from the person's own rows only:
- `account`: their `users` row, every column except `password_hash` (`ACCOUNT_COLUMNS`; a test fails on a new column that is in neither list);
- `consent`: their analytics answer in words (yes, no, not asked);
- `sessions`: their live sessions as Active sessions shows them, never a token, a hash or an address;
- `survey_answers`: their `survey_answers` rows with any text they wrote;
- `analytics_events`: their stored events, as the admin's export finds them (`lib/analytics/userrequest.js`);
- `audit_lines`: the lines of `audit.jsonl` where they are the `username`, without `ip`, `prev` or `line_hash`, the newest 50,000 at most.
Lines where the person is only the `target` of an admin's action are left out until counsel answers L14.
A section that cannot be read is named `unavailable` with its reason, never dropped.
Once downloaded the file is the person's to keep safe; this inventory's retention and deletion no longer reach it.
Each download writes one `my_data_exported` line with the person and the count per section, never the content.
A person may download 3 times an hour; a 4th is refused (429) and writes nothing.
The admin's CLI export (`events_exported`) and the Audit page's CSV export are unchanged.

## What "delete my account" removes and keeps

The self-delete (#543) and an admin's deletion run the same steps:
- removed: the `users` row with its consent, every session, reset links, account links, notification state (`notification_reads`, `notification_settings`), the person's analytics events and their `survey_answers`;
- an event or survey step that fails is reported (`events_error`, `survey_answers_error`); orphan survey answers are removed at the next start;
- kept: the audit lines that name the person (as `username` or as `target`, including `account_deleted`) and the log lines, because the trail is the evidence of who did what — **proposal**, see the questions;
- not touched: exchange accounts and funds, which are not the monitor's.

## Questions for counsel

1. L13: the lawful basis of each store above (account, sessions, audit, logs), and which need consent.
2. L14: how long may the audit archives and logs keep lines that name a person who deleted their account?
   **Proposal**: keep the lines, never the account row; or replace the name by a neutral id at deletion.
3. L14: a retention period for audit archives and for the Docker logs (today none for the archives).
4. L14: the `email` column is optional and unused; keep, or stop collecting it?
5. L16: may survey answers stay linked to an account, and for how long?
6. L22: the breach duties for the user database and the audit trail, and the minimum security the terms promise.
7. L14: exporting audit lines (#702): the lawful basis for handing a copy to a person or an authority, how long an exported file may be kept, and who may receive it.
   **Fallback in place**: only admins may export, and every export is an audit line.
8. L14, L16: "Download my data" (#750): the notice wording, and whether audit lines naming the person only as an admin's `target` may be in their file.
   **Fallback in place**: only the person's own actions.
9. PRD §11: manager–account links (#760) let a manager act only on the accounts linked to them; may a manager who is not the account owner (an adviser) be linked, and must their links be in their "Download my data"?
   **Fallback in place**: no link is set for an adviser until counsel answers.
