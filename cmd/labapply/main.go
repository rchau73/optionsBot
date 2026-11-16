// Command labapply turns a manager's confirmed lab change (#741, the record
// COMPARE_DIR/applied/<run id>.json) into a checked config candidate (#806,
// epic E9): it re-checks the record on the bot side — tunable key, value in
// the catalogue's range, shipped value still the run's, a core policy only
// with the product owner's approval reference, the whole file through
// config.Load — and writes COMPARE_DIR/candidates/<run id>/
// (config.candidate.yaml when ready, diff.md, candidate.json). It never
// writes a shipped config, edits the record, calls a bot or the exchange: a
// person takes a ready candidate into a deploy through the usual pull request
// (docs/operations.md "Lab candidates"). Exit 0 ready, 2 refused, 1 error.
//
//	go run ./cmd/labapply -id 20261104T230000Z -compare data/compare
package main

import (
	"flag"
	"log/slog"
	"os"
	"strings"

	"optionsbot/internal/compare"
)

func main() {
	o := compare.CandidateOptions{}
	id := flag.String("id", "", "the run id of the record (COMPARE_DIR/applied/<id>.json)")
	flag.StringVar(&o.CompareDir, "compare", "data/compare", "cmd/compare's reports folder (the monitor's COMPARE_DIR)")
	flag.StringVar(&o.ConfigDir, "configs", ".", "the folder of the shipped config files")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	st, err := compare.BuildCandidate(o, *id)
	if err != nil {
		log.Error("lab candidate failed", "component", "lab", "run_id", *id, "error", err)
		os.Exit(1)
	}
	keys := make([]string, len(st.Changes))
	for i, c := range st.Changes {
		keys[i] = c.Key
	}
	if st.Status != compare.CandidateReady {
		log.Warn("lab candidate refused", "component", "lab", "run_id", st.RunID, "config", st.Config,
			"keys", strings.Join(keys, ","), "needs_product_owner", st.NeedsProductOwner, "reasons", strings.Join(st.Reasons, "; "))
		os.Exit(2)
	}
	log.Info("lab candidate ready", "component", "lab", "run_id", st.RunID, "config", st.Config, "keys", strings.Join(keys, ","), "applied", false)
}
