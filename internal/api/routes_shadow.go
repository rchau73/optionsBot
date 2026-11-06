package api

import (
	"log/slog"
	"net/http"

	"optionsbot/internal/shadow"
)

// WithShadowLegs serves GET /api/shadow-legs from the record at path (#676).
func WithShadowLegs(path string) Option { return func(s *Server) { s.shadowLegs = path } }

func (s *Server) routesShadow(mux *http.ServeMux) {
	if s.shadowLegs == "" {
		return
	}
	mux.HandleFunc("GET /api/shadow-legs", s.shadowSummary)
}

// shadowSummary reads the file on each request: nothing polls this route.
func (s *Server) shadowSummary(w http.ResponseWriter, r *http.Request) {
	recs, skipped, err := shadow.Read(s.shadowLegs)
	if err != nil {
		slog.Warn("monitor API: shadow legs unreadable", "err", err)
		writeError(w, http.StatusInternalServerError, "the shadow legs record cannot be read")
		return
	}
	writeJSON(w, shadow.Summarize(recs, skipped))
}
