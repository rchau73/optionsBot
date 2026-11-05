# internal/accounts

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

- **`internal/accounts`** — the account registry (#585, E8, `docs/account-isolation.md` §5 item 1): customer → exchange account → bots, in an owner-only `data/accounts.json`.
  - Records hold opaque ids, labels, the exchange (`exchange.Names`), the environment (testnet|live), a keystore account id (`key_id`, never the key) and each bot's currency and config file name.
  - Unknown JSON fields are refused, so no key, money, position or personal field slips in.
  - `File.Validate` names every break: opaque unique ids, one bot per (account, currency) — the isolation unit A, account × currency — and a key used by one account only.
  - `New`/`Load` join each account to the key store (`keystore.ListFile`, no master key): an account is `blocked` when its key is missing, revoked, can move funds or has an unknown scope (`KeyStatus`, pure).
  - `go run ./cmd/accounts -check` prints one row per account (no key material); exit 0 clean, 1 a rule broken or an account blocked, 2 a file unreadable.
  - `-check -json` (#655, `status.go`) prints `StatusDocument` instead: per account its id, label, exchange, environment, bots and `key_status` (`KeyState`: ok | missing | revoked | can_move_funds | unknown_scope), the check time and the rule breaks by name (`File.Violations`, `Rule*`).
    A fixed field list: no key id, client id, scope string, customer id or free-text reason (a reason or a break's message may quote a key id); an invalid registry lists its breaks and no accounts.
    `-out <path>` also writes it atomically (temp file, rename), owner-only; same exit codes, and an unreadable file writes nothing. The table without `-json` is unchanged.
  - Key lifecycle (#625, `keys.go`, `keycmd.go`, `audit.go`): `go run ./cmd/accounts key add|rotate|revoke --account <id>` — the key from an owner-only JSON file or stdin, never an argument; parse errors never quote it.
    `AddKey`/`RotateKey`/`RevokeKey` take a `KeyStore` (the real store, or a test's failing one); new key ids are `<account>-key-<n>`.
    Rotate = seal the new key, move `key_id`, revoke the old; a failure at any step leaves the account on its old key (the new one revoked again).
    Each change is one owner-only line of `data/accounts_audit.jsonl` (time, OS user, account id, action), opened before anything changes; a refusal is not audited.
  - Not wired: nothing in `cmd/bot`, `scripts/env_lib.sh` or the deploy tooling uses it yet (the isolation unit and the deploy tooling are the product owner's call).
  Tests: `tests/accounts_test.go`, `tests/accounts_keys_test.go`, `tests/accounts_json_test.go`.
