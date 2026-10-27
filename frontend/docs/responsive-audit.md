# Monitor at phone and tablet widths: audit (#207)

The input for E12's prototype ("every monitor and settings screen works on a phone, from 360 px wide"). It records what breaks today at **360 px** (small Android), **390 px** (iPhone 12–15) and **768 px** (iPad portrait), and why, so the prototype fixes the real problems. **It decides nothing**: the fixes belong to the prototype and its stories.

## How it was measured

- **By reading the layout code** (`app/`, `components/`, `app/globals.css`) against Tailwind v4's breakpoints: `sm` 640 px, `md` 768 px, `lg` 1024 px, `xl` 1280 px. At 360 and 390 px no breakpoint applies; at 768 px `sm` and `md` apply, `lg` does not. No headless browser was run (the agent that wrote this has no browser), so there are no screenshots; widths of text are estimates from the font sizes (`text-xs` 12 px, `text-sm` 14 px, `text-base` 16 px) and are marked "about". The Playwright check `npm run test:phone` (#309, below) now measures them on `cmd/monitor-demo` data.
- **Space a panel's content gets** (the page has `p-3`, a panel a 1 px border and a `p-3` body): 360 − 24 − 2 − 24 = **310 px** at 360, **340 px** at 390, **718 px** at 768. The investor summary cards have `p-4` inside: 302 / 332 / 710 px. Panel headers have `px-4`: 302 / 332 / 710 px.
- **Touch targets** against 44 × 44 px (WCAG 2.5.5, AAA; WCAG 2.2's AA minimum, 2.5.8, is 24 × 24 px). Heights come from Tailwind v4's line heights: `text-xs` 16 px, `text-sm` 20 px, `text-base` 24 px, plus padding and border. So `px-2 py-0.5 text-xs` = **20 px**, `px-3 py-1 text-sm` = **28 px**, a `select` with `py-1 text-sm` and a border = **30 px**.
- **Contrast** with the WCAG 2.1 formula, computed by `__tests__/responsive-audit.test.js` from the tokens in `app/globals.css` and Tailwind's own slate scale (`node_modules/tailwindcss/theme.css`, converted from oklch to sRGB). A colour with an opacity modifier (`bg-profit/15`) is painted over what is behind it (`panel` inside a card, `surface` on the page). The test fails if a ratio here stops matching the tokens.

## Measured in a browser (#309)

`npm run test:phone` (`e2e/`, frontend/README.md "Develop") starts two `cmd/monitor-demo` bots, a production build of the monitor and headless Chromium, signs in as a throwaway admin, and measures the page at 360 × 800, 390 × 844 and 768 × 1024 and each dialog at 360 × 640 and 640 × 360. It writes `docs/responsive-measured.json` and screenshots under `test-results/phone/` (both git-ignored: demo data only, regenerated on every run), and fails only when something is worse than `e2e/phone-baseline.json`, which records today's state.

| Estimate above | Where the measurement is (`responsive-measured.json`) | How it is measured |
|---|---|---|
| Working orders pushes the whole page sideways at 360 and 390 px, fits at 768 | `widths.<w>.overflow`: `overflow_px` and `causes` (panel and element) | `scrollWidth − clientWidth` of the page; each panel (and each table in it) hidden in turn — a cause is one whose absence lowers the overflow |
| Almost every control is 20–30 px tall (pattern 4) | `widths.<w>.controls`: `total`, `under_44`, `under_24`, and the kinds of control under each with their smallest box | the box of every visible button, link, field, tab and menu item |
| Hover-only explanations (pattern 1) | `widths.<w>.title_only` | elements with a `title` whose words are not on screen |
| Header about 400–500 px tall at 360 px (pattern 7) | `widths.<w>.header_height_px` | the top header's box |
| Dialogs fit portrait, not landscape (pattern 8) | `dialogs.<close\|rebalance\|change_password>.<portrait\|landscape>`: box size, `fits` | the dialog's card inside the viewport. Close and Rebalance are enabled for the browser only (the demo has neither) and their POSTs answered by the test: nothing reaches a bot |

What counts as worse: a new cause of page overflow or 16 px more of it, a new kind of control under 24 or 44 px (named in the failure), a new hover-only explanation, a header 24 px taller, a dialog that stopped fitting. Counts are reported but not compared: the demo's feed and trades grow at random, so the number of rows (and of their buttons) differs between runs. A fix that makes a number better passes; record it with `UPDATE_PHONE_BASELINE=1 npm run test:phone` and commit the new baseline.

## What holds across the page

These apply to several panels; the table below refers to them by number.

1. **Hover-only information.** Many explanations live only in `title` attributes: column-header definitions (P&L %, Stop @, Side, Mark…), the reason a **Close** button is gray (`components/ManualClose.jsx`), the reason **Rebalance** is gray (`components/Rebalance.jsx`), LOOP STALLED's cause, the SMA trend's reason, badge details (`components/Badge.jsx` takes a `title`). A phone has no hover: none of these can be read by touch.
2. **Wide tables scroll sideways with no cue.** Tables sit in `overflow-x-auto` boxes; on a phone the scroll bar is hidden until you scroll, no column stays fixed, so once you scroll the instrument name is gone and you no longer know which row you are on.
3. **Small text.** Secondary lines are `text-xs` (12 px), some `text-[10px]`/`text-[11px]` (position freshness, trade detail, chart axes at 11 px). Readable on a desktop at arm's length; small on a phone held at reading distance, more so with the `muted` colour.
4. **Controls under 44 px.** Almost every control is 20–30 px tall: tabs, filters, Close, Rebalance, collapse headers, dialog buttons (sizes per row below).
5. **iOS zooms on focus.** Safari on iPhone zooms the page when a form field under 16 px gets focus. Every `select` (positions filters, trade-history filters) and the sign-in inputs are `text-sm` (14 px), so tapping them zooms the page and leaves it zoomed.
6. **Collapsed summaries disappear.** A collapsed panel shows its summary beside its title (`components/Panel.jsx`, `min-w-0 truncate`); the uppercase title keeps its width first, so on a phone the summary is truncated to a few characters or to nothing (details per panel below).
7. **The header is not sticky and grows tall.** At 360 px the bot chips wrap into many lines (below), so the first screen is mostly header; scrolling hides it, and with it the TESTNET/LIVE badge and freshness.
8. **Dialogs do not scroll.** Both confirmation dialogs are a fixed, centred box with no maximum height and no scrolling of their own (only the rebalance action list scrolls). When the content is taller than the screen (phone in landscape, about 360 px tall; browser bars; large text) the top and the buttons are cut off and cannot be reached.

## Panels at 360, 390 and 768 px

| Panel | Component (file) | 360 px | 390 px | 768 px | Actions and touch targets |
|---|---|---|---|---|---|
| Investor summary | `components/InvestorSummary.jsx` | One card per bot (`xl:grid-cols-2` never applies), the four answers stacked (`md:grid-cols-2` off). No overflow: `text-base`, cells wrap. The income table has no scroll box: its 4 columns fit 302 px only because cells wrap (e.g. "(this month)", "3 closes" on two lines; a number and its coin may split). Each card is tall: about 3 screens for two bots before anything else | Same as 360, a little less wrapping | Answers in 2 columns (about 347 px each): reads well | No controls. Readable by design (16 px). Model label is text, not hover |
| Phone overview (#320, under 640 px only) | `components/PhoneOverview.jsx`, `components/Monitor.jsx` | Replaces the header, summary and panels under `sm`: one bot with a BTC \| ETH switch, the month's result as the `text-xl` headline, then four stacked cards (holdings with the range bar, making or losing with six month bars, worst case, last action) and the footer. Nothing is wider than the screen: no table, the range bar and month bars are percentage widths. The full monitor is under the menu's Details (then as described in the rows below) | Same | Not shown: the desktop monitor (`sm:block`) | Coin switch, menu button, the menu's links and every card link are `min-h-11` (44 px). Warnings (HALTED, OFFLINE/STALE, LOOP STALLED, IM over limit) are text badges in the compact header, never hover-only |
| First-day panel (#321, a brand-new bot) | `components/FirstDayPanel.jsx` | In place of that bot's summary (and on the phone overview): headline, four step cards stacked (`sm:grid-cols-2 xl:grid-cols-4` off), the "how it earns" card. Text wraps, nothing scrolls | Same | Step cards 2 per row | No controls |
| Income page (#319, its own route) | `components/IncomePage.jsx`, `app/income/page.jsx` | Built for 390 px: header wraps (back link, title, badge, toggle); tiles one column (`sm:grid-cols-2 lg:grid-cols-4`), `text-lg` values; the month table (6 columns, `min-w-[40rem]`) scrolls inside its card (`overflow-x-auto`), never the page | Same as 360 | Tiles in 2 columns; the table still scrolls a little inside its card | "← Overview" link and the coin / USD toggle are `min-h-11` (44 px) targets; no hover-only text |
| Header | `components/Header.jsx`, `components/UserMenu.jsx`, `components/SignOut.jsx` | `flex-wrap`; no horizontal overflow, but each bot chip wraps its market line, flip band and SMA levels into about 6–8 lines, so with two bots the header is about 400–500 px tall — most of the first screen (pattern 7). Spot, DVOL, regime, trend, flip and SMA run together as one `text-muted` run of text; the SMA line is `text-xs`. Explanations (LOOP STALLED cause, SMA reason, flip meaning) are hover-only (1) | Same as 360 | Chips about half as tall; still 3–4 lines per bot | Account ▾ (#212): `py-1 text-sm` + border = 30 px tall, under 44 (4); its menu (`w-44`, right-aligned) holds Change password and Sign out, `py-1.5 text-sm` = 32 px each (4). Badges are not controls |
| KPI strip | `components/KpiStrip.jsx`, `components/StatTile.jsx` | `grid-cols-2`: 8 tiles in 4 rows, each about 151 px wide. Values (`text-lg`) fit; the `text-xs` sub-lines ("net of opening fees · open legs at live mark") wrap to 3–4 lines, so tiles in one row differ in content height | 2 columns, tiles about 166 px | `sm:grid-cols-4`: 2 rows of 4, about 174 px each | No controls |
| Account & collateral | `components/AccountPanel.jsx`, `components/MarginBar.jsx`, `components/Panel.jsx` | Title "Account & collateral (shared by all bots)" (uppercase, about 360 px of text) wraps to 2 lines and the collapsed summary (IM vs limit per bot, worst MM, MARGIN HIGH / FROZEN / STALE badges) is truncated to nothing (6) — the folded state that should carry the warnings shows none. Open: totals tiles 2 per row and the margin bars full width read well; the asset table (`min-w-[900px]`, 11 columns) scrolls sideways (2); the margin-policy table (`min-w-[700px]`) scrolls too and **Rebalance** is in its last column (see "Close and Rebalance" below) | Same; summary still hidden | Summary partly visible (title about 360 px of 710; the rest truncated, badges at the end cut first). Asset table still scrolls (900 > 718); margin-policy table fits (700 ≤ 718) | Collapse header: the button is the heading line only, 20 px tall (40 px when the title wraps) (4). Rebalance 20 px (4) |
| Open positions | `components/PositionsTable.jsx`, `components/Filters.jsx`, `components/ManualClose.jsx`, `components/Badge.jsx` | The table is `min-w-[1100px]` with **19 columns** in an `overflow-x-auto` box: about 3.5 screens wide (2). The group header row spans 10 columns, so its P&L and Greeks are off screen at first. **Close** is the last column, about 800 px to the right. The five filters sit in the panel header beside the title: the title wraps to two lines ("OPEN / POSITIONS") and the filters stack in about five rows, about 170 px of header before the first row of data. Bid/Ask flashes on every change (`.flash`) — fine on a phone | Same | Still scrolls (1100 > 718): about 1.5 widths. Filters wrap to 2 rows beside the title | Filters: `select` 30 px tall (4), iOS zoom on focus (5). Close 20 px tall (4), its reason when gray is hover-only (1). Column definitions hover-only (1) |
| Missing legs | `components/HeldLegs.jsx`, `components/Panel.jsx` | 7 columns, `w-full` in a scroll box; its narrowest layout is about 700 px (the instrument "with BTC-27NOV26-90000-C" in mono does not wrap), so it scrolls sideways (2). Title "Missing legs: why repair holds them" (about 300 px) leaves no room: the "n held" summary is hidden when collapsed (6). Column meanings (release rules) are hover-only (1) | Same | Fits, tightly (about 700 of 718) | Collapse header 20 px (4). No other controls |
| Not managed by the bot | `components/OtherPositions.jsx`, `components/Panel.jsx` | 9 columns, `w-full` in a scroll box, narrowest about 730 px: scrolls (2). Title (about 360 px) hides the summary when collapsed (6) | Same | About at the edge: may scroll by a few pixels | Collapse header 20 px (4). No other controls |
| Trade history & profitability | `components/TradeHistory.jsx`, `components/StatTile.jsx`, `components/Panel.jsx`, `components/Details.jsx` | Stat tiles 2 per row (fine). "By exit reason" and "By slot" stack (`lg:grid-cols-2` off) and fit (4 columns, no scroll box needed). The trade list has 13 columns, three of them `whitespace-nowrap`, narrowest about 1,200 px: scrolls about 4 screens (2); detail lines are 11 px (3). Collapsed summary truncated to a few characters (6) | Same | Tiles 3 per row (`sm:grid-cols-3`). Trade list still scrolls (about 1.7 widths) | Filters: `select` `py-0.5 text-sm` ≈ 24 px tall, no visible focus style, iOS zoom on focus (4, 5). "Show 50 more" 28 px (4). Collapse header 20 px (4). The reason cell's "details" toggle (#287) 16 px (4) |
| Activity | `components/ActivityFeed.jsx`, `components/Panel.jsx`, `components/Details.jsx` | Full width (`lg:grid-cols-3` off), after Trade history. The list has its own scroll (`max-h-[max(20rem,60vh)]`, 384 px on a 640 px tall screen) inside the page's scroll: a swipe on the list scrolls the list, not the page, until it ends. Lines wrap (`flex-wrap`); context lines `text-xs` muted (3) | Same (60vh = 506 px on an 844 px screen) | Same as phones: still stacked, full width | Four filter tabs `px-2 py-0.5 text-xs` = 20 px tall, 4 px apart (4). The "details" toggle under each item (#287) is `text-xs`, no padding = 16 px (4) |
| P&L chart | `components/PnlChart.jsx`, `hooks/usePnlHistory.js` | Full width, height `clamp(14rem,30vh,28rem)` = 224 px. Y axis takes 70 px, so the plot is about 220 px wide; X labels thin out (`minTickGap` 50) to about 3–4. Axis text 11 px (3). The tooltip shows on touch (Recharts) and can cover the line being read | Plot about 250 px | Plot about 630 px: reads well | Seven range tabs, 20 px tall, wrap to 2 rows at 360 (4) |
| P&L in coin | `components/CoinPnlChart.jsx` | One chart per bot, height `clamp(10rem,20vh,20rem)` = 160 px; the line "BTC · total … · realized …" wraps. Axis 11 px (3) | Same | Reads well | Six range tabs, 20 px tall (4) |
| Working orders | `components/PendingOrders.jsx` | **The table has no scroll box.** 7 columns, narrowest about 540 px (mono instrument + "(repair)", slot, fill, limit, timeout, age), wider than the 310 px panel, so it pushes **the whole page** sideways: every panel then shifts with a sideways swipe | Same (340 px) | Fits (full-width panel, 718 px) | No controls |
| Close dialog | `components/ManualClose.jsx` (`ConfirmCloseModal`) | 328 px wide box (294 px of content). Fits a portrait screen (about 450–500 px tall: title on 2 lines, a 6-row `dl` whose values wrap, two notes, buttons). Not in landscape or with large text (8). See "Close and Rebalance" | Same | Fits (`max-w-lg`) | Cancel and "Buy back at market" `px-3 py-1 text-sm` = 28 px tall, 8 px apart (4); Escape closes it, but a phone has no Escape — Cancel does |
| Rebalance dialog | `components/Rebalance.jsx` (`RebalanceModal`) | 328 px wide. The action list scrolls (`max-h-[max(16rem,40vh)]`: 256 px on a phone, since 40vh is less); with the title, the IM line (3 lines at this width) and the buttons the box is about 450 px tall: fits portrait, cut off in landscape (8). Action details are `text-xs` muted (3) | Same | Fits (`max-w-2xl`) | Cancel and "Run rebalance" 28 px tall, 8 px apart (4) |
| Change password dialog (#212) | `components/ChangePassword.jsx` | `max-w-sm` box in `p-4`: 328 px wide. Three fields, the 12-character rule and the buttons, about 330 px tall: fits portrait; the box does not scroll, so in landscape with the on-screen keyboard its buttons can be off screen (8) | Same | Fits | Inputs `py-1.5 text-sm` + border = 34 px (4), 14 px text: iOS zooms on focus (5). Cancel and Change password 28 px tall, 8 px apart (4) |
| Worst case right now (`/worst-case?bot=`, #318) | `components/WorstCaseView.jsx`, `components/WorstCaseReport.jsx` | One column: headline (`text-xl`, wraps to 3–4 lines), the five-day table in its card's own scroll box (`min-w-[34rem]`, so it scrolls inside the card, never the page), then the two cards stacked (`md:grid-cols-2` off) and "How it's worked out" folded. Body text 16 px | Same; the table still scrolls inside its card (544 > 326 px) | The table fits; the two cards side by side (about 347 px each) | "← Overview", the bot picker links and the "How it's worked out" toggle are `min-h-11` (44 px). No other controls |
| How it works (`/how-it-works?bot=`, #322) | `components/HowItWorksView.jsx`, `components/HowItWorks.jsx` | Built for 390 px: one column — the two leg cards and today's price stacked (`md:grid-cols-3` off), the payoff and price-line pictures are SVGs scaled to the card width (`w-full h-auto`, `viewBox` 360 wide, so nothing scrolls; their in-picture labels are 9–10 px at that width, the same figures are in the text under each picture), the four defence cards and the six glossary cards stacked. Body text 16 px | Same | Leg cards in 3 columns, defence cards in 2 (`sm:grid-cols-2`), glossary in 2 | "← Overview" and the bot picker links are `min-h-11` (44 px). No other controls |
| Users (#306, admins only, `/admin/users`) | `components/UsersAdmin.jsx`, `components/AddUserForm.jsx`, `components/UserActionDialog.jsx`, `app/admin/users/page.jsx` | Add user: the five fields stack in one column (`sm:grid-cols-2` off). The users table (6 columns, the actions column holds three buttons that wrap) is in an `overflow-x-auto` box, narrowest about 650 px: scrolls sideways (2). The confirmation dialog is `max-w-sm` like Change password: fits portrait, does not scroll (8) | Same | Add user in 2 columns; the table fits, tightly | Row buttons `px-2 py-0.5 text-xs` + border = 22 px (4); the role `select` and the form's inputs are `text-sm`: iOS zooms on focus (5). Why your own Delete and Reset password are gray is hover-only (1) |
| Reset link (#307, Users page) | `components/ResetLinkDialog.jsx` | `max-w-md` box in `p-4`: 328 px wide. The link is a read-only `font-mono text-xs` input that holds it in one line (scrolls inside the field); the expiry and buttons below: about 260 px tall, fits portrait and landscape (not measured in a browser yet) | Same | Fits | Copy link and Done `px-3 py-1` = 28 px tall, 8 px apart (4) |
| Set a new password (`/reset`, #307, public) | `components/ResetPasswordForm.jsx`, `app/reset/page.jsx` | `max-w-sm` form like Sign in: fits (not measured in a browser yet) | Fits | Fits | Inputs `py-1.5 text-sm` + border = 34 px (4), 14 px text: iOS zooms on focus (5); Set password 38 px |
| Audit (#308, #249, admins only, `/admin/audit`) | `components/AuditTrail.jsx`, `app/admin/audit/page.jsx` | Filters wrap (`flex-wrap`): person, action, from, to, Apply, Clear on 3–4 rows. The table (6 columns; detail `break-all` mono `text-xs`) is in an `overflow-x-auto` box: scrolls sideways (2) (not measured in a browser yet) | Same | Filters on 1–2 rows; the table fits, tightly | Filter fields and buttons `py-1 text-sm` + border ≈ 30 px (4); 14 px text: iOS zooms on focus (5). "Older" 30 px |
| Sign in (not in the README list; the only other screen) | `components/LoginForm.jsx`, `app/login/page.jsx` | `max-w-sm` form, full width minus margins: fits | Fits | Fits | Inputs `py-1.5 text-sm` + border = 34 px; Sign in button `py-1.5` at 16 px = 38 px (4). Inputs are 14 px: iOS zooms on focus (5) |

`components/Monitor.jsx` lays these out in this order (one column below `lg`); `components/Badge.jsx`, `components/Panel.jsx`, `components/StatTile.jsx` and `components/MarginBar.jsx` are the shared pieces named above.

## Close and Rebalance at 360 px

| Action | Reachable at 360 px | Usable at 360 px | Why |
|---|---|---|---|
| Close button | partly: only by scrolling the positions table about 800 px sideways, past 18 columns, with no fixed column to show which instrument the row is | partly: 20 px tall (target 44); when gray, the reason is a hover-only `title`, so on a phone you cannot learn why it is off | `components/ManualClose.jsx` `CloseButton`, in the last column of `components/PositionsTable.jsx` (`min-w-[1100px]`) |
| Close dialog | yes in portrait; no in landscape or with large text: the box does not scroll, so its top or its buttons can be off screen | partly: everything fits 294 px by wrapping; buttons 28 px tall and 8 px apart, and "Buy back at market" (an order at market) sits next to Cancel | `components/ManualClose.jsx` `ConfirmCloseModal`: `fixed inset-0 flex items-center`, no `max-h`/`overflow-y-auto` on the box |
| Rebalance button | partly: only with the Account panel open (it is collapsible and remembered closed per browser) and by scrolling the margin-policy table sideways to its last column (`min-w-[700px]` in 310 px) | partly: 20 px tall; when gray the reason is hover-only; the "IM over limit" badge below it is readable | `components/Rebalance.jsx` `RebalanceButton`, in `components/AccountPanel.jsx` `RiskTable` |
| Rebalance dialog | yes in portrait (about 450 px tall); no in landscape: only the action list scrolls, the box does not | partly: list readable (wraps), details `text-xs`; Cancel and "Run rebalance" 28 px tall and 8 px apart | `components/Rebalance.jsx` `RebalanceModal` |

## Contrast of the design tokens

Text colours on the backgrounds they are used on. `#e2e8f0` is the page's default text colour (set on `html, body` in `app/globals.css`, not a token). WCAG 2.1 AA: **4.5:1** for body text, **3:1** for large text (≥ 24 px, or ≥ 18.7 px bold). Nearly all text on the monitor is 12–16 px, so the **body** column is the one that applies; only `text-lg` bold values (KPI tiles, 18 px) and headings come close to "large".

| Text | Background | Used in | Ratio | AA body (4.5:1) | AA large (3:1) |
|---|---|---|---|---|---|
| `#e2e8f0` | `surface` | text outside cards (rare) | 15.27:1 | pass | pass |
| `#e2e8f0` | `panel` | default text in every card, the header, dialogs, tooltips | 14.07:1 | pass | pass |
| `white` | `panel` | coin name in P&L in coin | 17.34:1 | pass | pass |
| `muted` | `surface` | footer line under the panels | 7.34:1 | pass | pass |
| `muted` | `panel` | panel titles, labels, secondary lines, chart axes | 6.76:1 | pass | pass |
| `muted` | `slate-800/40 over panel` | "· n legs" on a positions group row | 6.36:1 | pass | pass |
| `slate-200` | `slate-800/40 over panel` | group rows; "After the close" note in the Close dialog | 13.23:1 | pass | pass |
| `profit` | `panel` | gains, P&L > 0 | 7.61:1 | pass | pass |
| `loss` | `panel` | losses, MM at its limit, errors | 4.72:1 | pass | pass |
| `warn` | `panel` | "last cycle", pending, (repair), Unknown | 8.07:1 | pass | pass |
| `profit` | `profit/15 over panel` | good badge (allowed, freshness, BUY, Top up) | 5.89:1 | pass | pass |
| `loss` | `loss/15 over panel` | bad badge (HALTED, LOOP STALLED, ITM, MARGIN HIGH); dialog errors and "Would be rebuilt" | 4.1:1 | **fail** | pass |
| `warn` | `warn/15 over panel` | warn badge (STALE, FROZEN, SELL, ATM, MVP) | 6.26:1 | pass | pass |
| `call` | `call/15 over panel` | call badge | 5.29:1 | pass | pass |
| `put` | `put/15 over panel` | put badge | 5.16:1 | pass | pass |
| `testnet` | `testnet/20 over panel` | TESTNET badge | 5.46:1 | pass | pass |
| `white` | `live` | LIVE badge | 3.76:1 | **fail** | pass |
| `slate-200` | `slate-700/60 over panel` | neutral badge (margin model, OPEN) | 10.56:1 | pass | pass |
| `muted` | `slate-800` | muted badge (OTM, CLOSE), inactive tabs, "Show more" | 5.72:1 | pass | pass |
| `white` | `slate-600` | selected tab | 7.56:1 | pass | pass |
| `#e2e8f0` | `slate-700` | Cancel / Close window buttons | 8.39:1 | pass | pass |
| `loss` | `loss/20 over panel` | Close button (active) | 3.85:1 | **fail** | pass |
| `action` | `action/20 over panel` | Rebalance button (active) | 4.24:1 | **fail** | pass |
| `white` | `loss` | "Buy back at market" | 3.67:1 | **fail** | pass |
| `white` | `action` | "Run rebalance" | 2.98:1 | **fail** | **fail** |
| `slate-500` | `slate-800` | gray Close / Rebalance (disabled: WCAG exempts it, listed for completeness) | 3.07:1 | fail | pass |
| `loss` | `loss/10 over surface` | kill-switch banner | 4.73:1 | pass | pass |
| `profit` | `profit/10 over panel` | "Allowed:" note, "Done." | 6.48:1 | pass | pass |
| `warn` | `warn/10 over panel` | "Partly done" | 6.9:1 | pass | pass |

**What fails, in short:** every text in the `loss` (red) family on a tinted red background, and white on the strong `live`, `loss` and `action` fills — i.e. the danger badges (HALTED, LOOP STALLED, MARGIN HIGH, ITM), the LIVE badge, the Close button and both dialogs' order buttons ("Buy back at market", "Run rebalance"; the latter fails even as large text). These are the elements that matter most when something goes wrong. `loss` text on the plain panel passes, narrowly (4.72:1). Everything else passes AA with room.

## Contrast of non-text marks

Borders, bars, chart lines and focus rings need **3:1** against what is next to them (WCAG 2.1, 1.4.11) when they carry meaning.

| Colour | Background | Used for | Ratio | AA non-text (3:1) |
|---|---|---|---|---|
| `line` | `surface` | card borders | 1.29:1 | **fail** |
| `line` | `panel` | table rules, chip and input borders | 1.19:1 | **fail** |
| `testnet` | `panel` | focus ring (`outline-testnet`); P&L chart "Total" line (`#38bdf8`) | 8.09:1 | pass |
| `profit` | `panel` | P&L chart "Realized" line | 7.61:1 | pass |
| `warn` | `panel` | first per-bot chart line (`#f59e0b`) | 8.07:1 | pass |
| `put` | `panel` | second per-bot chart line (`#c084fc`) | 6.56:1 | pass |
| `loss` | `panel` | third per-bot chart line (`#f43f5e`) | 4.72:1 | pass |
| `#2dd4bf` | `panel` | fourth per-bot chart line | 9.32:1 | pass |
| `#475569` | `panel` | the chart's zero line | 2.29:1 | **fail** |
| `profit` | `slate-800` | margin bar fill, under 50 % | 6.43:1 | pass |
| `warn` | `slate-800` | margin bar fill, 50–80 % | 6.82:1 | pass |
| `loss` | `slate-800` | margin bar fill, from 80 % | 3.99:1 | pass |
| `slate-200` | `slate-800` | limit ticks on the margin bars | 11.9:1 | pass |

`line` borders are decoration for cards and table rules (1.4.11 does not require 3:1 there), but they are also the **only edge of the form fields** (filters' `select`, sign-in inputs) and of the Sign out button, where it does apply. The zero line of the P&L chart is the reference that tells a gain from a loss, at 2.29:1. The focus ring passes, but the collapse headers use the browser's default outline colour (`focus-visible:outline` with no colour) and the trade-history filters have no focus style of their own.

## Patterns already in the frontend

Listed for the prototype to reuse or not; no choice is made here.

- **Collapsible panels with a one-line summary**, remembered per browser (`components/Panel.jsx`, `collapsible`, `storageKey`, `summary`): Account, Missing legs, Not managed, Trade history already use it.
- **Grouping and merging rows** (`lib/monitor.js` `groupRows`, `consolidateRows`; `components/Filters.jsx` "Group by"): positions grouped by strategy, slot, expiry or type, each group row with its totals — a group row is already a compact summary of its legs.
- **Plain-language summary per bot** (`components/InvestorSummary.jsx`): the four answers in 16 px text, no table, already readable at 360 px.
- **Stat tiles in a responsive grid** (`components/StatTile.jsx`, `grid-cols-2 sm:grid-cols-4 lg:grid-cols-8`): the one place that already changes column count by width.
- **Tabs as filters** (`components/ActivityFeed.jsx`, `components/PnlChart.jsx`: `role="tab"` buttons).
- **Scrolling boxes** (`overflow-x-auto` around wide tables; `max-h-…` + `overflow-y-auto` for the activity list and the rebalance action list).
- **Paging a long list** (`components/TradeHistory.jsx`: 50 rows, then "Show 50 more").
- **Sizes that follow the window** (`clamp()` on `vh` for chart heights; `max(…, vh)` for the activity and rebalance lists).
- **Plain-words tips for every label** (`content/glossary.json`, `lib/glossary.js` `findTerm`): the text that today lives in hover `title`s, ready for a tap-to-read tip.
- **Badges with tones** (`components/Badge.jsx`): one component for every status chip, so a colour or size change is one edit.
