package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	gw "optionsbot/internal/gateway"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// mockDeribitServer creates a minimal WebSocket echo server that handles
// auth, heartbeat, and subscribe requests.
func mockDeribitServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var req gw.JSONRPCRequest
			if err := json.Unmarshal(msg, &req); err != nil {
				continue
			}
			resp := gw.JSONRPCResponse{
				JsonRPC: "2.0",
				ID:      req.ID,
				Result:  map[string]interface{}{"access_token": "test-token"},
			}
			data, _ := json.Marshal(resp)
			conn.WriteMessage(websocket.TextMessage, data)
		}
	}))
	return srv
}

func TestGatewayConnect(t *testing.T) {
	srv := mockDeribitServer(t)
	defer srv.Close()

	// Rewrite ws:// URL
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// Send a test JSON-RPC request
	req := gw.JSONRPCRequest{
		JsonRPC: "2.0",
		ID:      1,
		Method:  "public/auth",
		Params:  map[string]string{"grant_type": "client_credentials"},
	}
	data, _ := json.Marshal(req)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Read response within timeout
	done := make(chan struct{})
	go func() {
		_, _, _ = conn.ReadMessage()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("response timeout")
	}
}

func TestGatewaySubscribeDeduplication(t *testing.T) {
	reg := gw.NewSubscriptionRegistry(10)

	if !reg.Add("ticker.BTC-PERPETUAL.100ms") {
		t.Fatal("first add should succeed")
	}
	if reg.Add("ticker.BTC-PERPETUAL.100ms") {
		t.Fatal("duplicate add should return false")
	}
	if reg.Count() != 1 {
		t.Fatalf("expected count 1, got %d", reg.Count())
	}
}

func TestGatewaySubscribeCapacity(t *testing.T) {
	reg := gw.NewSubscriptionRegistry(3)
	for i := 0; i < 3; i++ {
		ch := strings.Repeat("x", i+1)
		if !reg.Add(ch) {
			t.Fatalf("add %d failed unexpectedly", i)
		}
	}
	if reg.Add("overflow") {
		t.Fatal("should reject subscription over capacity")
	}
}

func TestRetryOnRateLimitError(t *testing.T) {
	// Verify that WithRetry retries on rate-limit codes and succeeds on 3rd attempt
	attempt := 0
	cfg := gw.RetryConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := gw.WithRetry(ctx, cfg, func() error {
		attempt++
		if attempt < 3 {
			return &gw.RPCError{Code: 10028, Message: "rate limited"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if attempt != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempt)
	}
}
