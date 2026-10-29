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
//
// -compare-direction writes the direction trial scorecard instead (#245):
// stable's BTC (bias) and mvp's (sma) live, and stable's books replayed with
// each source, as percentages only → <stable>/data/evidence/direction/.
//
//	go run ./cmd/evidence -compare-direction -week 2026-W43
//
// -track-record (#279) and -trend (#323) join the weekly reports already
// written: the whole P&L history with one row per ISO week, and the weeks'
// percentages side by side → <root>/data/evidence/track-record/ and trend/.
//
//	go run ./cmd/evidence -env stable -track-record -trend
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
	compare := flag.Bool("compare-direction", false, "the direction trial scorecard (#245): stable's bot (bias) vs mvp's (sma), live and replayed both ways on stable's books → <stable>/data/evidence/direction/<week>.md|.json")
	stableRoot := flag.String("stable-root", "", "with -compare-direction: the stable clone (default ~/dev/go/optionsBot-env/stable)")
	mvpRoot := flag.String("mvp-root", "", "with -compare-direction: the mvp clone (default ~/dev/go/optionsBot-env/mvp)")
	track := flag.Bool("track-record", false, "the track record (#279): the whole P&L history and one row per ISO week from the weekly reports → <root>/data/evidence/track-record/<bot>.md|.json")
	trend := flag.Bool("trend", false, "the evidence trend (#323): the weekly reports week by week, percentages only → <root>/data/evidence/trend/<bot>.md|.json")
	flag.Parse()

	if *track || *trend {
		if err := runTrack(*env, *root, *bots, *track, *trend); err != nil {
			fmt.Fprintln(os.Stderr, "evidence:", err)
			os.Exit(1)
		}
		return
	}
	if *compare {
		if err := runCompare(*stableRoot, *mvpRoot, *bots, *week, *from, *to); err != nil {
			fmt.Fprintln(os.Stderr, "evidence:", err)
			os.Exit(1)
		}
		return
	}
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
		in.Account = evidence.DeribitAccount(root) // live → the replay vs live section shows % only (#488)
		evidence.RunStress(&in, stressCmd)
		md, js := evidence.OutPaths(root, w.Label, bot)
		if err := evidence.Write(evidence.Build(in), md, js); err != nil {
			return err
		}
		fmt.Printf("%s %s %s: %s, %s\n", env, bot, w.Label, md, js)
	}
	return nil
}

// runTrack writes the track record and/or the trend of each bot from the
// files already in the clone (the history and the weekly reports).
func runTrack(env, root, bots string, track, trend bool) error {
	if env != "stable" && env != "mvp" {
		return fmt.Errorf("-env %q: stable | mvp", env)
	}
	root, err := envRoot(root, env)
	if err != nil {
		return err
	}
	for _, bot := range strings.Split(bots, ",") {
		bot = strings.ToLower(strings.TrimSpace(bot))
		in := evidence.LoadTrack(env, bot, root, os.Stderr)
		if track {
			md, js := evidence.TrackOutPaths(root, bot)
			if err := evidence.WriteTrack(evidence.BuildTrackRecord(in), md, js); err != nil {
				return err
			}
			fmt.Printf("%s %s track record: %s, %s\n", env, bot, md, js)
		}
		if trend {
			md, js := evidence.TrendOutPaths(root, bot)
			if err := evidence.WriteTrend(evidence.BuildTrend(env, bot, in.WeeklySource, in.Weekly, in.Unreadable), md, js); err != nil {
				return err
			}
			fmt.Printf("%s %s trend: %s, %s\n", env, bot, md, js)
		}
	}
	return nil
}

// runCompare writes the direction trial scorecard of one bot (BTC unless
// -bot names another single bot).
func runCompare(stableRoot, mvpRoot, bots, week, from, to string) error {
	bot := strings.ToLower(strings.TrimSpace(bots))
	if bot == "btc,eth" {
		bot = "btc" // the trial's bot (#168)
	}
	if strings.Contains(bot, ",") {
		return fmt.Errorf("-compare-direction: give a single -bot")
	}
	var err error
	if stableRoot, err = envRoot(stableRoot, "stable"); err != nil {
		return err
	}
	if mvpRoot, err = envRoot(mvpRoot, "mvp"); err != nil {
		return err
	}
	w, err := window(week, from, to)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	envs := []evidence.DirectionEnvRoot{{Env: "stable", Root: stableRoot}, {Env: "mvp", Root: mvpRoot}}
	in := evidence.LoadDirection(bot, envs, "stable", stableRoot, w, os.Stderr)
	md, js := evidence.DirectionOutPaths(stableRoot, w.Label)
	if err := evidence.WriteDirection(evidence.BuildDirection(in), md, js); err != nil {
		return err
	}
	fmt.Printf("direction %s %s: %s, %s\n", bot, w.Label, md, js)
	return nil
}

func envRoot(root, env string) (string, error) {
	if root != "" {
		return root, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "dev", "go", "optionsBot-env", env), nil
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
