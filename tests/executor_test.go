package tests

import (
	"math"
	"testing"

	"optionsbot/internal/orders"
)

// ── ParsePriceTooLow tests ────────────────────────────────────────────────────
//
// Deribit's price_too_low error (code 10005) embeds the exchange minimum price
// in the message. The executor parses this to retry at the correct price.
// These tests guard against message format changes and edge cases.

func TestParsePriceTooLow_StandardMessage(t *testing.T) {
	// The exact format seen in production logs.
	price := orders.ParsePriceTooLow("price_too_low 0.0046")
	if math.Abs(price-0.0046) > 1e-9 {
		t.Errorf("expected 0.0046, got %v", price)
	}
}

func TestParsePriceTooLow_MessageWithSuffix(t *testing.T) {
	// The RPCError.Error() appends "(code N)" — ParsePriceTooLow should still work
	// whether called on Message alone or the full Error() string.
	price := orders.ParsePriceTooLow("price_too_low 0.005 (code 10005)")
	if math.Abs(price-0.005) > 1e-9 {
		t.Errorf("expected 0.005, got %v", price)
	}
}

func TestParsePriceTooLow_LargerMinimum(t *testing.T) {
	price := orders.ParsePriceTooLow("price_too_low 0.0550")
	if math.Abs(price-0.055) > 1e-9 {
		t.Errorf("expected 0.055, got %v", price)
	}
}

func TestParsePriceTooLow_UnrelatedError(t *testing.T) {
	// Non-price_too_low errors must return 0 so the retry is not triggered.
	for _, msg := range []string{
		"Invalid params (code -32602)",
		"not_enough_funds",
		"",
		"order_not_found",
	} {
		if price := orders.ParsePriceTooLow(msg); price != 0 {
			t.Errorf("unrelated error %q: expected 0, got %v", msg, price)
		}
	}
}

func TestParsePriceTooLow_MalformedNumber(t *testing.T) {
	// If the number can't be parsed, return 0 safely (no retry attempted).
	price := orders.ParsePriceTooLow("price_too_low NaN")
	if price != 0 {
		t.Errorf("malformed number: expected 0, got %v", price)
	}
}

// ── Retry price calculation tests ─────────────────────────────────────────────
//
// After parsing the minimum price, the executor rounds it UP to the next valid
// tick before retrying. This ensures the retry price satisfies both the tick
// constraint and the exchange minimum.

// retryPrice mirrors the adjustment in executor.Submit:
// ceil(minPrice / tick) * tick, then eliminate float residue.
func retryPrice(minPrice, tick float64) float64 {
	n := math.Ceil(minPrice / tick)
	return math.Round(n*tick*1e8) / 1e8
}

func TestRetryPrice_AlreadyOnTickBoundary(t *testing.T) {
	// 0.0046 is not a multiple of 0.0005, so ceil rounds up to 0.005.
	got := retryPrice(0.0046, 0.0005)
	if math.Abs(got-0.005) > 1e-9 {
		t.Errorf("expected 0.005 (next tick above 0.0046 at 0.0005 step), got %v", got)
	}
}

func TestRetryPrice_ExactMultipleOfTick(t *testing.T) {
	// 0.005 is already a multiple of 0.0005 → no change.
	got := retryPrice(0.005, 0.0005)
	if math.Abs(got-0.005) > 1e-9 {
		t.Errorf("expected 0.005 (exact multiple), got %v", got)
	}
}

func TestRetryPrice_SmallTickSize(t *testing.T) {
	// 0.0046 with tick 0.0001 → next valid price is 0.0046 itself.
	got := retryPrice(0.0046, 0.0001)
	if math.Abs(got-0.0046) > 1e-9 {
		t.Errorf("expected 0.0046, got %v", got)
	}
}

func TestRetryPrice_AlwaysAboveMinimum(t *testing.T) {
	// The adjusted price must always be ≥ the exchange minimum.
	cases := [][2]float64{
		{0.0046, 0.0005},
		{0.003, 0.0005},
		{0.0023, 0.0001},
		{0.0551, 0.005},
	}
	for _, c := range cases {
		minPrice, tick := c[0], c[1]
		got := retryPrice(minPrice, tick)
		if got < minPrice-1e-9 {
			t.Errorf("retryPrice(%.4f, %.4f)=%.6f is below minimum", minPrice, tick, got)
		}
	}
}
