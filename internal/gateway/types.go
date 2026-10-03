package gateway

import (
	"encoding/json"
	"fmt"
	"time"
)

// JSONRPCRequest is the standard Deribit JSON-RPC 2.0 request envelope.
type JSONRPCRequest struct {
	JsonRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

// JSONRPCResponse is the standard Deribit JSON-RPC 2.0 response envelope.
type JSONRPCResponse struct {
	JsonRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  *Notification   `json:"params,omitempty"`
}

// RPCError is an error object returned by Deribit. Code identifies the
// failure (e.g. 10005 price_too_low, 10028 too_many_requests).
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("%s (code %d)", e.Message, e.Code)
}

// Notification is the params payload of a subscription push message.
// Data stays raw so each consumer decodes it once into its own type.
type Notification struct {
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

// AuthParams for Deribit client_credentials auth.
type AuthParams struct {
	GrantType    string `json:"grant_type"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// HeartbeatParams for setting heartbeat interval.
type HeartbeatParams struct {
	Interval int `json:"interval"`
}

// Request is an outbound API call waiting in the priority queue.
type Request struct {
	Priority int
	Payload  JSONRPCRequest
	// Deadline is when the caller stops caring. A request still queued after
	// its deadline is dropped instead of being sent late at a stale price.
	Deadline time.Time
	reply    chan callResult
}

// callResult is what the gateway hands back to a waiting Call.
type callResult struct {
	resp JSONRPCResponse
	err  error
}

const (
	PriorityHigh = 0 // stop-loss, kill switch, gamma close
	PriorityLow  = 1 // market data, Greek refresh
)
