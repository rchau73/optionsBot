# Analytics

Part of the [monitor README](../README.md). The Analytics page (usage and delivery) and the events behind it.

## Analytics page (#445)

`/admin/analytics?env=stable|mvp&week=YYYY-Www`, linked as **Account ▾ → Analytics** for admins only (the person menu's admin items are `ADMIN_PAGES` in `lib/menu.js`; anyone else sees "Only an admin reads the analytics", the route answers them 403; two topic tabs, **Usage** and **Delivery (DORA)**, below).
It shows the weekly usage report `cmd/usage` wrote (`usage.sh`, Mondays) **as written — nothing is recomputed in the browser**, so the page, the E13 post and the drop issue agree: the header (environment switch, week arrows, "counts of people and actions only"), a **small-sample** note under 10 active people (`usage.DropMinBase`), **Needs your attention** (each of the report's `drops`, flagged assistant answers with the path of #438's weekly review, never-opened screens and panels — tags DROP / FLAG / NEVER / OK carry text, never colour alone; or one "Nothing dropped" line), eight cards Q1–Q8 (two columns, one on a phone; Q8 counts "What we do best (9–10)" and "What to improve first (0–8)" apart, `why_best`/`why_improve`), and the data behind it (report file and time, accepted/rejected events, this monitor's consent counts, "this week isn't in a report yet").
States: analytics off (`ANALYTICS_SALT` unset: a setup checklist, never the value, no figure), no report yet, no report for that week, no events in the week, loading, error.
The page counts itself as `screen_viewed {screen: analytics}` (`lib/analytics/screens.js`).
Components: `UsageAnalytics` (fetch, header, states), `UsageCards` (attention, cards, data); pure helpers in `lib/analytics/usageView.js`, file reading in `lib/analytics/reports.js`.

### Routes

#### `GET /api/admin/analytics?env=&week=`

**Does:** `{"off", "env", "envs", "current_week", "weeks", "week", "report", "file", "written_at", "consent"}`: `report` = `<USAGE_REPORTS_DIR>/<env>/<week>.json` as stored (the events file's path cut to its name), `weeks` the reported weeks oldest first, no `week` → the latest, none at all → `week`/`report` null; `consent` `{yes, no, unasked}` users for this monitor's own environment (`MONITOR_DEPLOYMENT`; the default `env`), null for the other one.
`USAGE_REPORTS_DIR` defaults to `data/reports`; compose mounts each clone's `data/monitor/reports` read-only at `/app/data/reports/<env>`.
`ANALYTICS_SALT` unset → `{"off": true}` only

**Refusals:** 400 unknown env, a malformed week · 403 not admin · 404 no report for that week (same fields, `report` null) · 500 an unreadable report (logged)

## Delivery (DORA) topic (#473)

`/admin/analytics?topic=delivery&env=stable|mvp` (admins only; no `env` → **stable**, production, whatever the monitor's own environment) shows the delivery report `cmd/dora` writes once a day (`scripts/agents/dora.sh`, #470) **as written — nothing is recomputed in the browser**, so the page and the Monday post on the E6 epic agree.
Times are UTC, as the report's.

- **Toolbar:** the topic tabs (`components/AnalyticsTopics.jsx`), the **stable · production | mvp · test** switch (links), **Copy link** (this view's address) and **Weekly summary** (the text `dora.sh` posts on E6 for the last week that ended: each week's `summary` in `dora.json`).
- **Headline:** one line, the median time from merge to live on mvp and on stable over the report's PRs ("Every change reaches mvp in minutes.
  Stable waits about 5 days for the 3-day soak and a release.").
- **Four tiles** (`components/DeliveryTiles.jsx`), this week so far: **Deployment frequency** (mvp deploys a day, stable releases a week), **Lead time** (merge → live, median), **Change failure rate**, **Time to restore**.
  Each has its value, a band chip in words (ELITE, HIGH, MEDIUM, LOW from the report; NO FAILURE when nothing failed to measure; NOT YET when there are too few samples or the week is not recorded), a context line, four weeks of bars (a week before recording is dimmed and says "n/r", never 0) and a "what this counts" line.
- **Release train** (`components/ReleaseTrain.jsx`): the last 8 days, a stable lane (a mark per release, `releases`) and an mvp lane (a tick per healthy deploy, `mvp_deploys`).
  The opened PR has a dot on each: filled where it went live, hollow at the right edge while it waits; one line says where it went live.
  The card scrolls sideways inside itself on a phone.
- **What went live** (`components/DeliveryTable.jsx`): every merged PR, newest first — PR, title, epic, merged, live on mvp ("29 Oct 00:49 · +4 min"), live on stable ("28 Oct 11:43 (v0.3.0)", or "waiting · soak ends …" for 3 days after its mvp deploy, then "waiting · soak over, waits for the next release") and a result chip: **ROLLED BACK** (a revert of the PR, or an mvp rollback / failed deploy of its merge commit), **ON STABLE**, **LIVE ON MVP**, **NOT LIVE**.
  Filters All / On mvp only / On stable, a search by PR, epic or date, sortable columns under a sticky header.
  Opening a row (`components/PrTimeline.jsx`) shows commit → merge → mvp → stable as one bar with a legend, the failures it caused, a link to the PR, a link to the Activity page with the window from 1 h before to 24 h after live on mvp (named in the link: the Activity page does not filter by time yet), and "the P&L chart with this PR marked: later" (#475).
- **States:** no `dora.json` yet (what will appear, and that `dora.sh` makes it with no setup; no figure), first days of recording (NOT YET chips, "n/r" bars), a failed deploy (ROLLED BACK, counted once in the failure tile, its time to restore), loading, error.
- The view is `screen_viewed {screen: analytics, topic: delivery}` (`topic: usage` on the Usage topic; `lib/analytics/screens.js` `screenViewOf`).
  Pure helpers: `lib/analytics/deliveryView.js`; file reading: `lib/analytics/delivery.js`.

### Routes

#### `GET /api/admin/analytics/delivery`

**Does:** `{"report", "file", "written_at"}`: `report` = `<DORA_DIR>/dora.json` as stored (both environments; the page's switch picks one), `null` before `cmd/dora`'s first run.
`DORA_DIR` defaults to `data/delivery`; compose mounts `<envs>/history/delivery` read-only at `/app/data/delivery`

**Refusals:** 403 not admin · 500 an unreadable report (logged)

## Analytics (#193)

Usage events from the event plan (`docs/product/analytics/event-plan.md`) — what a person looked at or did, **never an amount, size, price, instrument, secret or email**.
Nothing leaves the machine: no third-party tracker, SDK or script.

- **Sender:** `lib/track.js` `track(event, props)` posts to the monitor's own `POST /api/events`, without waiting and without ever throwing.
  It sends nothing until `setAnalyticsConsent(true)`; "no" or not asked yet (the default) means nothing at all is sent.
  Events outside the plan are dropped in the page too.
  The session id is random (32 hex), new after 30 min without an event.
  The consent can be set through `/api/consent` (below); the consent notice that asks it waits for E13's prototype.
- **Page views** (#424): `components/ScreenTracker.jsx`, in the root layout for a signed-in person, reads the person's stored answer once (`GET /api/consent`, `fetchConsent` in `lib/api/survey.js`: anything but a boolean is null), hands it to `setAnalyticsConsent`, then sends one `screen_viewed` per page shown — `screen` is the menu item's key with `_` for `-` (`lib/analytics/screens.js` `screenOf`: `overview`, `income`, `worst_case`, `positions`, `how_it_works`, `activity`, `account`, `details`; a page outside the menu, such as admin, sends none).
  A yes is the only answer that sends; until the read returns nothing is sent.
  `__tests__/analytics-screens.test.jsx` fails, naming the page, when a page of `lib/menu.js` has no `screen` value in the plan (`pagesWithoutScreen`): a new page needs its value in `docs/product/analytics/event-plan.md` §4, `lib/analytics/plan.js` and `internal/usage/plan.go`.
- **Consent (#363):** the person's own answer, on their row in `auth.db` (`users.analytics_consent`; null = never answered).

  - `GET /api/consent`
    - Does: `{"consent": true | false | null}` for the signed-in person (re-read from the database, `no-store`)
    - Refusals: 401 no session
  - `POST /api/consent {"consent": true | false}`
    - Does: stores it and answers `{"consent", "note"}`; audited as `analytics_consent_changed` (the person and the new value only).
      *No* stops collection at once (`/api/events` and `/api/survey` answer 403); the note says events already stored stay until the person asks for their deletion (the CLI below)
    - Refusals: 401 no session · 403 cross-origin · 415 not JSON · 400 any other body (only the one boolean field)
- **Survey answers (#363):** `POST /api/survey {"survey_id", "session_id", "codes": {…}, "text"?}` — signed-in, same-origin JSON ≤ 4 KB, consent yes (403 otherwise, every survey: `surveys.md` §1–§2 show their cards only with consent and §3 stores no exit answer without it until counsel answers C3), `ANALYTICS_SALT` set (503).
  `lib/analytics/surveys.js` checks it against `surveys.md`: a known `survey_id`; only that survey's codes (`nps`: `score` required, `reason_code` of the NPS codes; `answers`: `came_for`, `hardest_to_find`, at least one; `exit`: `reason_code` of the exit codes); `text` only on NPS or with the exit's `other`, ≤ 500 characters.
  Anything else is refused whole: 400, nothing stored, a WARN naming the field, never the value.
  The codes and text go to the `survey_answers` table of `auth.db` (user row id, `survey_id`, codes as JSON, `free_text`, `created_at`); the `survey_answered` event carries the codes only and passes the collector's allow-list.
  When the event cannot be written the answer is removed again (500). 204 when stored.
  An NPS `reason_code` must be of the list asked for its score (#446: what works for 9–10, to improve for 0–8, both with `other`).
  The NPS card in the assistant panel sends it.
- **Monthly NPS in the assistant (#446, `surveys.md` §1):** `GET /api/survey/nps` → `{"due", "reason", "opted_out"}`, decided on the server from the user's row (consent yes, account ≥ 14 days, ≥ 3 sign-ins, not shown in 30 days, not opted out; `lib/analytics/nps.js`).
  `POST /api/survey/nps {"action": "shown" | "opt_out" | "opt_in"}` (same-origin JSON) records the day the card was shown — only while due, else 409 — or the person's switch; INFO log `nps survey state`.
  The page adds two rules: no card right after a losing week (the sign of each bot's `/api/performance?range=7d`; a loss or an unreadable sign postpones; never sent or stored) and none while a manual close or rebalance dialog is open (`lib/dialogs.js`).
  While due, Ask carries a static "1" badge and a one-line hint (accessible name "Ask (one optional question waiting)"); opening the panel shows `components/NpsCard.jsx` at the top of the Ask tab and sends `survey_shown {nps}` once.
  Step 2 shows only its band's chips; Send / Send the score only go through `POST /api/survey` (the comment stays in `auth.db`); ×, Not now or closing the panel send `survey_dismissed`; "Don't ask me again" opts out, turned back on by the switch on the Account page's Privacy card (`components/PrivacySettings.jsx`).
- **The four answers in the assistant (#498, `surveys.md` §2):** `GET /api/survey/answers` → `{"due", "reason"}`, decided on the server from the user's row (consent yes, account ≥ 14 days, ≥ 3 sign-ins, not shown in 30 days, NPS not shown in the last 7 days; `lib/analytics/answers.js`, sharing NPS's rules).
  `POST /api/survey/answers {"action": "shown"}` (same-origin JSON) records `answers_last_shown_at` only while due, else 409; INFO log `answers survey shown`, audit `answers_survey_shown`.
  The page shows it only when NPS is not due in that tab (NPS goes first; never both in one session) and no manual close or rebalance dialog is open; the Ask badge's accessible name is "Ask (two optional questions waiting)".
  Opening the panel sends "shown" first and shows `components/AnswersCard.jsx` (with `survey_shown {answers}`) only on the server's yes, so a second tab shows nothing.
  Two optional chip questions (came for, hardest to find; the same five codes, no text); Send needs one and goes through `POST /api/survey`; ×, Not now or closing the panel send `survey_dismissed {answers}`.
- **Allow-list:** `lib/analytics/plan.js` — each event and the only properties it may carry (enums, flags, capped millisecond counts, help catalogue keys of letters and `_` only, so no number fits).
  `__tests__/events-plan.test.js` keeps it equal to the plan's tables: change the plan first.
- **Surveys (#297):** `survey_shown`, `survey_answered` and `survey_dismissed` carry codes only — `survey_id` (`nps`, `answers`, `exit`), `score` (the strings `0`–`10`), `reason_code`, `came_for`, `hardest_to_find`.
  A survey's free text never goes into an event; it is kept in `auth.db` beside the user (`survey_answers`, `POST /api/survey` above) and deleted with them.
  Wording, triggers, frequency caps and the legal review that must come first: `docs/product/analytics/surveys.md`.
  The NPS card (#446) and the four answers card (#498) are built (above); the exit survey is not on screen yet.
- **Collector:** `app/api/events/route.js` stores an event only for a signed-in person (re-read from the database), same-origin JSON ≤ 2 KB, whose `users.analytics_consent` is yes (403 otherwise) and when `ANALYTICS_SALT` (≥ 16 chars) is set (503 otherwise).
  An unknown event, a property not in the plan or a value outside its rule is refused whole: 400, nothing written, a WARN log naming the field, never its value. 204 when stored.
- **Stored:** one JSON line per event in `data/monitor/events.jsonl` beside `auth.db` (`EVENTS_PATH`; off for an in-memory database), append-only, owner-only: `event`, `ts` (UTC, to the second), `user_id` (HMAC-SHA256 of the user's row id with `ANALYTICS_SALT`, 16 hex), `session_id`, `role`, `environment` (`stable`|`mvp`, from `MONITOR_DEPLOYMENT`), `app_version` (`MONITOR_VERSION`) and the event's properties.
  Never the username, email, IP, user agent or URL.
  Not read by the bot.
- **Retention (#296):** `lib/analytics/retention.js` keeps raw events `EVENT_RETENTION_DAYS` (395, 13 months; needs legal review, change it there only).
  The monitor prunes older lines when it starts and then every 24 h (`instrumentation-node.js`).
  Each run writes an `events_pruned` audit line with the kept and removed counts; a failure is an ERROR log and an `events_prune_failed` line.
  Every rewrite goes through `events.jsonl.tmp` (0600, fsync) and a rename, so a crash leaves the old file or the new one whole.
- **Deleting a user** deletes their events in the same step: the `user_deleted` audit line is followed by `events_deleted` with the count.
  If the events cannot be deleted (e.g. `ANALYTICS_SALT` unset while the file exists), the user is still deleted, the failure is logged at ERROR and audited as `events_delete_failed`, and the answer's `events_error` gives the CLI command to retry with `--row-id`.
  Their survey answers (codes and free text) go in the same step: `survey_answers_deleted` with the count; a failure is an ERROR log, a `survey_answers_delete_failed` line and `survey_answers_error` in the answer, and the monitor deletes the rows left without a user when it next starts.
- **Access and deletion requests:** run from the repository root with that environment's values (both are required: stable and mvp each have their own; `frontend/node_modules` must be installed):

  ```bash
  AUTH_DB_PATH=…/data/monitor/auth.db ANALYTICS_SALT=… node frontend/scripts/analytics-user.mjs export alice [--out FILE]
  AUTH_DB_PATH=… ANALYTICS_SALT=… node frontend/scripts/analytics-user.mjs delete alice
  AUTH_DB_PATH=… ANALYTICS_SALT=… node frontend/scripts/analytics-user.mjs delete --row-id 7   # a user already deleted
  ```

  `EVENTS_PATH` and `AUDIT_PATH` default to the files beside `auth.db`.
  An export writes a new 0600 file (default `analytics-export-<user>-<time>.jsonl` beside the events) and never overwrites one: the person's events, then one `{"record": "survey_answer", "survey_id", "codes", "text", "created_at"}` line per survey answer (#363).
  `delete` removes events only; survey answers go with the user's deletion.
  The CLI reads `auth.db` without writing it, so the monitor can keep running.
  It prints counts and paths only and writes an `events_exported` / `events_deleted` audit line (`username` = `cli:<host account>`).
  It does not change the person's consent: to stop collection they answer no, or the admin deletes the user.
