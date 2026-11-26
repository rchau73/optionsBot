// Command labpr opens the draft pull request of a ready lab candidate (#862,
// epic E9): cmd/labapply's COMPARE_DIR/candidates/<run id>/ becomes branch
// lab/<run id> from origin/main with only the recorded values changed (in the
// shipped config and its twin, config_btc.yaml / config_btc.mvp.yaml) and one
// draft PR with the backtested numbers and the risks. It never merges, never
// pushes to main, restarts no bot and sends no order: a person's merge is the
// approval (docs/product/decisions/2026-11-20-lab-change-reaches-bot-by-pr.md).
// Run by hand only. A second run reports the PR already open.
// Exit 0 opened or already there, 2 refused, 1 error.
//
//	go run ./cmd/labpr -id 20261104T230000Z -compare data/compare [-dry-run]
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"optionsbot/internal/compare"
)

// execRunner runs git and gh for real.
type execRunner struct{}

func (execRunner) Run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

func main() {
	o := compare.LabPROptions{}
	flag.StringVar(&o.ID, "id", "", "the run id of the candidate (COMPARE_DIR/candidates/<id>/)")
	flag.StringVar(&o.CompareDir, "compare", "data/compare", "cmd/compare's reports folder (the monitor's COMPARE_DIR)")
	flag.StringVar(&o.RepoDir, "repo", ".", "the repository clone the branch is made from (origin = GitHub)")
	flag.BoolVar(&o.DryRun, "dry-run", false, "print the branch, the diff and the body; create no branch, commit or pull request")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if !o.DryRun {
		dir, err := os.MkdirTemp("", "labpr-*")
		if err != nil {
			log.Error("lab pr failed", "component", "lab", "run_id", o.ID, "error", err)
			os.Exit(1)
		}
		defer os.RemoveAll(dir)
		o.WorkDir = dir
	}
	res, err := compare.RunLabPR(o, execRunner{}, os.Stdout)
	var refused *compare.LabPRRefusal
	switch {
	case errors.As(err, &refused):
		log.Warn("lab pr refused", "component", "lab", "run_id", o.ID, "reasons", strings.Join(refused.Reasons, "; "))
		for _, r := range refused.Reasons {
			fmt.Fprintln(os.Stderr, "- "+r)
		}
		os.RemoveAll(o.WorkDir)
		os.Exit(2)
	case err != nil:
		log.Error("lab pr failed", "component", "lab", "run_id", o.ID, "branch", res.Branch, "error", err)
		os.RemoveAll(o.WorkDir)
		os.Exit(1)
	}
	log.Info("lab pr", "component", "lab", "run_id", o.ID, "branch", res.Branch, "state", res.State, "url", res.URL, "merged", false)
}
