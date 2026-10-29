# tests

Folder notes for `CLAUDE.md`: all tests live here (package `tests`), not next to the source.

Notable files:
- `strategy_run_test.go` (end-to-end `Strategy.Run` against a fake exchange), `margin_policy_run_test.go` + `risk_test.go` (margin policy end to end and its pure rules),
  `gateway_test.go` (real `Gateway` against a mock Deribit WebSocket),
- `restart_invariance_test.go` (#220: boots twice on one book with a held stop, a shed anchor, a shared strike and IM in the startup band, through main's restore paths
  — no order, same slots, holds kept; a new restore path belongs in it),
- `marketdata_test.go`, `gex_manager_test.go`, `orderlog_test.go`, `state_test.go`, `config_test.go`, `backtest_*_test.go`.
- `claude_docs_test.go` (#460): the root `CLAUDE.md` is under 25 KB and indexes every folder file; no prose line over 300 characters (the split's one-off "nothing lost" check against a frozen copy was retired after it merged).
