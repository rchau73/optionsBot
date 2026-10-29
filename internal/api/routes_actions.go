package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// routesActions registers the two routes that change trading state, each only
// when its source and the admin token are set.
func (s *Server) routesActions(mux *http.ServeMux) {
	if s.closer != nil && s.token != "" {
		mux.HandleFunc("POST /api/positions/close", s.manualClose)
	}
	if s.rebal != nil && s.token != "" {
		mux.HandleFunc("POST /api/rebalance", s.manualRebalance)
	}
}

const (
	maxCloseIDs     = 20
	manualCloseWait = 60 * time.Second // a market buy-back is seconds; the loop may be mid-cycle
	// A pass runs a few margin simulations per strangle (Deribit allows one a
	// second) plus its orders; the loop may be mid-cycle.
	manualRebalanceWait = 120 * time.Second
)

// authorised checks the admin bearer token in constant time.
func (s *Server) authorised(r *http.Request) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

// actorName is a monitor username: 3–64 of a-z 0-9 . _ - (frontend lib/auth/store.js).
var actorName = regexp.MustCompile(`^[a-z0-9._-]{3,64}$`)

// actorRoles are the monitor's roles (PRD §3).
var actorRoles = map[string]bool{"admin": true, "manager": true, "user": true}

// ActorOf checks the person a state-changing request names (#171): nil →
// "monitor" (a request that names nobody, as before); otherwise a valid
// username and role, else an error. The monitor enforces who may act; the
// bot only records who did — its own check stays the bearer token.
func ActorOf(a *orders.Actor) (orders.Actor, error) {
	if a == nil {
		return orders.Actor{User: "monitor"}, nil
	}
	if !actorName.MatchString(a.User) || !actorRoles[a.Role] {
		return orders.Actor{}, errors.New(`actor must be {"user": 3–64 of a-z 0-9 . _ -, "role": admin|manager|user}`)
	}
	return *a, nil
}

// manualClose: {"position_ids": [...], "actor": {"user", "role"}} → the
// strategy's results. Audited in the bot log with the actor; each close is
// journaled by the strategy with it.
func (s *Server) manualClose(w http.ResponseWriter, r *http.Request) {
	if !s.authorised(r) {
		slog.Warn("manual close refused: bad token", "remote", r.RemoteAddr)
		writeError(w, http.StatusUnauthorized, "unauthorised")
		return
	}
	var body struct {
		PositionIDs []string      `json:"position_ids"`
		Actor       *orders.Actor `json:"actor"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "body must be {\"position_ids\": [...]}")
		return
	}
	if len(body.PositionIDs) == 0 || len(body.PositionIDs) > maxCloseIDs {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("between 1 and %d position ids", maxCloseIDs))
		return
	}
	actor, err := ActorOf(body.Actor)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	slog.Warn("manual close requested", "positions", body.PositionIDs, "by", actor, "remote", r.RemoteAddr)
	ctx, cancel := context.WithTimeout(r.Context(), manualCloseWait)
	defer cancel()
	res, err := s.closer.ManualClose(ctx, body.PositionIDs, actor)
	if err != nil {
		code := http.StatusConflict
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, strategy.ErrManualCloseBusy) {
			code = http.StatusGatewayTimeout
		}
		writeError(w, code, err.Error())
		return
	}
	slog.Warn("manual close done", "results", res)
	writeJSON(w, map[string]any{"results": res})
}

// manualRebalance: POST /api/rebalance[?dry_run=true] → the pass's report.
// 409 when the gates refuse it (the reason says which), 429 inside the
// cooldown, 504 when the decision loop did not take it. Audited in the bot
// log; a real pass is journaled (risk_limit rebalance_manual).
func (s *Server) manualRebalance(w http.ResponseWriter, r *http.Request) {
	if !s.authorised(r) {
		slog.Warn("manual rebalance refused: bad token", "remote", r.RemoteAddr)
		writeError(w, http.StatusUnauthorized, "unauthorised")
		return
	}
	dryRun := false
	if v := r.URL.Query().Get("dry_run"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "dry_run must be true or false")
			return
		}
		dryRun = b
	}
	// The body is optional ({"actor": {...}}); without one the actor is "monitor".
	var body struct {
		Actor *orders.Actor `json:"actor"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "body must be empty or {\"actor\": {\"user\": …, \"role\": …}}")
		return
	}
	actor, err := ActorOf(body.Actor)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	slog.Warn("manual rebalance requested via API", "dry_run", dryRun, "by", actor, "remote", r.RemoteAddr)
	ctx, cancel := context.WithTimeout(r.Context(), manualRebalanceWait)
	defer cancel()
	rep, err := s.rebal.RequestRebalance(ctx, dryRun, actor)
	var refused strategy.RebalanceRefused
	var cooldown strategy.RebalanceCooldown
	switch {
	case err == nil:
		writeJSON(w, rep)
	case errors.As(err, &cooldown):
		w.Header().Set("Retry-After", strconv.Itoa(int(cooldown.RetryAfter.Seconds()+0.999)))
		writeError(w, http.StatusTooManyRequests, err.Error())
	case errors.As(err, &refused):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, strategy.ErrRebalanceBusy):
		writeError(w, http.StatusGatewayTimeout, err.Error())
	default:
		writeError(w, http.StatusConflict, err.Error())
	}
}
