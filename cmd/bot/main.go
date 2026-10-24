package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"optionsbot/internal/account"
	"optionsbot/internal/api"
	"optionsbot/internal/backtest"
	"optionsbot/internal/config"
	"optionsbot/internal/gateway"
	"optionsbot/internal/gex"
	"optionsbot/internal/hedge"
	"optionsbot/internal/history"
	"optionsbot/internal/logger"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/strategy"
)

// journalPath is the decision journal: every decision, fill and close. It is
// replayed at startup so P&L, counts and the trade history continue.
const journalPath = "orders.log"

// pnlHistoryPath is where the P&L history for the monitor chart is kept.
const pnlHistoryPath = "data/pnl_history.jsonl"

// marketHistoryDir holds the recorded market (one gzip CSV per UTC day). It
// is not part of the trading history: --reset-history leaves it in place.
const marketHistoryDir = "data/market"

// linearHistoryDir holds the same record for the underlying's USDC-settled
// options (prices in USDC), to compare their liquidity with data/market.
const linearHistoryDir = "data/market_usdc"

// gexHistoryDir holds every GEX snapshot (one JSON line a minute, a gzip file
// per UTC day) for the replay; like data/market, never reset.
const gexHistoryDir = "data/market_gex"

// regimeHistoryPath keeps the gamma regime at each daily close: Deribit has
// no history of it, and the margin policy confirms regime changes on it.
const regimeHistoryPath = "data/regime_history.jsonl"
const directionHistoryPath = "data/direction_history.jsonl"

func main() {
	// Flags
	mode := flag.String("mode", "live", "live | backtest")
	btFrom := flag.String("from", "2022-01-01", "backtest start date (YYYY-MM-DD)")
	btTo := flag.String("to", "2024-12-31", "backtest end date (YYYY-MM-DD)")
	sweep := flag.Bool("sweep", false, "run parameter sweep in backtest mode")
	sweepLadders := flag.Bool("sweep-ladders", false, "run the DTE ladder sweep (slot occupancy, return on margin, worst week) in backtest mode")
	csvPath := flag.String("csv", "data/historical/options.csv", "historical data CSV path")
	cfgPath := flag.String("config", "config.yaml", "config file path")
	debug := flag.Bool("debug", false, "enable debug logging")
	envFile := flag.String("env-file", ".env", "platform settings file (credentials, DERIBIT_ENV, limits)")
	resetHistory := flag.Bool("reset-history", false, "archive the journal and P&L history to data/archive/<time>/ and exit (nothing is deleted)")
	flag.Parse()

	// Platform settings. Variables already set in the environment win, so a
	// shared .env can be overridden per process (e.g. BOT_API_ADDR).
	if err := godotenv.Load(*envFile); err != nil {
		slog.Warn("no .env file found", "path", *envFile)
	}

	// Init logger
	if err := logger.Init("bot.log", *debug); err != nil {
		fmt.Fprintf(os.Stderr, "logger init: %v\n", err)
		os.Exit(1)
	}

	if *resetHistory {
		dir := filepath.Join("data", "archive", time.Now().UTC().Format("20060102T150405Z"))
		moved, err := history.Archive([]string{journalPath, pnlHistoryPath}, dir)
		if err != nil {
			slog.Error("reset history failed", "err", err, "moved", moved)
			os.Exit(1)
		}
		slog.Info("history archived; the next start begins a fresh history", "to", dir, "files", moved)
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	if *mode == "backtest" {
		runBacktest(cfg, *btFrom, *btTo, *sweep, *sweepLadders, *csvPath)
		return
	}

	if err := runLive(cfg, *cfgPath); err != nil {
		slog.Error("bot exited with error", "err", err)
		os.Exit(1)
	}
}

// runLive wires the live components and runs the strategy until shutdown.
// It returns instead of calling os.Exit so deferred cleanup always runs.
func runLive(cfg *config.Config, cfgFile string) error {
	if err := cfg.RequireCredentials(); err != nil {
		return err
	}
	if cfg.IsLive() {
		slog.Warn("LIVE MODE ENABLED — trading real capital")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// The trading gateway outlives the signal: on SIGTERM Strategy.Run still
	// cancels its resting closes and books their fills (#239) before it
	// returns; the gateway closes after it.
	gwCtx, gwStop := context.WithCancel(context.WithoutCancel(ctx))
	defer gwStop()
	gw := gateway.New(cfg, gateway.WithName("trading"))
	if err := gw.Connect(gwCtx); err != nil {
		return fmt.Errorf("gateway connect: %w", err)
	}
	defer gw.Close()

	// Market structure (open interest → GEX) always comes from mainnet: it
	// describes the real dealers whose hedging moves the BTC index, which
	// testnet mirrors. Testnet open interest belongs to test accounts and
	// says nothing about the market. On testnet a second, public-only
	// connection reads it (no credentials; private methods refused).
	marketGW := gw
	if !cfg.IsLive() {
		pub := gateway.New(cfg, gateway.WithEndpoint(config.MainnetWSEndpoint), gateway.PublicOnly(), gateway.WithName("mainnet-public"))
		if err := pub.Connect(ctx); err != nil {
			return fmt.Errorf("mainnet market-data connect: %w", err)
		}
		defer pub.Close()
		marketGW = pub
	}

	// If the gateway gives up reconnecting, stop the bot cleanly and exit
	// non-zero so the supervisor restarts it; startup reconciles positions.
	fatalErr := make(chan error, 1)
	fail := func(err error) {
		select {
		case fatalErr <- err:
		default: // already failing for another reason
		}
		cancel()
	}
	go func() {
		select {
		case err := <-gw.Fatal():
			fail(fmt.Errorf("gateway: %w", err))
		case err := <-marketGW.Fatal(): // the mainnet data connection gave up
			fail(fmt.Errorf("market data: %w", err))
		case <-ctx.Done():
		}
	}()

	md := marketdata.New(cfg, gw)
	if err := md.Start(ctx); err != nil {
		return fmt.Errorf("marketdata start: %w", err)
	}

	exec := orders.NewExecutor(gw)

	// Replay the journal before appending to it: realised P&L, closes, event
	// counts, the activity feed and the trade history continue across restarts.
	replay, err := orders.ReplayFile(journalPath, orders.RecentEvents)
	if err != nil {
		return fmt.Errorf("journal replay: %w", err)
	}
	orderLog, err := orders.NewLogger(journalPath, cfg.SpreadAlertThreshold)
	if err != nil {
		return fmt.Errorf("order logger init: %w", err)
	}
	defer orderLog.Close()
	orderLog.Restore(replay)
	// Date every settings change in the journal (#256): the effective
	// settings' fingerprint and what changed since the previous start.
	settings := orderLog.LogStartConfig(replay.LastConfig, cfg, cfgFile, cfg.Deployment, cfg.Version, time.Now())

	// The GEX manager polls public/get_book_summary_by_currency (mainnet) every
	// 60 s and classifies the gamma regime the strategy uses to shed legs and
	// the margin policy uses to cap margin.
	gexMgr := gex.NewManager(marketGW, gex.Params{
		Underlying: cfg.Underlying, NExpiries: 5, StrikeRangePct: cfg.GEXStrikeRangePct, Method: cfg.GEXMethod,
	}, cfg.GammaRegimeBandPct)
	gexMgr.StartBackground(ctx, 60*time.Second)
	// Market history for backtests and stress tests: the GEX poll's mainnet
	// quotes, once per interval. Reads only what is already fetched.
	if cfg.MarketRecordMinutes > 0 && cfg.RecordMarket {
		go recorder.New(gexMgr, md.DVOL, marketHistoryDir, time.Duration(cfg.MarketRecordMinutes)*time.Minute).Run(ctx)
		go recorder.New(recorder.NewLinearBook(marketGW, cfg.Underlying), md.DVOL, linearHistoryDir,
			time.Duration(cfg.MarketRecordMinutes)*time.Minute).Run(ctx)
		// Every GEX snapshot (one a minute), so the replay can step the GEX
		// side at the bot's pace (#122).
		flipBuffer := func() float64 { return strategy.FlipBufferPct(cfg.GammaFlipBufferSD, md.DVOL()) }
		go recorder.NewGEXRecorder(gexMgr, md.DVOL, flipBuffer,
			time.Duration(max(cfg.GEXBiasFlipWindowMinutes, 0))*time.Minute, gexHistoryDir).Run(ctx)
	}

	// P&L history for the monitor chart; data/ is a mounted volume in Docker,
	// so it survives restarts and rebuilds.
	pnlHistory, err := history.Open(pnlHistoryPath, 366*24*time.Hour, time.Now())
	if err != nil {
		return fmt.Errorf("pnl history: %w", err)
	}
	defer pnlHistory.Close()
	regimes, err := history.OpenRegimes(regimeHistoryPath)
	if err != nil {
		return fmt.Errorf("regime history: %w", err)
	}
	defer regimes.Close()
	directions, err := history.OpenDirections(directionHistoryPath)
	if err != nil {
		return fmt.Errorf("direction history: %w", err)
	}
	defer directions.Close()

	strat := strategy.New(cfg, strategy.Deps{
		Market:   md,
		Exchange: exec,
		State:    orders.NewStateManager(),
		Journal:  orderLog,
		Hedge:    hedge.New("hedge_report.json", cfg.HedgeReportThreshold),
		GEX:      gexMgr,
		OI:       gexMgr,
		History:  pnlHistory,
		Regimes:  regimes,
		// The SMA trend next to the GEX bias, for comparing the two.
		Directions: directions,
	})
	strat.RestorePnL(replay.Realised, replay.Closed)
	strat.RestoreStops(replay.Trades) // stopped, hand-closed and GEX-shed sides stay held across the restart
	realised := 0.0
	for _, v := range replay.Realised {
		realised += v
	}
	slog.Info("history restored from the journal",
		"events", replay.Events, "trades", len(replay.Trades), "closes", replay.Counts[orders.EventClosed],
		"realised", fmt.Sprintf("%.6f", realised), "since", replay.FirstAt, "skipped_lines", replay.Skipped)

	// Kill switch: `kill -USR1 <pid>` (or `docker kill -s USR1 <container>`)
	// flattens every position at market and halts trading.
	killCh := make(chan os.Signal, 1)
	signal.Notify(killCh, syscall.SIGUSR1)
	defer signal.Stop(killCh)
	go func() {
		select {
		case <-killCh:
			slog.Warn("kill switch triggered via SIGUSR1")
			strat.KillSwitch()
		case <-ctx.Done():
		}
	}()

	// Read-only monitor API for the frontend (off unless BOT_API_ADDR is set).
	if cfg.APIAddr != "" {
		// Account/collateral summary for the monitor, cached: one read-only
		// call every BOT_ACCOUNT_POLL_SEC, never one per page refresh.
		acct := account.NewPoller(gw)
		acct.Start(ctx, time.Duration(cfg.AccountPollSec)*time.Second)
		opts := []api.Option{api.WithPnLHistory(pnlHistory), api.WithAccount(acct), api.WithConnection("trading", gw),
			api.WithDeployment(cfg.Deployment, cfg.Version), api.WithConfig(settings.Fingerprint, settings.Since)}
		if marketGW != gw {
			opts = append(opts, api.WithConnection("mainnet-public", marketGW))
		}
		// Manual close (monitor): testnet only, and only with BOT_ADMIN_TOKEN.
		switch {
		case cfg.IsLive():
			strat.SetManualClose(false, "manual close is testnet-only")
		case cfg.AdminToken == "":
			strat.SetManualClose(false, "manual close is off: BOT_ADMIN_TOKEN is not set")
		default:
			strat.SetManualClose(true, "")
			opts = append(opts, api.WithManualClose(strat, cfg.AdminToken))
			slog.Info("monitor API: manual close enabled (regime-side block only)")
		}
		// Manual rebalance: live and testnet, with BOT_ADMIN_TOKEN. It runs the
		// bot's own rebalance with its own gates, as a restart would.
		if cfg.AdminToken != "" {
			opts = append(opts, api.WithManualRebalance(strat, cfg.AdminToken))
			slog.Info("monitor API: manual rebalance enabled", "live", cfg.IsLive())
		}
		mon := api.New(strat, orderLog, opts...)
		go func() {
			if err := mon.ListenAndServe(ctx, cfg.APIAddr); err != nil {
				slog.Error("monitor API stopped", "addr", cfg.APIAddr, "err", err)
			}
		}()
	}

	slog.Info("bot starting",
		"environment", cfg.WSEndpoint(),
		"deployment", cfg.Deployment,
		"version", cfg.Version,
		"record_market", cfg.RecordMarket,
		"underlying", cfg.Underlying,
		"target_dte", cfg.TargetDTE,
		"entry_delta", cfg.EntryDelta,
		"rollout_dte", cfg.RolloutDTE,
		"stop_loss_mult", cfg.StopLossMultiplier,
		"roi_take_profit", cfg.ROITakeProfit,
		"delta_drift_threshold", cfg.DeltaDriftThreshold,
		"iv_margin_bands", cfg.IVMarginBands,
		"max_mm_pct", cfg.MaxMMPct,
		"iv_band_confirm_days", cfg.IVBandConfirmDays,
		"iv_percentile_window", cfg.IVPercentileWindow,
	)

	// If the decision loop stops cycling, exit so the supervisor restarts the
	// bot (startup reconciles positions). A cycle is normally eval_interval_ms;
	// every exchange call is bounded by the gateway's request timeout, so a
	// healthy cycle never comes close to this limit.
	stallAfter := max(10*time.Duration(cfg.EvalIntervalMS)*time.Millisecond, 10*time.Minute)
	go strategy.WatchProgress(ctx, strat.Progress, stallAfter, 30*time.Second, func(idle time.Duration) {
		slog.Error("decision loop stalled: exiting for a supervised restart",
			"idle", idle.Round(time.Second), "limit", stallAfter)
		fail(fmt.Errorf("decision loop stalled for %s", idle.Round(time.Second)))
	})

	if err := strat.Run(ctx); err != nil {
		slog.Info("bot stopped", "reason", err)
	}
	select {
	case err := <-fatalErr:
		return err
	default:
		return nil
	}
}

func runBacktest(cfg *config.Config, fromStr, toStr string, sweep, sweepLadders bool, csvPath string) {
	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		slog.Error("invalid from date", "err", err)
		os.Exit(1)
	}
	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		slog.Error("invalid to date", "err", err)
		os.Exit(1)
	}

	outputDir := "data/results"

	if sweepLadders {
		slog.Info("running DTE ladder sweep", "from", fromStr, "to", toStr)
		if _, err := backtest.RunLadderSweep(cfg, csvPath, from, to, outputDir); err != nil {
			slog.Error("ladder sweep failed", "err", err)
			os.Exit(1)
		}
		slog.Info("ladder sweep complete", "output", outputDir+"/ladder_comparison.csv")
		return
	}

	if sweep {
		slog.Info("running parameter sweep", "from", fromStr, "to", toStr)
		if err := backtest.RunScenarioSweep(cfg, csvPath, from, to, outputDir); err != nil {
			slog.Error("scenario sweep failed", "err", err)
			os.Exit(1)
		}
		slog.Info("scenario sweep complete", "output", outputDir+"/scenario_comparison.csv")
		return
	}

	slog.Info("running backtest", "from", fromStr, "to", toStr)

	feed, err := backtest.NewHistoricalFeed(csvPath, from, to, cfg.IVPercentileWindow)
	if err != nil {
		slog.Error("feed init failed", "err", err)
		os.Exit(1)
	}

	exec := backtest.NewSimExecutor(cfg.Backtest, 100_000)
	engine := backtest.NewEngine(cfg, feed, exec)

	summary, err := engine.Run(context.Background())
	if err != nil {
		slog.Error("backtest run failed", "err", err)
		os.Exit(1)
	}

	writer, err := backtest.NewResultWriter(outputDir)
	if err != nil {
		slog.Error("results dir init failed", "err", err)
		os.Exit(1)
	}
	if err := writer.WriteSummary(summary); err != nil {
		slog.Error("write summary failed", "err", err)
	}
	if err := writer.WriteEquityCurve(engine.Snapshots()); err != nil {
		slog.Error("write equity curve failed", "err", err)
	}
	if err := writer.WriteDrawdown(engine.Snapshots()); err != nil {
		slog.Error("write drawdown failed", "err", err)
	}
	if err := writer.WriteTrades(engine.Trades()); err != nil {
		slog.Error("write trades failed", "err", err)
	}

	slog.Info("backtest complete",
		"total_trades", summary.TotalTrades,
		"total_pnl_usd", summary.TotalPnLUSD,
		"sharpe", summary.SharpeRatio,
		"max_drawdown_pct", summary.MaxDrawdownPct,
		"win_rate_pct", summary.WinRatePct,
		"output_dir", outputDir,
	)

	// Walk-forward validation
	slog.Info("running walk-forward validation")
	if err := backtest.RunWalkForward(cfg, csvPath, from, to, 4, outputDir); err != nil {
		slog.Warn("walk-forward failed", "err", err)
	}
}
