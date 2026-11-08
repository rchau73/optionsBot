package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/exchange/transport"
	"optionsbot/internal/gateway"
)

// Manager keeps the latest option chain snapshot, index price and DVOL for
// one underlying, fed by Deribit subscription pushes. Readers get copies.
type Manager struct {
	cfg             *config.Config
	gw              transport.Streamer
	chain           ChainSource
	dvol            *DVOLTracker
	mu              sync.RWMutex
	instruments     map[string]*Instrument
	underlyingPrice float64

	subMu      sync.Mutex      // serialises Track calls
	subscribed map[string]bool // instruments with a ticker subscription
}

// New builds a manager fed by gw's pushes; chain names their channels and
// reads the chain and the volatility history.
func New(cfg *config.Config, gw transport.Streamer, chain ChainSource) *Manager {
	return &Manager{
		cfg:         cfg,
		gw:          gw,
		chain:       chain,
		dvol:        NewDVOLTracker(cfg.IVPercentileWindow),
		instruments: make(map[string]*Instrument),
		subscribed:  make(map[string]bool),
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
	m.subMu.Lock()
	for _, ch := range channels {
		if name, ok := m.chain.TickerInstrument(ch); ok {
			m.subscribed[name] = true
		}
	}
	m.subMu.Unlock()
	return nil
}

// Track subscribes to the tickers of instruments that have none yet, e.g.
// a position loaded from the exchange in an expiry the strategy would not
// open today. Without a ticker such a position would have no live mark at
// all. Already-tracked and unknown names are skipped; it is cheap to call
// every cycle.
func (m *Manager) Track(ctx context.Context, instruments []string) error {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	var channels, names []string
	for _, name := range instruments {
		if m.subscribed[name] {
			continue
		}
		if _, known := m.GetInstrument(name); !known {
			continue
		}
		names = append(names, name)
		channels = append(channels, m.chain.TickerChannel(name))
	}
	if len(channels) == 0 {
		return nil
	}
	if err := m.gw.Subscribe(ctx, channels); err != nil {
		return fmt.Errorf("track %v: %w", names, err)
	}
	for _, name := range names {
		m.subscribed[name] = true
	}
	slog.Info("subscribed to tickers of held instruments", "instruments", names)
	return nil
}

// subscriptionChannels lists the index and DVOL channels plus one ticker per
// instrument of a relevant expiry. Subscribing to the whole chain (1,000+
// instruments) hits testnet subscription limits and starves the expiries
// the strategy actually needs.
func (m *Manager) subscriptionChannels(now time.Time) []string {
	channels := []string{
		m.chain.VolIndexChannel(m.cfg.Underlying),
		// The index price is reliable even when options have no quotes (testnet).
		m.chain.IndexChannel(m.cfg.Underlying),
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
		channels = append(channels, m.chain.TickerChannel(name))
	}
	return channels
}

func (m *Manager) fetchOptionsChain(ctx context.Context) error {
	instruments, err := m.chain.Instruments(ctx, m.cfg.Underlying)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, inst := range instruments {
		m.instruments[inst.Name] = inst
	}
	slog.Info("options chain loaded", "underlying", m.cfg.Underlying, "instruments", len(instruments))
	return nil
}

// seedDVOLHistory loads one daily DVOL close per day for the percentile
// window, so the IV percentile is meaningful from the first minute instead
// of after a year of uptime.
func (m *Manager) seedDVOLHistory(ctx context.Context, now time.Time) error {
	if m.cfg.IVPercentileWindow <= 0 {
		return nil
	}
	days := m.cfg.IVPercentileWindow + DVOLHistoryExtraDays
	closes, err := m.chain.VolHistory(ctx, m.cfg.Underlying, now.AddDate(0, 0, -days-1), now)
	if err != nil {
		return err
	}
	for _, c := range closes {
		m.dvol.Record(c.At, c.Close)
	}
	slog.Info("DVOL history seeded", "days", len(closes), "iv_percentile", m.dvol.Percentile())
	return nil
}

// selectRelevantExpiries returns the expiries the strategy may trade: for
// each target DTE every expiry PickExpiry could choose — its window and the
// stretch beyond it used when another slot holds its own expiry — plus the
// next three expiries beyond the largest target for rollouts.
func (m *Manager) selectRelevantExpiries(now time.Time) map[time.Time]bool {
	all := m.AllInstruments()
	result := make(map[time.Time]bool)

	maxTarget := 0
	for _, slot := range m.cfg.Slots() {
		lo, _ := ExpiryWindow(slot.TargetDTE, m.cfg.MaxDTEDeviation, m.cfg.RolloutDTE)
		hi := StretchHi(slot.TargetDTE, m.cfg.MaxDTEDeviation, m.cfg.ExpiryStretch)
		for _, inst := range all {
			if d := DaysToExpiry(inst.Expiry, now); d >= lo && d <= hi {
				result[inst.Expiry] = true
			}
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

	switch m.chain.ChannelKind(ch) {
	case ChannelVolIndex:
		var d struct {
			Volatility float64 `json:"volatility"`
		}
		if err := json.Unmarshal(data, &d); err == nil && d.Volatility > 0 {
			m.dvol.Record(now, d.Volatility)
		}

	case ChannelIndex:
		var d struct {
			Price float64 `json:"price"`
		}
		if err := json.Unmarshal(data, &d); err == nil && d.Price > 0 {
			m.mu.Lock()
			m.underlyingPrice = d.Price
			m.mu.Unlock()
		}

	case ChannelTicker:
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

// DVOLDaily returns recent daily DVOL closes and today, each with its percentile.
func (m *Manager) DVOLDaily() ([]DayIV, DayIV) {
	return m.dvol.Daily()
}

// UnderlyingPrice returns the latest index price, or 0 before the first push.
func (m *Manager) UnderlyingPrice() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.underlyingPrice
}
