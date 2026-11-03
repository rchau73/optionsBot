// Command paramdocs writes docs/parameters.md from the tunable-parameter
// catalogue (internal/config/catalogue.go, #255), so the page cannot drift
// from the code: tests/config_catalogue_test.go fails until it is re-run.
// It also writes the lab's settings data (#586,
// frontend/content/lab-settings.json): the catalogue plus each shipped
// config's current values and cross-rule bounds, read by the monitor's
// POST /api/lab/validate; tests/lab_settings_test.go fails until it is re-run.
//
//	go run ./cmd/paramdocs              # writes docs/parameters.md and the lab data
//	go run ./cmd/paramdocs -o other.md -lab other.json
package main

import (
	"flag"
	"fmt"
	"os"

	"optionsbot/internal/config"
)

func main() {
	out := flag.String("o", "docs/parameters.md", "file to write")
	lab := flag.String("lab", "frontend/content/lab-settings.json", "lab settings data to write")
	flag.Parse()
	if err := os.WriteFile(*out, []byte(config.CatalogueMarkdown(config.Tunables())), 0o644); err != nil {
		fail(err)
	}
	fmt.Println("wrote", *out)
	b, err := config.ShippedLabSettingsJSON(".")
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(*lab, b, 0o644); err != nil {
		fail(err)
	}
	fmt.Println("wrote", *lab)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "paramdocs:", err)
	os.Exit(1)
}
