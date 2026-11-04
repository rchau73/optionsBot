package accounts

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"time"

	"optionsbot/internal/keystore"
)

// maxKeyFileBytes bounds what is read from the key file or stdin.
const maxKeyFileBytes = 64 << 10

// keyFile is the key as the admin gives it, in a file or on stdin — never in
// an argument: the client id, the secret and the scope string Deribit shows
// for the key. Unknown fields are refused.
type keyFile struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Scope        string `json:"scope"`
}

// errKeyFile is every unreadable key file: the decoder's own errors can quote
// the input, so none of them is shown.
var errKeyFile = errors.New(`accounts: the key file is not {"client_id": …, "client_secret": …, "scope": …} (JSON, those fields only)`)

// ReadKeyFile reads a key from r (a file or stdin) as credentials labelled
// label. Its errors never hold any of the input.
func ReadKeyFile(r io.Reader, label string) (keystore.Credentials, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxKeyFileBytes+1))
	if err != nil {
		return keystore.Credentials{}, errors.New("accounts: could not read the key file")
	}
	defer clear(raw)
	if len(raw) > maxKeyFileBytes {
		return keystore.Credentials{}, errKeyFile
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var k keyFile
	if err := dec.Decode(&k); err != nil {
		return keystore.Credentials{}, errKeyFile
	}
	if k.ClientID == "" || k.ClientSecret == "" {
		return keystore.Credentials{}, errors.New("accounts: the key file needs a client_id and a client_secret")
	}
	return keystore.NewCredentials(label, k.ClientID, k.ClientSecret, k.Scope), nil
}

// RunKey runs `accounts key add|rotate|revoke --account <id>` (#625): the key
// is read from --key-file, or stdin when it is absent or "-"; the master key
// from the environment (keystore.LoadMasterKey). Each action that changes a
// key appends one line to the audit trail. Exit ExitOK done, ExitViolation
// refused or failed (nothing changed unless the message says so),
// ExitUnreadable bad usage or an unreadable file.
func RunKey(args []string, stdin io.Reader, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "usage: accounts key add|rotate|revoke --account <id> [--key-file <path>|-]")
		return ExitUnreadable
	}
	action := args[0]
	fs := flag.NewFlagSet("accounts key "+action, flag.ContinueOnError)
	fs.SetOutput(errOut)
	accountID := fs.String("account", "", "the registry account id")
	registry := fs.String("registry", DefaultPath, "the account registry file")
	keysPath := fs.String("keystore", keystore.DefaultPath, "the key store file")
	auditPath := fs.String("audit", DefaultAuditPath, "the audit trail (JSON lines)")
	var keyFilePath *string
	if action == "add" || action == "rotate" {
		keyFilePath = fs.String("key-file", "-", "an owner-only JSON file with client_id, client_secret and scope; - for stdin")
	} else if action != "revoke" {
		fmt.Fprintf(errOut, "accounts: unknown key action %q: add, rotate or revoke\n", action)
		return ExitUnreadable
	}
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUnreadable
	}
	if *accountID == "" || fs.NArg() > 0 {
		fmt.Fprintln(errOut, "accounts: --account <id> is required and nothing else may follow (the key is read from a file or stdin, never an argument)")
		return ExitUnreadable
	}

	_, acct, err := loadAccount(*registry, *accountID)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitFor(err)
	}
	var creds keystore.Credentials
	if keyFilePath != nil {
		if creds, err = readKeyFrom(*keyFilePath, stdin, acct.Label); err != nil {
			fmt.Fprintln(errOut, err)
			return ExitUnreadable
		}
	}
	masterKey, err := keystore.LoadMasterKey()
	if err != nil {
		fmt.Fprintln(errOut, err)
		return ExitUnreadable
	}
	store, err := keystore.Open(*keysPath, masterKey)
	clear(masterKey)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return ExitUnreadable
	}
	audit, err := OpenAudit(*auditPath)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return ExitUnreadable
	}
	defer audit.Close()

	var auditAction string
	switch action {
	case "add":
		keyID, err := AddKey(*registry, store, *accountID, creds)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return ExitViolation
		}
		auditAction = ActionKeyAdd
		fmt.Fprintf(out, "key added for account %q (key id %s).\n", *accountID, keyID)
		fmt.Fprintln(out, "A running bot of that account keeps its old key until a person restarts it; the startup reconcile then applies. This command restarts nothing.")
	case "rotate":
		newID, oldID, err := RotateKey(*registry, store, *accountID, creds)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return ExitViolation
		}
		auditAction = ActionKeyRotate
		fmt.Fprintf(out, "key rotated for account %q: new key id %s, old key %s revoked.\n", *accountID, newID, oldID)
		fmt.Fprintln(out, "Replacing a key means restarting that account's bot (a person does it); the startup reconcile then applies. This command restarts nothing.")
	case "revoke":
		keyID, already, err := RevokeKey(*registry, store, *accountID)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return ExitViolation
		}
		if already {
			fmt.Fprintf(out, "key %s of account %q was already revoked: nothing changed.\n", keyID, *accountID)
			return ExitOK
		}
		auditAction = ActionKeyRevoke
		fmt.Fprintf(out, "key %s of account %q revoked: the account is now blocked (accounts -check).\n", keyID, *accountID)
		fmt.Fprintln(out, "The bot of that account must be restarted or stopped by a person; this command does neither.")
	}
	if err := WriteAudit(audit, AuditLine{Time: time.Now().UTC(), Actor: Actor(), AccountID: *accountID, Action: auditAction}); err != nil {
		fmt.Fprintf(errOut, "%v — the action above was done but is not in the audit trail\n", err)
		return ExitViolation
	}
	return ExitOK
}

// readKeyFrom reads the key from path, or stdin for "-". A file must be
// owner-only, like the store.
func readKeyFrom(path string, stdin io.Reader, label string) (keystore.Credentials, error) {
	if path == "-" {
		return ReadKeyFile(stdin, label)
	}
	info, err := os.Stat(path)
	if err != nil {
		return keystore.Credentials{}, fmt.Errorf("accounts: key file: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return keystore.Credentials{}, fmt.Errorf("accounts: key file %s is readable by group or others (%04o): chmod 600 it", path, info.Mode().Perm())
	}
	file, err := os.Open(path)
	if err != nil {
		return keystore.Credentials{}, fmt.Errorf("accounts: key file: %w", err)
	}
	defer file.Close()
	return ReadKeyFile(file, label)
}

// exitFor maps a registry load error: a broken rule or an unknown account is
// ExitViolation, an unreadable file ExitUnreadable.
func exitFor(err error) int {
	if errors.Is(err, ErrInvalid) || errors.Is(err, ErrUnknownAccount) {
		return ExitViolation
	}
	return ExitUnreadable
}

// Actor is the person running the command: the OS user.
func Actor() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	if name := os.Getenv("USER"); name != "" {
		return name
	}
	return "unknown"
}
