package gateway

import "strings"

// Key scope status words (KeyScope.Status, /api/status key_scope.status).
const (
	KeyScopeTradeOnly    = "trade_only"     // no granted scope can move funds
	KeyScopeCanMoveFunds = "can_move_funds" // a granted scope can move funds: refused by policy (#538)
	KeyScopeUnknown      = "unknown"        // the scope could not be read: never reported as trade-only
)

// fundScopes are the Deribit API key scopes that let the key move money out
// of the account, directly or by granting itself that right (#538).
//
// ASSUMPTION, to be verified by a person against Deribit's API documentation
// (https://docs.deribit.com/, "Authentication" → "Access scope", as read from
// memory on 2026-10-30; the Dev agent had no web access):
//   - wallet:read_write  — withdrawals and transfers (private/withdraw,
//     private/submit_transfer_to_user, …_to_subaccount);
//   - account:read_write — account and subaccount management, including
//     private/create_api_key and private/edit_api_key, so the key could
//     create a key that withdraws;
//   - custody:read_write — custody (third-party settlement) transfers.
//
// Read-only and "none" scopes (wallet:read, wallet:none, account:read) never
// move funds; trade:read_write moves positions, not money.
var fundScopes = map[string]bool{
	"wallet:read_write":  true,
	"account:read_write": true,
	"custody:read_write": true,
}

// KeyScope is what the API key may do, from the scope Deribit returns at
// authentication. It holds scope names only — never a client id, secret or
// token.
type KeyScope struct {
	Scopes     []string `json:"scopes"`      // every granted scope, as Deribit names it
	FundScopes []string `json:"fund_scopes"` // the granted scopes that can move funds
	// CanMoveFunds is nil when the scope could not be read: unknown, never false.
	CanMoveFunds *bool  `json:"can_move_funds"`
	Status       string `json:"status"` // trade_only | can_move_funds | unknown
}

// ParseKeyScope reads the space-separated scope string of a public/auth
// reply ("account:read trade:read_write wallet:none …"). A string with no
// "name:access" scope in it (empty, or only words such as "connection") is
// unknown: CanMoveFunds nil. Pure.
func ParseKeyScope(scope string) KeyScope {
	ks := KeyScope{Scopes: []string{}, FundScopes: []string{}, Status: KeyScopeUnknown}
	known := false
	for _, s := range strings.Fields(scope) {
		ks.Scopes = append(ks.Scopes, s)
		if strings.Contains(s, ":") {
			known = true
		}
		if fundScopes[s] {
			ks.FundScopes = append(ks.FundScopes, s)
		}
	}
	if !known {
		return ks
	}
	can := len(ks.FundScopes) > 0
	ks.CanMoveFunds = &can
	ks.Status = KeyScopeTradeOnly
	if can {
		ks.Status = KeyScopeCanMoveFunds
	}
	return ks
}

// KeyScope returns the scope of the last successful authentication; ok is
// false before the first one (and always on a PublicOnly gateway).
func (g *Gateway) KeyScope() (KeyScope, bool) {
	ks := g.keyScope.Load()
	if ks == nil {
		return KeyScope{}, false
	}
	return *ks, true
}

// noteKeyScope keeps the scope Deribit granted and reports it: an ERROR when
// the key can move funds, a WARN when the scope is unreadable. Warn only — the
// bot still starts (#538: refusing to start is a person's decision).
func (g *Gateway) noteKeyScope(scope string) {
	ks := ParseKeyScope(scope)
	g.keyScope.Store(&ks)
	switch ks.Status {
	case KeyScopeCanMoveFunds:
		g.log.Error("API key can move funds: withdrawal rights are refused by policy; replace it with a trade-only key (account:read trade:read_write)",
			"fund_scopes", ks.FundScopes, "scopes", ks.Scopes)
	case KeyScopeUnknown:
		g.log.Warn("API key scope unreadable: whether it can move funds is unknown", "scopes", ks.Scopes)
	default:
		g.log.Info("API key is trade-only", "scopes", ks.Scopes)
	}
}
