package evidence

import (
	"fmt"
	"io"
	"strings"
)

// WritePackMarkdown writes the pack: the label, then one table per section,
// its risks as rows of that table. Nothing in a table is bold, so no figure
// outweighs another.
func WritePackMarkdown(w io.Writer, pk Pack) error {
	b := &strings.Builder{}
	p := func(format string, a ...any) { fmt.Fprintf(b, format, a...) }
	p("# Evidence pack — %s %s\n\n", pk.Env, strings.ToUpper(pk.Bot))
	p("**%s**\n\n", pk.Label)
	p("Account: %s. Built only from the reports named beside each figure; no figure is recomputed, and a source that is missing reads \"%s\".", pk.Account, NotReported)
	if !pk.AmountsShown {
		p(" Live account: percentages, ratios and counts only.")
	}
	p("\n\n")
	for _, s := range pk.Sections {
		p("## %s\n\n%s · replay vs live: %s\n\n", s.Title, s.Caveat, s.ReplayVsLive)
		p("| Figure | Value | Source | Window | Caveat |\n|---|---|---|---|---|\n")
		for _, f := range s.Figures {
			p("| %s | %s | %s | %s | %s |\n", packCell(f.Name), packCell(f.Display), packCell(f.Source), packCell(f.Window), packCell(f.Caveat))
		}
		for _, risk := range s.Risks {
			p("| %s | %s | | | %s |\n", PackRiskName, packCell(risk), packCell(s.Caveat))
		}
		p("\n")
	}
	if len(pk.Unreadable) > 0 {
		p("Files not read (their figures read \"%s\"): %s\n", NotReported, strings.Join(pk.Unreadable, ", "))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// packCell keeps a note's text inside its table cell.
func packCell(s string) string {
	return strings.NewReplacer("|", `\|`, "\n", " ").Replace(s)
}
