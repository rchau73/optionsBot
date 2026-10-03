package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/gateway"
)

// gatewayClient is the slice of the gateway the manager uses.
type gatewayClient interface {
	Call(ctx context.Context, method string, params any, priority int) (gateway.JSONRPCResponse, error)
	Subscribe(ctx context.Context, channels []string) error
	Notifications() <-chan gateway.JSONRPCResponse
}

// Channel prefixes of the subscription pushes the manager handles.
const (
	dvolChannelPrefix   = "deribit_volatility_index."
	indexChannelPrefix  = "deribit_price_index."
	tickerChannelPrefix = "ticker."
)

// Manager keeps the latest option chain snapshot, index price and DVOL for
// one underlying, fed by Deribit subscription pushes. Readers get copies.
type Manager struct {
	cfg             *config.Config
	gw              gatewayClient
	dvol            *DVOLTracker
	mu              sync.RWMutex
	instruments     map[string]*Instrument
	underlyingPrice float64
}

func New(cfg *config.Config, gw gatewayClient) *Manager {
	return &Manager{
		cfg:         cfg,
		gw:          gw,
		dvol:        NewDVOLTracker(cfg.IVPercentileWindow),
		instruments: make(map[string]*Instrument),
	}
}

// Start loads the option chain, seeds DVOL history and subscribes to the
// index, DVOL and the tickers of the expiries the strategy can trade.
// The notification consumer runs until ctx is cancelled.
func (m *Manager) Start(ctx context.Context) error {
	slog.Info("marketdata starting", "underlying", m.cfg.Underlying)
	if err := m.fetchOptionsChain(ctx); err != nil {
		return fmt.Errorf("fetch options chain: %w", err)
	}
	if err := m.seedDVOLHistory(ctx, time.Now()); err != nil {
		slog.Warn("DVOL history seed failed; IV percentile starts at 50", "err", err)
	}

	channels := m.subscriptionChannels(time.Now())
	slog.Info("subscribing to market data", "channels", len(channels), "total_instruments", m.instrumentCount())

	// Start the consumer before subscribing so ticker data arriving during the
	// subscription batches is drained immediately rather than dropped.
	go m.processNotifications(ctx)

	if err := m.gw.Subscribe(ctx, channels); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	return nil
}

// subscriptionChannels lists the index and DVOL channels plus one ticker per
// instrument of a relevant expiry. Subscribing to the whole chain (1,000+
// instruments) hits testnet subscription limits and starves the expiries
// the strategy actually needs.
func (m *Manager) subscriptionChannels(now time.Time) []string {
	u := strings.ToLower(m.cfg.Underlying)
	channels := []string{
		dvolChannelPrefix + u + "_usd",
		// The index price is reliable even when options have no quotes (testnet).
		indexChannelPrefix + u + "_usd",
	}
	relevant := m.selectRelevantExpiries(now)
	slog.Info("relevant expiries for subscription", "count", len(relevant), "slots", len(m.cfg.Slots()))

	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.instruments))
	for name, inst := range m.instruments {
		if relevant[inst.Expiry] {
			names = append(names, name)
		}
	}
	sort.Strings(names) // deterministic order for logs and tests
	for _, name := range names {
		channels = append(channels, tickerChannelPrefix+name+".100ms")
	}
	return channels
}

// instrumentInfo is the subset of public/get_instruments we use.
type instrumentInfo struct {
	InstrumentName string  `json:"instrument_name"`
	Strike         float64 `json:"strike"`
	ExpirationTS   int64   `json:"expiration_timestamp"`
	OptionType     string  `json:"option_type"`
	TickSize       float64 `json:"tick_size"`
	MinTradeAmount float64 `json:"min_trade_amount"`
	TickSizeSteps  []struct {
		AbovePrice float64 `json:"above_price"`
		TickSize   float64 `json:"tick_size"`
	} `json:"tick_size_steps"`
}

func (m *Manager) fetchOptionsChain(ctx context.Context) error {
	resp, err := m.gw.Call(ctx, "public/get_instruments", map[string]any{
		"currency": m.cfg.Underlying,
		"kind":     "option",
		"expired":  false,
	}, gateway.PriorityLow)
	if err != nil {
		return err
	}
	var infos []instrumentInfo
	if err := json.Unmarshal(resp.Result, &infos); err != nil {
		return fmt.Errorf("decode instruments: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, info := range infos {
		m.instruments[info.InstrumentName] = m.newInstrument(info)
	}
	slog.Info("options chain loaded", "underlying", m.cfg.Underlying, "instruments", len(infos))
	return nil
}

func (m *Manager) newInstrument(info instrumentInfo) *Instrument {
	tickSize := info.TickSize
	if tickSize <= 0 {
		tickSize = 0.0001
	}
	minTA := info.MinTradeAmount
	if minTA <= 0 {
		minTA = 0.1 // Deribit default for BTC/ETH options
	}
	steps := make([]TickSizeStep, len(info.TickSizeSteps))
	for i, s := range info.TickSizeSteps {
		steps[i] = TickSizeStep{AbovePrice: s.AbovePrice, TickSize: s.TickSize}
	}
	return &Instrument{
		Name:           info.InstrumentName,
		Underlying:     m.cfg.Underlying,
		Strike:         info.Strike,
		Expiry:         time.UnixMilli(info.ExpirationTS),
		OptionType:     info.OptionType,
		TickSize:       tickSize,
		TickSizeSteps:  steps,
		MinTradeAmount: minTA,
	}
}

// seedDVOLHistory loads one daily DVOL close per day for the percentile
// window, so the IV percentile is meaningful from the first minute instead
// of after a year of uptime.
func (m *Manager) seedDVOLHistory(ctx context.Context, now time.Time) error {
	days := m.cfg.IVPercentileWindow
	if days <= 0 {
		return nil
	}
	resp, err := m.gw.Call(ctx, "public/get_volatility_index_data", map[string]any{
		"currency":        m.cfg.Underlying,
		"start_timestamp": now.AddDate(0, 0, -days-1).UnixMilli(),
		"end_timestamp":   now.UnixMilli(),
		"resolution":      "1D",
	}, gateway.PriorityLow)
	if err != nil {
		return err
	}
	var result struct {
		Data [][]float64 `json:"data"` // [timestamp, open, high, low, close]
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("decode volatility index data: %w", err)
	}
	for _, candle := range result.Data {
		if len(candle) < 5 {
			continue
		}
		m.dvol.Record(time.UnixMilli(int64(candle[0])), candle[4])
	}
	slog.Info("DVOL history seeded", "days", len(result.Data), "iv_percentile", m.dvol.Percentile())
	return nil
}

// selectRelevantExpiries returns the expiries the strategy may trade: for
// each target DTE the expiry SelectExpiry would pick (same window rule), plus
// the next three expiries beyond the largest target for rollouts.
func (m *Manager) selectRelevantExpiries(now time.Time) map[time.Time]bool {
	all := m.AllInstruments()
	result := make(map[time.Time]bool)

	maxTarget := 0
	for _, slot := range m.cfg.Slots() {
		lo, hi := ExpiryWindow(slot.TargetDTE, m.cfg.MaxDTEDeviation, m.cfg.RolloutDTE)
		if exp, ok := NearestExpiry(all, now, lo, hi, nil); ok {
			result[exp] = true
		}
		maxTarget = max(maxTarget, slot.TargetDTE)
	}

	var beyond []time.Time
	seen := make(map[time.Time]bool)
	for _, inst := range all {
		if !seen[inst.Expiry] && DaysToExpiry(inst.Expiry, now) > maxTarget {
			seen[inst.Expiry] = true
			beyond = append(beyond, inst.Expiry)
		}
	}
	sort.Slice(beyond, func(i, j int) bool { return beyond[i].Before(beyond[j]) })
	for i := 0; i < len(beyond) && i < 3; i++ {
		result[beyond[i]] = true
	}
	return result
}

func (m *Manager) processNotifications(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case notif := <-m.gw.Notifications():
			m.handleNotification(notif, time.Now())
		}
	}
}

func (m *Manager) handleNotification(notif gateway.JSONRPCResponse, now time.Time) {
	if notif.Params == nil {
		return
	}
	ch, data := notif.Params.Channel, []byte(notif.Params.Data)

	switch {
	case strings.HasPrefix(ch, dvolChannelPrefix):
		var d struct {
			Volatility float64 `json:"volatility"`
		}
		if err := json.Unmarshal(data, &d); err == nil && d.Volatility > 0 {
			m.dvol.Record(now, d.Volatility)
		}

	case strings.HasPrefix(ch, indexChannelPrefix):
		var d struct {
			Price float64 `json:"price"`
		}
		if err := json.Unmarshal(data, &d); err == nil && d.Price > 0 {
			m.mu.Lock()
			m.underlyingPrice = d.Price
			m.mu.Unlock()
		}

	case strings.HasPrefix(ch, tickerChannelPrefix):
		m.handleTicker(ch, data, now)
	}
}

// tickerData is the subset of a ticker push we use.
type tickerData struct {
	InstrumentName  string  `json:"instrument_name"`
	BestBidPrice    float64 `json:"best_bid_price"`
	BestAskPrice    float64 `json:"best_ask_price"`
	MarkPrice       float64 `json:"mark_price"`
	UnderlyingPrice float64 `json:"underlying_price"`
	Greeks          Greeks  `json:"greeks"`
	MarkIV          float64 `json:"mark_iv"` // percent, e.g. 55.2
}

func (m *Manager) handleTicker(channel string, data []byte, now time.Time) {
	var t tickerData
	if err := json.Unmarshal(data, &t); err != nil {
		slog.Warn("ticker parse error", "channel", channel, "err", err)
		return
	}

	mid := (t.BestBidPrice + t.BestAskPrice) / 2
	if t.BestBidPrice == 0 && t.BestAskPrice == 0 {
		mid = t.MarkPrice // testnet options often have no quotes; mark price is always computed
	}
	greeks := t.Greeks
	greeks.IV = t.MarkIV / 100

	m.mu.Lock()
	defer m.mu.Unlock()
	inst, ok := m.instruments[t.InstrumentName]
	if !ok {
		return
	}
	inst.Bid = t.BestBidPrice
	inst.Ask = t.BestAskPrice
	inst.Mid = mid
	// Per-option underlying_price can be a stale synthetic value on testnet;
	// the authoritative spot comes from the index channel above.
	inst.UnderlyingPrice = t.UnderlyingPrice
	inst.Greeks = greeks
	inst.DVOLIndex = m.dvol.Current()
	inst.IVPercentile = m.dvol.Percentile()
	inst.UpdatedAt = now
}

// GetInstrument returns a snapshot of one instrument.
func (m *Manager) GetInstrument(name string) (*Instrument, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inst, ok := m.instruments[name]
	if !ok {
		return nil, false
	}
	snap := *inst
	return &snap, true
}

// AllInstruments returns snapshots of all tracked instruments.
func (m *Manager) AllInstruments() []*Instrument {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Instrument, 0, len(m.instruments))
	for _, inst := range m.instruments {
		snap := *inst
		out = append(out, &snap)
	}
	return out
}

func (m *Manager) instrumentCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.instruments)
}

// DVOL returns the latest Deribit volatility index value (%), 0 if unknown.
func (m *Manager) DVOL() float64 {
	return m.dvol.Current()
}

// IVPercentile ranks today's DVOL against the configured window of days.
func (m *Manager) IVPercentile() float64 {
	return m.dvol.Percentile()
}

// UnderlyingPrice returns the latest index price, or 0 before the first push.
func (m *Manager) UnderlyingPrice() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.underlyingPrice
}
