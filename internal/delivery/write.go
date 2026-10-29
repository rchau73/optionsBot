package delivery

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Write writes dir/dora.json and one dir/<week>.md per week, each through a
// temporary file renamed into place, so a reader never sees half a file.
func Write(dir string, r Report) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	js, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encode dora.json: %w", err)
	}
	if err := writeFile(filepath.Join(dir, "dora.json"), append(js, '\n')); err != nil {
		return err
	}
	for _, w := range r.Weeks {
		if err := writeFile(filepath.Join(dir, w.Week+".md"), []byte(WeekMarkdown(r, w))); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	return nil
}
