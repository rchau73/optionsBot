# Testing the monitor

Part of the [monitor README](../README.md). The commands are in its "Develop" section.

## Viewport check

**Viewport check (#309, every page since #425).** `npm run test:phone` has its config and tests in `e2e/`, runs Chromium only, and needs Go for `cmd/monitor-demo`.
It starts two demo bots (BTC on 127.0.0.1:18191, ETH on :18192).
It builds and starts the monitor on 127.0.0.1:3290, wired to those two bots only.
The monitor runs with a throwaway admin and a random `AUTH_SECRET` on an in-memory user database.
It has no audit, analytics or service token, and a `.env.local` cannot redirect it.
It renders **every page of the menu**, plus the assistant panel opened over the overview, each at 360, 768 and 1280 px.
The page list is read from `lib/menu.js` (`viewportPages()` in `e2e/phone.js`), so a page added to the menu is checked without editing the test.
On each it measures horizontal overflow of the document and its cause (panel, table, menu bar), every control's box (kinds under 24 and 44 px), hover-only `title`s and the header's height; on the overview, whether each dialog fits 360 × 640 and 640 × 360.
Results: `docs/responsive-measured.json` and screenshots `test-results/phone/<page>-<width>.png`, both git-ignored.
**Baseline:** `e2e/phone-baseline.json` keeps today's overflows and small controls per page and width (`pages.<page>.widths.<width>`), so the check is green on them and fails on anything new, naming page, viewport and element (e.g. `income 360 px: page overflow now caused by Income › table (540 px wide)`).
A page not yet in the baseline is held to what the recorded pages accept at that width (the shared header and menu).
`UPDATE_PHONE_BASELINE=1 npm run test:phone` records a new baseline (also when the file is missing) and prints its entries one per line — the follow-up notes of the PR that commits it; commit it with the fix, never to hide a new overflow.
What it measures and why: [`docs/responsive-audit.md`](responsive-audit.md) "Measured in a browser".
Not part of `npm test` (Jest only) nor of CI yet (the browser download).
`__tests__/phone-check.test.jsx` covers its measuring and comparing code in Jest.

## The Overview's four answers

**The Overview's four answers (#489).** `__tests__/overview-four-answers.test.jsx` renders the desktop Overview with a mocked `useMonitor` on four fixtures: a profitable month, a loss, nothing held and stale data.
It fails when the page stops answering one of the four questions a newcomer asks in the first five minutes: what the bot holds, whether it makes money (net of fees, coin and USD, a loss as prominent as a gain), the worst case in money, and why it made its last trade (one click).

## Losses as visible as gains

**Losses as visible as gains (#582).** `__tests__/loss-visibility.test.jsx` extends the four answers to every other page that shows a result: Income, Details (pro), the phone overview, Worst case and Positions.
Its fixtures, in `test/resultFixtures.js`, carry the four answers' figures (the month ±0.00400 BTC, ±$410, net of fees) in the shape of each page's API: a profitable month, a loss and nothing held.
On the loss fixture each page's result must show a minus sign and its unit (colour is never the only cue), with the same size and weight classes, its own and inherited, as the gain on the profit fixture.
Worst case shows one estimate on both fixtures, so its gain is the calm day and its loss the 3σ fall of the same table.
A result inside a closed `<details>`, a `hidden` block or screen-reader-only text fails, naming the page; so does one dropped from the page.
On the nothing-held fixture each page says so in words and shows no 0.00000 or $0 standing for "nothing".
A new page that shows a result gets its row in the test's `PAGES` table.
Its checks (size and weight, folded away) are in `test/lossVisibility.js`, shared with the scenario test below.

## The four answers on the demo scenarios

**The four answers on the demo scenarios (#743).** `__tests__/scenario-answers.test.jsx` renders the desktop investor summary and the phone overview on the demo's own data for each usability scenario (baseline, mixed, drawdown), from `test/demoScenarios.json`.
It checks, from the rendered text, one answer per question of the usability kit's answer key: what is held, making or losing (each month's sign, a losing month named "· loss" on desktop and "down" in the phone's month bars), the worst case in money with its unit, and the last action with its reason.
In drawdown each loss must be in the same element, place and size as the baseline's gain, and not folded away; in mixed the open positions stay apart from the month's result.
A failure names the scenario, the layout (desktop or phone) and the question.
`test/demoScenarios.json` is what `cmd/monitor-demo` serves on `/api/positions`, `/api/status` and `/api/summary`; `tests/usability_keys_test.go` checks it against the kit's keys and rewrites it with `go test ./tests -run TestUsabilityKeys_FrontendScenariosAnswerTheKeys -args -update-scenarios` when the demo changes.

## Accessibility baseline

**Accessibility baseline (#310).** `__tests__/a11y.test.jsx` renders every panel and dialog (and the sign-in form) with test data and runs axe-core (`jest-axe`, helper `test/a11y.js`; colour contrast is left to the browser audit — jsdom has no layout).
Today's violations are counted per component and rule in `__tests__/a11y-baseline.json`: a new rule or a higher count fails `npm test` naming the component and the rule; a lower count fails asking to lower the baseline.
`UPDATE_A11Y=1 npm test` rewrites the file from real axe output (never hand-edit it; commit it with the fix).
A new panel or dialog gets its case in that test.
