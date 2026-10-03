package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/gateway"
)

// Manager subscribes to live Deribit feeds and maintains current instrument state.
type Manager struct {
	cfg             *config.Config
	gw              *gateway.Gateway
	dvol            *DVOLTracker
	mu              sync.RWMutex
	instruments     map[string]*Instrument
	underlyingPrice float64
	tickCh          chan *Tick
}

func New(cfg *config.Config, gw *gateway.Gateway) *Manager {
	return &Manager{
		cfg:         cfg,
		gw:          gw,
		dvol:        NewDVOLTracker(cfg.IVPercentileWindow),
		instruments: make(map[string]*Instrument),
		tickCh:      make(chan *Tick, 1024),
	}
}

// Start subscribes to market data feeds for the configured underlying.
func (m *Manager) Start(ctx context.Context) error {
	slog.Info("marketdata starting", "underlying", m.cfg.Underlying)
	instruments, err := m.fetchOptionsChain(ctx)
	if err != nil {
		return fmt.Errorf("fetch options chain: %w", err)
	}
	slog.Info("options chain loaded", "underlying", m.cfg.Underlying, "instruments", len(instruments))

	// Subscribe only to instruments for the expiries the strategy will actually trade.
	// Subscribing to all 1040+ instruments hits testnet subscription limits (~100) and
	// causes instruments for target expiries to have Mid=0 (no ticker data), which
	// makes SelectStrike return "no OTM candidates".
	relevantExpiries := m.selectRelevantExpiries()
	slog.Info("relevant expiries for subscription",
		"count", len(relevantExpiries),
		"slots", len(m.cfg.Slots()),
	)

	channels := make([]string, 0, 512)
	channels = append(channels, fmt.Sprintf("deribit_volatility_index.%s_usd.1d", m.cfg.Underlying))
	// Index price channel gives a reliable spot price even when options have no quotes (e.g. testnet).
	channels = append(channels, fmt.Sprintf("deribit_price_index.%s_usd", strings.ToLower(m.cfg.Underlying)))

	for _, name := range instruments {
		m.mu.RLock()
		inst, ok := m.instruments[name]
		m.mu.RUnlock()
		if ok {
			if _, relevant := relevantExpiries[inst.Expiry]; relevant {
				channels = append(channels, fmt.Sprintf("ticker.%s.100ms", name))
			}
		}
	}

	slog.Info("subscribing to market data",
		"channels", len(channels),
		"total_instruments", len(instruments),
	)

	// Start the consumer before subscribing so ticker data arriving during the
	// subscription batches is drained immediately rather than dropped.
	go m.processNotifications(ctx)

	if err := m.gw.Subscribe(ctx, channels); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	return nil
}

func (m *Manager) fetchOptionsChain(ctx context.Context) ([]string, error) {
	resp, err := m.gw.Call(ctx, "public/get_instruments", map[string]interface{}{
		"currency": m.cfg.Underlying,
		"kind":     "option",
		"expired":  false,
	}, gateway.PriorityLow)
	if err != nil {
		return nil, err
	}

	var insts []struct {
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
	if err := json.Unmarshal(resp.Result, &insts); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(insts))
	for _, inst := range insts {
		expiry := time.UnixMilli(inst.ExpirationTS)
		tickSize := inst.TickSize
		if tickSize <= 0 {
			tickSize = 0.0001
		}
		steps := make([]TickSizeStep, len(inst.TickSizeSteps))
		for i, s := range inst.TickSizeSteps {
			steps[i] = TickSizeStep{AbovePrice: s.AbovePrice, TickSize: s.TickSize}
		}
		minTA := inst.MinTradeAmount
		if minTA <= 0 {
			minTA = 0.1 // Deribit default for BTC/ETH options
		}
		m.mu.Lock()
		m.instruments[inst.InstrumentName] = &Instrument{
			Name:           inst.InstrumentName,
			Underlying:     m.cfg.Underlying,
			Strike:         inst.Strike,
			Expiry:         expiry,
			OptionType:     inst.OptionType,
			TickSize:       tickSize,
			TickSizeSteps:  steps,
			MinTradeAmount: minTA,
		}
		m.mu.Unlock()
		names = append(names, inst.InstrumentName)
	}
	return names, nil
}

// selectRelevantExpiries returns the set of expiry times the strategy needs:
// the nearest expiry within the deviation window for each targetDTE, plus the
// next 3 future expiries beyond the max target for rollout.
func (m *Manager) selectRelevantExpiries() map[time.Time]struct{} {
	m.mu.RLock()
	all := make([]*Instrument, 0, len(m.instruments))
	for _, inst := range m.instruments {
		all = append(all, inst)
	}
	m.mu.RUnlock()

	result := make(map[time.Time]struct{})
	now := time.Now()

	// Collect unique target DTEs from slots (works for both dte_delta_matrix and legacy config).
	uniqueDTEs := make(map[int]struct{})
	for _, slot := range m.cfg.Slots() {
		uniqueDTEs[slot.TargetDTE] = struct{}{}
	}

	for targetDTE := range uniqueDTEs {
		lo := targetDTE - m.cfg.MaxDTEDeviation
		hi := targetDTE + m.cfg.MaxDTEDeviation
		var best time.Time
		bestDTE := math.MaxInt32
		seen := make(map[time.Time]bool)
		for _, inst := range all {
			if seen[inst.Expiry] {
				continue
			}
			seen[inst.Expiry] = true
			dte := int(math.Round(inst.Expiry.Sub(now).Hours() / 24))
			if dte >= lo && dte <= hi && dte < bestDTE {
				bestDTE = dte
				best = inst.Expiry
			}
		}
		if !best.IsZero() {
			result[best] = struct{}{}
		}
	}

	// Collect all future expiries, sorted ascending, and add the next 3 beyond the
	// max target DTE for rollout support.
	type expDTE struct {
		exp time.Time
		dte int
	}
	seen := make(map[time.Time]bool)
	var futures []expDTE
	for _, inst := range all {
		if seen[inst.Expiry] {
			continue
		}
		seen[inst.Expiry] = true
		dte := int(math.Round(inst.Expiry.Sub(now).Hours() / 24))
		if dte >= 0 {
			futures = append(futures, expDTE{inst.Expiry, dte})
		}
	}
	sort.Slice(futures, func(i, j int) bool { return futures[i].dte < futures[j].dte })

	maxTargetDTE := 0
	for dte := range uniqueDTEs {
		if dte > maxTargetDTE {
			maxTargetDTE = dte
		}
	}
	added := 0
	for _, f := range futures {
		if f.dte > maxTargetDTE && added < 3 {
			result[f.exp] = struct{}{}
			added++
		}
	}

	return result
}

// EnsureSubscribed subscribes to all instruments for expiry if not already subscribed.
// Called by the strategy when rolling to an expiry not covered at startup.
func (m *Manager) EnsureSubscribed(ctx context.Context, expiry time.Time) {
	m.mu.RLock()
	var channels []string
	for _, inst := range m.instruments {
		if inst.Expiry.Equal(expiry) && inst.Mid == 0 {
			channels = append(channels, fmt.Sprintf("ticker.%s.100ms", inst.Name))
		}
	}
	m.mu.RUnlock()

	if len(channels) == 0 {
		return
	}
	slog.Info("subscribing to rollout expiry", "expiry", expiry.Format("2006-01-02"), "channels", len(channels))
	if err := m.gw.Subscribe(ctx, channels); err != nil {
		slog.Error("EnsureSubscribed failed", "expiry", expiry.Format("2006-01-02"), "err", err)
	}
}

func (m *Manager) processNotifications(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case notif := <-m.gw.Notifications():
			m.handleNotification(notif)
		}
	}
}

func (m *Manager) handleNotification(notif gateway.JSONRPCResponse) {
	if notif.Params == nil {
		return
	}

	data := []byte(notif.Params.Data)

	switch {
	case isDVOLChannel(notif.Params.Channel):
		var d struct {
			Volatility float64 `json:"volatility"`
		}
		if err := json.Unmarshal(data, &d); err == nil {
			m.dvol.Push(d.Volatility)
		}

	case isIndexPriceChannel(notif.Params.Channel):
		var d struct {
			Price float64 `json:"price"`
		}
		if err := json.Unmarshal(data, &d); err == nil && d.Price > 0 {
			m.mu.Lock()
			m.underlyingPrice = d.Price
			m.mu.Unlock()
		}

	case isTickerChannel(notif.Params.Channel):
		m.handleTicker(notif.Params.Channel, data)
	}
}

func (m *Manager) handleTicker(channel string, data []byte) {
	var t struct {
		InstrumentName  string  `json:"instrument_name"`
		BestBidPrice    float64 `json:"best_bid_price"`
		BestAskPrice    float64 `json:"best_ask_price"`
		MarkPrice       float64 `json:"mark_price"`
		UnderlyingPrice float64 `json:"underlying_price"`
		Greeks          struct {
			Delta float64 `json:"delta"`
			Gamma float64 `json:"gamma"`
			Theta float64 `json:"theta"`
			Vega  float64 `json:"vega"`
			Rho   float64 `json:"rho"`
		} `json:"greeks"`
		MarkIV float64 `json:"mark_iv"`
	}
	if err := json.Unmarshal(data, &t); err != nil {
		slog.Warn("ticker parse error", "channel", channel, "err", err)
		return
	}

	mid := (t.BestBidPrice + t.BestAskPrice) / 2
	if mid == 0 {
		mid = t.MarkPrice // testnet options often have no quotes; mark price is always computed
	}
	ivPercentile := m.dvol.Percentile()

	m.mu.Lock()
	inst, ok := m.instruments[t.InstrumentName]
	if !ok {
		m.mu.Unlock()
		return
	}
	inst.Bid = t.BestBidPrice
	inst.Ask = t.BestAskPrice
	inst.Mid = mid
	inst.UnderlyingPrice = t.UnderlyingPrice
	inst.Greeks = Greeks{
		Delta: t.Greeks.Delta,
		Gamma: t.Greeks.Gamma,
		Theta: t.Greeks.Theta,
		Vega:  t.Greeks.Vega,
		Rho:   t.Greeks.Rho,
		IV:    t.MarkIV / 100,
	}
	inst.DVOLIndex = m.dvol.Current()
	inst.IVPercentile = ivPercentile
	inst.UpdatedAt = time.Now()
	// Do NOT update m.underlyingPrice here. On testnet the per-option
	// underlying_price field reflects a stale synthetic price from when the
	// testnet contract was created, not the live index. The authoritative
	// source is the deribit_price_index channel handler above.
	snap := *inst
	m.mu.Unlock()

	tick := &Tick{
		Timestamp:       time.Now(),
		Instrument:      snap.Name,
		Underlying:      snap.Underlying,
		UnderlyingPrice: snap.UnderlyingPrice,
		Strike:          snap.Strike,
		Expiry:          snap.Expiry,
		OptionType:      snap.OptionType,
		Bid:             snap.Bid,
		Ask:             snap.Ask,
		Mid:             snap.Mid,
		Greeks:          snap.Greeks,
		DVOLIndex:       snap.DVOLIndex,
		IVPercentile:    snap.IVPercentile,
	}

	select {
	case m.tickCh <- tick:
	default:
	}
}

func (m *Manager) Ticks() <-chan *Tick {
	return m.tickCh
}

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

// AllInstruments returns a snapshot of all tracked instruments.
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

func (m *Manager) IVPercentile() float64 {
	return m.dvol.Percentile()
}

func (m *Manager) UnderlyingPrice() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.underlyingPrice
}

func isDVOLChannel(ch string) bool {
	return len(ch) > 22 && ch[:22] == "deribit_volatility_index"
}

func isIndexPriceChannel(ch string) bool {
	return len(ch) > 20 && ch[:20] == "deribit_price_index."
}

func isTickerChannel(ch string) bool {
	return len(ch) > 7 && ch[:6] == "ticker"
}
