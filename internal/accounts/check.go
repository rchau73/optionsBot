package accounts

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"optionsbot/internal/keystore"
)

// Exit codes of Check (cmd/accounts -check).
const (
	ExitOK         = 0 // every account valid, its key trade-only
	ExitViolation  = 1 // the registry breaks a rule, or an account is blocked
	ExitUnreadable = 2 // the registry or the key store could not be read, or the status file not written
)

// CheckOptions are cmd/accounts -check's output choices; the zero value
// prints the table only.
type CheckOptions struct {
	JSON    bool      // print the StatusDocument instead of the table
	OutPath string    // also write the StatusDocument there, atomically ("" = no file)
	Now     time.Time // the document's checked_at
}

// Check loads the registry at registryPath, joins it to the key store at
// keystorePath (read without the master key: keystore.ListFile) and prints
// one row per account to out — label, exchange, environment, bots, key-scope
// status — never a key, client id or scope string. Problems go to errOut.
func Check(out, errOut io.Writer, registryPath, keystorePath string) int {
	return CheckWith(out, errOut, registryPath, keystorePath, CheckOptions{})
}

// CheckWith is Check with the -json and -out choices (#655), same exit
// codes. An unreadable file writes nothing, so a status file already at
// OutPath stays as it was; an invalid registry writes its rule breaks.
func CheckWith(out, errOut io.Writer, registryPath, keystorePath string, opts CheckOptions) int {
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
	reg, invalid := New(f, keys)
	if opts.JSON || opts.OutPath != "" {
		doc := NewStatusDocument(opts.Now, reg, f.Violations())
		if opts.OutPath != "" {
			if err := WriteStatusFile(opts.OutPath, doc); err != nil {
				fmt.Fprintln(errOut, err)
				return ExitUnreadable
			}
		}
		if opts.JSON {
			raw, err := doc.Encode()
			if err != nil {
				fmt.Fprintln(errOut, err)
				return ExitUnreadable
			}
			out.Write(raw)
		}
	}
	if invalid != nil {
		fmt.Fprintln(errOut, invalid)
		return ExitViolation
	}
	if !opts.JSON {
		printTable(out, reg)
	}
	if n := reg.Blocked(); n > 0 {
		fmt.Fprintf(errOut, "accounts: %d of %d accounts blocked\n", n, len(reg.List()))
		return ExitViolation
	}
	return ExitOK
}

// printTable is -check's one row per account.
func printTable(out io.Writer, reg *Registry) {
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
}
