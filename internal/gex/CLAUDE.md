# internal/gex

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

- **`internal/gex`** — every 60 s computes dealer gamma exposure from **mainnet** open interest (always: testnet prices mirror mainnet but its open interest belongs to test accounts, so testnet GEX describes nothing real).
  `gex.Build(rows, now, params)` is pure;
  - `gex_method: script` (default) reproduces GestaoCarteira's `deribit_tc_export_v3.py` exactly (5 nearest expiries of the chain, strikes ±`gex_strike_range_pct` of each expiry's future, regime = sign of the weighted sum, flip = lowest crossing, no hysteresis)
  - — `tests/gex_parity_test.go` checks it against the script's own output on captured mainnet data;
  - one intended divergence (#660): a strike with no open interest is not a zero crossing — the script counts it, and on 2026-11-05 an empty 67,000 strike became BTC's flip (real ≈ 77,400) and shed every call; `tests/gex_test.go` pins it on that day's book.
  - `nearest_flip` is the original rule (crossing nearest spot, regime = spot vs flip, hysteresis `gamma_regime_band_pct`).
  The book summary comes from `deribit.BookReader` (`internal/exchange/deribit`, #687), the one fetch it shares with the recorder.
  Option names are parsed with `marketdata.ParseOptionName` (daily expiries have one-digit days, e.g. `4OCT26`).
