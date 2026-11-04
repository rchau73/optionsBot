package replay

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"optionsbot/internal/history"
	"optionsbot/internal/orders"
)

// The bot's own margin policy over time, from its journal's risk_limit lines
// (#638): the replay rebuilds the IM limit from the recorded DVOL and its own
// regime closes, which need not match what the bot held — 2026-10-24 stable
// BTC held 20 % (frozen on and off by a flapping regime) where the replay
// rebuilt 35 %. To check the rebalance model against the bot's downsizes,
// -limits journal takes the limit, the freeze and the restarts from the
// journal instead.

// LimitAt is the margin policy the journal shows from At on.
type LimitAt struct {
	At       time.Time
	LimitPct float64
	Frozen   bool
}

// JournalPolicy is the policy and the restarts the journal's risk records
// show, oldest first. Every record carries the status in force; a restart is
// a "limit_changed" record whose detail starts with "startup:" (the first
// line of each start, noteRiskChange).
func JournalPolicy(recs []orders.RiskRecord) (limits []LimitAt, restarts []time.Time) {
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Timestamp.Before(recs[j].Timestamp) })
	for _, r := range recs {
		if r.LimitIMPct <= 0 {
			continue
		}
		limits = append(limits, LimitAt{At: r.Timestamp, LimitPct: r.LimitIMPct, Frozen: r.Frozen})
		if r.Change == orders.RiskLimitChanged && strings.HasPrefix(r.Detail, "startup:") {
			restarts = append(restarts, r.Timestamp)
		}
	}
	return limits, restarts
}

// BalanceAt is a margin balance the bot recorded, in the coin, and where.
type BalanceAt struct {
	Coin   float64
	At     time.Time
	Source string // "risk_limit" (orders.log) | "pnl_history" (data/pnl_history.jsonl)
}

// usdMatchWindow is how far a USD risk record may be from the P&L point whose
// spot converts it to the coin.
const usdMatchWindow = time.Hour

// BalanceSamples are the margin balances the bot recorded, in the coin,
// oldest first (#638): every risk_limit line and P&L point that carries one.
// A balance in USD (cross collateral) is converted at the spot of its P&L
// point, or of the nearest one within an hour for a risk line; one that
// cannot be converted is left out. A risk line written before the first
// margin read (a start's first line) has no balance and is left out too.
func BalanceSamples(recs []orders.RiskRecord, points []history.Point, coin string) []BalanceAt {
	var out []BalanceAt
	for _, p := range points {
		if p.MarginBalance == nil || *p.MarginBalance <= 0 {
			continue
		}
		if b := toCoin(*p.MarginBalance, p.Unit, coin, p.Spot); b > 0 {
			out = append(out, BalanceAt{Coin: b, At: p.T, Source: "pnl_history"})
		}
	}
	for _, r := range recs {
		if r.MarginBalance <= 0 {
			continue
		}
		if b := toCoin(r.MarginBalance, r.Unit, coin, spotNear(points, r.Timestamp)); b > 0 {
			out = append(out, BalanceAt{Coin: b, At: r.Timestamp, Source: "risk_limit"})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// toCoin is a balance in the coin: as recorded when its unit is the coin (or
// unset), divided by spot when in USD; 0 when it cannot be converted.
func toCoin(balance float64, unit, coin string, spot float64) float64 {
	switch {
	case unit == "" || strings.EqualFold(unit, coin):
		return balance
	case strings.EqualFold(unit, "USD") && spot > 0:
		return balance / spot
	}
	return 0
}

// spotNear is the spot of the P&L point nearest t within usdMatchWindow (0 = none).
func spotNear(points []history.Point, t time.Time) float64 {
	spot, best := 0.0, usdMatchWindow+1
	for _, p := range points {
		d := p.T.Sub(t)
		if d < 0 {
			d = -d
		}
		if p.Spot > 0 && d <= usdMatchWindow && d < best {
			spot, best = p.Spot, d
		}
	}
	return spot
}

// JournalBalanceAt is the balance in force at t: the last sample at or
// before it, else the first after it (the bot's records may start after
// -from); ok false when there is no sample at all.
func JournalBalanceAt(samples []BalanceAt, t time.Time) (BalanceAt, bool) {
	if len(samples) == 0 {
		return BalanceAt{}, false
	}
	i := sort.Search(len(samples), func(i int) bool { return samples[i].At.After(t) })
	if i == 0 {
		return samples[0], true
	}
	return samples[i-1], true
}

// ReadPnLHistory reads the P&L points of data/pnl_history.jsonl; a missing
// file is no points, a torn line is skipped (as history.Store does).
func ReadPnLHistory(path string) ([]history.Point, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []history.Point
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		var p history.Point
		if json.Unmarshal(sc.Bytes(), &p) == nil && !p.T.IsZero() {
			out = append(out, p)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

// ReadRiskRecords reads the risk_limit lines of a journal (orders.log).
func ReadRiskRecords(path string) ([]orders.RiskRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []orders.RiskRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	marker := []byte(`"event":"` + orders.EventRisk + `"`)
	for sc.Scan() {
		if !bytes.Contains(sc.Bytes(), marker) {
			continue
		}
		var r orders.RiskRecord
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			out = append(out, r)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

// journalLimit is the journal's policy in force at `at` (ok false before its
// first record).
func journalLimit(limits []LimitAt, at time.Time) (LimitAt, bool) {
	i := sort.Search(len(limits), func(i int) bool { return limits[i].At.After(at) })
	if i == 0 {
		return LimitAt{}, false
	}
	return limits[i-1], true
}
