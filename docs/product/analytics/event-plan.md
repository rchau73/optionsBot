# Analytics event plan (E13)

**Status:** plan (#192); the collector exists (#193: `POST /api/events`, `frontend/lib/track.js`, `frontend/README.md` "Analytics") and accepts exactly this file's events and properties — anything else is refused whole (400). Consent can be set through `GET`/`POST /api/consent` and a survey answer stored through `POST /api/survey` (#363); the consent notice and the survey screens wait for E13's prototype. Instrumented (#424): every signed-in page sends `screen_viewed` (one `screen` per page of `frontend/lib/menu.js`) and the assistant panel its four events — only for a person whose stored answer is yes (the monitor reads it from `GET /api/consent` when a page loads; not asked or no sends nothing). The in-product surveys (NPS, the four answers, the exit survey) and their wording, triggers, caps and data rules are in [`surveys.md`](surveys.md) (#251, #297).

**Rule in one line:** an event says *what a person looked at or did*, never *what the account holds*. No amount, size, price, P&L, margin figure, instrument name, secret or email can travel in an event — the allow-list below is the only set of properties the collector accepts, and `tests/event_plan_test.go` checks this file against that rule.

## 1. Questions the data must answer

Every event answers at least one of these. An event that answers none is not collected. The weekly usage report (`cmd/usage`, #295, README "Usage report": `data/monitor/reports/<week>.md|json`, counts and shares only) answers each question in the section named in the last column.

| ID | Question | Feeds | Answered in (`cmd/usage`) |
|---|---|---|---|
| Q1 | How many accounts are active each month — a person visited at least once while a strategy ran? | PRD §7 north star (product phase) | "Active users" (day, week, 30 days; by role and environment) |
| Q2 | Do people come back: weekly and monthly cohort retention of signed-in users? | PRD §7 retention | "Weekly cohort retention" (+1 … +4 weeks), "Active users" |
| Q3 | Which parts of the monitor do people actually use, and which do they never open? | E3 investor view, E13 engagement | "What is opened" (panels, never-opened panels, drill levels, filters, page views per screen) |
| Q4 | Can a new person answer E3's four questions within 5 minutes without help (what it holds, making or losing, worst case, why the last trade)? | E3 5-minute test | "First answers within the first session" (share, median `elapsed_ms`) |
| Q5 | Which help (tips, videos, empty-state text) is used, and does it resolve the question without a person? | PRD §7 self-service understanding, E11, E10 | "Help" (tips, videos played/completed, share helped), "Assistant" (opened and asked per entry, replies per kind, flagged) |
| Q6 | Are the state-changing flows (manual close, rebalance) understood — how often is one started, previewed, cancelled or confirmed, and do they fail? | E7 roles, guardrails | "Manual close funnel", "Rebalance funnel" |
| Q7 | Do people trust what they see: how often is the page stale, errored or signed out under them? | guardrails, E6 | "Trust" (stale data per source, sign-outs per reason) |
| Q8 | Why do people stay or leave: NPS, its coded reason, and every exit's coded reason (`surveys.md`)? | PRD §7 retention (every cancellation gets a coded reason), E13 churn | "Why they stay or leave" (#342: each survey's shown/answered/dismissed and response rate, NPS bands and the score from 10 answers, coded why, came for / hardest to find, exit reasons with the top 3) |

**Are users stopping?** The engagement-drop alert (§7) answers it every week from Q1, Q4, Q5 and Q6.

## 2. Events

Every event carries the envelope (§3) plus its own properties. Property names are the allow-list's (§4); an event with a property not listed there, or with a value outside its rule, is refused whole by the collector (400, nothing stored).

| Event | Trigger | Properties | Answers |
|---|---|---|---|
| `session_started` | first page load after sign-in, or after 30 min without an event | `entry_screen`, `viewport` | Q1, Q2 |
| `screen_viewed` | a signed-in page is shown (one `screen` per page of the menu, `frontend/lib/analytics/screens.js`, #424); `login` is in the list but never sent (signed out, no consent known) | `screen` | Q1, Q2, Q3 |
| `panel_expanded` | a collapsible panel is opened | `panel` | Q3, Q4 |
| `panel_collapsed` | a collapsible panel is closed | `panel`, `open_ms` | Q3 |
| `filter_changed` | a filter or grouping of Open positions changes (which one, not its value) | `filter` | Q3 |
| `drill_opened` | a row is expanded to its detail (strategy → slot → leg → orders) | `level` | Q3, Q4 |
| `details_section_jump` | a link of the Details (pro) page's section bar is followed (#405) | `section` | Q3 |
| `details_trade_expand` | a trade row of Details (pro) is opened to its detail row (#405) | — | Q3 |
| `tip_opened` | a contextual tip (E11) is shown for longer than 1 s | `tip_id`, `screen` | Q5 |
| `video_played` | a how-to video starts | `video_id` | Q5 |
| `video_completed` | a how-to video reaches 90 % | `video_id`, `watched_ms` | Q5 |
| `help_resolved` | the person answers "did this help?" under a tip or video | `tip_id`, `video_id`, `helped` | Q5 |
| `first_answers_seen` | in the first session, the KPI strip, the open positions, the worst-case figure and the last trade's reason have all been on screen | `elapsed_ms` | Q4 |
| `manual_close_opened` | the manual-close dialog opens for a leg | `side`, `allowed` | Q6 |
| `manual_close_cancelled` | the dialog is dismissed without sending | `side` | Q6 |
| `manual_close_confirmed` | the person confirms the close | `side`, `last_leg` | Q6 |
| `manual_close_result` | the close request returns | `side`, `outcome` | Q6, Q7 |
| `rebalance_previewed` | the rebalance dry run is shown | `outcome` | Q6 |
| `rebalance_confirmed` | the person confirms the real rebalance | `outcome` | Q6, Q7 |
| `data_stale_shown` | the page shows a stale or failed-poll badge | `source` | Q7 |
| `signed_out` | sign-out, or the session expired and the page went to `/login` | `reason` | Q2, Q7 |
| `survey_shown` | an in-product survey card or the exit question is shown (`surveys.md`: frequency caps, never in the first 14 days) | `survey_id` | Q8 |
| `survey_answered` | the person sends a survey: NPS with at least a score, the four-answers card or the exit reason; free text never travels here | `survey_id`, `score`, `reason_code`, `came_for`, `hardest_to_find` | Q4, Q8 |
| `survey_dismissed` | the card is closed or the exit question skipped (one click) without an answer | `survey_id` | Q8 |
| `assistant_open` | the assistant panel opens: the menu bar's Ask, or Why? on a trade (E10, #408) | `entry` | Q5 |
| `assistant_ask` | a question is sent from the panel; never its text | `entry` | Q5 |
| `assistant_answered` | the panel shows the monitor's reply to a question (status 200; a limit, an error or the assistant being off is no reply) — its kind only, never the question, the answer or their ids (#424) | `answer` | Q5 |
| `assistant_flag` | the person flags an answer as wrong; never the answer or its id | — | Q5, Q7 |
| `overview_unit_toggle` | the overview's P&L chart switches between Coin and USD (#404) | `unit` | Q3 |
| `overview_layer_toggle` | a layer pill of a bot band's price chart is toggled (#404) | `layer` | Q3 |

What is deliberately **not** an event: each poll of the bot API, mouse movement, scrolling, keystrokes, text typed anywhere (the sign-in form included), hovering over numbers, and anything the bot itself does (that is the journal's job, not analytics).

## 3. Envelope (every event)

| Field | What | Why it is safe |
|---|---|---|
| `event` | the event name from §2 | — |
| `ts` | UTC time, rounded to the second | stored in UTC like every other file; shown in BRT only in reports |
| `user_id` | pseudonymous id (§5) | not the username, not the email |
| `session_id` | random id per browser session, new after 30 min idle | cannot be linked to a person without `user_id` |
| `role` | `admin`, `manager` or `user` (E7) | coarse, needed to split usage by role |
| `environment` | `stable` or `mvp` | the deployment, not the account |
| `app_version` | the monitor's release tag or commit | ties a behaviour change to a release |

Never in the envelope: IP address, full user agent, referrer URL, query strings (filters live in the URL), the Deribit account or client id.

## 4. Property allow-list

The only properties an event may carry. Every one is an enumeration, a flag, a count of milliseconds or an id from the product's own help catalogue — none can hold a free-text value or a number about money.

| Property | Type | Allowed values |
|---|---|---|
| `entry_screen` | enum | `monitor`, `login` |
| `screen` | enum | `overview`, `income`, `worst_case`, `positions`, `how_it_works`, `activity`, `account`, `details`, `login` (one per page of the menu, #424) |
| `viewport` | enum | `phone` (< 640 px), `tablet` (< 1024 px), `desktop` |
| `panel` | enum | `account`, `positions`, `held`, `others`, `trades`, `activity`, `pnl`, `pnl_coin`, `orders` (the `monitor.panel.*` keys) |
| `open_ms` | duration | milliseconds, capped at 1 h |
| `filter` | enum | `bot`, `strategy`, `type`, `moneyness`, `group_by` |
| `level` | enum | `strategy`, `slot`, `structure`, `leg`, `orders` |
| `section` | enum | `positions`, `held`, `trades`, `orders`, `others` (the Details (pro) sections) |
| `tip_id` | catalogue id | `tip.` + a key of `frontend/content/glossary.json` in letters and `_` (e.g. `tip.gamma_flip`, `tip.stop_loss`); an id not in the glossary is refused (#300); never generated from data |
| `video_id` | catalogue id | a key of `frontend/content/videos.json` (e.g. `video.read_summary`); an id not in it is refused (#300) |
| `watched_ms` | duration | milliseconds, capped at 10 min |
| `helped` | bool | `true`, `false` |
| `elapsed_ms` | duration | milliseconds since `session_started`, capped at 1 h |
| `side` | enum | `call`, `put` |
| `allowed` | bool | whether the rule allowed the close when the dialog opened |
| `last_leg` | bool | whether the leg was its strangle's last (the slot is re-entered) |
| `outcome` | enum | `ok`, `refused`, `error`, `timeout` |
| `source` | enum | `bot_api`, `account`, `pnl_history` |
| `reason` | enum | `user`, `expired`, `refresh_failed` |
| `survey_id` | enum | `nps`, `answers`, `exit` |
| `score` | enum | `0`, `1`, `2`, `3`, `4`, `5`, `6`, `7`, `8`, `9`, `10` (strings: the NPS score) |
| `reason_code` | enum | `value_unclear`, `losses`, `too_complex`, `price`, `trust`, `switched`, `steady_income`, `easy_to_follow`, `saves_time`, `clear_risk`, `explains_trades`, `trust_numbers`, `helpful_answers`, `hard_to_find`, `missing_feature`, `other` (`surveys.md` §4) |
| `came_for` | enum | `holding`, `income`, `worst_case`, `last_action`, `none` (E3's four answers) |
| `hardest_to_find` | enum | `holding`, `income`, `worst_case`, `last_action`, `none` |
| `entry` | enum | `menu`, `why` (the assistant panel's opener: the menu bar's Ask, or Why? on a trade) |
| `answer` | enum | `answered`, `dont_know`, `refused`, `fallback` (the kind of the assistant's reply: a cited answer, "I don't know", the refusal, the safe fallback) |
| `unit` | enum | `coin`, `usd` (the unit picked) |
| `layer` | enum | `sma`, `sma21`, `swings`, `flip`, `strikes` (the layer toggled) |

Rejected properties and why: instrument name (it carries the strike, a price), quantity, premium, P&L, margin figures, equity, the bot's currency balances, error messages (free text that can echo a number or a token), filter values (a strategy id is fine but a search box later would not be), the leg's delta or DTE (position detail, not usage), a survey's free text (the NPS "anything else" and the exit `other` box: personal data, kept in `auth.db` beside the user and deleted with them, never in an event — *needs legal review*, `surveys.md` §6).

## 5. Identity, consent, retention

- **Pseudonymous user id:** `user_id` = HMAC-SHA256 of the user's row id in `auth.db` (E7) with a per-environment `ANALYTICS_SALT`, first 16 hex characters. The salt lives in the environment's `.env` beside `AUTH_SECRET` and never in an event or a log. No email, username or IP in any event. Reports count ids; mapping an id back to a person needs the salt and the users table, which only an admin holds — used only to honour a deletion or access request.
- **Consent:** analytics is off until the person says yes once, on a short notice after sign-in ("help us improve the monitor: we record which screens and help you use, never amounts or positions"), with *Yes* and *No* equally prominent. The answer is stored on the user (not a cookie) and can be changed at any time in the account menu. *No* means no event is sent at all — not a reduced set. *(needs legal review: whether LGPD art. 7 legitimate interest or GDPR art. 6(1)(f) would allow strictly first-party usage analytics without consent; until a lawyer says so, consent is required.)*
- **Opt-out:** turning analytics off stops collection from the next event; already-collected events stay until retention ends unless the person asks for deletion.
- **As built (#363):** `POST /api/consent {"consent": true|false}` (signed-in, same-origin) sets the answer on the user's row and writes an `analytics_consent_changed` audit line (the person and the new value only); `GET /api/consent` reads it (null = never answered). *No* makes `/api/events` and `/api/survey` answer 403 at once, and the reply says events already stored stay until the person asks for their deletion. Still no screen: the notice waits for E13's prototype.
- **Retention:** raw events are kept 13 months (enough for a year-over-year cohort), then deleted by a daily job; aggregated reports (counts per week and cohort, no `user_id`) may be kept longer. *(needs legal review: the 13-month period and whether aggregates count as anonymous under LGPD art. 12.)*
- **Deletion and access on request:** an admin removes every event of a `user_id` (and the user's consent record) within 15 days of the request, and can export them for an access request. Deleting a user in `auth.db` deletes their events in the same step. *(needs legal review: LGPD art. 18 / GDPR arts. 15 and 17 deadlines and the request channel.)*
- **Storage:** append-only JSON lines in `data/monitor/events.jsonl` on the environment's own monitor volume (stable and mvp separate, like `auth.db`), written by the monitor's server — the browser posts events to a same-origin route that checks the session, the consent flag and the allow-list, and drops anything else. No third-party tracker, SDK, pixel or CDN script; nothing leaves the host. Not archived with the trading history and never read by the bot.
- **How the monitor enforces these promises** (#296, `frontend/lib/analytics/retention.js`; commands in `frontend/README.md` "Analytics"):
  - *Retention:* `EVENT_RETENTION_DAYS` (395) is the one place the period is set. `pruneEvents` runs when the monitor starts and then every 24 h. It removes the lines older than the period and logs an `events_pruned` audit line with the kept and removed counts. A line without a readable time is kept, counted and logged at WARN; it is never deleted on a guess.
  - *Deleting a user:* `AuthService.deleteUser` removes the user's row (the consent is a column of it) and their sessions first, then every line with their `user_id` (`events_deleted`). If that second step fails, it logs at ERROR, writes an `events_delete_failed` audit line and tells the admin to retry with the CLI using the row id.
  - *A request from a person:* `node frontend/scripts/analytics-user.mjs export|delete <username>` (`--row-id <n>` for a user already deleted). It reads `auth.db` without writing to it. It prints counts and the export path only, never an event, and writes an `events_exported`/`events_deleted` audit line. It does not change the person's consent answer: to stop collection, the person says no or the admin deletes the user.
  - *Every rewrite is atomic:* the kept lines go to a temp file (mode 0600, flushed to disk), which is renamed over `events.jsonl`. A crash therefore leaves the old file or the new one whole, never a partial file.
- **Surveys** (`surveys.md`): the survey events carry codes only. A survey's free text is personal data: it is kept in `auth.db` beside the user (`survey_answers`, #363), deleted with them, and never sent in an event; `cmd/usage` reads events only (`tests/usage_free_text_test.go`). Deleting a user today also deletes their exit answer (`surveys.md` §3). *(needs legal review: the survey consent, the free text's retention and an anonymous exit count, questions C1–C6 in `surveys.md` §6.)*
- **Access:** only admins read raw events; managers and users see aggregates at most. *(needs legal review: whether a processing record or a DPIA is required before beta customers, and who is the controller once customers bring their own accounts — E8.)*

## 6. How this file is kept true

- `tests/event_plan_test.go` reads the tables above and fails when an event uses a property not in §4, when a property's name or type could carry an amount, a size, a price or a secret, or when an event answers no question of §1.
- A new event or property is added here first, in the same PR that instruments it, and must pass that test.

## 7. Engagement-drop alert ("are users stopping?")

`cmd/usage -alert` (#341, #252) compares each week with the 3 weeks before it. `scripts/agents/usage.sh` runs it every Monday and files one `[E13] Engagement drop: <metrics> <week>` issue in Triage when a key number falls (AGENTIC.md "Weekly usage report"). The alert rules are fixed in `internal/usage/drops.go`:

| Metric | From | Drop when |
|---|---|---|
| weekly active users | Q1 "Active users" | ≥ 30 % under the mean of the 3 weeks before |
| help used (tips opened + videos played) | Q5 "Help" | ≥ 30 % under that mean |
| first answers found (with ÷ first sessions) | Q4 | completion share ≥ 20 points under the 3 weeks' pooled share |
| manual close (confirmed ÷ opened) | Q6 | ≥ 20 points under |
| rebalance (confirmed ÷ previewed) | Q6 | ≥ 20 points under |

A base under 10 users (or 10 funnel starts, in the base weeks and in the week) is "too few to judge" and raises nothing. A week with no events is noted and left out of the base, never counted as zero; a week with no events itself raises nothing. The issue and the epic comment carry counts and percentages only (percentages only for a live account).
