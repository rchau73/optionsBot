package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
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
	"optionsbot/internal/strategy"
)

// pnlHistoryPath is where the P&L history for the monitor chart is kept.
const pnlHistoryPath = "data/pnl_history.jsonl"

// regimeHistoryPath keeps the gamma regime at each daily close: Deribit has
// no history of it, and the margin policy confirms regime changes on it.
const regimeHistoryPath = "data/regime_history.jsonl"

func main() {
	// Flags
	mode := flag.String("mode", "live", "live | backtest")
	btFrom := flag.String("from", "2022-01-01", "backtest start date (YYYY-MM-DD)")
	btTo := flag.String("to", "2024-12-31", "backtest end date (YYYY-MM-DD)")
	sweep := flag.Bool("sweep", false, "run parameter sweep in backtest mode")
	csvPath := flag.String("csv", "data/historical/options.csv", "historical data CSV path")
	cfgPath := flag.String("config", "config.yaml", "config file path")
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	// Load .env
	if err := godotenv.Load(); err != nil {
		slog.Warn("no .env file found")
	}

	// Init logger
	if err := logger.Init("bot.log", *debug); err != nil {
		fmt.Fprintf(os.Stderr, "logger init: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	if *mode == "backtest" {
		runBacktest(cfg, *btFrom, *btTo, *sweep, *csvPath)
		return
	}

	if err := runLive(cfg); err != nil {
		slog.Error("bot exited with error", "err", err)
		os.Exit(1)
	}
}

// runLive wires the live components and runs the strategy until shutdown.
// It returns instead of calling os.Exit so deferred cleanup always runs.
func runLive(cfg *config.Config) error {
	if err := cfg.RequireCredentials(); err != nil {
		return err
	}
	if cfg.IsLive() {
		slog.Warn("LIVE MODE ENABLED — trading real capital")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	gw := gateway.New(cfg, gateway.WithName("trading"))
	if err := gw.Connect(ctx); err != nil {
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
	gatewayErr := make(chan error, 1)
	go func() {
		select {
		case err := <-gw.Fatal():
			gatewayErr <- err
			cancel()
		case err := <-marketGW.Fatal(): // the mainnet data connection gave up
			gatewayErr <- fmt.Errorf("market data: %w", err)
			cancel()
		case <-ctx.Done():
		}
	}()

	md := marketdata.New(cfg, gw)
	if err := md.Start(ctx); err != nil {
		return fmt.Errorf("marketdata start: %w", err)
	}

	exec := orders.NewExecutor(gw)

	orderLog, err := orders.NewLogger("orders.log", cfg.SpreadAlertThreshold)
	if err != nil {
		return fmt.Errorf("order logger init: %w", err)
	}
	defer orderLog.Close()

	// The GEX manager polls public/get_book_summary_by_currency (mainnet) every
	// 60 s and classifies the gamma regime the strategy uses to shed legs and
	// the margin policy uses to cap margin.
	gexMgr := gex.NewManager(marketGW, gex.Params{
		Underlying: cfg.Underlying, NExpiries: 5, StrikeRangePct: cfg.GEXStrikeRangePct, Method: cfg.GEXMethod,
	}, cfg.GammaRegimeBandPct)
	gexMgr.StartBackground(ctx, 60*time.Second)

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
	})

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
		mon := api.New(strat, orderLog, api.WithPnLHistory(pnlHistory), api.WithAccount(acct))
		go func() {
			if err := mon.ListenAndServe(ctx, cfg.APIAddr); err != nil {
				slog.Error("monitor API stopped", "addr", cfg.APIAddr, "err", err)
			}
		}()
	}

	slog.Info("bot starting",
		"environment", cfg.WSEndpoint(),
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

	if err := strat.Run(ctx); err != nil {
		slog.Info("bot stopped", "reason", err)
	}
	select {
	case err := <-gatewayErr:
		return fmt.Errorf("gateway: %w", err)
	default:
		return nil
	}
}

func runBacktest(cfg *config.Config, fromStr, toStr string, sweep bool, csvPath string) {
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
