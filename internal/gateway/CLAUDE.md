# internal/gateway

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

- **`internal/gateway`** — every Deribit connection goes through it.
  `PublicOnly()` gateways never authenticate and refuse `private/*` (`ErrPrivateOnPublic`); `WithName` tags their logs (`gateway=trading|mainnet-public`).
  `Call` → priority queue (high lane first) → rate limiter (matching-engine pool for buy/sell/edit/cancel, non-matching pool otherwise) → circuit breaker (skipped for high priority) → socket.
  One writer goroutine never waits for replies; `readLoop` routes replies by request ID.
  Every request gets exactly one answer (reply, RPC error, timeout, `ErrConnectionLost`, `ErrCircuitOpen`).
  Retries only for idempotent methods (`public/*`, `private/get_*`) — never for orders.
  Breaker counts transport failures and exchange-health errors only.
  Callers also hold a hard deadline, so a request that is never sent still times out.
  A refused WebSocket handshake names the endpoint and Deribit's HTTP status and body start (`dialError`, #524: "bad handshake" alone hid a testnet outage for an hour).
  Reconnect runs on the root context; a drop during a reconnect re-arms it (never ignored) and a failed subscription restore is a failed attempt; giving up is reported via `Fatal()`.
  Key scope (#538, `keyscope.go`): each authentication parses Deribit's `scope` with pure `ParseKeyScope` (scopes, `fund_scopes`, `can_move_funds` — nil = unknown, never false) and keeps it for `Gateway.KeyScope()`;
  a key that can move funds (`wallet:read_write`, `account:read_write`, `custody:read_write` — an assumption to verify against docs.deribit.com) logs an ERROR, warn only: the bot still starts.
  `main` also runs a decision-loop watchdog (`strategy.WatchProgress`): no cycle for max(10 × eval interval, 10 min) → exit for a supervised restart (never when halted by the kill switch).
