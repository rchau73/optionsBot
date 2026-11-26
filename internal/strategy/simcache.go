package strategy

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
)

// The margin simulation cache (#888). Sizing an entry or a rebalance pass
// asks Deribit's private/simulate_portfolio several times, and each vacant
// slot and each strangle of a pass asked again for the same book — on testnet
// ETH most of those were refused as too_many_requests (10028) and the slot
// stayed empty. A result is reused only while nothing it depends on changed:
//
//   - the same request (add to the book or alone, the same positions);
//   - the same book: this cycle's margin reading (IM, MM, balance, unit), the
//     IM limit, and every position the bot holds with its size — a fill
//     booked, a position adopted or a limit change is a new key;
//   - no order sent, amended or cancelled since (orderWatch drops the cache:
//     Deribit counts resting orders in the simulation);
//   - and at most SimReuse old.
//
// An error is never kept: a refused simulation is asked again, never
// replaced by a guess. The margin guard is unchanged — only a result Deribit
// gave for this very book is used.

// SimReuse is how long a simulation result may be reused (the issue's
// "about 30 s"; one decision cycle is 40 s, so a result never outlives the
// margin reading it was taken against).
const SimReuse = 30 * time.Second

// SimCache keeps recent simulation results by key. Safe for concurrent use.
type SimCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]simEntry
}

type simEntry struct {
	at  time.Time
	sum orders.AccountSummary
}

// NewSimCache returns an empty cache whose results live ttl.
func NewSimCache(ttl time.Duration) *SimCache {
	return &SimCache{ttl: ttl, entries: make(map[string]simEntry)}
}

// Get returns the result stored under key if it is younger than the ttl.
func (c *SimCache) Get(key string, now time.Time) (orders.AccountSummary, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return orders.AccountSummary{}, false
	}
	if age := now.Sub(e.at); age < 0 || age >= c.ttl {
		delete(c.entries, key)
		return orders.AccountSummary{}, false
	}
	return e.sum, true
}

// Put stores a successful result taken at now.
func (c *SimCache) Put(key string, sum orders.AccountSummary, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = simEntry{at: now, sum: sum}
}

// Drop forgets every result: the book changed.
func (c *SimCache) Drop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
}

// SimKey is the cache key of one simulation request against one book. Pure:
// positions are sorted, so the same request always gives the same key.
func SimKey(alone bool, positions map[string]float64, book string) string {
	var b strings.Builder
	if alone {
		b.WriteString("alone|")
	} else {
		b.WriteString("book|")
	}
	b.WriteString(sortedSizes(positions))
	b.WriteString("|")
	b.WriteString(book)
	return b.String()
}

// SimBookKey describes the book a simulation is taken against: the margin
// reading of the cycle, the IM limit and the bot's positions (instrument →
// size, summed). Pure.
func SimBookKey(u risk.Usage, limitIMPct float64, positions []*orders.Position) string {
	held := make(map[string]float64, len(positions))
	for _, p := range positions {
		held[p.Instrument] += p.Qty
	}
	return fmt.Sprintf("%s:%g/%g/%g|limit %g|%s", u.Unit, u.IM, u.MM, u.MarginBalance, limitIMPct, sortedSizes(held))
}

func sortedSizes(sizes map[string]float64) string {
	names := make([]string, 0, len(sizes))
	for n := range sizes {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("%s=%g", n, sizes[n])
	}
	return strings.Join(parts, ",")
}

// orderWatch is the strategy's exchange with every order action dropping the
// simulation cache: an order sent, amended or cancelled changes the margin
// Deribit simulates (resting orders count), whatever the outcome.
type orderWatch struct {
	Exchange
	sims *SimCache
}

func (w orderWatch) Submit(ctx context.Context, o orders.Order) (orders.Fill, error) {
	defer w.sims.Drop()
	return w.Exchange.Submit(ctx, o)
}

func (w orderWatch) Cancel(ctx context.Context, orderID string) error {
	defer w.sims.Drop()
	return w.Exchange.Cancel(ctx, orderID)
}

func (w orderWatch) CancelAllOrders(ctx context.Context, currency string) error {
	defer w.sims.Drop()
	return w.Exchange.CancelAllOrders(ctx, currency)
}

func (w orderWatch) CancelByLabel(ctx context.Context, currency, label string) (int, error) {
	defer w.sims.Drop()
	return w.Exchange.CancelByLabel(ctx, currency, label)
}

func (w orderWatch) AmendOrder(ctx context.Context, orderID string, qty, price float64) error {
	defer w.sims.Drop()
	return w.Exchange.AmendOrder(ctx, orderID, qty, price)
}

// cachedSimulation answers a simulation from the cache or asks Deribit,
// keeping a successful answer for SimReuse.
func (s *Strategy) cachedSimulation(ctx context.Context, alone bool, positions map[string]float64, u risk.Usage) (orders.AccountSummary, error) {
	limit := 0.0
	if s.lastRisk != nil {
		limit = s.lastRisk.LimitIMPct
	}
	key := SimKey(alone, positions, SimBookKey(u, limit, s.state.AllPositions()))
	if sum, ok := s.sims.Get(key, time.Now()); ok {
		slog.Debug("margin simulation reused: same book, nothing sent since", "alone", alone, "positions", len(positions))
		return sum, nil
	}
	var sum orders.AccountSummary
	var err error
	if alone {
		sum, err = s.exch.SimulateAlone(ctx, s.cfg.Underlying, positions)
	} else {
		sum, err = s.exch.SimulatePortfolio(ctx, s.cfg.Underlying, positions)
	}
	if err == nil {
		s.sims.Put(key, sum, time.Now())
	}
	return sum, err
}
