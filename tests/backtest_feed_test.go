package tests

import (
	"encoding/csv"
	"os"
	"strconv"
	"testing"
	"time"

	"optionsbot/internal/backtest"
)

func writeTempCSV(t *testing.T, rows [][]string) string {
	t.Helper()
	f, err := os.CreateTemp("", "bt_feed_*.csv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })

	w := csv.NewWriter(f)
	header := []string{"date", "instrument", "underlying", "underlying_price",
		"strike", "expiry", "option_type", "bid", "ask", "mid",
		"delta", "gamma", "theta", "vega", "iv", "dvol_index"}
	_ = w.Write(header)
	for _, row := range rows {
		_ = w.Write(row)
	}
	w.Flush()
	f.Close()
	return f.Name()
}

func TestHistoricalFeed_LoadsAndFilters(t *testing.T) {
	rows := [][]string{
		{"2023-01-01", "BTC-27JAN23-20000-C", "BTC", "20000", "20000",
			"2023-01-27", "call", "100", "110", "105", "0.20", "0.001", "-5", "50", "0.8", "70"},
		{"2023-01-02", "BTC-27JAN23-20000-C", "BTC", "20100", "20000",
			"2023-01-27", "call", "110", "120", "115", "0.22", "0.001", "-5", "52", "0.8", "71"},
		// Out of range — should be filtered
		{"2022-12-31", "BTC-27JAN23-20000-C", "BTC", "19900", "20000",
			"2023-01-27", "call", "90", "100", "95", "0.18", "0.001", "-5", "48", "0.8", "68"},
	}
	path := writeTempCSV(t, rows)

	from := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	feed, err := backtest.NewHistoricalFeed(path, from, to, 10)
	if err != nil {
		t.Fatalf("NewHistoricalFeed: %v", err)
	}

	count := 0
	for !feed.Done() {
		tick, err := feed.NextTick()
		if err != nil {
			break
		}
		if tick == nil {
			break
		}
		count++
	}
	if count != 2 {
		t.Errorf("expected 2 ticks (filtered to range), got %d", count)
	}
}

func TestHistoricalFeed_DVOLPercentile(t *testing.T) {
	rows := make([][]string, 0)
	for i := 0; i < 10; i++ {
		date := time.Date(2023, 1, i+1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		dvol := float64(50 + i*5) // rising from 50 to 95
		rows = append(rows, []string{
			date, "BTC-X-C", "BTC", "20000", "20000",
			"2023-03-31", "call", "100", "110", "105",
			"0.16", "0.001", "-5", "50", "0.8",
			formatFloat(dvol),
		})
	}
	path := writeTempCSV(t, rows)
	from := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2023, 1, 31, 0, 0, 0, 0, time.UTC)
	feed, _ := backtest.NewHistoricalFeed(path, from, to, 252)

	var lastPct float64
	for !feed.Done() {
		tick, _ := feed.NextTick()
		if tick == nil {
			break
		}
		lastPct = tick.IVPercentile
	}
	// After rising DVOL, the last percentile should be high (>= 50%)
	if lastPct < 50 {
		t.Errorf("expected high IV percentile for rising DVOL, got %.1f", lastPct)
	}
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', 4, 64)
}
