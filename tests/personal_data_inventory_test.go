package tests

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The personal-data inventory (frontend/docs/personal-data.md, #647) must
// name every column of auth.db and every audit action: these tests read the
// monitor's schema and audit list and fail, naming the table and the column.

const (
	authStorePath     = "../frontend/lib/auth/store.js"
	auditActionsPath  = "../frontend/lib/audit.js"
	inventoryPath     = "../frontend/docs/personal-data.md"
	signInDocPath     = "../frontend/docs/sign-in.md"
	inventoryAuditHdr = "## `audit.jsonl`"
)

var (
	createTable = regexp.MustCompile("(?s)CREATE TABLE IF NOT EXISTS (\\w+) \\((.*?)\\n\\);")
	addedColumn = regexp.MustCompile(`\["(\w+)", "[A-Z]+[^"]*"\]`)
	tickedName  = regexp.MustCompile("`([a-z_]+)`")
)

// schemaColumns returns table → columns from the CREATE TABLE statements and
// from the columns migrate() adds to users and refresh_tokens.
func schemaColumns(t *testing.T) map[string][]string {
	t.Helper()
	src := readDoc(t, authStorePath)
	cols := map[string][]string{}
	for _, m := range createTable.FindAllStringSubmatch(src, -1) {
		for _, line := range strings.Split(m[2], "\n") {
			f := strings.Fields(line)
			if len(f) < 2 || strings.HasPrefix(f[0], "--") || f[0] == "PRIMARY" {
				continue
			}
			cols[m[1]] = append(cols[m[1]], f[0])
		}
	}
	migrate := src[strings.Index(src, "migrate() {"):]
	usersEnd := strings.Index(migrate, "const sessionCols")
	for _, m := range addedColumn.FindAllStringSubmatch(migrate[:usersEnd], -1) {
		cols["users"] = append(cols["users"], m[1])
	}
	for _, m := range addedColumn.FindAllStringSubmatch(migrate[usersEnd:], -1) {
		cols["refresh_tokens"] = append(cols["refresh_tokens"], m[1])
	}
	if len(cols["users"]) < 10 || len(cols["refresh_tokens"]) < 7 {
		t.Fatalf("schema parse found too little: %v", cols)
	}
	return cols
}

// inventoryRows returns table → column → the row's cells after the column
// name, for each "## `table`" section; "login_failures" and "login_locks"
// share a section and rows written "table.column" belong to their table.
func inventoryRows(t *testing.T) map[string]map[string][]string {
	t.Helper()
	rows := map[string]map[string][]string{}
	section := ""
	for _, line := range strings.Split(readDoc(t, inventoryPath), "\n") {
		if strings.HasPrefix(line, "## ") {
			section = line
			continue
		}
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "| "), " | ")
		name := strings.Trim(cells[0], "`")
		table := strings.Trim(strings.TrimPrefix(section, "## "), "`")
		if i := strings.Index(name, "."); i > 0 {
			table, name = name[:i], name[i+1:]
		}
		if rows[table] == nil {
			rows[table] = map[string][]string{}
		}
		rows[table][name] = cells[1:]
	}
	return rows
}

func TestPersonalData_EveryStoredFieldIsDescribed(t *testing.T) {
	rows := inventoryRows(t)
	for table, columns := range schemaColumns(t) {
		for _, c := range columns {
			cells, ok := rows[table][c]
			if !ok {
				t.Errorf("personal-data.md has no row for %s.%s", table, c)
				continue
			}
			if len(cells) < 2 || strings.TrimSpace(cells[0]) == "" || strings.TrimSpace(cells[1]) == "" {
				t.Errorf("personal-data.md row %s.%s needs a purpose and a retention", table, c)
			}
		}
	}
}

func TestPersonalData_NoRowForAColumnThatIsGone(t *testing.T) {
	schema := schemaColumns(t)
	for table, named := range inventoryRows(t) {
		if _, ok := schema[table]; !ok {
			t.Errorf("personal-data.md describes table %s, which auth.db does not have", table)
			continue
		}
		have := map[string]bool{}
		for _, c := range schema[table] {
			have[c] = true
		}
		for c := range named {
			if !have[c] {
				t.Errorf("personal-data.md describes %s.%s, which auth.db does not have", table, c)
			}
		}
	}
}

// auditSection cuts a document from a heading to the next "## " heading.
func auditSection(doc, heading string) string {
	i := strings.Index(doc, heading)
	if i < 0 {
		return ""
	}
	rest := doc[i+len(heading):]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

func TestPersonalData_AuditActionsMatchTheCodeAndSignIn(t *testing.T) {
	var actions []string
	list := auditSection(readDoc(t, auditActionsPath), "export const AUDIT_ACTIONS = [")
	for _, m := range regexp.MustCompile(`"([a-z_]+)",`).FindAllStringSubmatch(list[:strings.Index(list, "];")], -1) {
		actions = append(actions, m[1])
	}
	if len(actions) < 20 {
		t.Fatalf("parsed only %d audit actions", len(actions))
	}
	inventory := map[string]bool{}
	for _, m := range tickedName.FindAllStringSubmatch(auditSection(readDoc(t, inventoryPath), inventoryAuditHdr), -1) {
		inventory[m[1]] = true
	}
	sort.Strings(actions)
	for _, a := range actions {
		if !inventory[a] {
			t.Errorf("personal-data.md audit section lacks the action %s", a)
		}
	}

	// Every action sign-in.md lists must be in the inventory too.
	signIn := readDoc(t, signInDocPath)
	block := signIn[strings.Index(signIn, "### Audit"):]
	block = block[:strings.Index(block, "Admins read it on the Audit page")]
	known := map[string]bool{}
	for _, a := range actions {
		known[a] = true
	}
	for _, line := range strings.Split(block, "\n") {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		for _, m := range tickedName.FindAllStringSubmatch(line, -1) {
			if strings.Contains(m[1], "_") && known[m[1]] && !inventory[m[1]] {
				t.Errorf("sign-in.md lists %s but the inventory does not", m[1])
			}
		}
	}
}

func TestPersonalData_OpenChoicesAreProposalsWithQuestions(t *testing.T) {
	doc := readDoc(t, inventoryPath)
	if !strings.Contains(doc, "**proposal**") {
		t.Error("personal-data.md marks no retention as a proposal")
	}
	q := doc[strings.Index(doc, "## Questions for counsel"):]
	for _, l := range []string{"L13", "L14", "L16", "L22"} {
		if !strings.Contains(q, l) {
			t.Errorf("Questions for counsel does not cite %s", l)
		}
	}
	for _, want := range []string{"What \"delete my account\" removes", "notification", "survey_answers", "reset links", "analytics events", "kept"} {
		if !strings.Contains(doc, want) {
			t.Errorf("deletion section lacks %q", want)
		}
	}
}
