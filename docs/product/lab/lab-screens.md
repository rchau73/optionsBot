# Lab screens: the spec for the prototype (E9, #841)

The developer-ready spec of the strategy-change lab page, the brief the designer prototypes from and a person approves.
It is built only from what the existing `/api/lab/*` routes return (`frontend/docs/api-routes.md`, `frontend/docs/users-and-roles.md`, [README](README.md)).
Nothing here is built: no page, no menu entry, no route, no setting changes with this document.
Where the spec needs something that does not exist, it says so under "Open questions" instead of assuming it.

## 1. Job and metric

- **Who:** the manager (PRD §3 "Roles"): the subscriber or their adviser, who may adjust a strategy setting of an account they manage after a comparison backtest.
- **Admin:** reads everything, as staff; PRD §3 says an admin does not change the settings of an account already onboarded and running, so the page shows an admin no Confirm button (section 5, "Admin").
- **User** (the money owner): no access; every lab route answers 403 to the role `user`.
- **Job:** "Before I change a setting of my customer's strategy, show me what it would have done, its risk beside its return, and let me confirm only once I understand it."
- **Metric:** the share of lab requests that reach a decision, from `cmd/labstats` over 28 days.
  - A confirmed decision is a done run with a recorded change (`applied/<id>.json`, counted by `cmd/labstats`).
  - A deliberately dropped decision has no route or record yet (open question 1); until it exists the metric is recorded changes ÷ done runs.
- **Guardrail:** no change is recorded without a preview token: `POST /api/lab/runs/<id>/apply` refuses one without a `confirm_token` that matches what `GET /api/lab/runs/<id>/preview` showed, at most 15 minutes old.
- **Counter-metrics** (also from `cmd/labstats`): the share of requests ending `failed` or `expired`, and the median minutes to a finished run (targets ≥ 90 % done, ≤ 30 min, to confirm after four weeks).

## 2. Flow

One page, `/lab`, in seven steps.
Each step names its route, the fields it shows and its role check; the role checks are on the server, the page only follows them.

1. **Pick a setting.**
   Source: `frontend/content/lab-settings.json`, imported by the page as `lib/labvalidate.js` imports it (`settings`, one entry per tunable key); no route is needed for the list.
   Shows `title`, `unit`, `min`, `max`, `step`, `kind`, `cross_rules` and whether it is `core_policy`; the tip comes from step 2's `tip` (`GET /api/help/tips`'s answer).
   A key with `[]` (`dte_delta_matrix[].dte`) is offered as one value for every element, as `POST /api/lab/requests` accepts it.
   Role: none of its own; the page itself is for managers and admins (step 0 below).
2. **Validate.** `POST /api/lab/validate` `{"key", "value", "underlying"?}` on each change of the value (debounced, 500 ms).
   Shows `valid`, `reasons` (under the field), `range` (`min`–`max`, `step`, `unit`), `current` (the shipped value, beside the field), `tip`, `core_policy` with `policy_note`, and `cross_rules_note` when `cross_rules_checked` is false.
   Role: manager or admin (`STATE_CHANGING_ROUTES`, audited `lab_validate`, never the value); 403 for a user.
3. **Request a run.** `POST /api/lab/requests` `{"settings": [{"key", "value"}], "underlying"?}`, at most 5 values, BTC by default.
   Shows the 201 record (`id`, `requested_at`, `status: "queued"`) or the refusal's `error` and `reasons`; 429 says three requests are already waiting.
   Role: manager or admin (`STATE_CHANGING_ROUTES`, audited `lab_request` with the keys, never the values).
4. **Wait.** `GET /api/lab/runs`, polled every 15 s while a request of this person is `queued` or `running`, stopped otherwise.
   Shows each entry of `requests`: `settings`, `requested_at`, `status` (`queued`, `running`, `failed` with `reason`, `expired`), `updated_at`.
   A request whose report is listed moves to `runs`; its row there opens step 5.
   Role: manager or admin (`ROLE_GUARDED_READS`).
5. **Read the comparison.** `GET /api/lab/runs/<id>`: the run's list row (`changed`, `backtest`, `replay`, `stress_ran`, `windows`, `cliffs`, `label`), `report` (its `disclaimer`) and `analysis`.
   Shows the comparison screen (section 3); `analysis` stays hidden (section 4).
   Role: manager or admin (`ROLE_GUARDED_READS`).
6. **Preview the change.** `GET /api/lab/runs/<id>/preview`, on the button "Preview this change".
   Shows per change `title`, `tip`, `current`, `base`, `proposed`, `core_policy`, `policy_note`; then `account.underlying`, `account.config`, `person`, `backtested`, `fragility`, `audit_line`, `reasons`.
   `confirmable` and `confirm_token` decide whether Confirm is enabled; `stale` and `needs_product_owner` choose the state (section 5).
   Role: manager or admin (`ROLE_GUARDED_READS`); each answered preview writes one audit line, `parameter_change_previewed`.
7. **Confirm.** `POST /api/lab/runs/<id>/apply` `{"confirm_token"}` from the confirm dialog (section 6).
   Shows the 201 answer's `message`, `next_step`, `risks` and `changes` (`old` → `new`), or the refusal's `error`, `reasons` and `approver`.
   Role: manager or admin (`STATE_CHANGING_ROUTES`, `linked: true`: a linked manager is refused on another account); audited `parameter_change_recorded`.

Step 0, opening `/lab`: the page reads the signed-in person's role (the `person` the layout already passes to `MenuBar`); a user is shown the no-access state (section 5), and every route refuses them anyway.

## 3. Comparison screen

The heart of the page: current against proposed, risk beside return, for each source that ran.

**Header.**
- The changed settings, from `changed`: "Slot entry delta: {base} → {proposed}", the `key`'s title from `lab-settings.json`, `base` → `proposed`.
- The run's time (`time`, in the person's local time with "UTC" on hover) and the underlying.
- The label **"Backtested, not a forecast"** (`label`, first letter capitalised), in the same type size and weight as the figures, never smaller, never in a footnote.

**Figures table** (desktop): one block per source, Backtest (`backtest`) and Replay of recorded books (`replay`); rows Current (`base`) and Proposed (`proposed`).

| | Return | Worst week | Max drawdown | Fees |
|---|---|---|---|---|
| Current | `return_pct` | `worst_week_pct` | `max_drawdown_pct` | `fees_pct` |
| Proposed | `return_pct` | `worst_week_pct` | `max_drawdown_pct` | `fees_pct` |

- Every figure is a percentage exactly as `lib/lab.js` gives it: `formatPct(v, 1, { signed: true })` from `lib/format.js`; worst week, drawdown and fees are % of the run's starting equity.
- No amount, coin or USD, appears anywhere on the page: `lib/lab.js` serves percentages only.
- Return is never a column alone: worst week and drawdown sit right beside it in the same row, same size (the brief's honesty rule).
- A loss shows its minus sign and is as large and as dark as a gain (#582); the UI story checks each figure's sign in `report.json` and adds a row to `__tests__/loss-visibility.test.jsx`'s `PAGES`.
- A source that did not run (`backtest` or `replay` null) is a line "Not run for this request", never a row of zeros.
- A config whose run failed (`base` or `proposed` null) shows "Did not run" in its row, with the other row still shown.
- No colour says "better": the proposed row is not green when its return is higher, because a higher return with a deeper drawdown is not better.
  The difference column is left out on purpose; the reader compares the rows.
- Stress: `stress_ran` true → "Stress test ran: see the full report"; false → "Stress test not run for this request".

**Windows** (`windows`, null → the block is left out): "Over {run} windows, the proposed setting did better in {proposed_wins} and the current one in {base_wins}." per source.

**Fragility** (`cliffs`, null → "Fragility check not run"):
- 0 → "No sharp change found when the setting moves 10 % or 20 % either way."
- n > 0 → "{n} sharp changes found when the setting moves 10 % or 20 % either way: small moves change the result a lot."
- The preview's `fragility` (`checked`, `key`, `cliffs` or `note`) gives the same in step 6.

**Words with a tip** (E11): every term on this screen carries the glossary's tip; `backtest`, `worst week` and `exchange fees` exist.
The UI story adds glossary entries, each with `short` and `risk_income`, for `max drawdown`, `replay`, `stress test`, `fragility`, `window` and `core policy`.

## 4. Analysis slot

- Reserved on the comparison screen, below the fragility block, and **hidden**: nothing is rendered for it, not even a placeholder heading.
- It stays hidden until counsel's answer to legal question L10 is recorded in `docs/legal/preliminary-answers.md` and `L10_ANALYSIS_GATE_OPEN` (`frontend/lib/lab.js`) is changed to true in a reviewed PR; the route answers "withheld pending legal review" until then.
- While it is withheld, the fallback is the numbers only: the comparison screen above, unchanged, is the whole answer.
- When it is shown, it is labelled **"AI-generated from this run's numbers"** above the text, in the same size as the text (L12: AI-generated content is labelled).
- It sits below the figures and never above them, so the numbers are read first.
- It is a separate story, after L10; this spec reserves the place only.

## 5. States

Each state names the route field that triggers it.

- **Empty** (`runs` and `requests` both `[]`): "No comparison runs yet." "Pick a setting and a new value. The lab runs your current and proposed settings over the last 7 days and shows them side by side."
- **Loading** (any lab request in flight): a skeleton of the table, the label already in place; announced "Loading the comparison" once.
- **Queued** (`status: "queued"`): "Waiting to run. Requests run one at a time."
- **Running** (`status: "running"`): "Running now. You can leave this page and come back."
- **Failed** (`status: "failed"`, `reason`): "This run failed: {reason}. You can request it again."
- **Expired** (`status: "expired"`): "This request waited over a day without running. You can request it again."
- **Done, report not readable yet** (`status: "done"` in `requests`): "Finished. The results will appear in a moment."
- **Stale run** (preview `stale: true`, `reasons`): "This run is out of date: {reasons}. Request a new run before you confirm." Confirm disabled.
  `reasons` covers a run over 7 days old, a value the run started from that is no longer the account's, an account value that cannot be read, and a proposed value now refused.
- **No numbers for the proposal** (preview `confirmable: false`, `confirm_token: null`, not stale, not core policy): "There are no results for the proposed value, so it cannot be confirmed." Confirm disabled.
- **Core policy** (`core_policy: true` in validate or preview, `needs_product_owner: true`, `policy_note`): "Changing this default needs the product owner. You can still compare it, but you cannot confirm it here."
  Confirm hidden, the reason in its place; the apply route refuses it with `approver` anyway.
- **Manager not linked to this account** (apply's 403, its `error` in words): "You do not manage this account, so you cannot confirm a change to it." followed by the route's own text.
  The preview does not say whether the person may confirm (open question 3), so the page learns it only from the confirm.
- **Admin** (session role `admin`): the comparison and the preview are shown, Confirm is not: "Admins see the lab but do not change a running account's settings. The account's manager confirms."
  The apply route accepts an admin today (open question 2).
- **User** (session role `user`; every lab route answers 403): "The lab is for managers. Ask your account's manager to compare a change." No form, no figures.
- **Recorded** (apply 201): `message` and `next_step` from the answer, then "Nothing changed on the exchange." Confirm gone; the run row shows "Recorded".
- **Already recorded** (apply 409, "a confirm is used once"): "This change was already recorded."
- **Token too old or changed** (apply 409, "open the preview again"): "The preview is older than 15 minutes. Open the preview again to confirm."
- **Too many requests** (requests 429): "You already have 3 requests waiting or running. Wait for one to finish."
- **Server error** (any 500): "The lab could not answer. Try again in a minute." The figures already on screen stay.

## 6. Copy

Every sentence is within the glossary's checks: at most 25 words, no promised return, no jargon word without a tip.

**Labels.**
- Page title: "Strategy lab".
- Menu: "Lab".
- Steps: "1. Choose a setting", "2. New value", "3. Compare", "4. Preview", "5. Confirm".
- Field: "{title} ({unit})", hint "From {min} to {max}, in steps of {step}. Now: {current}."
- Buttons: "Check value", "Request a comparison", "Preview this change", "Confirm change", "Cancel".
- Table: "Return", "Worst week", "Max drawdown", "Fees", rows "Current" and "Proposed", blocks "Backtest" and "Replay of recorded books".
- The label: "Backtested, not a forecast".

**Validation and errors** (the route's `reasons` are shown as they come, after the lead):
- "This value cannot be tried: {reasons}."
- "This value is checked again with the whole list when the run starts." (`cross_rules_checked: false`)
- "Choose at most 5 settings for one comparison."

**Confirm dialog** (title "Confirm this change?"):
- "You are about to record: {title} from {current} to {proposed}, for {account.underlying} ({account.config})."
- "These results are backtested, not a forecast. Past data does not tell what the strategy will do next."
- "Worst week {worst_week_pct}, max drawdown {max_drawdown_pct}, fees {fees_pct}, from the backtest."
- "Confirming records the change with your name in the audit trail. It then goes to a person's pull request for review."
- "Nothing changes on the exchange, and no bot runs the new value, until that pull request is merged."
- Buttons "Confirm change" and "Cancel"; focus starts on "Cancel".

## 7. Menu

- **Label:** "Lab". **Path:** `/lab`. **Key:** `lab`.
- **Position:** the issue asks for "after the existing comparison entry", but the menu has none: `/api/compare` has no page and `lib/menu.js` no comparison item.
  So the entry goes in the Operational ▾ group, after Account, its last item: Positions · Income · Worst case · Activity · Account · **Lab**.
- **Roles:** managers and admins only; the link is a convenience, the routes decide.
- **`frontend/lib/menu.js`**, in the UI story that ships the page (#385: a page and its menu entry in one PR):
  - add `{ key: "lab", label: "Lab", path: "/lab", group: "operational", roles: ["manager", "admin"] }` after the `account` item;
  - `menuItems(pathname, { bot, role })` drops an item with `roles` that does not list `role` (no role: dropped); `menuGroups` passes `role` through;
  - `MenuBar.jsx` passes `person?.role`, as it already does to `UserMenu`.
- **`frontend/__tests__/menu.test.jsx`** expectations:
  - with role `manager` or `admin`, `GROUP["Operational ▾"]` ends with "Lab", and on `/lab` the group is marked current and "Lab" has `aria-current="page"`;
  - with role `user` or no role, "Lab" is not in the menu (the existing cases keep their lists);
  - `menuItems("/", { role: "manager" })` hrefs end with `/lab`; `menuGroups` gives Operational the keys `positions, income, worst-case, activity, account, lab`;
  - "Lab is placed after Account", like "Account is a menu item placed after Activity";
  - the page walk finds `/lab` in the menu for a manager, so it needs no `NOT_IN_MENU` line.
- The same story adds a `## Lab` section to `frontend/content/monitor-guide.md`, a golden question to `docs/product/assistant/golden.jsonl` and runs `go run ./cmd/helpindex` (#483), and a tip-placement section (`docs/product/education/tip-placement.md`, #728).

## 8. Phone (360 px, E12)

- The steps stack in one column; the setting picker is a native `<select>`, the value field full width with its hint below.
- The figures table becomes stacked cards, one per source × config: "Backtest · Current", "Backtest · Proposed", then Replay.
- Each card lists Return, Worst week, Max drawdown and Fees as label–value rows in that order, so the risk stays next to the return on the same card.
- The label "Backtested, not a forecast" is at the top of the first card and repeated at the top of the Replay pair, same size as the figures.
- Current and Proposed cards of one source sit one under the other, never side by side, never behind a tab.
- No sideways scroll at 360 px; every button and link is a 44 px target (`min-h-11`), as the menu's.
- The confirm dialog fills the screen width with its buttons stacked, "Cancel" first.
- The UI story adds `/lab` to the responsive audit's panels (`frontend/docs/responsive-audit.md`, kept in order) and runs `npm run test:phone`.

## 9. Accessibility

- **Contrast:** only the text colours already pinned in `__tests__/a11y-tokens.test.js`; a new pair gets its row there (#763).
- **Focus order:** setting → value → Check value → Request a comparison → the requests list → the run's figures → Preview this change → Confirm change.
  The confirm dialog traps focus, starts on "Cancel" and returns focus to "Preview this change" when it closes.
- **Announcements:** one `aria-live="polite"` region for status changes ("Run queued", "Run running", "Run failed: {reason}", "Results ready", "Change recorded"), never repeated on a poll that changed nothing.
  A refusal on confirm uses `role="alert"`.
- **Figures for a screen reader:** a real `<table>` with `<caption>` "Backtest, current and proposed. Backtested, not a forecast." and `<th scope>` on rows and columns.
  On the phone cards each value is read with its label ("Worst week, minus 3.2 percent"); a missing figure reads "did not run", never blank.
- **Errors** sit under their field and are linked by `aria-describedby`; the Confirm button that is disabled says why in text next to it, not only by its colour.

## 10. Prototype brief

Ready to post on the E9 epic with the label `prototype:review`; only a person adds `prototype:approved`.

- **Problem:** a manager who is not technical wants to change a strategy setting for a customer without betting the customer's money on a guess.
- **Flow:** choose a setting → new value → request a comparison → wait → read current against proposed, risk beside return → preview → confirm (section 2).
- **Decisions:**
  - numbers only until L10 is answered; the analysis slot exists but is hidden;
  - "Backtested, not a forecast" at the figures' size; no colour or arrow says "better";
  - the confirm records the change for a person's pull request and applies nothing;
  - core policies can be compared but never confirmed here;
  - admins read, managers confirm;
  - stacked cards on a phone, return and risk on the same card;
  - "Lab" in Operational ▾ after Account, for managers and admins.
- **Screens to prototype:** empty, picker with a refused value, queued/running/failed/expired list, comparison (desktop and 360 px), preview with a stale run, preview with a core policy, confirm dialog, recorded.
- **Open questions:**
  1. A "Drop this proposal" action: no route records a deliberate drop, so the metric counts confirmations only. A small route and a `cmd/labstats` count would close it.
  2. Admins: the apply route accepts an admin today; should it refuse one on an onboarded account, as PRD §3 says, or is hiding the button enough?
  3. Linked managers: the preview does not say whether this person may confirm for this account; a field would let the page disable Confirm before the 403.
  4. The menu position: no comparison entry exists; is Operational ▾ after Account right, or should Lab be a bar item of its own?
  5. How a recorded change reaches the running bot is not decided (README item 7); the confirm copy says "a person's pull request", which matches `cmd/labapply`'s candidates.
- **Metric:** the share of done runs that reach a recorded change (and, once question 1 is answered, a deliberate drop), from `cmd/labstats`.

## 11. UI stories that follow once approved

Each goes to Ready only after the prototype is approved (`prototype:approved` on E9).

1. **Page and menu:** `/lab` with the role states (user, admin, manager), the menu entry and its tests (section 7), the monitor guide's section, the golden question and the help index.
   Routes: `GET /api/lab/runs`.
2. **Request form:** setting picker, value with live check, request and the requests list with its states.
   Routes: `POST /api/lab/validate`, `POST /api/lab/requests`, `GET /api/lab/runs` (`requests`).
3. **Comparison view:** the figures table and phone cards, windows, fragility, the label, the hidden analysis slot, the new glossary entries.
   Routes: `GET /api/lab/runs`, `GET /api/lab/runs/<id>`.
4. **Preview and confirm:** the preview, the stale, core-policy and no-numbers states, the confirm dialog, the recorded and refused states.
   Routes: `GET /api/lab/runs/<id>/preview`, `POST /api/lab/runs/<id>/apply`.
