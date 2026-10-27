# Empty-state inventory and copy for every monitor panel (E11)

Every panel of the monitor, every way it can be empty or unavailable, the plain-words text to show, and the field that tells the cases apart (#250). Content only: no UI change here; E11's UI story will read this file and `frontend/content/empty-states.json`.

## How to read it

- **The copy is the catalogue's.** The text in each table is the sentence in `frontend/content/empty-states.json` for that panel and state (`emptyStateFor(panel, state)`, #298). `frontend/__tests__/education-docs.test.js` fails when a table and the catalogue differ, so change the JSON first and copy it here. Each text is at most two short sentences.
- **Three conditions at least, for every panel:** *no data yet* (`loading`: the first answer has not come), *empty because normal* (`empty`: the bot answered and there is nothing to list yet) and *unavailable* (`error`: the bot did not answer or the figure could not be read). `stale` (an answer that stopped coming) is added for every panel too.
- **Honesty rules.** The copy never implies a return. A freeze or a hold says what the bot is waiting for. **An unreachable bot never looks like "nothing held":** the *unavailable* copy says that nothing is shown rather than a guess, and the panel is not drawn as an empty list.
- **Field** names the API answer that tells the condition apart: a field of `/api/status` (the bot's view; `as_of`, `loop_at`, `halted`, `risk`, `held_legs`, `strangles`, `pending`), another route, or "no answer" when the request itself failed or timed out. A failed request is a different case from a field that is present and empty.
- **Terms.** A word that has a tip (`frontend/content/glossary.json`) is named in the panel's *Tips* line, so the UI can link it.

## Panels

### monitor.header

Tips: `bot`. Bots are the ones the monitor's BOT_APIS setting lists; the chip shows TESTNET or LIVE.

| Condition | Copy | Field |
|---|---|---|
| `empty` · normal | No bot is configured for this monitor yet. Each bot appears here as a chip once BOT_APIS lists it. | the monitor's `BOT_APIS` lists no bot |
| `loading` · no data yet | Connecting to the bots. Each chip shows its spot, DVOL and data age after the first answer. | no answer yet from `/api/status` |
| `stale` · answers stopped | No update from this bot in the last seconds. The figures shown are the last ones received; the monitor keeps asking. | `/api/status` `as_of` older than a few polls |
| `error` · bot unreachable | The bot is not answering (OFFLINE). Nothing on this page updates for it until it answers again. | no answer from `/api/status` |

### monitor.gex

Tips: `gex`, `gamma regime`, `gamma flip`.

| Condition | Copy | Field |
|---|---|---|
| `empty` · normal | GEX not computed yet. The bot builds it every 60 s from mainnet open interest; new entries and repairs wait until then. | `/api/status` `market` has no GEX figure, `risk.entries_allowed` false |
| `loading` · no data yet | Waiting for the bot's first GEX snapshot. Regime, trend and gamma flip appear once it is built. | `/api/status` `market` has no GEX figure after a start |
| `stale` · answers stopped | The last GEX snapshot is over 5 minutes old. New entries, top-ups and repairs wait; exits keep running. | GEX snapshot age over 5 minutes |
| `error` · unavailable | GEX could not be read from this bot. Regime and flip show as missing, never as a guess. | no answer from `/api/status` |

### monitor.summary

Tips: `strangle`, `income over months`, `realized p&l`. The investor summary card, one per bot.

| Condition | Copy | Field |
|---|---|---|
| `empty` · normal | Nothing held and no trade yet. This card fills in after the bot's first entry or close. | `/api/summary` `holding` zero strangles, `last_action` null |
| `loading` · no data yet | Loading the summary: what the bot holds, its monthly income, the worst case now and its last action. | no answer yet from `/api/summary` |
| `stale` · answers stopped | The bot stopped answering. The summary shown is from its last answer and is not refreshed. | `/api/summary` request failing after an earlier answer |
| `error` · bot unreachable | Summary unavailable: the bot did not answer. No numbers are shown rather than old or partial ones. | no answer from `/api/summary` |

### monitor.kpi

Tips: `p&l`, `close`, `skipped`. The strip of counts since the journal began.

| Condition | Copy | Field |
|---|---|---|
| `empty` · normal | No counts yet. Orders, closes and P&L are counted from the first decision the bot journals. | `/api/events` and `/api/trades` empty lists |
| `loading` · no data yet | Loading the counts from each bot's journal. They are restored when a bot restarts. | no answer yet from `/api/events` |
| `stale` · answers stopped | At least one bot is not updating. Its counts are the last ones received. | `/api/status` `as_of` older than a few polls |
| `error` · unavailable | The counts could not be read. Missing figures show as a dash, never as 0. | no answer from `/api/events` or `/api/trades` |

### monitor.panel.account

Tips: `margin`, `im`, `mm`, `equity`. Account and collateral, as Deribit reports it.

| Condition | Copy | Field |
|---|---|---|
| `empty` · normal | No account data yet. Deribit's equity and margin appear here after the bot's first account poll. | `/api/account` has no summary before the first poll |
| `loading` · no data yet | Loading the account summary the bots read from Deribit. Margin figures are Deribit's, not recomputed. | no answer yet from `/api/account` |
| `stale` · answers stopped | The account figures are older than the bot's poll interval. They are the last ones Deribit reported. | `/api/account` `as_of` older than the poll interval |
| `error` · unavailable | Account not loaded: the bot could not read it. New entries wait while margin is unknown; exits keep running. | `/api/status` `risk.error` set |

### monitor.panel.positions

Tips: `position`, `strangle`, `leg`. Open positions, one row per leg.

| Condition | Copy | Field |
|---|---|---|
| `empty` · normal | No open strangle yet. A row appears once an entry order fills on a vacant slot. | `/api/status` `strangles` empty and `pending` empty |
| `loading` · no data yet | Loading the bots' open positions. Rows show each leg with its mark, P&L and Greeks. | no answer yet from `/api/positions` |
| `stale` · answers stopped | Positions from a bot that stopped updating. Marks and P&L are the last ones received. | `/api/status` `as_of` older than a few polls |
| `error` · unavailable | Positions could not be read. The bot keeps managing them; this view shows nothing rather than a guess. | no answer from `/api/positions` |

### monitor.panel.held

Tips: `repair`, `missing leg`, `held`. Missing legs and the rule holding each repair.

| Condition | Copy | Field |
|---|---|---|
| `empty` · normal | No missing leg. A row appears when a strangle is left with one leg and the bot holds its repair. | `/api/status` `held_legs` empty |
| `loading` · no data yet | Loading the missing legs and the rule holding each repair. | no answer yet from `/api/status` |
| `stale` · answers stopped | The bot stopped updating. The holds shown are the last ones it reported. | `/api/status` `as_of` older than a few polls |
| `error` · unavailable | Missing legs could not be read. The bot's repair rules keep running without this view. | no answer from `/api/status` |

### monitor.panel.others

Tips: `position`, `margin`. Positions on the account that no bot manages.

| Condition | Copy | Field |
|---|---|---|
| `empty` · normal | No position outside the bots. Any other position on the account appears here, since it uses margin. | `/api/account` positions list empty |
| `loading` · no data yet | Loading the account's other positions from Deribit. | no answer yet from `/api/account` |
| `stale` · answers stopped | The list is older than the bot's account poll. It shows the last positions Deribit reported. | `/api/account` `as_of` older than the poll interval |
| `error` · unavailable | Other positions unavailable: the list could not be read. This is not the same as none. | `/api/account` positions error |

### monitor.panel.trades

Tips: `close`, `realized p&l`, `reason`. Trade history and profitability.

| Condition | Copy | Field |
|---|---|---|
| `empty` · normal | No trade yet. Opens and closes appear here from the first fill, with realised P&L from the first close. | `/api/trades` empty list |
| `loading` · no data yet | Loading the trade history from each bot's journal. | no answer yet from `/api/trades` |
| `stale` · answers stopped | A bot stopped updating. Its trades are listed up to its last answer. | `/api/status` `as_of` older than a few polls |
| `error` · unavailable | The trade history could not be read. The journal on the bot is unchanged. | no answer from `/api/trades` |

### monitor.panel.activity

Tips: `skipped`, `filled`, `cancelled`, `freeze`. The feed of events.

| Condition | Copy | Field |
|---|---|---|
| `empty` · normal | No activity yet. Every order, fill, close, skip and margin change the bot journals appears here. | `/api/events` empty list |
| `loading` · no data yet | Loading the bots' recent events from their journals. | no answer yet from `/api/events` |
| `stale` · answers stopped | A bot stopped updating. The feed resumes from where it stopped once the bot answers. | `/api/status` `as_of` older than a few polls |
| `error` · unavailable | Events could not be read. The bot keeps journaling them; they appear here once it answers. | no answer from `/api/events` |

### monitor.panel.pnl

Tips: `p&l`, `realized p&l`. The P&L chart in USD, all bots.

| Condition | Copy | Field |
|---|---|---|
| `empty` · normal | No P&L history yet. The bot writes a point each report interval; the first point starts this chart. | `/api/pnl/history` has no point in the range |
| `loading` · no data yet | Loading the stored P&L history for the selected range. | no answer yet from `/api/pnl/history` |
| `stale` · answers stopped | No new P&L point lately. The chart ends at the last point a bot reported. | last point of `/api/pnl/history` older than the report interval |
| `error` · unavailable | P&L history could not be read. Missing time is left blank, never filled in. | no answer from `/api/pnl/history` |

### monitor.panel.pnl_coin

Tips: `p&l`. One chart per bot, in its own coin.

| Condition | Copy | Field |
|---|---|---|
| `empty` · normal | No P&L history in coin yet. Each bot's chart starts at its first stored point. | `/api/pnl/history` has no point for the bot |
| `loading` · no data yet | Loading each bot's P&L in its own coin. | no answer yet from `/api/pnl/history` |
| `stale` · answers stopped | No new point lately. Each chart ends at the last point its bot reported. | last point older than the report interval |
| `error` · unavailable | P&L in coin could not be read for this bot. Missing time is left blank, never filled in. | no answer from `/api/pnl/history` |

### monitor.panel.orders

Tips: `working order`, `fill timeout`, `limit price`. Orders resting on Deribit.

| Condition | Copy | Field |
|---|---|---|
| `empty` · normal | No working order. Entry and repair orders show here while they rest on Deribit, until filled or cancelled. | `/api/orders` empty list |
| `loading` · no data yet | Loading the orders the bots have resting on Deribit. | no answer yet from `/api/orders` |
| `stale` · answers stopped | A bot stopped updating. Its working orders are the last ones it reported. | `/api/status` `as_of` older than a few polls |
| `error` · unavailable | Working orders could not be read. Unfilled orders are still cancelled by the bot after the fill timeout. | no answer from `/api/orders` |

## Screens with their own messages

The income page and the worst-case page already say, in their own words, what is missing and why; they are described in `frontend/README.md` ("Income page", "Worst case right now") and are not in the catalogue: loading ("not yet"), unavailable (with the reason), a failed refresh (keeps the last figures and says so), nothing held or no `?bot=`. A missing figure is "not yet" or "unknown", never 0. Signed in without data (a new person on a bot with no history) is the *no data yet* and *empty because normal* rows of the panels above, not a separate message.
