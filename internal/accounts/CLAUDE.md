# internal/accounts

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

- **`internal/accounts`** — the account registry (#585, E8, `docs/account-isolation.md` §5 item 1): customer → exchange account → bots, in an owner-only `data/accounts.json`.
  - Records hold opaque ids, labels, the exchange (`exchange.Names`), the environment (testnet|live), a keystore account id (`key_id`, never the key) and each bot's currency and config file name.
  - Unknown JSON fields are refused, so no key, money, position or personal field slips in.
  - `File.Validate` names every break: opaque unique ids, one bot per (account, currency) — the isolation unit A, account × currency — and a key used by one account only.
  - `New`/`Load` join each account to the key store (`keystore.ListFile`, no master key): an account is `blocked` when its key is missing, revoked, can move funds or has an unknown scope (`KeyStatus`, pure).
  - `go run ./cmd/accounts -check` prints one row per account (no key material); exit 0 clean, 1 a rule broken or an account blocked, 2 a file unreadable.
  - Read-only: nothing in `cmd/bot`, `scripts/env_lib.sh` or the deploy tooling uses it yet (the isolation unit and the deploy tooling are the product owner's call).
  Tests: `tests/accounts_test.go`.
