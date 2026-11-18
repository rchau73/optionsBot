# Accessibility baseline of the monitor (E12, #763)

What the monitor misses today on colour contrast, text size, tap targets and screen-reader structure, ranked by the pages affected, and what each fix needs from the designer.
It measures only: no component or colour changes with it.
The fixes come with the E12 UI stories, once the prototype (#654) is approved.

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

## Screen-reader structure

What a screen reader meets on each whole page (#840): landmarks, regions, heading order, the names of buttons, links and fields, table headers and dialog names.
`frontend/__tests__/a11y-pages.test.jsx` renders the eight menu pages and the phone overview, each under the menu bar as the layout puts it, on the shared fixtures (the demo's default scenario, `frontend/test/demoScenarios.json`, and `frontend/test/resultFixtures.js`).
It runs axe with only those rules and pins the counts in `frontend/__tests__/a11y-pages-baseline.json` (page → rule → nodes), every page with an entry even when it has none.
A new rule or a higher count fails naming the page, the rule and the nodes; a lower count asks for `UPDATE_A11Y=1 npm test`, never an edit by hand.
The panels one by one stay in `a11y.test.jsx` (#310), contrast in the token test above.
The overview is measured at both widths: the layout the width hides is hidden as the browser would, so the phone overview is what a phone's screen reader reads.

| # | Finding | Rule | Pages affected | Nodes | What the fix needs | Owner |
|---|---|---|---|---|---|---|
| 1 | The phone overview has no main landmark: a screen reader cannot jump to the page's content past the menu | `landmark-one-main` | 1: Phone overview | 1 | wrap the phone overview's cards in one `<main>`, as the desktop overview is; no visual change | developer |
| 2 | Three blocks of the phone overview sit outside any landmark: the "This month" heading, "Since you last looked" and the footer note | `region` | 1: Phone overview | 3 | the same `<main>` as #1 takes in the heading and "Since you last looked"; the footer note goes in a `<footer>` | developer |
| 3 | The payoff card's Delta box is an `<aside>` inside the main content: a page-level landmark once per strangle | `landmark-complementary-is-top-level` | 1: Positions | 1 | a plain group (`<div role="group" aria-label="Delta">`) in place of the `<aside>`; no visual change | developer |
| 4 | The Delta box's heading jumps from the page's h1 to an h4 | `heading-order` | 1: Positions | 1 | the designer decides the payoff card's heading levels (the card's own title, then Delta under it); until then an h2 | designer, then developer |

Every other page — Overview (desktop), Details (pro), Income, Worst case, Activity, Account, How it works — has none of these findings on the fixtures, nor has the menu bar above each page.
Ties are ordered by how often a person meets them (the overview before Positions).
The assistant panel and the dialogs are measured one by one in `a11y.test.jsx`, not here: they open over a page.

## What passes

Body text (`#e2e8f0`), the muted grey and every accent colour (profit, loss, warn, testnet, call, put, action) on the page and the panels meet AA, as do the badges' green, amber, blue and purple on their tints.

## What changes this list

- A fix lands: remove its line from `KNOWN_FINDINGS` (the test asks for it) and from this table.
- A new failing pair: the token test fails naming it; the designer either fixes it or accepts it here first.
- A light theme: its tokens and pairs go into the test before it ships.
