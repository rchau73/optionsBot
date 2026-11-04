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
	flag.Parse()
	if !*check {
		fmt.Fprintln(os.Stderr, "accounts: -check, or key add|rotate|revoke --account <id>, is required")
		os.Exit(accounts.ExitUnreadable)
	}
	os.Exit(accounts.Check(os.Stdout, os.Stderr, *registry, *keys))
}
