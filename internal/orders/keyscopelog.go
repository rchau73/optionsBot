package orders

import (
	"log/slog"
	"time"

	"optionsbot/internal/gateway"
)

// KeyScopeRecord is the startup line that records what the bot's API key may
// do (#538): the scope names Deribit granted and whether any can move funds
// (null = unknown). Scope names only — never a client id, secret or token.
type KeyScopeRecord struct {
	Timestamp  time.Time `json:"timestamp"`
	Event      string    `json:"event"` // always "key_scope"
	StrategyID string    `json:"strategy_id,omitempty"`
	gateway.KeyScope
}

// LogKeyScope journals this start's key_scope line.
func (l *Logger) LogKeyScope(strategyID string, ks gateway.KeyScope, now time.Time) {
	r := KeyScopeRecord{Timestamp: now, Event: EventKeyScope, StrategyID: strategyID, KeyScope: ks}
	l.writeJSON(EventKeyScope, r, slog.Default().With("event", EventKeyScope, "status", ks.Status))
}
