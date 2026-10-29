# internal/orders

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too; its **Journal rule** is the rule every decision follows).

- **`internal/orders`** — `Executor` (Deribit calls behind `rpcCaller`; `ErrForbidden`; order `label`),
  `StateManager` (in-memory book; readers get snapshots; greeks short-signed), decision journal `orders.log` (`journal.go` types: `EventContext`, `MarketSnapshot`, `OrderLog`, `PnLRecord`), price/lot helpers.
- **Journal rule, P&L and the start line:**
  - Realised P&L is booked per slot in `buyToClose`, **net of fees** (`orders.ClosedNetPnL`: premium − buy-back − the position's share of its opening fees − the closing fee);
    `Position.Fees` holds the opening fees (read from Deribit with `OrderFee` when a fill is booked) and unrealised P&L is `NetPnL()`.
  - The exit rules keep using the gross premium.
  - The backtest charges Deribit's fee (`orders.OptionFee`: 0.03 % of the underlying, capped at 12.5 % of the price) and reports net trades; `strategy/pnl.go` writes periodic P&L lines.
  - Every start journals one `config_loaded` line (`orders.LogStartConfig`, #256):
    the fingerprint of `config.Effective` (YAML keys and fixed values only — never anything from `.env`), the full settings and `config.Diff` against the journal's last `config_loaded` (`Replay.LastConfig`, kept raw so the replay's fast path is unchanged);
    `first`/`unchanged` flags; `/api/status` `config: {fingerprint, since}`.
- **The journal is the history:** at startup `main` replays `orders.log` (`orders.ReplayFile`, fast path: only fills/closes are decoded)
  and restores realised P&L per slot (`Strategy.RestorePnL`), event counts, the last 500 events and every open/close (`Logger.Restore`, served at `/api/trades`), so nothing the monitor shows resets on restart.
  Realised P&L has one source (the journal's closes); `history.Store` records it as given and adds nothing.
  `--reset-history` archives `orders.log` and `data/pnl_history.jsonl` to `data/archive/<time>/` and exits — never delete history.
