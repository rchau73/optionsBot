package marketdata

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SettlementHourUTC is when Deribit options expire: 08:00 UTC on the expiry date.
const SettlementHourUTC = 8 * time.Hour

// ParseOptionName splits a Deribit option name such as BTC-27JUN25-70000-C.
// expiry is the expiry date at 00:00 UTC; add SettlementHourUTC for the
// exact settlement time.
func ParseOptionName(name string) (underlying string, expiry time.Time, strike float64, optType string, err error) {
	parts := strings.Split(name, "-")
	if len(parts) != 4 {
		return "", time.Time{}, 0, "", fmt.Errorf("expected 4 parts in %q", name)
	}
	underlying = parts[0]
	expiry, err = time.Parse("2Jan06", parts[1]) // day has 1 or 2 digits: 4OCT26, 27NOV26
	if err != nil {
		return "", time.Time{}, 0, "", fmt.Errorf("parse expiry %q: %w", parts[1], err)
	}
	strike, err = strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return "", time.Time{}, 0, "", fmt.Errorf("parse strike %q: %w", parts[2], err)
	}
	switch parts[3] {
	case "C":
		optType = "call"
	case "P":
		optType = "put"
	default:
		return "", time.Time{}, 0, "", fmt.Errorf("unknown option type %q", parts[3])
	}
	return underlying, expiry, strike, optType, nil
}
