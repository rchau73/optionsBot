package orders

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Logger writes structured JSON order events to both orders.log and stdout
// (so docker compose logs -f captures them alongside bot.log).
type Logger struct {
	mu                   sync.Mutex
	file                 *os.File
	w                    io.Writer
	spreadAlertThreshold float64
}

func NewLogger(path string, spreadAlertThreshold float64) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &Logger{
		file:                 f,
		w:                    io.MultiWriter(os.Stdout, f),
		spreadAlertThreshold: spreadAlertThreshold,
	}, nil
}

// closeReasonLabel maps internal trigger constants to human-readable close reasons.
func closeReasonLabel(trigger string) string {
	switch trigger {
	case TriggerStopLoss200Pct:
		return "stop_loss"
	case TriggerRollout19DTE:
		return "dte_rollout"
	case TriggerRolloutDelta:
		return "delta_drift"
	case TriggerRolloutROI:
		return "roi_target"
	case TriggerGammaClose:
		return "gamma_regime"
	case TriggerKillSwitch:
		return "kill_switch"
	default:
		return trigger
	}
}

// formatUSD formats a USD dollar amount with thousands separators and 2 decimal
// places, e.g. -1234567.89 → "-1,234,567.89".
func formatUSD(v float64) string {
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	dollars := int64(v)
	cents := int64(math.Round((v - float64(dollars)) * 100))
	if cents == 100 {
		dollars++
		cents = 0
	}
	s := strconv.FormatInt(dollars, 10)
	if len(s) > 3 {
		var b strings.Builder
		offset := len(s) % 3
		if offset > 0 {
			b.WriteString(s[:offset])
		}
		for i := offset; i < len(s); i += 3 {
			if b.Len() > 0 {
				b.WriteByte(',')
			}
			b.WriteString(s[i : i+3])
		}
		s = b.String()
	}
	return fmt.Sprintf("%s%s.%02d", sign, s, cents)
}

// formatROI formats a percentage with 4 decimal places and a % suffix,
// e.g. -7.142857 → "-7.1429%".
func formatROI(v float64) string {
	return fmt.Sprintf("%.4f%%", v)
}

// PendingOrderRecord carries the data available at order-submission time.
// Used to write orders.log entries before a fill is confirmed (status=submitted/cancelled).
type PendingOrderRecord struct {
	OrderID         string
	Instrument      string
	OptionType      string // "call" or "put"
	Direction       string
	TriggerReason   string
	Qty             float64
	LimitPrice      float64
	Strike          float64
	UnderlyingPrice float64
	Bid             float64
	Ask             float64
	Greeks          Greeks
	IV              float64
	IVPercentile    float64
}

// GEXContext carries the market-wide GEX fields to embed in every order log entry.
type GEXContext struct {
	Regime      string
	RegimeScore float64
	GammaFlip   float64
	FlipFound   bool
}

func applyGEX(rec *OrderLog, g GEXContext) {
	rec.GammaRegime = g.Regime
	rec.GammaRegimeScore = g.RegimeScore
	if g.FlipFound {
		rec.GammaFlip = g.GammaFlip
		rec.GammaFlipFound = true
	}
}

// LogOpen records a new position entry.
func (l *Logger) LogOpen(pos *Position, fill Fill, ivPercentile float64, spreadAlertThreshold float64, mkt MarketContext, gexCtx GEXContext) {
	rec := l.buildRecord(pos, fill, ivPercentile, TriggerEntry)
	rec.MarketTrend = mkt.Trend
	rec.PortDelta = mkt.NetDelta
	rec.PortGamma = mkt.NetGamma
	rec.PortVega = mkt.NetVega
	rec.PortTheta = mkt.NetTheta
	applyGEX(&rec, gexCtx)
	l.write(rec)
}

// LogClose records a position close with ROI fields.
func (l *Logger) LogClose(pos *Position, fill Fill, ivPercentile float64, trigger string, mkt MarketContext, gexCtx GEXContext) {
	rec := l.buildRecord(pos, fill, ivPercentile, trigger)
	rec.MarketTrend = mkt.Trend
	rec.PortDelta = mkt.NetDelta
	rec.PortGamma = mkt.NetGamma
	rec.PortVega = mkt.NetVega
	rec.PortTheta = mkt.NetTheta
	applyGEX(&rec, gexCtx)

	closeCost := fill.FillPrice * pos.Qty
	pnl := pos.PremiumReceived - closeCost
	holdDays := int(math.Round(time.Since(pos.EntryTime).Hours() / 24))
	roi := 0.0
	if pos.PremiumReceived > 0 {
		roi = pnl / pos.PremiumReceived * 100
	}
	roiAnn := 0.0
	if holdDays > 0 {
		roiAnn = roi / float64(holdDays) * 365
	}

	rec.CloseReason = closeReasonLabel(trigger)
	rec.PremiumReceived = pos.PremiumReceived
	rec.CloseCost = closeCost
	rec.PnLUSD = pnl
	rec.PnLUSDFmt = formatUSD(pnl * pos.UnderlyingPrice) // BTC × spot → USD
	rec.ROIPct = roi
	rec.ROIPctFmt = formatROI(roi)
	rec.HoldDays = holdDays
	rec.ThetaCapturedUSD = pos.CurrentGreeks.Theta * float64(holdDays)
	rec.ROIAnnualized = roiAnn

	l.write(rec)
}

// LogSubmit writes a status=submitted entry when a limit order is placed on the exchange.
// Called immediately after a successful Submit() call, before fill confirmation.
func (l *Logger) LogSubmit(r PendingOrderRecord, mkt MarketContext, gexCtx GEXContext) {
	mid := (r.Bid + r.Ask) / 2
	spreadAbs := r.Ask - r.Bid
	spreadPct := 0.0
	if mid > 0 {
		spreadPct = spreadAbs / mid * 100
	}
	if spreadPct > l.spreadAlertThreshold*100 {
		slog.Warn("wide spread on submission",
			"instrument", r.Instrument, "spread_pct", spreadPct)
	}
	rec := OrderLog{
		Timestamp:        time.Now(),
		OrderID:          r.OrderID,
		Instrument:       r.Instrument,
		Direction:        r.Direction,
		OrderType:        TypeLimit,
		TriggerReason:    r.TriggerReason,
		Qty:              r.Qty,
		LimitPrice:       r.LimitPrice,
		FillPrice:        0,
		Status:           "submitted",
		Delta:            r.Greeks.Delta,
		Gamma:            r.Greeks.Gamma,
		Theta:            r.Greeks.Theta,
		Vega:             r.Greeks.Vega,
		Rho:              r.Greeks.Rho,
		IV:               r.IV,
		IVPercentile:     r.IVPercentile,
		UnderlyingPrice:  r.UnderlyingPrice,
		Strike:           r.Strike,
		Bid:              r.Bid,
		Ask:              r.Ask,
		Mid:              mid,
		SpreadAbs:        spreadAbs,
		SpreadPct:        spreadPct,
		MarketTrend:      mkt.Trend,
		PortDelta:        mkt.NetDelta,
		PortGamma:        mkt.NetGamma,
		PortVega:         mkt.NetVega,
		PortTheta:        mkt.NetTheta,
		GammaRegime:      gexCtx.Regime,
		GammaRegimeScore: gexCtx.RegimeScore,
	}
	if gexCtx.FlipFound {
		rec.GammaFlip = gexCtx.GammaFlip
		rec.GammaFlipFound = true
	}
	l.write(rec)
}

// LogCancelled writes a status=cancelled entry when an open limit order is cancelled
// (e.g., due to fill-timeout or forced close).
func (l *Logger) LogCancelled(r PendingOrderRecord, mkt MarketContext, gexCtx GEXContext) {
	mid := (r.Bid + r.Ask) / 2
	rec := OrderLog{
		Timestamp:       time.Now(),
		OrderID:         r.OrderID,
		Instrument:      r.Instrument,
		Direction:       r.Direction,
		OrderType:       TypeLimit,
		TriggerReason:   r.TriggerReason,
		Qty:             r.Qty,
		LimitPrice:      r.LimitPrice,
		FillPrice:       0,
		Status:          "cancelled",
		Delta:           r.Greeks.Delta,
		Gamma:           r.Greeks.Gamma,
		Theta:           r.Greeks.Theta,
		Vega:            r.Greeks.Vega,
		Rho:             r.Greeks.Rho,
		IV:              r.IV,
		IVPercentile:    r.IVPercentile,
		UnderlyingPrice: r.UnderlyingPrice,
		Strike:          r.Strike,
		Bid:             r.Bid,
		Ask:             r.Ask,
		Mid:             mid,
		MarketTrend:     mkt.Trend,
		PortDelta:       mkt.NetDelta,
		GammaRegime:     gexCtx.Regime,
	}
	l.write(rec)
}

// LogReconciled writes a status=reconciled entry for positions loaded from the
// exchange on bot startup (reconcilePositions). These were opened in a previous
// session and have no original orders.log entry in the current run.
func (l *Logger) LogReconciled(pos *Position, ivPercentile float64, mkt MarketContext, gexCtx GEXContext) {
	rec := l.buildRecord(pos,
		Fill{OrderID: "reconciled", FillPrice: pos.EntryPrice, Qty: pos.Qty, Timestamp: pos.EntryTime},
		ivPercentile, TriggerReconciled)
	rec.Status = "reconciled"
	rec.MarketTrend = mkt.Trend
	rec.PortDelta = mkt.NetDelta
	rec.PortGamma = mkt.NetGamma
	rec.PortVega = mkt.NetVega
	rec.PortTheta = mkt.NetTheta
	applyGEX(&rec, gexCtx)
	l.write(rec)
}

func (l *Logger) buildRecord(pos *Position, fill Fill, ivPercentile float64, trigger string) OrderLog {
	mid := (pos.CurrentMid)
	spreadAbs := 0.0
	spreadPct := 0.0
	if pos.CurrentMid > 0 {
		// We store bid/ask on position after update
		spreadAbs = 0 // populated at call site if needed
		spreadPct = 0
	}

	// Intrinsic value based on underlying spot price vs strike (not the option premium).
	intrinsic := 0.0
	switch pos.OptionType {
	case "call":
		intrinsic = math.Max(pos.UnderlyingPrice-pos.Strike, 0)
	case "put":
		intrinsic = math.Max(pos.Strike-pos.UnderlyingPrice, 0)
	}

	extrinsic := fill.FillPrice - intrinsic
	intrinsicPct := 0.0
	extrinsicPct := 0.0
	if fill.FillPrice > 0 {
		intrinsicPct = intrinsic / fill.FillPrice * 100
		extrinsicPct = extrinsic / fill.FillPrice * 100
	}

	if mid > 0 {
		spreadPct = spreadAbs / mid * 100
	}

	if spreadPct > l.spreadAlertThreshold*100 {
		slog.Warn("wide spread alert",
			"instrument", pos.Instrument,
			"spread_pct", spreadPct,
		)
	}

	direction := DirectionSell
	if trigger == TriggerRollout19DTE || trigger == TriggerRolloutDelta ||
		trigger == TriggerRolloutROI || trigger == TriggerStopLoss200Pct ||
		trigger == TriggerGammaClose || trigger == TriggerKillSwitch {
		direction = DirectionBuy
	}

	return OrderLog{
		Timestamp:       fill.Timestamp,
		OrderID:         fill.OrderID,
		Instrument:      pos.Instrument,
		Direction:       direction,
		OrderType:       TypeLimit,
		TriggerReason:   trigger,
		Qty:             pos.Qty,
		LimitPrice:      pos.LimitPrice,
		FillPrice:       fill.FillPrice,
		Status:          "filled",
		Delta:           pos.CurrentGreeks.Delta,
		Gamma:           pos.CurrentGreeks.Gamma,
		Theta:           pos.CurrentGreeks.Theta,
		Vega:            pos.CurrentGreeks.Vega,
		Rho:             pos.CurrentGreeks.Rho,
		IV:              pos.CurrentGreeks.IV,
		IVPercentile:    ivPercentile,
		UnderlyingPrice: pos.UnderlyingPrice,
		Strike:          pos.Strike,
		IntrinsicValue:  intrinsic,
		ExtrinsicValue:  extrinsic,
		IntrinsicPct:    intrinsicPct,
		ExtrinsicPct:    extrinsicPct,
		Mid:             mid,
		SpreadAbs:       spreadAbs,
		SpreadPct:       spreadPct,
		FillVsMid:       fill.FillPrice - mid,
	}
}

func (l *Logger) write(rec OrderLog) {
	data, err := json.Marshal(rec)
	if err != nil {
		slog.Error("order log marshal error", "err", err)
		return
	}
	data = append(data, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	// orders.log is the audit trail of every fill; a failed write must be visible.
	if _, err := l.w.Write(data); err != nil {
		slog.Error("order log write failed", "order_id", rec.OrderID, "instrument", rec.Instrument, "err", err)
	}
}

func (l *Logger) Close() error {
	return l.file.Close()
}
