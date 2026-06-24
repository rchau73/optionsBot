package gateway

import (
	"fmt"
	"time"
)

// JSONRPCRequest is the standard Deribit JSON-RPC 2.0 request envelope.
type JSONRPCRequest struct {
	JsonRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

// JSONRPCResponse is the standard Deribit JSON-RPC 2.0 response envelope.
type JSONRPCResponse struct {
	JsonRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  interface{}     `json:"result"`
	Error   *RPCError       `json:"error,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  *Notification   `json:"params,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("%s (code %d)", e.Message, e.Code)
}

type Notification struct {
	Channel string      `json:"channel"`
	Data    interface{} `json:"data"`
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

// Request represents an outbound API call with priority and response channel.
type Request struct {
	Priority int
	Payload  JSONRPCRequest
	RespCh   chan<- JSONRPCResponse
	Deadline time.Time
}

const (
	PriorityHigh = 0 // stop-loss, kill switch, gamma close
	PriorityLow  = 1 // market data, Greek refresh
)
