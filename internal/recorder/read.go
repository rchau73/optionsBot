package recorder

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/marketdata"
)

// Reading the history back (cmd/stress -replay): each day file is a series
// of gzip members, one per snapshot, each row one option at the snapshot
// time in the first column. ReadDir returns the snapshots in time order.

// Quote is one option of a recorded snapshot, as Deribit mainnet quoted it.
// Prices are in the coin; 0 is an empty side of the book.
type Quote struct {
	Instrument      string
	Underlying      string
	UnderlyingPrice float64   // the expiry's future
	Strike          float64   //
	Expiry          time.Time // settlement time: the expiry date at 08:00 UTC
	OptionType      string    // call | put
	Bid, Ask        float64
	Mid, Mark       float64 // Deribit's mid (0 when a side is empty) and mark
	Delta, Gamma    float64 // Black–Scholes on the mark IV (Rows)
	Theta, Vega     float64
	IV              float64 // mark IV as a fraction (0.45)
	OpenInterest    float64
}

// LiveMid is the mid the bot reads from a ticker: (bid + ask) / 2, or the
// mark when both sides are empty (marketdata's handleTicker).
func (q Quote) LiveMid() float64 {
	if q.Bid == 0 && q.Ask == 0 {
		return q.Mark
	}
	return (q.Bid + q.Ask) / 2
}

// Snapshot is the whole recorded book at one time.
type Snapshot struct {
	At     time.Time
	DVOL   float64 // the bot's DVOL reading at the snapshot (0 = none yet)
	Quotes []Quote
}

// Spot is the snapshot's best stand-in for the index (not recorded): the
// future of the nearest expiry, which converges on the index.
func (s Snapshot) Spot() float64 {
	best, spot := time.Time{}, 0.0
	for _, q := range s.Quotes {
		if q.UnderlyingPrice > 0 && (best.IsZero() || q.Expiry.Before(best)) {
			best, spot = q.Expiry, q.UnderlyingPrice
		}
	}
	return spot
}

// ByName indexes the snapshot's quotes by instrument name.
func (s Snapshot) ByName() map[string]Quote {
	out := make(map[string]Quote, len(s.Quotes))
	for _, q := range s.Quotes {
		out[q.Instrument] = q
	}
	return out
}

// Instruments is the snapshot as the option chain the bot's pure rules read
// (SelectSlotExpiry, SelectStrikePair): live mid, recorded bid/ask and delta.
func (s Snapshot) Instruments(minTrade float64) []*marketdata.Instrument {
	out := make([]*marketdata.Instrument, 0, len(s.Quotes))
	for _, q := range s.Quotes {
		out = append(out, &marketdata.Instrument{
			Name: q.Instrument, Underlying: q.Underlying, Strike: q.Strike, Expiry: q.Expiry,
			OptionType: q.OptionType, MinTradeAmount: minTrade, Bid: q.Bid, Ask: q.Ask, Mid: q.LiveMid(),
			UnderlyingPrice: q.UnderlyingPrice, DVOLIndex: s.DVOL, UpdatedAt: s.At,
			Greeks: marketdata.Greeks{Delta: q.Delta, Gamma: q.Gamma, Theta: q.Theta, Vega: q.Vega, IV: q.IV},
		})
	}
	return out
}

// SummaryRows is the snapshot as the book summary gex.Build reads, so the
// regime and flip are recomputed with the bot's own rule.
func (s Snapshot) SummaryRows() []gex.SummaryRow {
	out := make([]gex.SummaryRow, 0, len(s.Quotes))
	for _, q := range s.Quotes {
		out = append(out, gex.SummaryRow{
			InstrumentName: q.Instrument, OpenInterest: q.OpenInterest, UnderlyingPrice: q.UnderlyingPrice,
			MarkIV: q.IV * 100, BidPrice: q.Bid, AskPrice: q.Ask, MidPrice: q.Mid, MarkPrice: q.Mark,
		})
	}
	return out
}

// ReadDir reads every day file in dir from the UTC day of from through the
// day of to (both included; a zero bound is open) and returns the snapshots
// in time order. Days without a file are skipped; a damaged day gives its
// complete members (ReadFile).
func ReadDir(dir string, from, to time.Time) ([]Snapshot, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.csv.gz"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths) // YYYY-MM-DD names sort by day
	var out []Snapshot
	for _, p := range paths {
		day, err := time.Parse("2006-01-02", filepath.Base(p)[:min(10, len(filepath.Base(p)))])
		if err != nil {
			continue // not a day file
		}
		if (!from.IsZero() && day.Before(from.UTC().Truncate(24*time.Hour))) || (!to.IsZero() && day.After(to.UTC())) {
			continue
		}
		snaps, err := ReadFile(p)
		if err != nil {
			return nil, err
		}
		out = append(out, snaps...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

// ReadFile reads one day file, one gzip member (one snapshot) at a time. A
// damaged member is dropped whole and the members after it are read, with a
// warning (readMembers); a bad row in a complete member is an error.
func ReadFile(path string) ([]Snapshot, error) {
	var out []Snapshot
	cols := columns(Header) // the current layout, in case the first member took the file's header with it
	err := readMembers(path, func(plain []byte) (first, last time.Time, err error) {
		recs, err := csv.NewReader(bytes.NewReader(plain)).ReadAll() // FieldsPerRecord is set by the first row of each member
		if err != nil {
			return first, last, err
		}
		for _, rec := range recs {
			if rec[0] == Header[0] { // the header (written once per file)
				cols = columns(rec)
				continue
			}
			at, q, dvol, err := parseRow(rec, cols)
			if err != nil {
				return first, last, err
			}
			if n := len(out); n == 0 || !out[n-1].At.Equal(at) {
				out = append(out, Snapshot{At: at, DVOL: dvol})
			}
			out[len(out)-1].Quotes = append(out[len(out)-1].Quotes, q)
			if first.IsZero() {
				first = at
			}
			last = at
		}
		return first, last, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func columns(header []string) map[string]int {
	cols := make(map[string]int, len(header))
	for i, name := range header {
		cols[name] = i
	}
	return cols
}

func parseRow(rec []string, cols map[string]int) (time.Time, Quote, float64, error) {
	get := func(name string) string {
		if i, ok := cols[name]; ok && i < len(rec) {
			return rec[i]
		}
		return ""
	}
	num := func(name string) float64 {
		v, _ := strconv.ParseFloat(get(name), 64) // empty or missing column → 0
		return v
	}
	at, err := time.Parse(time.RFC3339, get("date"))
	if err != nil {
		return time.Time{}, Quote{}, 0, fmt.Errorf("date %q: %w", get("date"), err)
	}
	name := get("instrument")
	und, expiry, strike, typ, err := marketdata.ParseOptionName(name)
	if err != nil {
		return time.Time{}, Quote{}, 0, err
	}
	q := Quote{
		Instrument: name, Underlying: und, UnderlyingPrice: num("underlying_price"), Strike: strike,
		Expiry: expiry.Add(marketdata.SettlementHourUTC), OptionType: typ,
		Bid: num("bid"), Ask: num("ask"), Mid: num("mid"), Mark: num("mark"),
		Delta: num("delta"), Gamma: num("gamma"), Theta: num("theta"), Vega: num("vega"),
		IV: num("iv"), OpenInterest: num("open_interest"),
	}
	return at.UTC(), q, num("dvol_index"), nil
}
