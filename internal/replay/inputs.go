package replay

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"optionsbot/internal/orders"
)

// The replay's inputs from before the recording, captured by
// scripts/stress_inputs.sh: market.json (daily closes and DVOL) and
// account.json (the starting margin balance). cmd/stress -replay and
// cmd/evidence read them the same way.

// ReadMarketJSON reads the daily closes and DVOL of market.json (both
// [[ms, value], …]).
func ReadMarketJSON(path string) (closes, dvol []orders.DailyClose, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var mk struct {
		Dvol   [][2]float64 `json:"dvol"`
		Closes [][2]float64 `json:"closes"`
	}
	if err := json.Unmarshal(data, &mk); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	day := func(ms float64) time.Time { return time.UnixMilli(int64(ms)).UTC().Truncate(24 * time.Hour) }
	for _, c := range mk.Closes {
		closes = append(closes, orders.DailyClose{Date: day(c[0]), Close: c[1]})
	}
	for _, d := range mk.Dvol {
		dvol = append(dvol, orders.DailyClose{Date: day(d[0]), Close: d[1]})
	}
	return closes, dvol, nil
}

// ReadEquity is the coin's margin balance in account.json (0 if absent or
// unreadable).
func ReadEquity(path, coin string) float64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var acct struct {
		Snapshot struct {
			Assets []struct {
				Currency      string  `json:"currency"`
				MarginBalance float64 `json:"margin_balance"`
			} `json:"assets"`
		} `json:"snapshot"`
	}
	if json.Unmarshal(data, &acct) != nil {
		return 0
	}
	for _, a := range acct.Snapshot.Assets {
		if strings.EqualFold(a.Currency, coin) {
			return a.MarginBalance
		}
	}
	return 0
}
