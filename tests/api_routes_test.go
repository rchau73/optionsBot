package tests

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"optionsbot/internal/api"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// TestAPI_RoutesRegisteredPerDomain (#466): api.go keeps the server and one
// registration call per domain; every route is registered in a routes_*.go
// file, so a new route never edits a shared list.
func TestAPI_RoutesRegisteredPerDomain(t *testing.T) {
	dir := filepath.Join("..", "internal", "api")
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no Go files in %s: %v", dir, err)
	}
	domains := 0
	for _, f := range files {
		name := filepath.Base(f)
		// A GOOS or GOARCH suffix would silently build the file on one platform only.
		for _, suffix := range []string{"_windows.go", "_linux.go", "_darwin.go", "_amd64.go", "_arm64.go"} {
			if strings.HasSuffix(name, suffix) {
				t.Errorf("%s: a %s file builds on one platform only; name the domain otherwise", name, suffix)
			}
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		registers := strings.Contains(string(src), ".HandleFunc(")
		isRoutes := strings.HasPrefix(name, "routes_")
		if isRoutes {
			domains++
		}
		if registers && !isRoutes {
			t.Errorf("%s registers a route; routes live in routes_<domain>.go", name)
		}
	}
	if domains < 5 {
		t.Errorf("%d routes_*.go files, want one per domain (status, book, trades, summary, journal, actions)", domains)
	}
}

type noopCloser struct{}

func (noopCloser) ManualClose(context.Context, []string, orders.Actor) ([]strategy.ManualCloseResult, error) {
	return nil, nil
}

type noopRebalancer struct{}

func (noopRebalancer) RequestRebalance(context.Context, bool, orders.Actor) (strategy.RebalanceReport, error) {
	return strategy.RebalanceReport{}, nil
}

// TestAPI_EveryRouteStillAnswers (#466): after the split by domain, every path
// answers with the method, status and auth it had before.
func TestAPI_EveryRouteStillAnswers(t *testing.T) {
	h := api.New(staticView{sampleView()}, orders.NewWriterLogger(io.Discard, 0),
		api.WithManualClose(noopCloser{}, "secret"), api.WithManualRebalance(noopRebalancer{}, "secret")).Handler()
	cases := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/api/health", http.StatusOK},
		{http.MethodGet, "/api/status", http.StatusOK},
		{http.MethodGet, "/api/account", http.StatusOK},
		{http.MethodGet, "/api/positions", http.StatusOK},
		{http.MethodGet, "/api/orders", http.StatusOK},
		{http.MethodGet, "/api/pnl", http.StatusOK},
		{http.MethodGet, "/api/strangles/payoff", http.StatusOK},
		{http.MethodGet, "/api/events", http.StatusOK},
		{http.MethodGet, "/api/trades", http.StatusOK},
		{http.MethodGet, "/api/summary", http.StatusOK},
		{http.MethodGet, "/api/performance", http.StatusOK},
		{http.MethodGet, "/api/income", http.StatusOK},
		{http.MethodGet, "/api/pnl/history", http.StatusOK},
		{http.MethodGet, "/api/explain", http.StatusBadRequest},            // needs seq or trade
		{http.MethodGet, "/api/journal", http.StatusServiceUnavailable},    // a writer-only logger has no file
		{http.MethodPost, "/api/positions/close", http.StatusUnauthorized}, // no token
		{http.MethodPost, "/api/rebalance", http.StatusUnauthorized},       // no token
		{http.MethodGet, "/api/positions/close", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/summary", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/unknown", http.StatusNotFound},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != c.want {
			t.Errorf("%s %s → %d, want %d", c.method, c.path, rec.Code, c.want)
		}
	}
}

// TestAPI_ActionRoutesOffWithoutToken (#466): without the admin token the
// state-changing routes are not registered at all, as before.
func TestAPI_ActionRoutesOffWithoutToken(t *testing.T) {
	h := api.New(staticView{sampleView()}, orders.NewWriterLogger(io.Discard, 0),
		api.WithManualClose(noopCloser{}, ""), api.WithManualRebalance(noopRebalancer{}, "")).Handler()
	for _, path := range []string{"/api/positions/close", "/api/rebalance"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s without a token → %d, want 404", path, rec.Code)
		}
	}
}
