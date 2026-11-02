package accounts

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"optionsbot/internal/keystore"
)

// Exit codes of Check (cmd/accounts -check).
const (
	ExitOK         = 0 // every account valid, its key trade-only
	ExitViolation  = 1 // the registry breaks a rule, or an account is blocked
	ExitUnreadable = 2 // the registry or the key store could not be read
)

// Check loads the registry at registryPath, joins it to the key store at
// keystorePath (read without the master key: keystore.ListFile) and prints
// one row per account to out — label, exchange, environment, bots, key-scope
// status — never a key, client id or scope string. Problems go to errOut.
func Check(out, errOut io.Writer, registryPath, keystorePath string) int {
	keys, err := keystore.ListFile(keystorePath)
	if err != nil {
		fmt.Fprintln(errOut, "accounts: key store:", err)
		return ExitUnreadable
	}
	f, err := ReadFile(registryPath)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return ExitUnreadable
	}
	reg, err := New(f, keys)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return ExitViolation
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CUSTOMER\tACCOUNT\tLABEL\tEXCHANGE\tENVIRONMENT\tBOTS\tKEY SCOPE\tSTATUS\tREASON")
	for _, a := range reg.List() {
		bots := make([]string, len(a.Bots))
		for i, b := range a.Bots {
			bots[i] = b.String()
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", a.CustomerID, a.AccountID, a.Label, a.Exchange,
			a.Environment, strings.Join(bots, ", "), a.KeyScope, a.Status, a.Reason)
	}
	tw.Flush()
	if n := reg.Blocked(); n > 0 {
		fmt.Fprintf(errOut, "accounts: %d of %d accounts blocked\n", n, len(reg.List()))
		return ExitViolation
	}
	return ExitOK
}
