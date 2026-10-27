// Command helpindex writes frontend/content/help-index.json (#312): every
// section of OptionStrategy.md, every glossary term and every sentence of the
// plain-words catalogue as a cited passage (internal/helpkb). It is the only
// corpus the E10 assistant may answer from; tests/helpindex_test.go fails
// until it is re-run after one of those sources changes.
//
//	go run ./cmd/helpindex              # writes frontend/content/help-index.json
//	go run ./cmd/helpindex -o other.json
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"optionsbot/internal/helpkb"
)

func main() {
	root := flag.String("root", ".", "repository root the sources are read from")
	out := flag.String("o", helpkb.IndexFile, "file to write (relative to -root unless absolute)")
	flag.Parse()
	if err := run(*root, *out); err != nil {
		fmt.Fprintln(os.Stderr, "helpindex:", err)
		os.Exit(1)
	}
}

func run(root, out string) error {
	f, err := helpkb.LoadIndex(root)
	if err != nil {
		return err
	}
	data, err := f.Encode()
	if err != nil {
		return err
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(root, out)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d passages\n", out, len(f.Chunks))
	return nil
}
