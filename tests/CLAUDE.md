# tests

Folder notes for `CLAUDE.md`: all tests live here (package `tests`), not next to the source.

Notable files:
- `strategy_run_test.go` (end-to-end `Strategy.Run` against a fake exchange), `margin_policy_run_test.go` + `risk_test.go` (margin policy end to end and its pure rules),
  `gateway_test.go` (real `Gateway` against a mock Deribit WebSocket),
- `restart_invariance_test.go` (#220: boots twice on one book with a held stop, a shed anchor, a shared strike and IM in the startup band, through main's restore paths
  — no order, same slots, holds kept; a new restore path belongs in it),
- `marketdata_test.go`, `gex_manager_test.go`, `orderlog_test.go`, `state_test.go`, `config_test.go`, `backtest_*_test.go`.
- `prd_decisions_test.go` (#464): the PRD's decision log is one file per decision in `docs/product/decisions/`; the old rows are all there and each file is complete.
- `docs_line_length_test.go` (#463): no prose line over 400 characters in a tracked `*.md` (tables, code, link targets exempt); files that still have some are listed with a reason and a count that can only go down.
  `scripts/docs/reflow.py write FILE` puts one sentence per line, `compare REF FILE` lists every sentence of the old file missing from the new one (empty = nothing lost).
- `agentic_docs_test.go` (#461): `AGENTIC.md` is the overview and `docs/agents/*.md` one file per agent;
  every link and quoted section reference to them resolves, the overview indexes every agent file, no prose line over 400 characters in them
  (the one-off "nothing lost" check was retired after it merged).
- `tips_coverage_test.go` (#492): every `config.Tunables()` key has a tip in `docs/product/education/tips.json` and every glossary term has `short` + `risk_income`;
  no sentence over 25 words, no jargon word without a glossary entry, no promised return; a new tunable needs its tip in the same PR.
- `claude_docs_test.go` (#460): the root `CLAUDE.md` is under 25 KB and indexes every folder file; no prose line over 300 characters (the split's one-off "nothing lost" check against a frozen copy was retired after it merged).
