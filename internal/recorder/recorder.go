// Package recorder keeps a history of the option market for backtests and
// stress tests: once an interval (an hour by default) it writes every option
// of the underlying, as Deribit MAINNET quotes it, to a gzip CSV per UTC day.
//
// The quotes come from the book summary the GEX manager already fetches each
// minute (public/get_book_summary_by_currency), so recording costs no
// Deribit call and never touches trading. Mainnet, not testnet: testnet
// mirrors mainnet prices but its order books are thin, so its bid/ask would
// misstate what fills cost. The summary has no greeks; they are computed with
// Black–Scholes from each option's mark IV and its expiry's forward.
package recorder

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/marketdata"
)

// Source is where snapshots come from: the GEX manager's last book summary
// and the bot's DVOL reading.
type Source interface {
	LatestSummary() ([]gex.SummaryRow, time.Time)
}

// Header is the column layout. It extends the backtest CSV (date … dvol_index)
// with mark and open_interest; date is the snapshot time (RFC 3339, UTC) and
// prices are in the coin, as Deribit quotes them.
var Header = []string{
	"date", "instrument", "underlying", "underlying_price", "strike", "expiry", "option_type",
	"bid", "ask", "mid", "mark", "delta", "gamma", "theta", "vega", "iv", "open_interest", "dvol_index",
}

// maxSummaryAge: a summary older than this is not recorded (the GEX poll
// stalled); the snapshot is skipped rather than written with stale prices.
const maxSummaryAge = 5 * time.Minute

// Recorder writes snapshots into dir.
type Recorder struct {
	src   Source
	dvol  func() float64
	dir   string
	every time.Duration
}

// New returns a recorder writing to dir every interval.
func New(src Source, dvol func() float64, dir string, every time.Duration) *Recorder {
	return &Recorder{src: src, dvol: dvol, dir: dir, every: every}
}

// Run snapshots at each interval boundary (e.g. the top of every hour, plus
// a minute so the GEX poll has refreshed) until ctx ends.
func (r *Recorder) Run(ctx context.Context) {
	slog.Info("market recorder started", "dir", r.dir, "every", r.every.String())
	for {
		next := time.Now().Truncate(r.every).Add(r.every + time.Minute)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
		}
		n, err := r.Snapshot(time.Now())
		if err != nil {
			slog.Warn("market snapshot not recorded", "err", err)
			continue
		}
		slog.Info("market snapshot recorded", "instruments", n, "dir", r.dir)
	}
}

// Snapshot appends the current book summary to today's file and returns how
// many instruments it wrote.
func (r *Recorder) Snapshot(now time.Time) (int, error) {
	rows, at := r.src.LatestSummary()
	if len(rows) == 0 {
		return 0, fmt.Errorf("no book summary yet")
	}
	if now.Sub(at) > maxSummaryAge {
		return 0, fmt.Errorf("book summary is %s old", now.Sub(at).Round(time.Second))
	}
	records := Rows(rows, at, r.dvol())
	if len(records) == 0 {
		return 0, fmt.Errorf("no option in the book summary could be parsed")
	}
	return len(records), appendGzipCSV(filepath.Join(r.dir, at.UTC().Format("2006-01-02")+".csv.gz"), records)
}

// Rows turns a book summary taken at `at` into CSV records (Header order).
// Expired or unparseable instruments and those without a mark IV are skipped.
func Rows(rows []gex.SummaryRow, at time.Time, dvol float64) [][]string {
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	var out [][]string
	for _, row := range rows {
		underlying, expiry, strike, optType, err := marketdata.ParseOptionName(row.InstrumentName)
		if err != nil || !expiry.After(at) || row.MarkIV <= 0 || row.UnderlyingPrice <= 0 {
			continue
		}
		g := BlackScholes(optType, row.UnderlyingPrice, strike, expiry.Sub(at).Hours()/24/365, row.MarkIV/100)
		out = append(out, []string{
			at.UTC().Format(time.RFC3339), row.InstrumentName, underlying, f(row.UnderlyingPrice), f(strike),
			expiry.Format("2006-01-02"), optType,
			f(row.BidPrice), f(row.AskPrice), f(row.MidPrice), f(row.MarkPrice),
			f(round(g.Delta, 6)), f(round(g.Gamma, 10)), f(round(g.Theta, 4)), f(round(g.Vega, 4)),
			f(row.MarkIV / 100), f(row.OpenInterest), f(dvol),
		})
	}
	return out
}

// Greeks are Black–Scholes greeks on the forward, in Deribit's conventions:
// delta per contract, gamma per 1 USD of the underlying, theta in USD per day,
// vega in USD per 1 point of IV.
type Greeks struct{ Delta, Gamma, Theta, Vega float64 }

// BlackScholes prices greeks off the expiry's forward f (zero rate: the
// forward already carries the carry), strike k, years t and volatility sigma.
func BlackScholes(optType string, f, k, t, sigma float64) Greeks {
	if t <= 0 || sigma <= 0 || f <= 0 || k <= 0 {
		return Greeks{}
	}
	sqrtT := math.Sqrt(t)
	d1 := (math.Log(f/k) + 0.5*sigma*sigma*t) / (sigma * sqrtT)
	pdf := math.Exp(-0.5*d1*d1) / math.Sqrt(2*math.Pi)
	delta := 0.5 * math.Erfc(-d1/math.Sqrt2) // N(d1)
	if optType == "put" {
		delta -= 1
	}
	return Greeks{
		Delta: delta,
		Gamma: pdf / (f * sigma * sqrtT),
		Theta: -f * pdf * sigma / (2 * sqrtT) / 365,
		Vega:  f * pdf * sqrtT / 100,
	}
}

func round(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}

// appendGzipCSV appends records to path as one more gzip member (a new file
// starts with the header). Concatenated members read back as one stream with
// any gzip reader, so each snapshot is a cheap append and a crash can lose at
// most the snapshot being written.
func appendGzipCSV(path string, records [][]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(file)
	w := csv.NewWriter(zw)
	if info.Size() == 0 {
		if err := w.Write(Header); err != nil {
			return err
		}
	}
	if err := w.WriteAll(records); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return file.Sync()
}
