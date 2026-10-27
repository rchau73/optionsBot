package usdcstudy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"optionsbot/internal/coverage"
	"optionsbot/internal/recorder"
)

// Datasets are the book folders read under each data directory: the
// coin-settled books, the bot's own USDC books and SOL's and XRP's.
func Datasets() []string {
	out := []string{"market", recorder.USDCDataset("", true)}
	for _, u := range recorder.ExtraUSDC {
		out = append(out, recorder.USDCDataset(u, false))
	}
	return out
}

// Read reads every dataset folder under each dir (a bot folder such as
// data/btc or its data directory) for the days of [from, to). A folder that
// is absent is listed in notFound, not an error; a folder whose window holds
// no snapshot is still a book (all its hours missing), named after its
// folder (market_usdc_sol → SOL_USDC) when no quote names the underlying.
func Read(dirs []string, from, to time.Time) (books []Book, notFound []string, err error) {
	for _, dir := range dirs {
		base := coverage.BaseDir(dir)
		for _, name := range Datasets() {
			path := filepath.Join(base, name)
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
				notFound = append(notFound, path)
				continue
			} else if err != nil {
				return nil, nil, err
			}
			snaps, err := recorder.ReadDir(path, from, to.Add(-time.Nanosecond))
			if err != nil {
				return nil, nil, fmt.Errorf("read %s: %w", path, err)
			}
			books = append(books, Book{Underlying: underlyingOf(snaps, name, path), Path: path, Snapshots: snaps})
		}
	}
	return books, notFound, nil
}

func underlyingOf(snaps []recorder.Snapshot, dataset, path string) string {
	for _, s := range snaps {
		for _, q := range s.Quotes {
			return q.Underlying
		}
	}
	if u, ok := strings.CutPrefix(dataset, "market_usdc_"); ok {
		return strings.ToUpper(u) + "_USDC"
	}
	return path // the bot's own books: the folder says which bot
}
