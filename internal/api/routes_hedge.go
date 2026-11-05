package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// The futures hedge experiment (#657): GET /api/hedge (state, benchmark and,
// with ?preview=true, what switching off would do) and POST /api/hedge
// {"enabled": bool, "actor": {...}} with the admin token, testnet only.

// HedgeController is the strategy's side of the experiment.
type HedgeController interface {
	HedgeState() strategy.HedgeView
	HedgePreview(ctx context.Context) (strategy.HedgePreview, error)
	SetFuturesHedge(ctx context.Context, enable bool, by orders.Actor) (strategy.HedgeSwitchResult, error)
}

// WithFuturesHedge serves the experiment. live refuses the switch (the
// experiment is testnet-only); the state stays readable.
func WithFuturesHedge(h HedgeController, token string, live bool) Option {
	return func(s *Server) { s.hedge, s.hedgeLive, s.token = h, live, token }
}

const (
	hedgeSwitchWait = 60 * time.Second // a market close of the perpetual is seconds; the loop may be mid-cycle
	// hedgePreviewEvery bounds the preview's exchange reads (a position read,
	// an account summary and one margin simulation) to one per this period.
	hedgePreviewEvery = 10 * time.Second
)

type previewCache struct {
	mu  sync.Mutex
	at  time.Time
	out strategy.HedgePreview
	err error
}

func (s *Server) routesHedge(mux *http.ServeMux) {
	if s.hedge == nil {
		return
	}
	mux.HandleFunc("GET /api/hedge", s.hedgeState)
	if s.token != "" {
		mux.HandleFunc("POST /api/hedge", s.hedgeSwitch)
	}
}

func (s *Server) hedgeState(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"hedge": s.hedge.HedgeState(), "switchable": s.token != "" && !s.hedgeLive}
	if s.hedgeLive {
		out["switch_off_reason"] = "the futures hedge experiment is testnet-only"
	}
	if v := r.URL.Query().Get("preview"); v != "" {
		want, err := strconv.ParseBool(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "preview must be true or false")
			return
		}
		if want {
			p, err := s.cachedPreview(r.Context())
			if err != nil {
				out["preview_error"] = err.Error()
			} else {
				out["preview"] = p
			}
		}
	}
	writeJSON(w, out)
}

func (s *Server) cachedPreview(ctx context.Context) (strategy.HedgePreview, error) {
	c := &s.hedgePreview
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < hedgePreviewEvery {
		return c.out, c.err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c.out, c.err = s.hedge.HedgePreview(ctx)
	c.at = time.Now()
	return c.out, c.err
}

// hedgeSwitch: {"enabled": bool, "actor": {...}} → the switch's result.
// 403 on a live bot, 409 when the strategy refuses (e.g. the perpetual could
// not be closed: the experiment stays on), 504 when the loop did not take it.
func (s *Server) hedgeSwitch(w http.ResponseWriter, r *http.Request) {
	if !s.authorised(r) {
		slog.Warn("futures hedge switch refused: bad token", "remote", r.RemoteAddr)
		writeError(w, http.StatusUnauthorized, "unauthorised")
		return
	}
	if s.hedgeLive {
		writeError(w, http.StatusForbidden, "the futures hedge experiment is testnet-only")
		return
	}
	var body struct {
		Enabled *bool         `json:"enabled"`
		Actor   *orders.Actor `json:"actor"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || body.Enabled == nil {
		writeError(w, http.StatusBadRequest, `body must be {"enabled": true|false, "actor": {"user": …, "role": …}}`)
		return
	}
	actor, err := ActorOf(body.Actor)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	slog.Warn("futures hedge switch requested", "enabled", *body.Enabled, "by", actor, "remote", r.RemoteAddr)
	ctx, cancel := context.WithTimeout(r.Context(), hedgeSwitchWait)
	defer cancel()
	res, err := s.hedge.SetFuturesHedge(ctx, *body.Enabled, actor)
	switch {
	case err == nil:
		s.hedgePreview.mu.Lock()
		s.hedgePreview.at = time.Time{} // the next preview reads the new state
		s.hedgePreview.mu.Unlock()
		slog.Warn("futures hedge switch done", "result", res.Detail)
		writeJSON(w, res)
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, strategy.ErrHedgeBusy):
		writeError(w, http.StatusGatewayTimeout, err.Error())
	default:
		writeError(w, http.StatusConflict, err.Error())
	}
}
