// Command evidence writes the weekly evidence report of each bot of an
// environment (#176): live P&L net of fees from the journal and the P&L
// history, the bot's rules replayed on the recorded mainnet books of the
// same week, the replay − live gap, the standard stress shocks of the book
// (cmd/stress) and the hold-the-coin benchmark, each with its source. It
// reads the environment's files only (no Deribit call, no order) and writes
// data/evidence/<week>/<bot>.md and .json under the environment clone —
// amounts stay in data/ (gitignored). See README "Evidence report".
//
//	go run ./cmd/evidence -env stable -week 2026-W42
//	go run ./cmd/evidence -env mvp -from 2026-10-12 -to 2026-10-18 -books-root ~/dev/go/optionsBot-env/stable
package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"optionsbot/internal/evidence"
)

func main() {
	env := flag.String("env", "", "environment: stable | mvp")
	root := flag.String("root", "", "the environment's clone (default ~/dev/go/optionsBot-env/<env>)")
	booksRoot := flag.String("books-root", "", "clone whose data/<bot>/data/market books the replay reads (default -root; mvp records none: pass stable's)")
	bots := flag.String("bot", "btc,eth", "bots, comma-separated")
	week := flag.String("week", "", "ISO week, e.g. 2026-W42 (default: the last complete week)")
	from := flag.String("from", "", "first day, YYYY-MM-DD (with -to, instead of -week)")
	to := flag.String("to", "", "last day, YYYY-MM-DD (included)")
	equity := flag.Float64("equity", 0, "starting margin balance in the coin (one -bot only; default account.json of data/stress/<bot>)")
	stressCmd := flag.String("stress-cmd", "go run ./cmd/stress", "command that runs cmd/stress, from the repository (none = skip the stress section)")
	flag.Parse()

	if err := run(*env, *root, *booksRoot, *bots, *week, *from, *to, *equity, *stressCmd); err != nil {
		fmt.Fprintln(os.Stderr, "evidence:", err)
		os.Exit(1)
	}
}

func run(env, root, booksRoot, bots, week, from, to string, equity float64, stressCmd string) error {
	if env != "stable" && env != "mvp" {
		return fmt.Errorf("-env %q: stable | mvp", env)
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		root = filepath.Join(home, "dev", "go", "optionsBot-env", env)
	}
	if booksRoot == "" {
		booksRoot = root
	}
	w, err := window(week, from, to)
	if err != nil {
		return err
	}
	names := strings.Split(bots, ",")
	if equity > 0 && len(names) != 1 {
		return fmt.Errorf("-equity is in one coin: give a single -bot")
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil))) // the bot's rules log; the report says it all
	for _, bot := range names {
		bot = strings.ToLower(strings.TrimSpace(bot))
		in := evidence.Load(env, bot, evidence.BotPaths(root, booksRoot, bot), w, equity, os.Stderr)
		evidence.RunStress(&in, stressCmd)
		md, js := evidence.OutPaths(root, w.Label, bot)
		if err := evidence.Write(evidence.Build(in), md, js); err != nil {
			return err
		}
		fmt.Printf("%s %s %s: %s, %s\n", env, bot, w.Label, md, js)
	}
	return nil
}

func window(week, from, to string) (evidence.Window, error) {
	switch {
	case week != "" && (from != "" || to != ""):
		return evidence.Window{}, fmt.Errorf("-week or -from/-to, not both")
	case week != "":
		return evidence.ParseWeek(week)
	case from != "" || to != "":
		return evidence.ParseDays(from, to)
	}
	return evidence.LastWeek(time.Now()), nil
}
