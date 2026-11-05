// Command labrun is the lab's runner (#641, epic E9): it takes the run
// requests the monitor queued (POST /api/lab/requests →
// data/compare/requests/<id>.json) one at a time and runs cmd/compare on each
// — the shipped config of its coin against the same config with the proposed
// values (-set), -neighbours for the changed number, over the last -days
// whole UTC days — and writes the status back: running, then done with the
// report's id, or failed with a short reason. A request queued more than a
// day ago is expired. At most one runner works at a time (a lock folder in
// the queue). It writes no config and applies no setting; the AI analysis
// (cmd/analyze) still runs by hand. Run by scripts/lab_runner.sh.
//
//	go run ./cmd/labrun -queue data/compare/requests -out data/compare
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"optionsbot/internal/compare"
)

func main() {
	o := compare.LabRunOptions{}
	flag.StringVar(&o.QueueDir, "queue", "data/compare/requests", "the monitor's request queue")
	flag.StringVar(&o.OutDir, "out", "data/compare", "cmd/compare's reports folder (the monitor's COMPARE_DIR)")
	flag.StringVar(&o.Base, "base", "config_{u}.yaml", "the current config, {u} = the coin in lower case")
	flag.StringVar(&o.Replay, "replay", "data/{u}/data/market", "recorded books (skipped when the folder is missing)")
	flag.StringVar(&o.Stress, "stress", "data/stress/{u}", "captured book for the stress shocks (skipped when the folder is missing)")
	flag.StringVar(&o.CSV, "csv", "", "backtest data (empty = cmd/compare's default)")
	flag.IntVar(&o.Days, "days", 7, "the window: the last N whole UTC days")
	compareCmd := flag.String("compare-cmd", "go run ./cmd/compare", "command that runs cmd/compare, from the repository")
	timeout := flag.Duration("timeout", 3*time.Hour, "longest one comparison may take")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	sum, err := compare.RunLabQueue(context.Background(), o, time.Now, commandRunner(*compareCmd, *timeout, log))
	if errors.Is(err, compare.ErrRunnerBusy) {
		log.Info("lab runner: another run is in progress, nothing taken", "component", "lab")
		return
	}
	if err != nil {
		log.Error("lab runner failed", "component", "lab", "error", err)
		os.Exit(1)
	}
	log.Info("lab runner pass done", "component", "lab", "done", sum.Done, "failed", sum.Failed, "expired", sum.Expired, "orphaned", sum.Orphaned)
}

// commandRunner runs cmdLine with the arguments; a failure's reason is the
// last line cmd/compare wrote to its error output.
func commandRunner(cmdLine string, timeout time.Duration, log *slog.Logger) compare.CompareFunc {
	return func(ctx context.Context, args []string) (string, error) {
		parts := strings.Fields(cmdLine)
		if len(parts) == 0 {
			return "", errors.New("-compare-cmd is empty")
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		c := exec.CommandContext(ctx, parts[0], append(parts[1:], args...)...)
		var stdout, stderr bytes.Buffer
		c.Stdout, c.Stderr = &stdout, &stderr
		log.Info("lab run started", "component", "lab", "args", strings.Join(args, " "))
		err := c.Run()
		if ctx.Err() != nil {
			return "", fmt.Errorf("the comparison took longer than %s", timeout)
		}
		if err != nil {
			if line := lastLine(stderr.String()); line != "" {
				return "", errors.New(line)
			}
			return "", err
		}
		return stdout.String(), nil
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
