// Command paramdocs writes docs/parameters.md from the tunable-parameter
// catalogue (internal/config/catalogue.go, #255), so the page cannot drift
// from the code: tests/config_catalogue_test.go fails until it is re-run.
//
//	go run ./cmd/paramdocs              # writes docs/parameters.md
//	go run ./cmd/paramdocs -o other.md
package main

import (
	"flag"
	"fmt"
	"os"

	"optionsbot/internal/config"
)

func main() {
	out := flag.String("o", "docs/parameters.md", "file to write")
	flag.Parse()
	if err := os.WriteFile(*out, []byte(config.CatalogueMarkdown(config.Tunables())), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "paramdocs:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", *out)
}
