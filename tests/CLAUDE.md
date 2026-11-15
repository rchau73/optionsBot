# tests

Folder notes for `CLAUDE.md`: all tests live here (package `tests`), not next to the source.

Notable files:
- `strategy_run_test.go` (end-to-end `Strategy.Run` against a fake exchange), `margin_policy_run_test.go` + `risk_test.go` (margin policy end to end and its pure rules),
  `gateway_test.go` (real `Gateway` against a mock Deribit WebSocket),
- `restart_invariance_test.go` (#220: boots twice on one book with a held stop, a shed anchor, a shared strike and IM in the startup band, through main's restore paths
  — no order, same slots, holds kept; a new restore path belongs in it),
- `marketdata_test.go`, `gex_manager_test.go`, `orderlog_test.go`, `state_test.go`, `config_test.go`, `backtest_*_test.go`.
- `usability_kit_test.go` (#202, #646): the E3 usability kit runs from the file alone, keeps participant data out of its templates, and its answer keys match `cmd/monitor-demo`
  — the profit key (section 6) against the no-flag demo, the losing-scenario keys (section 6b) against `-scenario mixed` and `-scenario drawdown`, a failure naming the scenario and the fact.
  The round scorer's own tests, `scripts/product/test_score_usability.py` (#701), read the kit's template, so a changed table fails there.
- `usability_keys_test.go` (#727): every figure in the kit's fenced `key` blocks equals `cmd/monitor-demo`'s value, per scenario and question; a stale key fails naming both.
  It checks `frontend/test/demoScenarios.json` (#743, the demo's answers per scenario for `scenario-answers.test.jsx`) against the same keys; `-args -update-scenarios` rewrites it from the demo.
  `usability_round_test.go` runs `scripts/usability_round.sh` on a copy of the layout with stub `go`, `npm`, `npx` and `curl`.
- `accounts_json_test.go` (#655): `cmd/accounts -check -json`/`-out` — decoded with unknown fields refused, no key material, key id, client id or scope string in it, the status file owner-only and atomic.
- `prd_decisions_test.go` (#464): the PRD's decision log is one file per decision in `docs/product/decisions/`; the old rows are all there and each file is complete.
- `docs_line_length_test.go` (#463): no prose line over 400 characters in a tracked `*.md` (tables, code, link targets exempt); files that still have some are listed with a reason and a count that can only go down.
  `scripts/docs/reflow.py write FILE` puts one sentence per line, `compare REF FILE` lists every sentence of the old file missing from the new one (empty = nothing lost).
- `agentic_docs_test.go` (#461): `AGENTIC.md` is the overview and `docs/agents/*.md` one file per agent;
  every link and quoted section reference to them resolves, the overview indexes every agent file, no prose line over 400 characters in them
  (the one-off "nothing lost" check was retired after it merged).
- `tips_coverage_test.go` (#492): every `config.Tunables()` key has a tip in `docs/product/education/tips.json` and every glossary term has `short` + `risk_income`;
  no sentence over 25 words, no jargon word without a glossary entry, no promised return; a new tunable needs its tip in the same PR.
- `tip_placement_test.go` (#728): `docs/product/education/tip-placement.md` has a section per menu page, every term and setting in it has a tip, every video id is in `videos.json`, keys are in order and nothing is dropped (placed or listed with a reason); a problem names the page and the key.
- `personal_data_inventory_test.go` (#647): `frontend/docs/personal-data.md` names every column of `auth.db` (from `store.js`) with a purpose and a retention, no column that is gone, every audit action of `lib/audit.js`;
  a new column or audit action needs its row in the same PR.
- `evidence_hedgetrial_test.go` (#678): the weekly report's futures hedge trial — one test per ticket scenario, the 7-day boundary and the live account's counts-only section.
- `evidence_pack_test.go` (#700): the evidence pack equals its source files; a loss's headline matches a gain's with the numbers masked; a live account has no amount; no output holds a promised return.
- `evidence_sample_test.go` (#772): every pack figure with a number and every weekly headline figure carries its sample; a thin one reads "too few to conclude" with its days, its number still shown; the floors at their boundary.
- `pipe_guard_test.go` (#683): no workflow step, script or git hook pipes a failing command into a filter (`tee`, `head`, `grep`, …) without `pipefail`, unless its left side only prints or `# pipe-ok: <why>` explains it.
- `ledger_coverage_test.go` (#722): every `scripts/agents/*.sh` opens and ends a run (`run_begin`/`run_end`) or is on `ledgerExempt` with a reason; every agent name is in `runs.py`, `docs/agents/run-ledger.md` and `scripts/agents/CLAUDE.md`.
- `claude_docs_test.go` (#460): the root `CLAUDE.md` is under 25 KB and indexes every folder file; no prose line over 300 characters (the split's one-off "nothing lost" check against a frozen copy was retired after it merged).
