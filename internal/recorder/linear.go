package recorder

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"optionsbot/internal/gateway"
	"optionsbot/internal/gex"
)

// rpcCaller is the slice of the gateway USDCBooks uses.
type rpcCaller interface {
	Call(ctx context.Context, method string, params any, priority int) (gateway.JSONRPCResponse, error)
}

// Fetcher is a Source that fetches its summary on demand: Run calls
// FetchSummary before each snapshot. (The GEX manager refreshes itself and
// is not one: an extra GEX refresh would count as an extra snapshot.)
type Fetcher interface {
	FetchSummary(ctx context.Context) error
}

// ExtraUSDC are the underlyings without a bot whose USDC-settled books the
// BTC bot records too, for the SOL/XRP decision (#117, #356). They have no
// DVOL index.
var ExtraUSDC = []string{"SOL", "XRP"}

// USDCDataset is the folder name under a bot's data/ of underlying's
// USDC-settled books: market_usdc for the bot's own, market_usdc_<sol|xrp>
// for ExtraUSDC.
func USDCDataset(underlying string, own bool) string {
	if own {
		return "market_usdc"
	}
	return "market_usdc_" + strings.ToLower(underlying)
}

// USDCBooks is the mainnet book summary of every USDC-settled option (e.g.
// BTC_USDC-27NOV26-96000-C, SOL_USDC-…, XRP_USDC-…, prices in USDC). One
// public call per snapshot, whatever the number of underlyings recorded from
// it (#356: SOL and XRP add no call); Book gives each underlying's rows.
// Trading never reads it.
type USDCBooks struct {
	gw rpcCaller

	mu  sync.Mutex
	all []gex.SummaryRow
	at  time.Time
}

// NewUSDCBooks reads the USDC options' book summary through gw (the mainnet
// public gateway, never the trading one).
func NewUSDCBooks(gw rpcCaller) *USDCBooks { return &USDCBooks{gw: gw} }

// FetchSummary reads the USDC options' book summary once.
func (b *USDCBooks) FetchSummary(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := b.gw.Call(ctx, "public/get_book_summary_by_currency", map[string]any{
		"currency": "USDC",
		"kind":     "option",
	}, gateway.PriorityLow)
	if err != nil {
		return err
	}
	var all []gex.SummaryRow
	if err := json.Unmarshal(resp.Result, &all); err != nil {
		return fmt.Errorf("decode USDC book summary: %w", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.all, b.at = all, time.Now()
	return nil
}

// Book is the underlying's view of the last fetch (a Source, not a Fetcher:
// only the group fetches).
func (b *USDCBooks) Book(underlying string) Source {
	return usdcView{books: b, prefix: underlying + "_USDC-"}
}

type usdcView struct {
	books  *USDCBooks
	prefix string
}

func (v usdcView) LatestSummary() ([]gex.SummaryRow, time.Time) {
	v.books.mu.Lock()
	defer v.books.mu.Unlock()
	rows := make([]gex.SummaryRow, 0, len(v.books.all))
	for _, r := range v.books.all {
		if strings.HasPrefix(r.InstrumentName, v.prefix) {
			rows = append(rows, r)
		}
	}
	return rows, v.books.at
}

// LinearBook is one underlying's USDC-settled options, fetched on its own
// (2026-10-13: tighter quotes on calm days, but under 0.5 % of the open
// interest). Several underlyings recorded together share one USDCBooks and
// one Group instead.
type LinearBook struct {
	books *USDCBooks
	view  Source
}

// NewLinearBook records underlying's options settled in USDC.
func NewLinearBook(gw rpcCaller, underlying string) *LinearBook {
	b := NewUSDCBooks(gw)
	return &LinearBook{books: b, view: b.Book(underlying)}
}

// FetchSummary reads the USDC options' book summary.
func (b *LinearBook) FetchSummary(ctx context.Context) error { return b.books.FetchSummary(ctx) }

// LatestSummary returns the underlying's rows of the last fetch and when it
// was fetched.
func (b *LinearBook) LatestSummary() ([]gex.SummaryRow, time.Time) { return b.view.LatestSummary() }

// Group runs several recorders on one fetch per snapshot: at each interval
// boundary it calls FetchSummary once, then writes every recorder's snapshot
// (#356: the BTC, SOL and XRP USDC books from one USDC summary).
type Group struct {
	fetch Fetcher
	recs  []*Recorder
	every time.Duration
}

// NewGroup returns a group snapshotting recs every interval after one fetch.
// The recorders' own sources must not be Fetchers (Tick never calls them).
func NewGroup(fetch Fetcher, every time.Duration, recs ...*Recorder) *Group {
	return &Group{fetch: fetch, recs: recs, every: every}
}

// Run snapshots at each interval boundary (plus a minute, as Recorder.Run)
// until ctx ends.
func (g *Group) Run(ctx context.Context) {
	dirs := make([]string, len(g.recs))
	for i, r := range g.recs {
		dirs[i] = r.dir
	}
	slog.Info("market recorder group started", "dirs", dirs, "every", g.every.String())
	for {
		next := time.Now().Truncate(g.every).Add(g.every + time.Minute)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
		}
		g.Tick(ctx, time.Now())
	}
}

// Tick fetches once and writes each recorder's snapshot taken at now. It
// returns how many instruments each wrote: 0 when its snapshot failed, which
// is logged and does not stop the others.
func (g *Group) Tick(ctx context.Context, now time.Time) []int {
	out := make([]int, len(g.recs))
	if err := g.fetch.FetchSummary(ctx); err != nil {
		slog.Warn("market snapshot not recorded", "recorders", len(g.recs), "err", err)
		return out
	}
	for i, r := range g.recs {
		n, err := r.Snapshot(now)
		if err != nil {
			slog.Warn("market snapshot not recorded", "dir", r.dir, "err", err)
			continue
		}
		out[i] = n
		slog.Info("market snapshot recorded", "instruments", n, "dir", r.dir)
	}
	return out
}
