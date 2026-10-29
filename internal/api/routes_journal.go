package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"optionsbot/internal/orders"
)

// routesJournal registers the facts behind one journal event and the
// journal's week read back from orders.log.
func (s *Server) routesJournal(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/explain", s.explain)
	mux.HandleFunc("GET /api/journal", s.journal)
}

// explain serves GET /api/explain?seq=<event seq> or ?trade=<trade seq>.
// 400 without exactly one of them; 404 when the event (or trade) is not in
// memory.
func (s *Server) explain(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	seqQ, tradeQ := q.Get("seq"), q.Get("trade")
	if (seqQ == "") == (tradeQ == "") {
		writeError(w, http.StatusBadRequest, "give exactly one of seq or trade")
		return
	}
	raw := seqQ
	if tradeQ != "" {
		raw = tradeQ
	}
	seq, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || seq == 0 {
		writeError(w, http.StatusBadRequest, "seq and trade must be positive integers")
		return
	}
	if tradeQ != "" && !s.hasTrade(seq) {
		writeError(w, http.StatusNotFound, NotInRecentHistory)
		return
	}
	window := s.events.Recent(0, 0)
	for _, e := range window {
		if e.Seq == seq {
			writeJSON(w, BuildExplanation(e, window))
			return
		}
	}
	writeError(w, http.StatusNotFound, NotInRecentHistory)
}

// hasTrade says whether the journal's trades in memory hold one with seq.
func (s *Server) hasTrade(seq uint64) bool {
	ts, ok := s.events.(TradeSource)
	if !ok {
		return false
	}
	t := ts.Trades(seq-1, 1)
	return len(t) == 1 && t[0].Seq == seq
}

// journal serves GET /api/journal?days=1–7&held=0|1.
func (s *Server) journal(w http.ResponseWriter, r *http.Request) {
	days := defaultJournalDays
	if q := r.URL.Query().Get("days"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 || n > maxJournalDays {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("days must be 1 to %d", maxJournalDays))
			return
		}
		days = n
	}
	withHeld := false
	switch r.URL.Query().Get("held") {
	case "", "0":
	case "1":
		withHeld = true
	default:
		writeError(w, http.StatusBadRequest, "held must be 0 or 1")
		return
	}
	src, ok := s.events.(JournalSource)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "journal not available")
		return
	}
	now := time.Now()
	from := now.Add(-time.Duration(days) * 24 * time.Hour)
	items, events, truncated, err := ReadJournal(src, from)
	var held []string
	if err == nil && withHeld {
		held = heldInstruments(s.strategy.View())
		var older []JournalItem
		var n int
		var cut bool
		older, n, cut, err = ReadHeldJournal(src, held, from)
		items, events, truncated = append(older, items...), events+n, truncated || cut
	}
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, orders.ErrNoJournalFile) {
			code = http.StatusServiceUnavailable
		}
		slog.Warn("monitor API: journal read failed", "days", days, "held", withHeld, "err", err)
		writeError(w, code, "journal not available")
		return
	}
	if truncated {
		slog.Warn("monitor API: journal answer truncated", "days", days, "held", len(held), "items", len(items), "max", MaxJournalItems)
	}
	writeJSON(w, JournalWindow{AsOf: now, Days: days, From: from, Events: events, Items: items, Truncated: truncated, Held: held})
}
