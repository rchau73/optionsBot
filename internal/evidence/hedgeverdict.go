package evidence

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
)

// The futures hedge trial's verdict (#780): the rule of docs/hedge-trial.md
// applied to the section's own figures, never a new calculation. A verdict
// only proposes: nothing here changes the hedge mode, a setting or a config.
// Every threshold below is an assumption until a person confirms it (the
// brief lists each one).

// HedgeTrialRule is the document the verdict follows.
const HedgeTrialRule = "docs/hedge-trial.md"

// The verdict's outcomes.
const (
	HedgeVerdictMoreDays = "needs more days"
	HedgeVerdictKeep     = "keep running"
	HedgeVerdictStop     = "stop"
	HedgeVerdictPropose  = "propose as a core-policy change"
)

// HedgeProposeMinDays is how many days a clear advantage needs before the
// verdict proposes it: the sample floor of #772, under which a figure is "too
// few to conclude".
const HedgeProposeMinDays = SampleMinDays

// HedgeWorstDayShareMax is the tail guard: the worst day of shed decisions may
// cost at most this share of the advantage.
const HedgeWorstDayShareMax = 0.5

// HedgeUnavailablePerDayMax is how many hedge_unavailable episodes a day, on
// average, the trial tolerates before it stops: past it the hedge cannot be
// relied on to hold the delta.
const HedgeUnavailablePerDayMax = 1.0

// HedgeEvents are the journal's hedge lines since the ledger began.
type HedgeEvents struct {
	Unavailable int `json:"unavailable"`  // hedge_unavailable episodes: the shed was back
	RateLimited int `json:"rate_limited"` // hedge_rate_limited: a due hedge waited for the hourly cap
}

// HedgeVerdict is the rule's outcome and why, in plain words.
type HedgeVerdict struct {
	Outcome string `json:"outcome"`
	Why     string `json:"why"`
	Rule    string `json:"rule"`
}

// hedgeVerdict applies the rule to a measured testnet section (amounts
// shown). The checks run in the brief's order: stop, then propose, then keep
// running.
func hedgeVerdict(t HedgeTrial) *HedgeVerdict {
	v := func(outcome, why string, a ...any) *HedgeVerdict {
		return &HedgeVerdict{Outcome: outcome, Why: fmt.Sprintf(why, a...), Rule: HedgeTrialRule}
	}
	days, adv, fees := *t.Days, *t.Advantage, *t.Fees
	switch {
	case adv < -fees:
		return v(HedgeVerdictStop, "the hedge did worse than the shed by more than its fees")
	case t.Unavailable != nil && float64(*t.Unavailable) > HedgeUnavailablePerDayMax*float64(days):
		return v(HedgeVerdictStop, "the hedge was unavailable more than %.0f time(s) a day on average", HedgeUnavailablePerDayMax)
	case adv <= fees:
		return v(HedgeVerdictKeep, "the advantage is within the fees paid: no difference yet")
	case t.WorstDay != nil && t.WorstDay.Value < -HedgeWorstDayShareMax*adv:
		return v(HedgeVerdictKeep, "mixed: the hedge beat the shed, but its worst day cost more than %.0f %% of the advantage", 100*HedgeWorstDayShareMax)
	case t.Unavailable == nil || t.RateLimited == nil:
		return v(HedgeVerdictKeep, "the hedge beat the shed, but the journal's hedge events were not read")
	case *t.RateLimited > 0:
		return v(HedgeVerdictKeep, "the hedge beat the shed, but %d hedge(s) waited for the %d orders/h cap", *t.RateLimited, config.FixedFuturesHedgeMaxPerHour)
	case days < HedgeProposeMinDays:
		return v(HedgeVerdictKeep, "the hedge beat the shed, but %d of the %d days a proposal needs: too few to conclude", days, HedgeProposeMinDays)
	}
	return v(HedgeVerdictPropose, "the hedge beat the shed net of fees with a worst day inside the guard, no wait for the cap and few unavailable episodes")
}

// hedgeTooFewVerdict names the days missing.
func hedgeTooFewVerdict(missing int) *HedgeVerdict {
	return &HedgeVerdict{Outcome: HedgeVerdictMoreDays, Rule: HedgeTrialRule,
		Why: fmt.Sprintf("%d day(s) missing of the %d the trial needs", missing, HedgeTrialMinDays)}
}

// ReadHedgeEvents counts the journal's hedge_unavailable and
// hedge_rate_limited lines at or after since. Lines it cannot parse are
// skipped: they are no hedge event.
func ReadHedgeEvents(path string, since time.Time) (HedgeEvents, error) {
	var ev HedgeEvents
	f, err := os.Open(path)
	if err != nil {
		return ev, fmt.Errorf("open journal %s: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var l struct {
			Timestamp time.Time `json:"timestamp"`
			Event     string    `json:"event"`
			Change    string    `json:"change"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Event != orders.EventHedge || l.Timestamp.Before(since) {
			continue
		}
		switch l.Change {
		case orders.HedgeUnavailable:
			ev.Unavailable++
		case orders.HedgeRateLimited:
			ev.RateLimited++
		}
	}
	if err := sc.Err(); err != nil {
		return ev, fmt.Errorf("read journal %s: %w", path, err)
	}
	return ev, nil
}
