package strategy

import (
	"fmt"
	"time"
)

// GEXStaleAfter is how old the last GEX snapshot may be before new risk
// waits: five missed refreshes (the manager computes one every 60 s).
const GEXStaleAfter = 5 * time.Minute

// GEXWaitReason says why new risk (entries, top-ups, repairs) must wait for
// the GEX data, or "" when it need not: with GEX wired, the regime and the
// trend decide which side may be sold (GEX shed, RegimeSideBlockReason), and
// before the first snapshot both read as unknown. 2026-10-09: both bots sold
// full strangles, puts included, seconds after a restart in a confirmed
// negative regime with a bear trend — the first snapshot came a minute later.
//
// A snapshot without a time has no known age and counts as fresh.
func GEXWaitReason(gexWired bool, dec GammaDecision, now time.Time) string {
	switch {
	case !gexWired:
		return ""
	case dec.Regime == "" || dec.Regime == "UNKNOWN":
		return "no GEX snapshot yet: regime and trend unknown"
	case !dec.SnapshotAt.IsZero() && now.Sub(dec.SnapshotAt) > GEXStaleAfter:
		return fmt.Sprintf("GEX snapshot is %s old (limit %s)", now.Sub(dec.SnapshotAt).Round(time.Second), GEXStaleAfter)
	}
	return ""
}
