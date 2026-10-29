# Glossary and help content

Part of the [monitor README](../README.md). The content files that explain the monitor in plain words, and the tests that keep them true.

## Glossary (#188)

`content/glossary.json` holds one short tip for every term the monitor shows and every config setting: `{ "<term>": { "short" (≤ 140 chars), "long" (≤ 600), "risk_income" (how it moves risk and income), "source" ("OptionStrategy.md#<anchor>" or "frontend/docs/<file>.md#<anchor>"), "labels" (optional: the exact monitor texts that mean this term) } }`.
Terms are in alphabetical order (#465, `tests/sorted_lists_test.go`): a new one goes in its place, not at the end.
Config keys are entries under their YAML name, nested ones as `parent.child` (`backtest.fill_model`).
`lib/glossary.js` `findTerm(label)` finds an entry by term, config key or label.
It is content only: the tips UI (E11) and the assistant (E10) will read it.

Tests keep it complete: `__tests__/glossary.test.js` fails when a literal column header (`<th>`), `<dt>`, `label="…"` prop, hold-rule, feed-event or exit-reason label in the monitor, or a key of `config_btc.yaml`/`config_eth.yaml`/`config_btc.mvp.yaml`, has no entry, when a source anchor does not exist, and on return promises; `tests/glossary_test.go` checks every key `config.Load` accepts.
**A new column or setting needs its entry in the same PR.** Writing rules: short sentences (8th-grade reading), no jargon without its own entry, no return promises.
**Plain-words checks (#339, `test/plainWords.js`):** the `short` text has at most 25 words a sentence (the long texts keep the 35-word limit above), and a jargon word (*delta, gamma, theta, vega, strangle, IV, DVOL, GEX, margin, strike*) may appear in a text only if it is a glossary key or label, or the entry defines it (`implied volatility` spells out "IV").
A test failure names the entry and the word.
An entry that must use an undefined jargon word goes in `JARGON_ALLOWED` in `__tests__/glossary.test.js` with the reason.

## Risk notes before confirming (#299)

`content/confirm-notes.json` says in plain words, before a person confirms, what a manual close or a rebalance does, what can go wrong and what the bot does next: `{ "manual_close": { "does", "risks", "follows", "follows_last_leg", "terms", "source" }, "rebalance": { "does", "risks", "follows", "dry_run", "terms", "source" } }`.
`terms` lists the glossary keys the texts name (settings by their YAML key, e.g. `repair_cooldown_hours`), so each can link to its tip; `source` is an `OptionStrategy.md#…` or `CLAUDE.md#…` anchor.
`lib/confirmNotes.js` `noteFor(action, { lastLeg, dryRun })` picks the texts: `lastLeg` (manual close of a strangle's last leg) gives `follows_last_leg`; `dryRun` (rebalance preview) gives `dry_run` as `does` and no risks or follows, since nothing is sent.
Content only: the confirmation dialogs read it once their prototype is approved (E11).
`__tests__/confirm-notes.test.js` keeps every text ≤ 280 characters, free of figures (no digit at all) and of forecast, advice and promise words, every named term a glossary key and every source anchor real.
The facts follow `internal/strategy/manualclose.go` and `manualrebalance.go`: **a rule change there needs its note changed in the same PR.** This is risk-disclosure wording drafted against `docs/legal/`: it may be shown to the team's own users, never to an external user before counsel approves it.

## Empty states (#298)

`content/empty-states.json` holds, for every panel, chart and list the monitor renders, one sentence per state: `{ "<panel>": { "empty", "loading", "stale", "error" (each ≤ 140 chars), "source" ("OptionStrategy.md#<anchor>" or "frontend/docs/<file>.md#<anchor>") } }` — what will appear there, when, and why it is not there now.
Facts only, in the bot's plain-words style (`internal/api/explain.go`): never a forecast or advice.
The keys are the collapsible panels' `monitor.panel.*` storage keys plus `monitor.header`, `monitor.gex`, `monitor.summary`, `monitor.kpi`, `monitor.positions`, `monitor.activity`, `monitor.pnl`, `monitor.pnl_coin` and `monitor.orders` (`PANELS` in `lib/emptyStates.js`); `emptyStateFor(panel, state)` returns the sentence or null.
Content only: the screens do not read it yet (E11's prototype first).

`__tests__/empty-states.test.js` (checks in `test/emptyStateChecks.js`) fails, naming the panel, when a `storageKey` or `<Panel title>` in `components/Monitor.jsx` has no key, when a key lacks a state, when a text is over 140 characters, on a forecast or advice word (deny-list: *will earn*, *guaranteed*, *should buy*, …) and when a source anchor does not exist.
**A new panel needs its entry in the same PR.** The same plain-words checks apply (#339): at most 25 words a sentence, and no jargon word without a glossary entry; the failure names the panel, the state and the word.

The inventory of every panel's conditions, with the copy and the API field that tells each one apart, is `docs/product/education/empty-states.md` (#250); `__tests__/education-docs.test.js` keeps its copy equal to this catalogue, so change the JSON first and copy the sentence there.

## How-to videos and the comprehension test (#337, #338)

`docs/product/education/video-storyboards.md` holds the script, shot list, captions, glossary terms, risk sentence and source anchor of each how-to video (30–90 s, one task each; ids `video.<task>` are the analytics catalogue ids of `video_played` / `video_completed`, letters and `_` only).
Content only: nothing is recorded or shown in the monitor yet; the player and the links where each task happens wait for the E11 prototype, and any public video with performance figures needs legal review.
`__tests__/video-storyboards.test.js` checks every section (length, numbered shots, captions equal to the spoken text, at most 200 spoken words, a risk sentence, an existing source anchor, glossary terms that exist, no amount, no promise).
`docs/product/education/comprehension-test.md` is the protocol that tests tips and videos with five real people; results stay under `data/product/`.
