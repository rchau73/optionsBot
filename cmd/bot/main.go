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

	"optionsbot/internal/backtest"
	"optionsbot/internal/config"
	"optionsbot/internal/gateway"
	"optionsbot/internal/gex"
	"optionsbot/internal/hedge"
	"optionsbot/internal/logger"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

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

	// Guard: require explicit env var for live mode
	if os.Getenv("DERIBIT_ENV") == "live" {
		slog.Warn("LIVE MODE ENABLED — trading real capital")
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

	runLive(cfg)
}

func runLive(cfg *config.Config) {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	gw := gateway.New(cfg)
	if err := gw.Connect(ctx); err != nil {
		slog.Error("gateway connect failed", "err", err)
		os.Exit(1)
	}
	defer gw.Close()

	md := marketdata.New(cfg, gw)
	if err := md.Start(ctx); err != nil {
		slog.Error("marketdata start failed", "err", err)
		os.Exit(1)
	}

	exec := orders.NewExecutor(gw)
	state := orders.NewStateManager()

	orderLog, err := orders.NewLogger("orders.log", cfg.SpreadAlertThreshold)
	if err != nil {
		slog.Error("order logger init failed", "err", err)
		os.Exit(1)
	}
	defer orderLog.Close()

	hedgeRpt := hedge.New("hedge_report.json", cfg.HedgeReportThreshold)

	strat := strategy.New(cfg, md, exec, state, orderLog, hedgeRpt)

	// Wire up the GEX manager: fetches public/get_book_summary_by_currency every
	// 60s and computes the market-wide gamma exposure (GEX) regime. The strategy
	// uses this instead of net portfolio gamma to decide when to shed a strangle leg.
	gexMgr := gex.NewManager(gw, md, cfg.Underlying, 5, cfg.GammaRegimeBandPct, cfg.GEXStrikeRangePct)
	gexMgr.StartBackground(ctx, 60*time.Second)
	strat.SetGEXManager(gexMgr)
	strat.LoadGammaPriceHistory(ctx)

	slog.Info("bot starting",
		"environment", cfg.WSEndpoint(),
		"underlying", cfg.Underlying,
		"target_dte", cfg.TargetDTE,
		"entry_delta", cfg.EntryDelta,
		"rollout_dte", cfg.RolloutDTE,
		"stop_loss_mult", cfg.StopLossMultiplier,
		"roi_take_profit", cfg.ROITakeProfit,
		"delta_drift_threshold", cfg.DeltaDriftThreshold,
		"max_margin_pct", cfg.MaxMarginPct,
		"iv_percentile_window", cfg.IVPercentileWindow,
	)

	if err := strat.Run(ctx); err != nil {
		slog.Info("bot stopped", "reason", err)
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

	writer := backtest.NewResultWriter(outputDir)
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
