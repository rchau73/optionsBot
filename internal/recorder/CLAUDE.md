# internal/recorder

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

```bash
go run ./cmd/datacoverage -dir data/btc -from 2026-10-12 -to 2026-10-18   # recorded-history coverage per dataset (market, market_gex, market_usdc, market_usdc_sol, market_usdc_xrp, pnl_history): days, missing hours, gaps, hourly-only replay days (README "Recorded-history coverage")
go run ./cmd/usdcstudy -dir data/btc,data/eth -from 2026-10-24 -to 2026-11-07   # liquidity of every recorded book (BTC/ETH coin, BTC/ETH/SOL/XRP USDC) per slot band: spread median/p90 (all and volatile hours), one-sided share, 0.16 Δ OI, premium at bid, liquidity-cap size; missing days listed → data/usdcstudy/<time>/report.md|json (#356, README "USDC-settled study")
```

- **`internal/recorder`** — market history for backtests and stress tests:
  - every `market_record_minutes` (60) it writes the GEX manager's last mainnet book summary (`LatestSummary`, no extra Deribit call) with Black–Scholes greeks to `data/market/YYYY-MM-DD.csv.gz` (gzip member per snapshot);
  - `USDCBooks` fetches the USDC-settled options' summary (one extra mainnet call per snapshot — the `Fetcher` interface; the GEX manager is never one)
    and a `Group` writes each underlying's view of that one fetch (`Book`):
    the coin's to `data/market_usdc/`, to compare liquidity, and from the BTC bot only SOL's and XRP's to `data/market_usdc_sol|xrp/` (`ExtraUSDC`, no DVOL → empty `dvol_index`, #356; XRP's `2d5` strikes parse in `ParseOptionName`);
  - `GEXRecorder` (`gexminute.go`, #122) writes every GEX snapshot (one a minute: flip, regime, median flip, GEX sum, DVOL, flip buffer — no Deribit call) as a JSON line to `data/market_gex/YYYY-MM-DD.jsonl.gz`, read by `ReadGEXDir`.
  Never touches trading; not archived by `--reset-history`.
