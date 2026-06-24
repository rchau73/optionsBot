package backtest

import (
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"time"

	"optionsbot/internal/marketdata"
)

// CSVRow is one row from the historical data CSV.
type CSVRow struct {
	Date            time.Time
	Instrument      string
	Underlying      string
	UnderlyingPrice float64
	Strike          float64
	Expiry          time.Time
	OptionType      string
	Bid             float64
	Ask             float64
	Mid             float64
	Delta           float64
	Gamma           float64
	Theta           float64
	Vega            float64
	IV              float64
	DVOLIndex       float64
}

// HistoricalFeed replays CSV rows as Ticks in chronological order.
type HistoricalFeed struct {
	rows  []CSVRow
	idx   int
	dvol  *marketdata.DVOLTracker
}

func NewHistoricalFeed(csvPath string, from, to time.Time, dvolWindow int) (*HistoricalFeed, error) {
	slog.Debug("loading csv feed", "path", csvPath, "from", from.Format("2006-01-02"), "to", to.Format("2006-01-02"))
	rows, err := loadCSV(csvPath, from, to)
	if err != nil {
		return nil, err
	}
	slog.Debug("csv feed loaded", "rows", len(rows))
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Date.Equal(rows[j].Date) {
			return rows[i].Instrument < rows[j].Instrument
		}
		return rows[i].Date.Before(rows[j].Date)
	})
	return &HistoricalFeed{
		rows: rows,
		dvol: marketdata.NewDVOLTracker(dvolWindow),
	}, nil
}

func (f *HistoricalFeed) NextTick() (*marketdata.Tick, error) {
	if f.idx >= len(f.rows) {
		return nil, io.EOF
	}
	r := f.rows[f.idx]
	f.idx++

	f.dvol.Push(r.DVOLIndex)

	return &marketdata.Tick{
		Timestamp:       r.Date,
		Instrument:      r.Instrument,
		Underlying:      r.Underlying,
		UnderlyingPrice: r.UnderlyingPrice,
		Strike:          r.Strike,
		Expiry:          r.Expiry,
		OptionType:      r.OptionType,
		Bid:             r.Bid,
		Ask:             r.Ask,
		Mid:             r.Mid,
		Greeks: marketdata.Greeks{
			Delta: r.Delta,
			Gamma: r.Gamma,
			Theta: r.Theta,
			Vega:  r.Vega,
			IV:    r.IV,
		},
		DVOLIndex:    r.DVOLIndex,
		IVPercentile: f.dvol.Percentile(),
	}, nil
}

func (f *HistoricalFeed) Done() bool {
	return f.idx >= len(f.rows)
}

// loadCSV parses the historical options data CSV.
// Expected columns: date,instrument,underlying,underlying_price,strike,expiry,
//   option_type,bid,ask,mid,delta,gamma,theta,vega,iv,dvol_index
func loadCSV(path string, from, to time.Time) ([]CSVRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open csv %s: %w", path, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	idx := make(map[string]int)
	for i, h := range header {
		idx[h] = i
	}

	var rows []CSVRow
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		date, err := time.Parse("2006-01-02", get(rec, idx, "date"))
		if err != nil {
			continue
		}
		if date.Before(from) || date.After(to) {
			continue
		}

		expiry, err := time.Parse("2006-01-02", get(rec, idx, "expiry"))
		if err != nil {
			continue
		}

		rows = append(rows, CSVRow{
			Date:            date,
			Instrument:      get(rec, idx, "instrument"),
			Underlying:      get(rec, idx, "underlying"),
			UnderlyingPrice: parseF(get(rec, idx, "underlying_price")),
			Strike:          parseF(get(rec, idx, "strike")),
			Expiry:          expiry,
			OptionType:      get(rec, idx, "option_type"),
			Bid:             parseF(get(rec, idx, "bid")),
			Ask:             parseF(get(rec, idx, "ask")),
			Mid:             parseF(get(rec, idx, "mid")),
			Delta:           parseF(get(rec, idx, "delta")),
			Gamma:           parseF(get(rec, idx, "gamma")),
			Theta:           parseF(get(rec, idx, "theta")),
			Vega:            parseF(get(rec, idx, "vega")),
			IV:              parseF(get(rec, idx, "iv")),
			DVOLIndex:       parseF(get(rec, idx, "dvol_index")),
		})
	}
	return rows, nil
}

func get(rec []string, idx map[string]int, key string) string {
	i, ok := idx[key]
	if !ok || i >= len(rec) {
		return ""
	}
	return rec[i]
}

func parseF(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
