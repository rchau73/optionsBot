package keystore

import (
	"encoding/json"
	"log/slog"
)

// Redacted is what every printed or marshalled form of Credentials shows.
const Redacted = "[redacted]"

// Credentials are one exchange account's API key: the client id and secret
// (secret), plus the label and the scope Deribit granted the key (not secret).
// The fields are unexported and every printing path — %v, %+v, %#v, %s, JSON
// and slog — prints [redacted], so a log line or journal entry cannot leak the
// key by accident. Read the key only with ClientID and Secret, at the one
// place that authenticates.
type Credentials struct {
	label    string
	clientID string
	secret   string
	scope    string
}

// NewCredentials builds credentials. scope is the space-separated scope string
// of Deribit's public/auth reply for this key (gateway.ParseKeyScope reads it).
func NewCredentials(label, clientID, secret, scope string) Credentials {
	return Credentials{label: label, clientID: clientID, secret: secret, scope: scope}
}

// Label is the account's display name (not secret).
func (c Credentials) Label() string { return c.label }

// ClientID is the API key's client id (secret: never log it).
func (c Credentials) ClientID() string { return c.clientID }

// Secret is the API key's client secret (secret: never log it).
func (c Credentials) Secret() string { return c.secret }

// Scope is the scope string Deribit granted the key (not secret, but kept out
// of logs and the API like any key detail).
func (c Credentials) Scope() string { return c.scope }

// String keeps %v, %+v and %s from printing the key.
func (c Credentials) String() string { return Redacted }

// GoString keeps %#v from printing the key.
func (c Credentials) GoString() string { return Redacted }

// MarshalJSON keeps encoding/json from printing the key.
func (c Credentials) MarshalJSON() ([]byte, error) { return json.Marshal(Redacted) }

// LogValue keeps log/slog from printing the key.
func (c Credentials) LogValue() slog.Value { return slog.StringValue(Redacted) }

// sealed is the plaintext that is encrypted on disk; it exists only inside
// this package, between decryption and NewCredentials.
type sealed struct {
	Label    string `json:"label"`
	ClientID string `json:"client_id"`
	Secret   string `json:"secret"`
	Scope    string `json:"scope"`
}
