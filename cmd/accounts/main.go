// Command accounts checks the account registry (#585, internal/accounts):
// which customer owns which exchange account and which bots run on it, joined
// to the key store's key-scope statuses. It prints one row per account and
// never a key; it reads the key store without the master key.
//
//	go run ./cmd/accounts -check [-registry data/accounts.json] [-keystore data/keystore.json]
//
// Exit 0: every account valid and its key trade-only; 1: a rule broken or an
// account blocked; 2: a file could not be read.
//
// -json prints the same check as a key-free status document
// (accounts.StatusDocument, #655) instead of the table; -out <path> also
// writes it there, atomically and owner-only. Same exit codes; an unreadable
// file writes nothing.
//
//	go run ./cmd/accounts -check -json [-out data/accounts_status.json]
//
// The key lifecycle (#625) adds, rotates and revokes an account's key in the
// key store, with the registry's key_id following; the key is read from an
// owner-only JSON file or stdin, never an argument, and each action is one
// line of data/accounts_audit.jsonl:
//
//	go run ./cmd/accounts key add    --account <id> [--key-file <path>|-]
//	go run ./cmd/accounts key rotate --account <id> [--key-file <path>|-]
//	go run ./cmd/accounts key revoke --account <id>
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"optionsbot/internal/accounts"
	"optionsbot/internal/keystore"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "key" {
		os.Exit(accounts.RunKey(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	}
	check := flag.Bool("check", false, "validate the registry and print one row per account")
	registry := flag.String("registry", accounts.DefaultPath, "the account registry file")
	keys := flag.String("keystore", keystore.DefaultPath, "the key store file (read without the master key)")
	asJSON := flag.Bool("json", false, "with -check: print the status document (JSON, no key id) instead of the table")
	outPath := flag.String("out", "", "with -check: also write the status document to this file, atomically and owner-only (e.g. "+accounts.DefaultStatusPath+")")
	flag.Parse()
	if !*check {
		fmt.Fprintln(os.Stderr, "accounts: -check, or key add|rotate|revoke --account <id>, is required")
		os.Exit(accounts.ExitUnreadable)
	}
	os.Exit(accounts.CheckWith(os.Stdout, os.Stderr, *registry, *keys,
		accounts.CheckOptions{JSON: *asJSON, OutPath: *outPath, Now: time.Now().UTC()}))
}
