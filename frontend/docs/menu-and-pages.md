# Menu and pages

Part of the [monitor README](../README.md). The pages, panels and charts of the monitor, and the menu.

## What you see

### Menu bar (every signed-in page; #385)

`components/MenuBar.jsx` is in `app/layout.jsx`, inside `Suspense` because it reads `?bot=`.
Its items come from `lib/menu.js` (`menuGroups`, #450): **Overview · Details (pro) · Operational ▾** (Positions, Income, Worst case, Activity, Account) **· Help ▾** (How it works).
A dropdown is filled like a current page while one of its pages is shown.
It closes on Escape, a click outside or a choice (`components/useDismiss.js`, shared with the person's menu).
`menuItems` stays the flat list of every page in that order (the analytics screens and the phone check read it).
Then comes the **notification bell**, pushed right (#447, below).
The last item is **the signed-in person's menu**, labelled with their name, e.g. "bob ▾" (`components/UserMenu.jsx`, #394; "Account ▾" when the name cannot be read).
It holds Users and Audit for an admin, Change password and Sign out.
The name and role come from the access token in `app/layout.jsx` (`lib/auth/viewer.js`, display only: the routes re-check the role).
The pages' own headers no longer carry it.
The current page is marked `aria-current="page"` and shown as a filled pill, or its dropdown is (none on the admin pages).
Every link carries the page's `?bot=`, else the first configured bot (`/api/bots`), so the chosen bot is kept from page to page (the phone overview's BTC | ETH switch rewrites `?bot=`, the menu follows).
**Details (pro)** is its own page, `/details?bot=<name>` (#405, below); the old `/?view=details` link redirects there (`detailsRedirect` in `lib/menu.js`, keeping a valid `?bot=`).
Items are `min-h-11` (44 px) and wrap on a phone.
Not shown on `/login` and `/reset`.
The TESTNET / LIVE badges and warning chips stay in each page's own header

### Notification bell (every signed-in page, before the person's menu; `/notifications?bot=<name>` on a phone; #447, prototype "Notifications: the bell in the menu bar" approved on #164)

The parts are:
- `components/NotificationBell.jsx` in the menu bar and `components/NotificationList.jsx`.
- `components/NotificationsPage.jsx` (`app/notifications/page.jsx`), which is in the menu test's allow-list because the bell opens it.
- `components/NotificationSettings.jsx`.

Data comes from `GET /api/notifications?bot=` every minute (`hooks/useNotifications.js`).
The rules are in `lib/notifications.js`, and `lib/notificationsServer.js` builds the list on the server.
Nothing is stored but the person's read marks and settings (`auth.db` `notification_reads`, `notification_settings`).

**Needs you** is counted on the bell's badge:
- *a short question from the team is waiting* (#446's due rule on the user's row and the losing-week rule, as the Ask button);
- *your request needs more* (a `needs_info` status of the private queue, `requests_status.jsonl`).

**Updates** are listed, never counted:
- The page's bot's journal events from `/api/events`: a close by stop-loss, delta exit or the kill switch, a working sell cancelled by `risk_frozen`, and `im_over_limit`.
  Never fills, skips or other events.
  Each has the event's `plain` sentence and **Why?** (the assistant with its journal sequence number, `/api/explain`).
- The status of the person's requests: accepted as public #N with its link, declined with its reason, or shipped.
- One **What's new** note per promoted release, `docs/product/releases/<tag>.md`, written by a person (`lib/releases.js`; compose mounts the folder as `RELEASE_NOTES_DIR`).

Every role gets every kind.
Items are from the last 90 days, newest first.
Unread is a dot **and** bold text (and a spoken "Unread:").
Following an item marks it read, and **Mark all read** marks the list.
This is per person on the server, so a second device agrees.
The call is `POST /api/notifications?bot=` with `{"action": "read", "ids"}`, `{"action": "read_all"}` or `{"action": "setting", "kind", "enabled"}` (same-origin JSON).
A Needs you kind cannot be turned off: 400.
The bell is a `<button>` named "Notifications, 2 need you" (`bellLabel`); on a desktop it opens a dropdown `dialog` that keeps focus and closes on Escape or a click outside (focus back on the bell); under 640 px it opens the page.
The page adds the settings: each Updates kind on or off; the survey's own "Don't ask me again" stays in its card.
No e-mail, no push, nothing promotional, no "act now".
A bot that does not answer is named and the rest still listed.
Analytics: `notifications_opened` and `notification_followed` (`kind`)

### Positions page (its own page, `/positions?bot=<name>`; #391)

"Positions · <coin>": `app/positions/page.jsx` → `components/PositionsPage.jsx`.
`hooks/usePayoff.js` polls `/api/strangles/payoff` every `MONITOR_POLL_MS`, no faster than 5 s.
At the top is a **BTC | ETH** switch.
It uses `aria-pressed` and 44 px targets, and rewrites `?bot=` (the first bot without it).
Below it is one card per strangle, ordered by its slot's DTE (`components/PayoffCard.jsx`).
- The title is "<coin> · <DTE> d slot · <expiry>" with "as of HH:MM".
- The **premium earned so far** bar and words: "62 % of the premium earned · +0.0058 of +0.0094 BTC".
  A loss reads "−43 %" and the bar is empty.
- A one-legged strangle's missing side: "no call held: repair waiting (<held_legs reason>)".
- The **chart** (`components/PayoffChart.jsx`, an SVG scaled to the card, helpers in `lib/payoff.js`) has x = the coin's price and y = P&L in coin.
  - The **at-expiry** curve is dashed.
  - The **today** curve is solid (bought back now: Black–Scholes on each leg's IV).
  - The area between 0 and the today curve is filled: **gain** solid green, **loss** hatched (pattern, not colour alone).
    It fills towards the expiry curve as days pass.
  - The **now** marker is the spot line and a dot on the today curve, with "now 82,400 · +0.0058 BTC (+$478)".
  - Vertical lines mark the strikes ("floor" put, "ceiling" call), the break-evens at expiry, and each leg's **exit line** (|Δ| at `delta_exit_threshold`) and **stop-loss level**.
  - Each line is labelled, and each kind is toggled by its legend button (`aria-pressed`, 44 px).
- The **delta box** is beside the chart (below it on a phone).
  It shows the strangle's net delta, "Δ −0.12 BTC · −$99 per +1 %" (short-signed), each leg's, and the whole bot's.
The chart's `aria-label` is its text alternative (the expiry payoff, where today's curve gains and loses, the current point).
A missing IV, spot or curve says "unknown: <reason>" and draws no today curve, never zeros.
**On the overview** each strangle is a compact row (`components/StrangleRow.jsx`, #404; the mini payoff charts are on this page only), a button that opens the **modal**.
**Modal** (`components/PayoffModal.jsx`): the full card for one strangle, opened from a compact card, the instrument of a row in Open positions or the open leg of a row in Missing legs (each a button, `aria-haspopup="dialog"`); `role="dialog"`, Esc or × closes it, Tab stays inside, focus returns to what opened it; full screen on a phone.
Data from each poll's `/api/strangles/payoff` (`fetchBotState` → `payoff`, `payoffError`: an older bot without the route shows "Charts unknown", the rest of the bot unchanged).
Any signed-in role

### Details (pro) (its own page, `/details`; #405, E3 prototype "Details (pro) — data first, small graphics")

for the active trader, numbers first and graphics small: `app/details/page.jsx` → `components/DetailsPage.jsx` (`hooks/useMonitor.js`, every bot; the existing APIs only — `/api/status` with `held_legs`, `/api/positions`, `/api/trades`, `/api/orders`, `/api/account`, and `delta_exit_threshold` from `/api/summary`), helpers in `lib/details.js`.
**Coin first, USD second:** P&L is in each bot's coin with fixed decimals (BTC 5, ETH 4, `formatCoinFirst`), USD only as a small "≈ $…" beside it (`approxUSD`); coins are never added together — where both bots appear, a BTC column and an ETH column.
From top to bottom:
- The **title and one text line of counts**: bots online, orders sent · filled, closes, entries skipped, since <date>, updated <time>.
  There are no tiles.
- A **sticky section bar** of anchor links with their counts: Positions · Missing legs · Trades · Working orders · Not managed.
  A link is amber when its section needs attention (a missing leg, a working order past its fill timeout, a position no bot manages).
- **Open positions** (`components/PositionsTable.jsx`, sticky header, scrolls inside its box).
  - It is grouped by strangle by default.
    A group reads "BTC · 25-day slot · expires 13 Nov · 16 days" with the group's P&L in coin ≈ USD, net Δ, Θ sum, Vega sum and Γ ×10⁻⁵.
  - There is one line per leg:
    - a call/put chip and the strike (mono; the full instrument in its `title`, a click opens the strangle's payoff modal);
    - qty, and entry → mark;
    - **premium kept**: a 64 px diverging bar around 0, −100..+100 % (`components/MicroBar.jsx`), and the %;
    - P&L in coin ≈ USD;
    - **|Δ| → exit**: a 64 px bar up to `delta_exit_threshold` with an amber tick at its end, amber from 80 % of it (0.24 for 0.30);
    - OTM %, stop at, Θ/day, Vega and Γ ×10⁻⁵;
    - **Close…** (outlined red, the bot's reason in its tooltip) only where the manual-close rule allows it, otherwise the cell is empty.
  - A missing leg is its own row "not held: <hold>" linking to Missing legs.
  - The filters (bot, strategy, type, moneyness, group by) are in the panel header.

**Missing legs** (`components/HeldLegs.jsx`) has one row each:
- the leg: bot, put/call chip, slot, expiry, and the leg it pairs with;
- held by: a chip, with the bot's reason in its tooltip;
- the market now: confirmed regime, bias and SMA trend;
- spot vs flip + buffer: a 120 px bar with the band shaded, the flip's tick and spot's dot, then the numbers;
- when it is released, in plain words: the cooldown's time, the snapshots counted and the release hours.

**Trades and results** (`components/TradeHistory.jsx`):
- The summary line has realized per coin with ≈ USD under it.
  It has the win rate with a win/loss dot strip (`components/DotStrip.jsx`, `role="img"` "5 of 9 won"), avg win ÷ avg loss (×), and opens.
- **By exit reason** (closes, won dots, BTC, ETH) and **By slot** (closes, won dots, realized in that slot's coin) sit side by side, stacked on a phone.
- Then comes the **log**, one line per trade:
  - time, bot, and an open/close chip;
  - the leg in short mono, "27 Nov 2,850 C", then slot, qty and price;
  - the reason chip and the first sentence of `plain`;
  - P&L in coin ≈ USD, % kept, and spot · DVOL.
- A line's caret (`aria-expanded`) opens a detail row with the full `plain`, the rule and its numbers (mono), and the greeks + instrument.
- The filters are All / Opens / Closes, bot and exit reason.
**Working orders** and **Not managed by the bot**: one line each when empty, else their tables, the explanation in the title's tooltip.
States: skeleton rows while loading, a stale bot's groups dimmed with the age of its last answer, a bot down says so above the table, an empty section is one line, a loss is red and nothing is hidden.
The bars are `aria-hidden` beside their number; section links, row carets and Close… are all keyboard-focusable.
Analytics: `details_section_jump` (`section`) and `details_trade_expand`.
Any signed-in role (Close… for a manager or an admin, as before)

### Activity page (its own page, `/activity?bot=<name>`; #385, E3 prototype screen 4)

"What the bot did, and why · <coin>": `app/activity/page.jsx` → `components/ActivityPage.jsx`, data from `/api/events` (the last 500 the bot keeps) and every open and close of `/api/trades` (`hooks/useActivity.js`, every 30 s), helpers in `lib/activity.js`.
There are four filters (`aria-pressed`):
- **Everything**.
- **Money in or out**: opens and closes.
- **Protection**: stop-loss, delta exit, GEX shed, MM cut, kill switch and manual close.
  Also a working sell cancelled by a freeze, the regime side, a shed or the kill switch, `risk_limit` frozen / MM breach, and every person's action.
- **Waiting**: skips and holds such as `gex_wait`, `repair_held`, `stop_deferred`, `no_free_expiry`, `bias_warmup`, `liquidity_cap`, `rebalance_wait_entry`, …
An event the mapping does not know is under Everything only, never dropped; `submitted` and `amended` (order mechanics) are left to Details (pro).
Entries are in day groups (BRT): **Today**, **Yesterday**, then the date.
Each entry has these parts:
- The time (HH:MM, monospace).
- A round icon with a text name: arrow down = money in, arrow up = bought back, shield = protection, clock = waiting, person = a person's action with "By <user> (<role>)".
- A **bold first line**: the `plain` sentence up to its first colon-and-space or full stop.
  The rest of the sentence follows it.
- **Paid to you** (opens) or **Result** (closes) in coin, signed and coloured by sign.
Three days at first, **Show earlier days** adds three.
Footer: "Every line comes from the bot's journal.
The raw entries, with all their numbers, are under Details (pro)." No `?bot=`, loading ("Not yet"), unavailable ("Unknown: <reason>"), a failed refresh (keeps the entries, "Not updating") and an empty journal each say so.
Any signed-in role

### Overview, answer first (#404; prototype "Overview — answer first" approved on #137)

**Coin first, USD second**: headline numbers are in each bot's coin with fixed decimals (BTC 5, ETH 4; `lib/tiles.js` `fmtCoin`), USD only as a secondary "≈ $… at today's prices" line, different coins never added together.
Top to bottom (`components/Monitor.jsx`): **1.
Status line** (`components/OverviewStatus.jsx`): a dot and "Both trading · updated 14:02" (or each bot's state, e.g. "BTC trading · ETH new trades paused"); below it one amber line per bot that holds something back, in plain words from its `held_legs` and a freeze (e.g. "ETH sells no new puts: negative gamma regime in a bear trend"; `heldBackLines`).
**2.
Four tiles** (`components/OverviewTiles.jsx` on `StatTile`, one row from `lg`, 2 columns below):
- *Coins earned this month* (realised, net of fees).
- *Open now* (unrealised).
- *Worst case* (one very bad day, with % of each account).
- *Margin used* (% of limit, a `MarginBar` per bot).

Each tile has one line per bot in its coin.
The footer is the ≈ USD sum, and a bot without a figure is named: "· without ETH".
The margin footer is the room left per bot.
Tiles are skeletons while loading, and a missing figure reads "unknown: <reason>", never 0.
**3.
P&L chart** (`components/OverviewPnl.jsx`, not a folding panel) has two switches:
- **Coin | USD**: Coin is the default, one small chart per bot; USD is one chart for all bots.
- **1d | 1w | 1m**: 1w is the default.

Both are buttons with `aria-pressed`, remembered per browser (`monitor.overview_pnl.unit`, `monitor.overview_pnl.range`).
The total is a solid line over a light area, realised is a dashed grey line, and there is a zero line.
Each chart's header reads "now … · realised …".
**4.
One band per bot** (`components/BotDashboard.jsx`, `aria-label` "<coin> dashboard").
- The header has the coin, spot, regime chip, trend chip with its ⓘ, "GEX n s ago" and DVOL.
  The margin bar "x % of limit" is on the right.
- The **price chart** (`components/BandChart.jsx`, an SVG, helpers in `lib/band.js`) is on the left, about 60 % wide and stacked above the list on a phone.
  - It draws 60 daily candles: up green, down neutral grey (`#64748b`), so red always means a loss.
  - The y-range covers the candles and every sold strike.
  - The corridor between the lowest and the highest sold strike is shaded (green 6 %).
  - It draws the flip with its shed band, SMA9 and SMA21 (dashed, action colour).
  - It draws the swing high/low walls (`sma.swing_high`/`swing_low`, labelled with their dates, in the y-range).
  - It draws the dashed sold strikes from `/api/strangles/payoff`.
    Each is labelled on the right edge ("put 74,000 · 25 d", days left), with the labels nudged apart so they never overlap.
  - It has a spot label.
  - The pills are SMA9 · SMA21 · Swing walls · Flip band · Sold strikes (`aria-pressed`, all on).
    SMA21 and the swing walls came back at the product owner's request (#415).

On the right is one **strangle row** per strangle (`components/StrangleRow.jsx`, a button).
- It shows the slot, expiry, days left, and P&L in coin with ≈ USD below.
- It has a range bar: the break-even corridor, a purple put tick, a blue call tick, the spot dot, and a hatched amber stub on a side with no leg.
- It says "spot x % from a break-even" ("past" outside it, red).
- It says "x % of the premium kept" with a small bar.
- It has an amber note when a leg is held back (`missingLine`).
- A click opens the payoff modal, and focus returns to the row.
A stale bot's band is dimmed with its age; a bot that is down says so; no strangles: "Nothing held: the bot opens a strangle when a slot is vacant and margin allows"; a brand-new bot shows its first-day panel in the list's place.
**5.** A "Details (pro) →" link to `/details` (#405: the detail panels are that page now), then the footer.
Analytics: `overview_unit_toggle` (`unit`) and `overview_layer_toggle` (`layer`)

### Overview dashboard (first block, one section per bot; #394 — superseded by #404 above: the tiles moved to the top row, the strangle cards became rows, the regime chart became the band's price chart)

charts first, few words (`components/BotDashboard.jsx`, helpers in `lib/regime.js`).
Per bot, top to bottom, **the header line** has:
- The coin and spot.
- A **regime badge**: POSITIVE / NEGATIVE from `market.gex_regime`.
  It adds "· confirmed" when the margin policy has confirmed the same regime on its daily closes (`risk.status.regime_known` / `regime_negative`), else "· live".
  A red "puts shed" / "calls shed" badge shows per side `held_legs` holds by a GEX shed.
- A **trend badge** (bull · bear · neutral, the direction the rules use, `trend`) with an ⓘ tooltip giving its reason (`sma.reason`, `SMATrendOf`).
  Under `gex_direction: bias` the tooltip gives the GEX bias and the SMA trend.
- The **GEX freshness** (`gex_as_of`): "GEX 40s ago", amber past 5 min, or "GEX unknown: no snapshot yet".

The warning chips stay in the page header.

**The regime chart** (`components/RegimeChart.jsx`, Recharts) draws:
- Daily candles from `sma.candles`: the last 60 UTC days and today's live one.
  A falling day is hollow, not colour alone.
- **SMA9** and **SMA21** as lines.
  Each is the day's mean of the 9/21 completed closes before it, so today's equal the bot's.
- The last confirmed **swing high and swing low** as horizontal walls labelled with their day.
- The **GEX flip** as a line, with its **shed band** flip × (1 ± `flip_buffer_pct`) shaded.
- The **median flip** of the bias window (dashed, `median_flip`, only under `gex_direction: bias`).
- The held strikes as faint lines labelled "floor"/"ceiling" with their DTE slot (`/api/positions`).
- A marker at the last price.

Every line kind toggles from the legend (`aria-pressed`, 44 px, wraps on a phone).
The chart's `aria-label` (and a `figcaption`) is its text alternative.
It gives regime, trend and reason, spot vs the flip and its band, spot vs SMA9 / SMA21, and the swings.
No candles → "Candles unknown: …", never a flat chart.
**The strangle payoff cards** (#391, `BotStrangles` of `components/StrangleCards.jsx`), ordered by DTE.
**Four stat tiles** (`components/DashboardTiles.jsx`):
- *This month* is realised, net of fees, in signed coin, e.g. "≈ +$410 · 1–27 Oct · as of 15:00".
  It also shows the six month bars of `components/MonthBars.jsx`, and links to the income page.
- *Open now* is unrealised coin and USD, marked "not realised".
- *Worst case* is the worse 3σ day as a signed loss in coin, USD and % of the account, with the move.
  It links to the worst-case page.
- *Margin used* is IM ÷ the active limit, e.g. "71 % of limit" or "IM 14.2 % of 20 % limit".
  It has a meter with the limit as a tick, and shows MM against its cap once over half of it.
Each tile's plain-words sentence is in its ⓘ tooltip (`components/InfoTip.jsx`: hover, focus or tap, `role="tooltip"` — never hover-only).
Missing data says "unknown: <reason>", never 0.
A brand-new bot shows its first-day panel instead of the tiles.
Then **the two P&L charts** (below), side by side from `xl`, open for every role.
Nothing after them: the detail panels (positions, missing legs, other positions, trades and working orders) are the **Details (pro)** page (#405); Activity and Account are their own pages too

### Investor summary (`components/InvestorSummary.jsx`; #246)

no longer on the overview (#394): its four answers moved into the dashboard tiles' tooltips; the component's `Signed` and `BotSummary` remain for the phone overview and the first-day panel.
The holder's four answers come from `/api/summary` (`components/InvestorSummary.jsx`, helpers in `lib/summary.js`), fetched with each poll:
- **What it holds**: strangles, legs, calls/puts, coin sold and its USD value, and nearest expiry.
- **Income per month**: the last 6 UTC months, newest first.
  Each has realised net of fees in coin and USD with its sign, and the number of closes.
  A month without closes says "no closes", never zeros.
  The open book's unrealised P&L is added.
- **Worst case now**: the one-day move each way in coin, USD and % of equity, signed.
  It says "% of equity unknown" without the equity, and "Unknown: reason" when spot or DVOL is missing.
  The API's model label sits beside it in the same type size.
- The **last action** in the journal's plain words, with its time, size, instrument and, for a close, its net result.
Losses and gains share size and weight; only the colour and the sign differ.
Empty states: nothing held, no closes yet (says what appears and when), no trade yet; a failed `/api/summary` or a bot that stopped answering → "Summary unavailable: …", no numbers.
Everything below is the detail on demand

### Phone overview (under 640 px only; #320)

`components/PhoneOverview.jsx`, helpers in `lib/overview.js`: **one bot at a time** (`?bot=<name>`, the first bot without it) instead of the header, dashboards and panels; under the headline, that bot's **regime chart** (#394, the same `components/RegimeChart.jsx`, scaled to the screen, its legend wrapping).
Compact header: "optionsBot", the TESTNET / LIVE chip, a **BTC | ETH** switch (`aria-pressed`, 44 px; it rewrites `?bot=`), a menu (Income, month by month · Worst case right now · **Details (pro)**, a link to `/details?bot=<name>`, #405) and the header's warnings as text badges (HALTED, OFFLINE / STALE, LOOP STALLED, IM over limit).
**Headline (h1)**: "This month: <±coin> after fees" (or "not yet: no close this month"), under it "About <±USD> · 1–19 Oct · running normally" (or the warning in words).
There are four stacked cards:
- **What it holds** reads "<n> positions.
  Nearest pays if <coin> stays between $put and $call until <expiry>", from `holding.nearest`.
  It has a **range bar**: the band between the strikes and a marker at spot, white inside the band and red outside.
  A one-legged nearest strangle names its missing side instead.
- **Making or losing** shows *This month* and *Open now*, signed.
  It has six month bars, with a loss bar hatched.
  The bars' label reads e.g. "Last 6 months: 5 up, July down".
  It links to the income page.
- **Worst case right now** is loss-styled: "A very bad day (<coin> −x %) would cost about <coin> (<USD>), y % of the account".
  It uses the worse of the two 3σ days.
  It reads "Unknown: <reason>" when not computed, and links to the worst-case page.
- **Last action · HH:MM** shows the plain-words sentence, the coin's move since (from `last_action.spot`) and a close's result in coin.
  "All actions, explained" opens the Activity page, `/activity?bot=<name>`, #385.
Footer: "From the bot's journal, after fees.
Worst case is an estimate.
Not investment advice." The desktop (640 px and up) is unchanged and shows every bot

### First-day panel (a brand-new bot; #321)

`components/FirstDayPanel.jsx`, in place of that bot's investor summary (and as the phone overview's headline) while the bot **holds nothing and its journal has no trade** (`isFirstDay`: `holding.legs` 0 and `last_action` null).
It says "Your <coin> bot started <20 minutes> ago" (from `/api/status` `uptime_sec`), then four steps:
- **Done** Connected to your account: the balance from `/api/status` `account.equity`, and "practice on testnet" off live.
- **Reading the market**:
  - *Now · about 1 more minute* until the first GEX snapshot (`market.gex_regime`).
  - *Now · waiting: …* in plain words while `risk.status.frozen`, e.g. "the market's mood must hold for 2 days".
  - Done after.
- **First positions**: *Next*, then *Now* once the market is read · usually within the hour.
- **Worst case, in money**: *Then*.

It ends with "While you wait: how it earns, in 60 seconds".
The first fill replaces it with the summary; a bot that is flat after trading keeps the summary's empty states.
No video button until E11's videos exist

### Header

one chip per bot (on the overview and the Account page): TESTNET / LIVE, data freshness (turns STALE / OFFLINE), **LOOP STALLED** when the API answers but the decision loop has not cycled for 3 intervals (no exits or stop-losses are being checked), HALTED after a kill switch, **IM over limit** (`risk.im_over_limit`), spot, DVOL, IV percentile.
The GEX regime, trend, SMA levels and the gamma flip with its ±1σ shed band (`flip_buffer_pct` = `gamma_flip_buffer_sd` × DVOL ÷ √365) are drawn on each bot's regime chart instead (#394); the person's menu is the menu bar's last item

### KPI strip

no longer shown (#405): Details (pro)'s line of counts replaced it; `components/KpiStrip.jsx` is kept for now.
It showed bots online, open legs, pending orders, orders sent / filled, closes, skipped entries, realised / open / total P&L (USD at spot) — **since the journal began** (the date is shown), restored when a bot restarts

### Account & collateral (its own page, `/account?bot=<name>`; #394)

The menu's **Account** item: `app/account/page.jsx` → `components/AccountPage.jsx`.
It uses `hooks/useMonitor.js` for every bot.
The account is shared, so every bot's policy row is shown, and `?bot=` only keeps the menu's choice.
The page has the header chips, then this panel's content, and **Not managed by the bot** below it.
No longer on the overview.
The Rebalance button and its dialog keep their roles and confirmation.
It shows the shared Deribit account, as Deribit reports it:
- The margin model (cross/segregated · portfolio/standard) and cross collateral on/off.
- USD totals (equity, margin balance, IM, MM) with **IM % / MM % bars**.
  The bars are amber from 50 % and red from 80 %; Deribit liquidates at MM 100 %.
- Per asset: balance, equity, margin balance, available, withdrawable, IM and MM (and projected, without the nearest expiry), IM % / MM %, and reserved.
Flags STALE data, a failed poll and high margin.
**Margin policy per bot** shows:
- IM used vs the active limit, and *why* (DVOL band, or negative gamma).
- MM used vs its limit.
- Whether new entries are allowed or FROZEN (with the reason).
- Any pending change with its countdown, e.g. *DVOL → band ≥70: 1 of 2 daily closes*.
- Beside the Rebalance button, an amber *IM over limit since HH:MM:SS — review with a dry run* badge while the bot reports `risk.im_over_limit`.
  That means IM is above the limit with no rebalance due; the bot resizes nothing by itself.
The limits are drawn as ticks on the IM / MM bars

### Open positions

On Details (pro) since #405, one line per leg (its layout there).
There is one row per bot + slot + strike.
Positions on the same strike (partial fills, rebalance top-ups) are merged and marked **×n**.
A merged row has the summed qty and P&L, the qty-weighted entry and the first stop to fire.
The columns are:
- instrument, slot (DTE · Δ) and call/put;
- **side**: SELL = short, premium collected; BUY = long, premium paid (e.g. an iron-condor wing);
- strike, DTE, qty, entry and mark;
- unrealized P&L (coin and USD);
- **P&L %**: unrealized P&L as % of the premium.
  +50% = half the premium is profit, and −100% = loss equal to the premium.
  The stop-loss fires at −200% with `stop_loss_multiplier: 2`.
- stop-loss mark;
- ITM/ATM/OTM and distance;
- Δ Γ Θ Vega.
Filter by bot, strategy, type, moneyness; group by strategy, slot, expiry or type; each group header totals unrealized P&L (coin and USD), **P&L %** (group P&L ÷ group premium, in USD) and the **position Greeks** (each leg's per-option Greek × qty, negative for shorts — so the header is on a different scale than the per-option Greeks in the rows; Δ and Γ blank when a group mixes BTC and ETH).
**Close…** button per row (manual buy-back at market): rendered only while the bot's regime-side block covers that leg's side — a confirmed negative gamma regime with puts in a bear trend or calls in a bull trend — outlined red with the bot's reason on hover; otherwise the cell is empty (#405).
A confirmation window shows the cost at the ask, the confirmed and live gamma regime, the flip ±1σ and the trend, and what the bot does next — **red** when the close would be rebuilt (the strangle's last leg: its slot is re-entered).
Needs `BOT_ADMIN_TOKEN` on the monitor and the bots; the proxy adds it server-side.
A manager or an admin only (Roles, below): a user gets 403

### Missing legs

On Details (pro) since #405 (its columns there).
There is one row per one-legged strangle (`/api/status` → `held_legs`, built by the bot from the same gate chain repair runs).
A row shows:
- the rule holding the repair now (regime side, stop hold, GEX anchor, GEX shedding, MM limit, …) and its reason;
- the confirmed regime, the bot's SMA trend and the reference script's bias (spot vs flip, negative regime);
- the **live flip + buffer** next to the **anchor flip + buffer** a GEX-shed leg waits on (snapshots counted, release hours), each with spot's distance to it;
- the stop cooldown's end

### Not managed by the bot

every other position on the account, of any kind (a hand-bought hedge, a future, …): side, size, average price, mark, P&L, Δ and its IM / MM, as Deribit reports them.
No bot trades or sizes these, but they use margin, so they are never hidden.
Collapsible; says "unavailable" (not "none") when the list could not be read

### Trade history & profitability

on Details (pro) since #405 as "Trades and results" (summary line, dot strips, one line per trade with a detail row; its layout there; the **details** toggle below is gone: each row's caret opens its detail row, folded for every role).
It covers every open and close since the journal began.
- Realized P&L (USD at each close, and per coin), closes and win rate.
- Average win vs average loss, and their ratio.
- P&L by exit reason and by slot (worst first).
- The list itself, with these columns:
  - time, bot, instrument, slot, qty and price;
  - the reason **with the rule and numbers that fired it**, e.g. "leg balance: call 262 vs put 162 … not a stop";
  - the option's **Δ (position Δ = −Δ × qty) · Θ · Vega at that moment**;
  - P&L coin/USD, P&L %, days held, and spot · DVOL at that moment.
**The reason cell leads with the bot's plain-words sentence** (`plain`, #287); the rule label and its numbers fold under a **details** toggle (`components/Details.jsx`, `<button aria-expanded>`), open at first for a manager or an admin, folded for a user (the role from the page's access token, `lib/auth/viewer.js` — a display default only).
A row without `plain` (an older bot) shows the rule and numbers as before, with no toggle; the generic "no plain-words explanation yet" sentence is shown as it is.
The filters by bot, open/close and exit reason read the underlying fields, never the sentence.
The activity feed shows the same reason and Δ on each close.
Collapsible

### Activity

**no longer on the overview** (#394): the Activity page above is the place.
`components/ActivityFeed.jsx` (the former panel) is kept for now.
Its feed holds:
- orders sent, re-priced, cancelled, opened and closed (take-profit, rolls, stop-loss, GEX shed, kill switch);
- skipped entries with the reason;
- margin-policy changes: entries frozen/unfrozen, IM limit changed, rebalance and MM breach, with DVOL, IV percentile and regime;
- **SMA trend changes** (`trend_changed`): old → new trend, the condition that fired, spot, SMA9, SMA21 and the swings.
  There is one per change, under either direction source.

Each has its market context (spot, DVOL, moneyness, open interest, regime).
**Each item's first line is the bot's plain-words sentence** (`plain`, #287) beside its time, bot and badge; the technical line (event, rule detail) and the market context fold under **details**, open at first for a manager or an admin, folded for a user.
An item without `plain` (an older bot) shows the technical line and context as before, no empty line; the generic sentence is shown as it is, and no event is hidden.
The tabs filter on the event, not the sentence

### P&L chart (Details (pro), "P&L in USD (every range)" since #404)

total and realized P&L across bots (USD), plus one dashed line per bot (BTC, ETH) when several run, so each coin's contribution is visible.
**Live** = the last 15 minutes of stored history, then this session every refresh (a reload never starts empty); **1h · 6h · 1d · 1w · 1m · All** = the bots' stored history (survives restarts), about 300 points per range.
Opens on **1w** (#394); a range the person picks is remembered per browser (`monitor.pnl.range`, `hooks/useStoredChoice.js`)

### P&L in coin (not shown since #404: the overview's P&L chart is Coin by default)

each bot's realized and total P&L **in its own coin** (BTC, ETH), net of fees, from the same stored history: the coin's own price moves are left out, so it shows whether the strategy is growing the coin stack.
One chart per bot (coins cannot be summed); **1h · 6h · 1d · 1w · 1m · All**, opens on **1w**, the choice remembered per browser on its own (`monitor.pnl_coin.range`)

### Working orders

entry/repair orders still on the book: fill progress, limit, time left before the fill timeout, age

### Worst case right now (its own page, `/worst-case?bot=<name>`; #318)

linked from the investor summary's worst-case answer; `app/worst-case/page.jsx` → `components/WorstCaseView.jsx` (polls `/api/summary` with `hooks/useBotSummary.js`, every `MONITOR_POLL_MS` but no faster than 5 s) → `components/WorstCaseReport.jsx`, helpers in `lib/worstcase.js`.
Header: **← Overview** back link, the bot, its TESTNET / LIVE badge.
**Headline**: the worse of the two 3σ days as a loss in coin, USD and % of the account, with the time it was estimated.
**What different days would do**: five rows from `worst_case.scenarios`.
- The rows are −3σ very bad, −2σ bad, 0 quiet, +2σ strong rise and +3σ very strong rise.
- Each row shows the move in whole %, how big it is in typical-day swings, and the result signed in coin · USD.
- It also shows the % of the account, signed, or "unknown: no equity reading".
- The table scrolls inside its card on a phone.
- **No row says how often such a day happens**, because a normal model understates crypto's tails and there is no measured figure.
**What the bot does on a bad day** (the stop multiple from `stop_loss_multiple`: "twice" for 2) and **What this estimate can't promise**, side by side from `md`, stacked on a phone; **How it's worked out** folded (`<details>`).
A loss carries a "▼ loss" mark beside its colour; gains and losses share size and weight.
Nothing held, DVOL or spot unknown, or an older bot without `scenarios` → the reason in words, no table and no zeros; loading → "Not yet"; a failed poll keeps the last figures under "Not updating: …".
No `?bot=` → links to each bot's page.
Any signed-in role

### How it works (its own page, `/how-it-works?bot=<name>`; #322, E3 prototype screen 7)

linked from the investor summary's "What it holds" answer ("How it works: a strangle in three pictures →"); `app/how-it-works/page.jsx` → `components/HowItWorksView.jsx` (polls `/api/summary` with `hooks/useBotSummary.js`) → `components/HowItWorks.jsx`, helpers in `lib/howitworks.js`.
Header: views nav **← Overview · How it works** (current), the bot, its TESTNET / LIVE badge. h1 "How your <coin> earns, in three pictures".
**1 · One position, two legs**: the put (the floor) and the call (the ceiling) of `strangle` — strike, what it promises on the expiry date, "paid now" in coin — with today's price between them and the total paid.
**2 · What you end with on <expiry>**: an SVG of the coin-settled payoff at expiry (keep all the payment between the strikes, zero at the break-evens, a hatched "loss grows" zone beyond; dashed line at today's price), a legend *you earn* / *you lose (hatched)*, the break-evens in text and a full `aria-label`.
**3 · How the bot defends you**: a price line (floor, ceiling, today, the exit lines when `exit_lines`; otherwise "exit lines move with the market (unknown: <reason>)"), then four cards — take profit, exit line, stop-loss ("twice" from `stop_loss_multiple`), wait then repair (days from `repair_cooldown_hours`).
**Words you'll see in Details (pro)**: leg, strike, premium, delta (+ "The exit line is a delta of <`delta_exit_threshold`>"), expiry, margin — each card's text is the glossary's `short` (`content/glossary.json`, one source of words).
No two-legged strangle held (`strangle` null) → the same page under "Example: a BTC (an ETH) strangle like the ones the bot sells" with fixed example figures and why, never zeros.
Loading → "Not yet"; a failed poll keeps the last figures under "Not updating: …".
No `?bot=` → links to each bot's page.
Any signed-in role

**Summary first, detail on demand (#288, #394, #404, #405).** The overview's P&L chart is not a panel: it never folds.
The detail is its own page, Details (pro): Open positions, Missing legs, Trades and results, Working orders, Not managed by the bot and the P&L in USD over every range.
It is reached from the menu or the overview's "Details (pro) →" link.
Its sections are not folded (the section bar jumps between them).
The exception is the "P&L in USD (every range)" panel at its end, folded at first and remembered per browser under `monitor.panel.pnl`.
The header's warnings (STALE, OFFLINE, LOOP STALLED, HALTED, IM over limit) are never folded.

### Income page

`/income?bot=<name>` (#319, prototype screen 3 of E3; `app/income/page.jsx`, `components/IncomePage.jsx`, helpers in `lib/income.js`, data from `/api/income` refreshed every minute by `hooks/useIncome.js`), reached from the investor summary's "Income, month by month →" link.
Header: "← Overview", "Income · <coin>", the TESTNET / LIVE badge and a **<coin> | USD** toggle (`aria-pressed`, remembered per browser under `income.unit`).
Four tiles: **Last 6 months, after fees** (with "% more <coin> than holding", or why it is unknown), **Average month** ("n months up, m down"), **Worst month** (with the coin's recorded move that month, omitted when not recorded) and **Worst week** (dates and % of the account; in coin in both units: the P&L history is in coin).
The card "Each month, where the money came from": newest first — month ("(so far)" for the current one), paid to you, paid to buy back and exchange fees (negative), result, holding alone; a loss month is tinted and labelled "· loss"; a month without closes says "no closes"; holding alone is 0 in coin, the USD change in USD, "unknown: <reason>" when not recorded.
Then "How the income works" and the footnotes (UTC months, closes only, testnet is practice, not investment advice).
Loading, unavailable (with the reason), a failed refresh (keeps the last figures and says so) and no `?bot=` each have their own message; every missing figure says "not yet" or "unknown", never 0.
The table scrolls inside its card at phone width; the toggle and links are 44 px targets.

### Every page

**The panels follow the window** (CSS only, no resize listeners).
On wide screens (`lg` and up) the Activity panel is as tall as its row: the P&L charts and working orders beside it, and never less than 20rem or 70 % of the window.
Its list scrolls inside it.
The P&L charts scale with the window height between a minimum and a maximum (`clamp()` on `vh`), and so does the rebalance dialog's action list.
On narrow screens the panels stack and the Activity list keeps a bounded height (60 % of the window, at least 20rem) with its own scroll bar.
What breaks at phone and tablet widths (360, 390, 768 px), touch-target sizes and the contrast of every token pair are in the [responsive audit](responsive-audit.md) (#207), the input of E12's prototype.

**Every P&L on the page is net of Deribit fees** (opening fees on open legs; opening share + closing fee on closes).
Closes journaled before fees were recorded (2026-10-06) are gross.
Counts, realized P&L, the activity feed and the trade history are **restored from each bot's journal** (`orders.log`) when it starts, so a restart of a bot or of the monitor loses nothing.
The P&L chart's ranges come from each bot's `data/pnl_history.jsonl` (one point every 60 s, kept for a year); its realized line is the journal's, so the chart, the KPIs and the trade history agree.
To start a fresh history, see `--reset-history` in `docs/operations.md`.
Other history (positions, events, change markers) is a later phase of the [dashboard spec](../../.claude/skills/option-bot-specialist/references/dashboard.md).
