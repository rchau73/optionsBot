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
Not part of `npm test` (Jest only).
`__tests__/phone-check.test.jsx` covers its measuring and comparing code in Jest.

### In CI

**The CI job (#739).** CI's "Phone viewport check" job (`.github/workflows/ci.yml`, `phone`) sets up Go and Node, runs `npx playwright install --with-deps chromium`, then `npm run test:phone`, the full set of widths (Chromium only).
It runs on a pull request whose files need it (`scripts/agents/checkfail.py suites`, the `phone` suite): any non-doc file under `frontend/`, `cmd/monitor-demo/`, `internal/api/` (the API the demo serves) and `ci.yml`; a push to main, the nightly run and a manual run always run it.
A pull request that changes only strategy code, scripts or docs skips it, and a skipped job counts as passed for the required check.
**Reading a failure:** the log's `worse than e2e/phone-baseline.json:` lists one line per problem in the check's own words, page, viewport and element, e.g. `income 360 px: page overflow now caused by Income › table (540 px wide)`.
The screenshots (`test-results/phone/`) and `docs/responsive-measured.json` are attached to the run as the `phone-check` artifact when it fails.
**CI never records the baseline:** `recordRefusal` (`e2e/phone.js`) fails the run when the baseline is missing or `UPDATE_PHONE_BASELINE=1` is set under `CI`, and the job's last step fails if `e2e/phone-baseline.json` changed.
Recording stays a local step, committed in a reviewed PR.
A run better than the baseline passes and prints `Better than e2e/phone-baseline.json (n): the baseline can be tightened…`, one line per entry gone (`baselineSlack`).

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

## Accessibility baseline

**Accessibility baseline (#310).** `__tests__/a11y.test.jsx` renders every panel and dialog (and the sign-in form) with test data and runs axe-core (`jest-axe`, helper `test/a11y.js`; colour contrast is left to the browser audit — jsdom has no layout).
Today's violations are counted per component and rule in `__tests__/a11y-baseline.json`: a new rule or a higher count fails `npm test` naming the component and the rule; a lower count fails asking to lower the baseline.
`UPDATE_A11Y=1 npm test` rewrites the file from real axe output (never hand-edit it; commit it with the fix).
A new panel or dialog gets its case in that test.
