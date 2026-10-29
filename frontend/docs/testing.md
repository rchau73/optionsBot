# Testing the monitor

Part of the [monitor README](../README.md). The commands are in its "Develop" section.

## Viewport check

**Viewport check (#309, every page since #425).** `npm run test:phone` (config and tests in `e2e/`, Chromium only, needs Go for `cmd/monitor-demo`) starts two demo bots (BTC on 127.0.0.1:18191, ETH on :18192), builds and starts the monitor on 127.0.0.1:3290 wired to them only, with a throwaway admin and a random `AUTH_SECRET` on an in-memory user database (no audit, analytics or service token; a `.env.local` cannot redirect it), and renders **every page of the menu** — the list is read from `lib/menu.js` (`viewportPages()` in `e2e/phone.js`), so a page added to the menu is checked without editing the test — plus the assistant panel opened over the overview, each at 360, 768 and 1280 px.
On each it measures horizontal overflow of the document and its cause (panel, table, menu bar), every control's box (kinds under 24 and 44 px), hover-only `title`s and the header's height; on the overview, whether each dialog fits 360 × 640 and 640 × 360.
Results: `docs/responsive-measured.json` and screenshots `test-results/phone/<page>-<width>.png`, both git-ignored.
**Baseline:** `e2e/phone-baseline.json` keeps today's overflows and small controls per page and width (`pages.<page>.widths.<width>`), so the check is green on them and fails on anything new, naming page, viewport and element (e.g. `income 360 px: page overflow now caused by Income › table (540 px wide)`).
A page not yet in the baseline is held to what the recorded pages accept at that width (the shared header and menu).
`UPDATE_PHONE_BASELINE=1 npm run test:phone` records a new baseline (also when the file is missing) and prints its entries one per line — the follow-up notes of the PR that commits it; commit it with the fix, never to hide a new overflow.
What it measures and why: [`docs/responsive-audit.md`](responsive-audit.md) "Measured in a browser".
Not part of `npm test` (Jest only) nor of CI yet (the browser download).
`__tests__/phone-check.test.jsx` covers its measuring and comparing code in Jest.

## Accessibility baseline

**Accessibility baseline (#310).** `__tests__/a11y.test.jsx` renders every panel and dialog (and the sign-in form) with test data and runs axe-core (`jest-axe`, helper `test/a11y.js`; colour contrast is left to the browser audit — jsdom has no layout).
Today's violations are counted per component and rule in `__tests__/a11y-baseline.json`: a new rule or a higher count fails `npm test` naming the component and the rule; a lower count fails asking to lower the baseline.
`UPDATE_A11Y=1 npm test` rewrites the file from real axe output (never hand-edit it; commit it with the fix).
A new panel or dialog gets its case in that test.
