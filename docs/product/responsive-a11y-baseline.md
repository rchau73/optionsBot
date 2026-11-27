# Accessibility baseline of the monitor (E12, #763)

What the monitor misses today on colour contrast, text size, tap targets and screen-reader structure, ranked by the pages affected, and what each fix needs from the designer.
It measures only: no component or colour changes with it.
The fixes come with the E12 UI stories, once the prototype (#654) is approved.
For each failing contrast pair, the nearest passing colours and what each does to the same token's other pairs are in [`responsive-a11y-contrast-candidates.md`](responsive-a11y-contrast-candidates.md) (#878), the designer's decision sheet.

## The bar

- **Contrast:** WCAG 2.x AA — 4.5:1 for body text, 3:1 for large text (24 px, or 18.66 px bold).
- **Text size:** at least 12 px (the story's assumption; `MIN_TEXT_PX` in `frontend/lib/a11y.js`).
- **Tap targets:** at least 44 × 44 CSS px (WCAG 2.5.5; `MIN_TAP_PX`).
- **Themes:** the monitor has one theme, dark; there is no light theme to check.

## How it is measured

- Contrast: `frontend/__tests__/a11y-tokens.test.js`, in `npm test`, from the tokens of `frontend/app/globals.css` and Tailwind's palette.
  The failures below are pinned there: a new one fails the test, a fixed one must leave the list.
- Text size and tap targets: `npm run test:phone` writes `frontend/e2e/a11y-report.json` at 360 px, report-only.
  Its `ranked` list is the browser's count of pages per finding and replaces the source estimates below once the first run is reviewed.

- Text resize and reflow (#887): the same run repeats every page with the root font size at 200 % (360 px wide) and at 320 px (normal text).
  It writes, per page and pass, under `resize` in `a11y-report.json`, with a `resize.ranked` list:
  `overflow_px`, `clipped_text` (text in an `overflow: hidden` box whose content is larger than the box)
  and `offscreen_controls` (visible controls past the screen's edge, not those inside a box that scrolls sideways by design).
  Report-only, like the rest: the run exits 0 whatever it finds.

## Findings, ranked by pages affected

Pages are the eight menu pages (Overview, Details, Positions, Income, Worst case, Activity, Account, How it works) and the assistant panel.
Contrast ratios are measured by the token test; text sizes and tap targets come from the source until the first `a11y-report.json` is reviewed; the pages are where the component is rendered in the source.

| # | Finding | Measured | Pages affected | What the fix needs | Owner |
|---|---|---|---|---|---|
| 1 | Controls under 44 × 44 px: the menu bar's links, the Account and Ask buttons in the header | source (sizes in the report) | 9: every page | a touch size for the header and menu bar on a phone (bigger hit area, same look on desktop) | designer, then developer |
| 2 | White on the red "LIVE" badge (`text-white on bg-live`) | 3.76:1 | 9: every page when the environment is live (the header; also Income, Worst case, How it works) | a darker live red, or dark text on it | designer (token) |
| 3 | Red on a light red tint (`text-loss on bg-loss/15`): the "bad" badge, form errors | 4.10:1 | 5: Overview, Details, Positions, Activity, Account | a lighter loss text on tints, or a stronger tint | designer (token) |
| 4 | Text under 12 px (`text-[9px]`–`text-[11px]`): table sub-labels, chart labels, month bars | source | 4: Overview (month bars), Details (positions, trades), Positions (payoff chart), How it works | a smallest type size of 12 px and what it does to dense tables | designer |
| 5 | Small tabs and chips: Activity filters, chart range buttons, the Close button | source (sizes in the report) | 3: Overview, Details, Activity | the same touch size as #1, with spacing so rows stay readable | designer, then developer |
| 6 | White on the red Close / Delete buttons (`text-white on bg-loss`) | 3.67:1 | 3: Overview, Details (Close), Account (Delete) | a darker red for filled buttons, kept apart from the loss colour of figures | designer (token) |
| 7 | Indigo on an indigo tint (`text-action on bg-action/20`): the Rebalance and hedge buttons | 4.24:1 | 3: Overview, Details, Positions | a lighter action text, or a stronger tint | designer (token) |
| 8 | Grey on slate (`text-slate-500 on bg-slate-800`): a disabled Rebalance / hedge button | 3.07:1 | 3: Overview, Details, Positions | WCAG exempts disabled controls; the designer decides whether the reason should read better | designer |
| 9 | White on the indigo primary button (`text-white on bg-action`) | 2.98:1 | 2: Overview (Rebalance), Account (password, download), and the admin pages | a darker action colour for filled buttons | designer (token) |
| 10 | Red on a faint red tint (`text-loss on bg-loss/10`): the "bot unreachable" banners | 4.34:1 | 2: Overview, Details (only when a bot is unreachable) | as #3 | designer (token) |
| 11 | Red on a red tint (`text-loss on bg-loss/20`): a request draft's chip | 3.85:1 | 1: the assistant panel | as #3 | designer (token) |

Ties are ordered by how often a person meets them (the header before a dialog).
The admin pages (Users, Audit, Analytics) are outside the customer's view and are counted only where noted; their small text (`DeliveryTiles`, `ReleaseTrain`, `UsageCards`) is in the report.

### Admin pages and sign-in (#883)

`npm run test:phone` measures them too since #883: `users`, `audit`, `analytics`, `analytics-delivery` (signed in as the throwaway admin) and `sign-in` (signed out), each in `a11y-report.json` at 360 px.
They enter `ranked` with the menu pages, so a finding shared with the header is counted on them as well; rows found only on these pages are marked **admin** (or **sign-in**) here.
Measured by the first reviewed run (2026-11-26, `e2e/phone-baseline.json` committed; counts at 360 px against the 44 px and 12 px bars, `resize` from #887):

| Finding | Measured (360 px) | Pages affected | What the fix needs | Owner |
|---|---|---|---|---|
| Table actions 22 px high ("Delete" 55×22, "Reset link" 73×22), the role `select` 87×25, "Reset password" and "Sign out everywhere" 38 px, the Add user form's inputs 34 px and button 28 px | 12 controls under 44 px, 3 under 24 px; no overflow, nothing clipped at 200 % or 320 px | admin: Users | the touch size of #1 for table actions and form fields | designer, then developer |
| Filter fields and buttons 29–30 px (Apply, Clear, Export CSV, From, To, Person, Action) | 8 controls under 44 px, 1 under 24 px; at 200 % text the filter form overflows by 118 px and pushes 2 fields off screen | admin: Audit | as #1, and a filter row that wraps at large text | designer, then developer |
| Small text in the tiles, release train and usage cards (`DeliveryTiles`, `ReleaseTrain`, `UsageCards`) | not seen: 0 small text, 1 control under 24 px (the shared "← Monitor" link). The phone check has no analytics data, so these cards may not render; re-measure with data before closing this row | admin: Analytics, Analytics · Delivery | as #4 | designer |
| Inputs 34 px (Username, Password), Sign in 38 px; 14 px inputs zoom on iOS | 3 controls under 44 px, none under 24 px; no overflow, nothing clipped at 200 % or 320 px | sign-in | as #1, and 16 px inputs | designer, then developer |

## Screen-reader structure

What a screen reader meets on each whole page (#840): landmarks, regions, heading order, the names of buttons, links and fields, table headers and dialog names.
`frontend/__tests__/a11y-pages.test.jsx` renders the eight menu pages and the phone overview, each under the menu bar as the layout puts it, on the shared fixtures (the demo's default scenario, `frontend/test/demoScenarios.json`, and `frontend/test/resultFixtures.js`).
It runs axe with only those rules and pins the counts in `frontend/__tests__/a11y-pages-baseline.json` (page → rule → nodes), every page with an entry even when it has none.
A new rule or a higher count fails naming the page, the rule and the nodes; a lower count asks for `UPDATE_A11Y=1 npm test`, never an edit by hand.
The panels one by one stay in `a11y.test.jsx` (#310), contrast in the token test above.
The overview is measured at both widths: the layout the width hides is hidden as the browser would, so the phone overview is what a phone's screen reader reads.

| # | Finding | Rule | Pages affected | Nodes | What the fix needs | Owner |
|---|---|---|---|---|---|---|
| 4 | The Delta box's heading jumps from the page's h1 to an h4 | `heading-order` | 1: Positions | 1 | the designer decides the payoff card's heading levels (the card's own title, then Delta under it); until then an h2 | designer, then developer |

Every other page — Overview (desktop and phone), Details (pro), Income, Worst case, Activity, Account, How it works — has none of these findings on the fixtures, nor has the menu bar above each page.
Ties are ordered by how often a person meets them (the overview before Positions).
Fixed by #851, with no visual change: #1 (the phone overview had no main landmark) and #2 (its "This month" heading, "Since you last looked" and footer note sat outside any landmark) — its cards, "Since you last looked" and the note, now a `<footer>`, are in one `<main>`;
#3 (the payoff card's Delta box was an `<aside>`, a page-level landmark inside the content) — it is a group named "Delta".
The assistant panel and the dialogs are measured one by one in `a11y.test.jsx`, not here: they open over a page.

## Text resize and reflow

Whether a page still shows all its content when the text is enlarged to 200 % (WCAG 1.4.4) or the screen is 320 px wide (WCAG 1.4.10).
Measured by `npm run test:phone` into `resize` of `frontend/e2e/a11y-report.json`; report-only, no component, colour or layout changed.
Counts from `resize.ranked` of the run of 2026-11-26 on demo data (pages affected, most first), out of the eight menu pages and the assistant panel; Positions, Income, Worst case, Activity and How it works have none in either pass.

| # | Finding | Pass | Pages affected | What the fix needs | Owner |
|---|---|---|---|---|---|
| 1 | Page scrolls sideways (`overflow_px` > 0) | 200 % text, 320 px | 4: account, assistant, details, overview at 200 %; details also at 320 px | the designer decides which tables wrap, stack or scroll inside their own box at these sizes | designer, then developer |
| 2 | Text clipped by its box (`clipped_text` > 0) | 200 % text, 320 px | 3: assistant, details, overview, at 200 % and at 320 px | boxes that grow with their text (no fixed height) or show the full text another way | designer, then developer |
| 3 | Controls pushed off screen (`offscreen_controls` > 0) | 200 % text | 3: assistant, details, overview at 200 %; none at 320 px | a header and menu bar that wrap or collapse instead of running past the edge | designer, then developer |

## What passes

Body text (`#e2e8f0`), the muted grey and every accent colour (profit, loss, warn, testnet, call, put, action) on the page and the panels meet AA, as do the badges' green, amber, blue and purple on their tints.

## What changes this list

- A fix lands: remove its line from `KNOWN_FINDINGS` (the test asks for it) and from this table, and regenerate the candidates sheet (`UPDATE_CONTRAST_SHEET=1 npm test -- contrast-candidates`).
- A new failing pair: the token test fails naming it; the designer either fixes it or accepts it here first.
- A light theme: its tokens and pairs go into the test before it ships.
