package helpkb

import (
	"fmt"
	"os"
	"path/filepath"

	"optionsbot/internal/api"
)

// LoadSources reads the index's sources from a checkout of the repository at
// root: the two files and the plain-words catalogue compiled into this
// binary (api.Catalogue), with the bytes of the file it is written in.
func LoadSources(root string) (Sources, error) {
	read := func(name string) ([]byte, error) {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return nil, fmt.Errorf("read help source: %w", err)
		}
		return b, nil
	}
	var (
		s   Sources
		err error
	)
	if s.Strategy, err = read(StrategyFile); err != nil {
		return Sources{}, err
	}
	if s.Parameters, err = read(ParametersFile); err != nil {
		return Sources{}, err
	}
	if s.Guide, err = read(GuideFile); err != nil {
		return Sources{}, err
	}
	if s.Glossary, err = read(GlossaryFile); err != nil {
		return Sources{}, err
	}
	if s.Explain, err = read(ExplainFile); err != nil {
		return Sources{}, err
	}
	for _, e := range api.Catalogue() {
		s.Catalogue = append(s.Catalogue, CatalogueEntry{Key: e.Key, Text: e.Text})
	}
	return s, nil
}

// LoadIndex builds the index from the sources at root.
func LoadIndex(root string) (File, error) {
	s, err := LoadSources(root)
	if err != nil {
		return File{}, err
	}
	return Build(s)
}
