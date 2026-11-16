package strategy

import (
	"fmt"
	"strings"
	"time"

	"optionsbot/internal/marketdata"
)

// NoExpiryDetail is the detail of a no_expiry skip (#768): the slot's DTE
// window and every expiry the exchange lists, each with its DTE, so the
// journal shows whether a weekly sits near the window or only monthlies are
// listed. Records only: the expiry rule (SelectSlotExpiry) is unchanged.
// Pure.
func NoExpiryDetail(instruments []*marketdata.Instrument, now time.Time, targetDTE, maxDeviationDays, rolloutDTE int) string {
	lo, hi := marketdata.ExpiryWindow(targetDTE, maxDeviationDays, rolloutDTE)
	window := fmt.Sprintf("window %d–%d DTE", lo, hi)
	expiries := AvailableExpiries(instruments)
	if len(expiries) == 0 {
		return window + "; the exchange lists no expiry"
	}
	listed := make([]string, 0, len(expiries))
	for _, e := range expiries {
		listed = append(listed, fmt.Sprintf("%s (%d DTE)", e.UTC().Format("2006-01-02"), marketdata.DaysToExpiry(e, now)))
	}
	return window + "; the exchange lists " + strings.Join(listed, ", ")
}
