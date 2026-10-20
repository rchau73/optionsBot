package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The legal pack (docs/legal/, #228) is prepared for a licensed lawyer: every
// question of the legal brief has a preliminary answer, and no file may be
// mistaken for legal advice or for a text ready to give users.

func TestLegalPack_EveryBriefQuestionAnswered(t *testing.T) {
	raw, err := os.ReadFile("../docs/legal/preliminary-answers.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	for i := 1; i <= 24; i++ {
		if !strings.Contains(doc, fmt.Sprintf("| **L%d** ", i)) {
			t.Errorf("preliminary-answers.md has no row for L%d", i)
		}
	}
}

func TestLegalPack_EveryFileCarriesItsNotice(t *testing.T) {
	files, err := filepath.Glob("../docs/legal/*.md")
	if err != nil {
		t.Fatal(err)
	}
	more, _ := filepath.Glob("../docs/legal/*/*.md")
	files = append(files, more...)
	if len(files) < 10 {
		t.Fatalf("found %d legal files: layout changed?", len(files))
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		head := string(raw)
		if len(head) > 600 {
			head = head[:600]
		}
		draft := strings.Contains(f, "/drafts/")
		switch {
		case draft && !strings.Contains(head, "MINUTA para revisão por advogado"):
			t.Errorf("%s: a draft must open with \"MINUTA para revisão por advogado\"", f)
		case !draft && !strings.Contains(head, "Not legal advice"):
			t.Errorf("%s: must open with \"Not legal advice\"", f)
		}
	}
}
