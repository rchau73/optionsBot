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
	recent               *recentRing // last events, for the monitor API
}

// recentEvents is how many journal events the logger keeps in memory.
const recentEvents = 500

// NewLogger appends JSON lines to the file at path and mirrors them to stdout,
// so `docker logs` shows fills alongside the bot log.
func NewLogger(path string, spreadAlertThreshold float64) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open order log %s: %w", path, err)
	}
	l := NewWriterLogger(io.MultiWriter(os.Stdout, f), spreadAlertThreshold)
	l.file = f
	return l, nil
}

// NewWriterLogger writes JSON lines to w only (used by tests and tools).
func NewWriterLogger(w io.Writer, spreadAlertThreshold float64) *Logger {
	return &Logger{w: w, spreadAlertThreshold: spreadAlertThreshold, recent: newRecentRing(recentEvents)}
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

// base fills the fields every journal entry shares.
func base(event string, ctx EventContext) OrderLog {
	ts := ctx.Market.AsOf
	if ts.IsZero() {
		ts = time.Now()
	}
	return OrderLog{
		Timestamp:  ts,
		Event:      event,
		Status:     event,
		StrategyID: ctx.StrategyID,
		Slot:       ctx.Slot,
		Market:     ctx.Market,
		Portfolio:  ctx.Portfolio,
	}
}

func (rec *OrderLog) setGreeks(g Greeks) {
	rec.Delta, rec.Gamma, rec.Theta, rec.Vega, rec.Rho = g.Delta, g.Gamma, g.Theta, g.Vega, g.Rho
}

func (rec *OrderLog) setPending(r PendingOrderRecord) {
	rec.OrderID = r.OrderID
	rec.Instrument = r.Instrument
	rec.OptionType = r.OptionType
	rec.Direction = r.Direction
	rec.OrderType = r.OrderType
	rec.TriggerReason = r.TriggerReason
	rec.Qty = r.Qty
	rec.LimitPrice = r.LimitPrice
	rec.setGreeks(r.Greeks)
}

func (rec *OrderLog) setPosition(pos *Position) {
	rec.Instrument = pos.Instrument
	rec.OptionType = pos.OptionType
	rec.Qty = pos.Qty
	rec.setGreeks(pos.CurrentGreeks)
}

// LogSubmit records an order sent to the exchange, before any fill.
func (l *Logger) LogSubmit(r PendingOrderRecord, ctx EventContext) {
	rec := base(EventSubmitted, ctx)
	rec.setPending(r)
	l.write(rec)
}

// LogAmend records a resting order re-priced from previousPrice to r.LimitPrice.
func (l *Logger) LogAmend(r PendingOrderRecord, previousPrice float64, ctx EventContext) {
	rec := base(EventAmended, ctx)
	rec.setPending(r)
	rec.PreviousPrice = previousPrice
	l.write(rec)
}

// LogCancelled records an order cancelled before it (fully) filled.
func (l *Logger) LogCancelled(r PendingOrderRecord, ctx EventContext) {
	rec := base(EventCancelled, ctx)
	rec.setPending(r)
	l.write(rec)
}

// LogOpen records a short position opened by a fill.
func (l *Logger) LogOpen(pos *Position, fill Fill, ctx EventContext) {
	rec := base(EventFilled, ctx)
	rec.setPosition(pos)
	rec.OrderID = fill.OrderID
	rec.Direction = DirectionSell
	rec.OrderType = TypeLimit
	rec.TriggerReason = TriggerEntry
	rec.Qty = fill.Qty
	rec.FillPrice = fill.FillPrice
	rec.PremiumReceived = fill.FillPrice * fill.Qty
	l.write(rec)
}

// LogClose records a (partial) buy-back of pos with its realised P&L. pos
// must describe only the closed part (qty and premium share). orderType is
// what was actually sent (market or limit).
func (l *Logger) LogClose(pos *Position, fill Fill, trigger, orderType string, ctx EventContext) {
	rec := base(EventClosed, ctx)
	rec.setPosition(pos)
	rec.OrderID = fill.OrderID
	rec.Direction = DirectionBuy
	rec.OrderType = orderType
	rec.TriggerReason = trigger
	rec.FillPrice = fill.FillPrice

	closeCost := fill.FillPrice * pos.Qty
	pnl := pos.PremiumReceived - closeCost
	holdDays := int(math.Round(rec.Timestamp.Sub(pos.EntryTime).Hours() / 24))
	roi := 0.0
	if pos.PremiumReceived > 0 {
		roi = pnl / pos.PremiumReceived * 100
	}
	spot := ctx.Market.Spot
	if spot <= 0 {
		spot = pos.UnderlyingPrice // best available conversion price
	}

	rec.CloseReason = closeReasonLabel(trigger)
	rec.PremiumReceived = pos.PremiumReceived
	rec.CloseCost = closeCost
	rec.PnL = pnl
	rec.PnLUSD = pnl * spot
	rec.PnLUSDFmt = formatUSD(rec.PnLUSD)
	rec.ROIPct = roi
	rec.ROIPctFmt = formatROI(roi)
	rec.HoldDays = holdDays
	if holdDays > 0 {
		rec.ROIAnnualized = roi / float64(holdDays) * 365
	}
	l.write(rec)
}

// LogReconciled records a position loaded from the exchange at startup.
func (l *Logger) LogReconciled(pos *Position, ctx EventContext) {
	rec := base(EventReconciled, ctx)
	rec.setPosition(pos)
	rec.OrderID = "reconciled"
	rec.Direction = DirectionSell
	rec.TriggerReason = TriggerReconciled
	rec.FillPrice = pos.EntryPrice
	rec.PremiumReceived = pos.PremiumReceived
	l.write(rec)
}

// LogSkipped records why a vacant slot was not entered, with the market at
// that moment — the decisions not taken matter as much as the trades.
func (l *Logger) LogSkipped(reason string, ctx EventContext) {
	rec := base(EventSkipped, ctx)
	rec.SkipReason = reason
	l.write(rec)
}

// LogPnL writes a periodic P&L line.
func (l *Logger) LogPnL(p PnLRecord) {
	p.Event = EventPnL
	l.writeJSON(EventPnL, p, slog.Default().With("event", EventPnL))
}

func (l *Logger) write(rec OrderLog) {
	if rec.Market.SpreadPct > l.spreadAlertThreshold*100 && l.spreadAlertThreshold > 0 {
		slog.Warn("wide spread", "instrument", rec.Instrument, "event", rec.Event,
			"spread_pct", rec.Market.SpreadPct, "threshold_pct", l.spreadAlertThreshold*100)
	}
	l.writeJSON(rec.Event, rec, slog.Default().With("order_id", rec.OrderID, "instrument", rec.Instrument))
}

// Recent returns up to limit journal events with sequence number > after,
// oldest first. Used by the read-only monitor API.
func (l *Logger) Recent(after uint64, limit int) []RecentEvent {
	return l.recent.since(after, limit)
}

// EventCounts returns how many events of each type were journaled since start.
func (l *Logger) EventCounts() map[string]int {
	return l.recent.countsCopy()
}

// writeJSON appends one JSON line; failures are logged with ctxLog's fields.
func (l *Logger) writeJSON(event string, v any, ctxLog *slog.Logger) {
	data, err := json.Marshal(v)
	if err != nil {
		ctxLog.Error("order log marshal error", "err", err)
		return
	}
	l.recent.add(event, data)
	data = append(data, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	// orders.log is the audit trail of every fill; a failed write must be visible.
	if _, err := l.w.Write(data); err != nil {
		ctxLog.Error("order log write failed", "err", err)
	}
}

// Close closes the underlying file, if any.
func (l *Logger) Close() error {
	if l.file == nil {
		return nil
	}
	return l.file.Close()
}
