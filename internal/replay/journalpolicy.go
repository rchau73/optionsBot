package replay

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

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

// JournalBalanceAt is the margin balance of the last risk record at or
// before t that has one (0 = none).
func JournalBalanceAt(recs []orders.RiskRecord, t time.Time) float64 {
	b, at := 0.0, time.Time{}
	for _, r := range recs {
		if r.MarginBalance > 0 && !r.Timestamp.After(t) && !r.Timestamp.Before(at) {
			b, at = r.MarginBalance, r.Timestamp
		}
	}
	return b
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
