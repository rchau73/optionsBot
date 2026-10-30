package evidence

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Paths are one bot's files in an environment clone (docs/operations.md
// "Inside an environment"): the bot runs in data/<bot>/.
type Paths struct {
	Journal, History, Market, GEX, Stress, Config string
	Directions                                    string // data/direction_history.jsonl (#245)
	Deploys                                       string // <envs>/history/deploys.tsv, beside the clone (#361)
}

// BotPaths lays out bot's files under root (an environment clone). Books
// (data/market, data/market_gex) are read under booksRoot — mainnet market
// data, the same for every environment; only stable records them.
func BotPaths(root, booksRoot, bot string) Paths {
	dir := filepath.Join(root, "data", bot)
	books := filepath.Join(booksRoot, "data", bot, "data")
	return Paths{
		Journal: filepath.Join(dir, "orders.log"),
		History: filepath.Join(dir, "data", "pnl_history.jsonl"),
		Market:  filepath.Join(books, "market"),
		GEX:     filepath.Join(books, "market_gex"),
		Stress:  filepath.Join(root, "data", "stress", bot),
		Config:  ConfigPath(root, bot),

		Directions: filepath.Join(dir, "data", "direction_history.jsonl"),
		Deploys:    filepath.Join(filepath.Dir(root), "history", "deploys.tsv"),
	}
}

// DirectionOutPaths are the scorecard's two files:
// <root>/data/evidence/direction/<week>.md and .json.
func DirectionOutPaths(root, label string) (md, js string) {
	dir := filepath.Join(root, "data", "evidence", "direction")
	return filepath.Join(dir, label+".md"), filepath.Join(dir, label+".json")
}

// OutPaths are the report's two files: <root>/data/evidence/<week>/<bot>.md
// and .json.
func OutPaths(root, label, bot string) (md, js string) {
	dir := filepath.Join(root, "data", "evidence", label)
	return filepath.Join(dir, bot+".md"), filepath.Join(dir, bot+".json")
}

// ConfigPath is the config the environment's bot runs: BOT_CONFIG_<BOT> in
// its deploy.env (mvp's BTC: ./config_btc.mvp.yaml), else config_<bot>.yaml.
func ConfigPath(root, bot string) string {
	name := "config_" + bot + ".yaml"
	key := "BOT_CONFIG_" + strings.ToUpper(bot) + "="
	if f, err := os.Open(filepath.Join(root, "deploy.env")); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(sc.Text()), "export "))
			if v, ok := strings.CutPrefix(line, key); ok {
				if v = strings.Trim(strings.TrimSpace(v), `"'`); v != "" {
					name = v
				}
			}
		}
	}
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(root, name)
}
