# Account isolation: design

Status: **design for a decision** (#326, with #184 and #254). Nothing here onboards a third party, changes the code or touches `.env` handling. The choice of isolation unit (section 3) is the product owner's, before any implementation story is filed. Parts marked **needs legal review** wait for counsel (PRD §11, `docs/product/legal-brief.md`); this is preparatory analysis, not legal advice.

Claims about Deribit's key scopes carry a source link and the date they were checked, or the label **assumption**. The authoring environment had no network access on 2026-10-27, so nothing below was re-read from Deribit's documentation that day: every Deribit statement is an **assumption** until a person confirms it against <https://docs.deribit.com/> (authentication, `public/auth`, API key scopes).

## 1. Facts from the code: every account-wide action

"Account-wide" means: acts on what the Deribit account (or the currency inside it) holds, not on what this bot opened. Today one bot process owns one account × one currency, and the environments rule keeps two processes off one account. Each row says what happens on a shared account and on a customer account that already holds positions or orders the bot did not open.

| # | Action | Where | Deribit call | On a shared account (two bots) | On a customer account with foreign positions/orders |
|---|---|---|---|---|---|
| 1 | Startup cancels every open order of the currency | `Strategy.reconcilePositions` (`internal/strategy/reconcile.go`, calls `s.exch.CancelAllOrders`) → `Executor.CancelAllOrders` (`internal/orders/executor.go`) | `private/cancel_all_by_currency` | The second bot to start cancels the first bot's working orders (entries, resting closes). Scoped to the currency, so BTC and ETH bots on one account do not touch each other. | Cancels the customer's own manual orders in that currency (limit orders, stop orders, other tools). Not acceptable without consent. |
| 2 | Adoption of every short found | `Strategy.checkPositions`, `adoptPosition`, `logAdopted` (`internal/strategy/orphans.go`); startup regroup in `reconcilePositions` and `assignSlots` (`slotassign.go`) | `private/get_positions` (`Executor.GetPositions`) | The bot adopts the other bot's shorts as its own, then manages, rolls, stops and balances them. Two managers on one position. | Adopts the customer's own short options (a covered strategy, a hedge) and may stop out, roll or buy them back. The book then differs from the intent of the owner. |
| 3 | Position check against the book | `Strategy.checkPositions` (`orphans.go`); `capToExchange` and the long-position rule in `safety.go` | `private/get_positions` | The exchange's size differs from the book's whenever the other bot trades; a difference on two cycles is adopted, so the bots chase each other. | A foreign long on an instrument the bot trades is sold at the bid (`close_long`, `safety.go`) only if the bot traded it before; other longs are left alone. A foreign short is adopted (row 2). |
| 4 | Unknown outcome: cancel by label | `Strategy.resolveUnconfirmed` (`orphans.go`), labels from `uniqueOrderLabel` / `orderLabel` (`internal/strategy/snapshot.go`) | `private/cancel_by_label` (`Executor.CancelByLabel`) | Safe between bots only while their labels differ: the label is `strategy_id:DTEd:delta:o-N` and `o-N` counts per process, so two bots with the same `strategy_id` and counter can collide on `cancel_by_label`. | Safe against the customer's orders (they carry no such label), assuming the label prefix is unique per bot. Not unique across customers unless the account is the scope (assumption: labels are per account). |
| 5 | Kill switch cancel-all | `Strategy.killSwitch` (`internal/strategy/killswitch.go`, `CancelAllOrders`) | `private/cancel_all_by_currency` | Cancels the other bot's orders as well. | Cancels all of the customer's orders in the currency. |
| 6 | Kill switch flatten | `killSwitch` → `syncBookToExchange` (`orphans.go`) → market buy-backs | `private/get_positions`, `private/buy` | `syncBookToExchange` copies the exchange's shorts into the book "at once, no drift confirmation", so the other bot's shorts are bought back. | Buys back every short option of the currency, including ones the customer opened. The one action with direct financial effect on foreign positions. |
| 7 | Account polling | `internal/account` (`Poller.fetch`, `attachPositions`; started in `cmd/bot/main.go`) | `private/get_account_summaries`, `private/get_positions` (all kinds, per held currency) | Read-only. Shows the other bot's positions as unmanaged margin users. | Read-only and useful: foreign positions are visible, and their margin is part of the IM the policy reads. |
| 8 | Margin policy reads account figures | `internal/risk`, `strategy/limits.go` (`AccountSummary.MarginUsage`) | account summary, `private/simulate_portfolio` | IM and MM include the other bot's book; each bot sizes against margin it did not create. | Sizing sees the customer's foreign margin and shrinks or freezes; the MM cut (`max_mm_pct`) buys back only bot-owned positions, so foreign margin can keep MM high with nothing the bot may close. |
| 9 | Rate limits | `internal/gateway` (`RateLimiter`, two pools: matching engine / other) | all | The pools are per process; two bots on one account share Deribit's per-account limit without coordinating (`docs/architecture.md` §14). | Per account (assumption). One process per account keeps each account's pool its own. |
| 10 | Environments refuse a shared client id | `env_check` in `scripts/env_lib.sh` (#538: `env_accounts_distinct` reads `DERIBIT_CLIENT_ID` of every environment folder's `.env`; "each environment needs its own subaccount", naming both folders) | none (local script) | The guard that exists: no two environment folders of the platform (stable, mvp and any other bot's clone under `<envs>`) may use one Deribit (sub)account. | Same idea, for every bot folder on this host; a customer bot run elsewhere is not seen. |

Also account-wide but not an order action: the startup `private/get_positions` regroup (`GroupPositions`) pairs positions whatever opened them, and the journal is per process (`data/<coin>/orders.log`), so a foreign position has no journal history and no slot hint.

Summary: rows 1, 2, 5 and 6 are the dangerous ones on a customer account. They assume the account belongs to the bot.

### What the code does not give us today

- No marker of which positions the bot opened. Positions on Deribit have no label; only orders do. Ownership can come only from the journal (`orders.log` fills) or from a dedicated account.
- The key-scope check warns, it does not refuse (#538).
  `Gateway.authenticate` parses the reply's scope with `gateway.ParseKeyScope` (`internal/gateway/keyscope.go`): the granted scopes, the ones that can move funds and `can_move_funds` (null when the scope is empty or unreadable: unknown, never false).
  A key that can move funds logs an ERROR at every authentication ("withdrawal rights are refused by policy"); `/api/status` reports `key_scope` and every start journals one `key_scope` line — scope names only, never the client id, secret or token.
  The bot still starts: refusing to start on a broad key is a decision for a person, with legal input (**needs legal review**).
  The scopes counted as moving funds are `wallet:read_write`, `account:read_write` (it can create API keys) and `custody:read_write` — an **assumption** (from memory of <https://docs.deribit.com/> "Access scope", 2026-10-30, no network access): a person confirms it against the page.

## 2. Isolation options

The unit of isolation is the thing that owns one set of credentials and one book.

### A. One bot process per account × currency (today's shape, per customer)

Each customer account gets its own container(s), its own secret, its own `data/` directory, its own rate-limit pools and its own monitor port or tenant route.

### B. One process with several accounts

One bot process holds N credential sets, N gateways, N books and N strategies, and a tenant router in front of the API.

| | A. Process per account × currency | B. One process, several accounts |
|---|---|---|
| Blast radius | A crash, bug, memory leak or kill switch touches one account. A bad deploy is rolled out account by account. | One panic, deadlock or leak stops every customer. A shared-state bug can cross accounts (a wrong map key places an order on another account). |
| Secrets | One secret per process, injected at start (env or secret mount), never co-resident with other customers' keys. A process dump leaks one key. | N keys in one address space and one decrypt path; a single compromise exposes all. Needs an in-process key vault. |
| Rate-limit pools | Per account by construction (`RateLimiter` is per gateway). No customer starves another. | Needs one limiter per account inside the process; a mistake shares a pool. Deribit's own limit is per account (assumption), so a shared limiter would be wrong. |
| Restart cost | Restart one account; startup reconcile (rows 1–3) touches only that account. Cost is a process per account (small memory for a Go bot; market-data and GEX subscriptions are duplicated per process). | One restart reconciles every account in sequence; one slow account delays the others. Fewer duplicated subscriptions. |
| Monitoring | Per-process `/api/status`, as stable/mvp do today; the monitor needs an account selector and an aggregator (E7). | One API, one place for tenant data; needs per-tenant authorisation inside the process. |
| Code change | Small: config and deploy tooling (the stable/mvp pattern generalised); `internal/exchange` already isolates the exchange. | Large: the strategy, state and journal assume one account (one `orders.log`, one `data/` path). |
| Compliance fit | Easy to show "this customer's data and key live only here" (**needs legal review**). | Harder to evidence separation. |

Public market data (option chain, DVOL, GEX from mainnet open interest) is account-independent; process-per-account duplicates it. A later optimisation, not part of this decision: one shared read-only market-data/GEX service feeding the account processes, since the GEX gateway is already `PublicOnly()`.

### Recommendation: A, one process per account × currency

1. The fault lines in section 1 (cancel-all, adoption, flatten) act on a whole account; a process boundary equal to the account boundary makes "never touch another's orders" a property of the deployment, not of every code path.
2. Secrets stay per process, so a compromise or a bug exposes one key.
3. It is the shape the platform already runs (stable/mvp, BTC and ETH as separate bots) and `env_check` already enforces "one account per environment"; the change is to generalise it.
4. Rate-limit pools are already per gateway, per account.
5. The cost of restarts and duplicated subscriptions is small at the expected scale and measurable; B can be revisited if the process count becomes the problem.

**Needs the person:** the choice of unit. No implementation story is filed before it.

### Required whichever unit is chosen

- **One account per bot, enforced.** Generalise `env_check`: refuse a second bot on a `DERIBIT_CLIENT_ID` (or account id) that another bot of the platform uses. For customers this means a dedicated account or subaccount for the bot (assumption: Deribit subaccounts keep orders and positions separate; confirm against the docs).
- **Prefer a dedicated (sub)account the customer funds for the bot.** Then rows 1, 2, 5, 6 act only on bot-owned positions and the danger does not arise. On a shared main account the bot must not run rows 1, 2, 5, 6 unscoped (below).
- **Label-scoped orders** where an account is shared: cancel only by label (`cancel_by_label`, row 4) instead of `cancel_all_by_currency` at startup and in the kill switch, with a platform-unique label prefix per bot (customer/account id in the label, within Deribit's 64 characters — the code already truncates there; the limit itself is an assumption).
- **Journal-owned positions.** Adoption and the kill-switch flatten should act only on instruments the journal shows the bot traded; a foreign position is reported (monitor), never managed. This is a rule change on the order path: it needs its own story, an `OptionStrategy.md` §9 update, a stress/replay check and a person's approval.
- **Startup guard on a non-empty account:** a bot connecting to an account with foreign open orders or positions stops with a clear message and waits for the owner, instead of cancelling or adopting.

## 3. Decision requested

| Question | Options | Recommendation |
|---|---|---|
| Isolation unit | A (process per account × currency) / B (one process, several accounts) | A |
| Customer account shape | dedicated (sub)account only / also shared main accounts | dedicated only for the first customers |
| Foreign positions on connect | stop and report / manage only journal-owned | stop and report (safest); journal-owned later |

## 4. Key handling requirements

Statements about Deribit's behaviour are **assumption** unless a source and date follow. Source to check: <https://docs.deribit.com/> (authentication and access scopes), not re-read on 2026-10-27.

1. **Trade-only.** Scopes needed: read account and read/write trade (the auth error text in `gateway.go` names `account:read` and `trade:read_write`; assumption that these are Deribit's scope names). **Withdrawal refused:** the wallet scope must be none or read-only (assumption: `wallet:none` / `wallet:read`; `wallet:read_write` allows withdrawals).
2. **Check at connect time from Deribit's own reply.** `public/auth` returns the granted `scope` string (already received in `Gateway.authenticate`). Parse it with a pure function (`KeyScopeOf(scope, now)`: trade, wallet, account levels, expiry; unknown tokens ignored), keep the last result on the trading gateway only (public-only gateways have none) and expose a `key_scope` object in `/api/status` (`trade`, `wallet`, `withdraw_rights`, `can_trade`, `checked_at`). Never expose the raw scope string or any key identifier.
3. **Warn, then refuse.** Report-only first: a WARN per authentication when withdrawal rights are present, an ERROR when trade is missing. For customers the bot must **refuse to start** with a withdraw-enabled key; for the platform's own running bots that is a person's call, because it could stop a live bot (product owner go/no-go).
4. **Encrypted at rest.** Keys stored encrypted (a data key per secret, the master key from the environment or a secret manager, never in the repository or `.env.example`); decrypted only into the one process that needs it; never logged (the repository already refuses secrets in commits: `make hooks`, gitleaks).
5. **Revocable and rotatable.** The customer can revoke the key at Deribit at any time; an authentication failure after a working session is treated as "key revoked": stop new risk and alert (nothing can be sent anyway). Rotation = replace the secret and restart that one account's process (the startup reconcile of section 1 then applies, so the startup guard matters). Whether Deribit keys expire by themselves is an assumption to confirm.
6. **No key, client id or raw scope in logs, journal or API.**
7. **Per-tenant view in the monitor:** account label, key-scope status, last authentication time, environment (live|testnet) — never the key.

**Needs legal review:** custody and control of a customer's key (who may hold it, for how long), the authorisation text for the API key, data-protection treatment of key material, and any claim that the key "cannot move funds" (L21/L22 in the legal brief depend on it).

## 5. Ownership of the work

| Topic | Epic / gate |
|---|---|
| Users, roles, tenant authorisation in the monitor, an account selector | **E7** (accounts and roles; builds on #170, #171) |
| Account registry (customer → account → bot process), deploy tooling per account, generalised `env_check` | **E8** |
| Key vault / encryption at rest, `KeyScopeOf` and `key_scope`, refusing withdrawal-enabled keys | **E8** (with E7 for who may add or revoke a key) |
| Label-scoped cancel, journal-owned adoption and flatten, startup guard on a non-empty account | **E8**, order path: a person's approval, `OptionStrategy.md` §9 |
| Custody, key authorisation, terms, onboarding a third party, regulated-activity analysis | **Legal gate (PRD §11) — needs legal review** |

Implementation stories this implies (to be filed only after the unit is chosen):

1. Account registry and per-account process deploy (generalise `env_check` to refuse a shared account for any two bots).
2. `KeyScopeOf` and `key_scope` in `/api/status` (report only), then refusal of withdraw-enabled keys for customer accounts.
3. Encrypted key storage and the rotation/revocation flow.
4. Startup guard: stop and report on foreign orders/positions.
5. Label-scoped startup cancel and kill-switch cancel; platform-unique label prefix.
6. Journal-owned adoption and flatten (stress/replay study first).
7. Monitor per-tenant view (with E7).

## 6. Not decided here

Onboarding any third party, Deribit's exact scope names and account semantics (to be read from the docs), multi-exchange keys (`internal/exchange`), pricing and legal text.
