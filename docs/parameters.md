# Tunable parameters

<!-- Generated from internal/config/catalogue.go: go run ./cmd/paramdocs. Do not edit by hand; tests/config_catalogue_test.go fails until it is regenerated. -->

Every setting a `config_*.yaml` file may hold, in plain words. Everything else is fixed in code (`internal/config/fixed.go`); a file that sets a fixed key fails to load. The range is what `Validate` accepts when the bot loads the file; "not checked" means it accepts any value. The texts are a first draft (#255).

**Core policies.** A setting whose policy is not `none` belongs to a core policy of the strategy: **a change to its default needs the product owner** (PRD §3). Test a change first with the tools listed under "Tested by".

| Key | Title | Policy | Range | Unit |
|---|---|---|---|---|
| `underlying` | Coin | none | not checked | — |
| `strategy_id` | Strategy name | none | not checked | — |
| `dte_delta_matrix[].dte` | Slot days to expiry | entry | see "Also" | days |
| `dte_delta_matrix[].deltas[]` | Slot entry delta | entry | 0.01 to 0.49 (step 0.01) | delta (absolute) |
| `rollout_dte` | Roll at days to expiry | exits | see "Also" | days |
| `delta_drift_threshold` | Roll when delta falls below | exits | ≥ 0 (step 0.01) | delta (absolute) |
| `roi_take_profit` | Take profit at | exits | not checked | fraction of premium |
| `stop_loss_multiplier` | Stop-loss at | exits | ≥ 0.1 (step 0.1) | × premium |
| `delta_exit_threshold` | Delta exit at | exits | ≤ 0.99 (step 0.01) | delta (absolute) |
| `repair_cooldown_hours` | Repair cooldown | repair | ≥ 0 (step 1) | hours |
| `gamma_flip_buffer_sd` | Gamma flip buffer | gex | 0 to 3 (step 0.1) | daily standard deviations |
| `gex_direction` | Direction source | gex | sma \| bias | — |
| `iv_margin_bands[].min_iv_pct` | Margin band start | margin | 0 to 100 (step 1) | DVOL percentile |
| `iv_margin_bands[].max_im_pct` | Margin band limit | margin | 1 to 100 (step 1) | % of margin balance |
| `max_mm_pct` | Maintenance-margin limit | margin | 1 to 99 (step 1) | % of margin balance |
| `max_leg_size_multiple` | Leg size cap | sizing | ≥ 1 (step 0.1) | × normal size |
| `min_premium` | Minimum premium | entry | not checked | coin per contract |
| `backtest.fill_model` | Backtest fill model | none | not checked | — |
| `backtest.slippage_pct` | Backtest market slippage | none | not checked | fraction of mid |
| `backtest.limit_fill_rule` | Backtest limit fills | none | not checked | — |
| `backtest.commission_per_contract` | Backtest fee rate | none | not checked | fraction of the underlying |
| `backtest.margin_call_liquidation` | Backtest margin-call liquidation | none | true \| false | — |

## `underlying` — Coin

The coin this bot trades (BTC or ETH). There is one config file per coin; it names the bot rather than tunes it.

- **Range:** not checked
- **Policy:** none

## `strategy_id` — Strategy name

Names the strategy in the journal and in order labels. The shipped files leave it unset; it does not change any trading rule.

- **Range:** not checked
- **Policy:** none

## `dte_delta_matrix[].dte` — Slot days to expiry

Each slot sells one strangle on an expiry about this many days away. Longer: more premium per trade and larger moves to sit through; shorter: faster decay, more rolls and fees.

- **Range:** see "Also"
- **Unit:** days
- **Also:** above rollout_dte, or a new strangle would be rolled at once
- **Policy:** entry — a change to its default needs the product owner
- **Tested by:** backtest --sweep; backtest --sweep-ladders; cmd/stress -slots; cmd/compare (two configs: backtest, replay, stress)

## `dte_delta_matrix[].deltas[]` — Slot entry delta

How far from the price each leg is sold, as option delta (0.16 is about one standard deviation away). Higher: more premium, and moves reach the strikes more often; lower: less premium, fewer stops.

- **Range:** 0.01 to 0.49 (step 0.01)
- **Unit:** delta (absolute)
- **Also:** delta_drift_threshold at most 75 % of it
- **Also:** delta_exit_threshold above it
- **Policy:** entry — a change to its default needs the product owner
- **Tested by:** backtest --sweep; cmd/stress -slots DTE:delta, -slotdelta; cmd/compare (two configs: backtest, replay, stress)

## `rollout_dte` — Roll at days to expiry

A strangle is bought back when its expiry is this close, and its slot opens a new one. Higher: leaves the riskier last weeks earlier and keeps less of the time decay.

- **Range:** see "Also"
- **Unit:** days
- **Also:** below every slot's dte
- **Policy:** exits — a change to its default needs the product owner
- **Tested by:** backtest --sweep; cmd/compare (two configs: backtest, replay, stress)

## `delta_drift_threshold` — Roll when delta falls below

A leg whose |delta| falls below this has little premium left: it is bought back and re-sold nearer the entry delta. Higher: rolls sooner and more often, paying spread and fees for fresher premium.

- **Range:** ≥ 0 (step 0.01)
- **Unit:** delta (absolute)
- **Also:** at most 75 % of every entry delta (config.MaxDriftToEntryRatio)
- **Policy:** exits — a change to its default needs the product owner
- **Tested by:** cmd/stress -drift; cmd/compare (two configs: backtest, replay, stress)

## `roi_take_profit` — Take profit at

A leg is bought back once it has earned this share of its premium (0.50 = half) and is re-sold. Lower: takes gains sooner and trades more; higher: holds longer for the rest, exposed to moves meanwhile.

- **Range:** not checked
- **Unit:** fraction of premium
- **Policy:** exits — a change to its default needs the product owner
- **Tested by:** backtest --sweep; cmd/stress -tp; cmd/compare (two configs: backtest, replay, stress)

## `stop_loss_multiplier` — Stop-loss at

A leg is bought back at market when its loss reaches this multiple of its premium (2 = twice the premium). Lower: cuts losses earlier with more stops on noise; higher: fewer stops, larger losses when one fires.

- **Range:** ≥ 0.1 (step 0.1)
- **Unit:** × premium
- **Policy:** exits — a change to its default needs the product owner
- **Tested by:** backtest --sweep; cmd/compare (two configs: backtest, replay, stress)

## `delta_exit_threshold` — Delta exit at

A leg whose |delta| reaches this is bought back before the stop-loss and re-sold only after the repair cooldown. Lower: exits earlier on smaller moves; higher: waits longer, closer to the stop.

- **Range:** ≤ 0.99 (step 0.01)
- **Unit:** delta (absolute)
- **When absent:** 0.30 (also when 0)
- **Also:** above every entry delta
- **Policy:** exits — a change to its default needs the product owner
- **Tested by:** cmd/stress -breach; cmd/compare (two configs: backtest, replay, stress)

## `repair_cooldown_hours` — Repair cooldown

After a stop-loss, delta exit or manual close, the leg is re-sold no sooner than this, and only while entries are not frozen and the gamma regime is not negative. Longer: less re-selling into a move, more time one-sided.

- **Range:** ≥ 0 (step 1)
- **Unit:** hours
- **When absent:** 72 (also when 0)
- **Policy:** repair — a change to its default needs the product owner
- **Tested by:** cmd/compare (two configs: backtest, replay, stress)

## `gamma_flip_buffer_sd` — Gamma flip buffer

In a negative gamma regime a leg is shed only once spot is this many daily standard deviations beyond the gamma flip. Higher: fewer sheds and more exposure in a falling market; lower: sheds sooner and more often.

- **Range:** 0 to 3 (step 0.1)
- **Unit:** daily standard deviations
- **When absent:** 1 (also when 0)
- **Policy:** gex — a change to its default needs the product owner
- **Tested by:** cmd/stress -flipbufsd; cmd/compare (two configs: backtest, replay, stress)

## `gex_direction` — Direction source

Which side to avoid in a negative gamma regime: sma (the trend of daily moving averages) or bias (spot against the gamma flip, as the reference script). It decides which legs are shed and when they are re-sold.

- **Range:** sma \| bias
- **When absent:** sma
- **Policy:** gex — a change to its default needs the product owner
- **Tested by:** cmd/stress -direction; cmd/compare (two configs: backtest, replay, stress)

## `iv_margin_bands[].min_iv_pct` — Margin band start

Where a margin band starts, as the percentile of today's DVOL in the last year. With max_im_pct it sets how much initial margin the bot may use when volatility is rich or calm.

- **Range:** 0 to 100 (step 1)
- **Unit:** DVOL percentile
- **When absent:** bands 70 / 30 / 0 at 50 / 35 / 20 %
- **Also:** the lowest band starts at 0
- **Also:** no two bands start at the same percentile
- **Policy:** margin — a change to its default needs the product owner
- **Tested by:** cmd/stress -bands (three bands); cmd/compare (two configs: backtest, replay, stress)

## `iv_margin_bands[].max_im_pct` — Margin band limit

The most initial margin the bot may use in that band. Higher: larger positions and more premium, and larger losses in a move. A confirmed negative gamma regime uses the lowest band.

- **Range:** 1 to 100 (step 1)
- **Unit:** % of margin balance
- **When absent:** bands 70 / 30 / 0 at 50 / 35 / 20 %
- **Policy:** margin — a change to its default needs the product owner
- **Tested by:** cmd/stress -bands (three bands); cmd/compare (two configs: backtest, replay, stress)

## `max_mm_pct` — Maintenance-margin limit

Above this maintenance margin the bot reduces positions at market at once and opens nothing new; Deribit liquidates at 100. Higher: more room before forced cuts, less before liquidation.

- **Range:** 1 to 99 (step 1)
- **Unit:** % of margin balance
- **When absent:** 35 (also when 0)
- **Policy:** margin — a change to its default needs the product owner
- **Tested by:** cmd/compare (two configs: backtest, replay, stress)

## `max_leg_size_multiple` — Leg size cap

A new leg is at most this many times its slot's normal size (its margin share ÷ the strangle's own margin per lot), so offsets in the book cannot inflate a size. Higher: larger legs when the book offsets them.

- **Range:** ≥ 1 (step 0.1)
- **Unit:** × normal size
- **When absent:** 2 (also when 0)
- **Policy:** sizing — a change to its default needs the product owner
- **Tested by:** cmd/stress -cap; cmd/compare (two configs: backtest, replay, stress)

## `min_premium` — Minimum premium

The least premium a leg must bring at the lowest price its order can step down to; a leg under it is not sold, and 0 accepts any. Higher: skips thin strikes, less income when premiums are low.

- **Range:** not checked
- **Unit:** coin per contract
- **Policy:** entry — a change to its default needs the product owner
- **Tested by:** cmd/compare (two configs: backtest, replay, stress)

## `backtest.fill_model` — Backtest fill model

Backtest only: mid, bid_ask or slippage. The simulator does not read it today; the trading bot never does.

- **Range:** not checked
- **Policy:** none
- **Tested by:** backtest

## `backtest.slippage_pct` — Backtest market slippage

Backtest only: a simulated market order fills this fraction of the mid worse than the mid. No effect on trading.

- **Range:** not checked
- **Unit:** fraction of mid
- **Policy:** none
- **Tested by:** backtest

## `backtest.limit_fill_rule` — Backtest limit fills

Backtest only: next_tick fills a simulated limit order on the next tick; any other value fills it at once. No effect on trading.

- **Range:** not checked
- **Policy:** none
- **Tested by:** backtest

## `backtest.commission_per_contract` — Backtest fee rate

Backtest only: the fee per contract as a fraction of the underlying (0.0003 = Deribit's 0.03 %), capped at 12.5 % of the price. No effect on trading.

- **Range:** not checked
- **Unit:** fraction of the underlying
- **Policy:** none
- **Tested by:** backtest

## `backtest.margin_call_liquidation` — Backtest margin-call liquidation

Backtest only. The simulator does not read it today; the trading bot never does.

- **Range:** true \| false
- **Policy:** none
- **Tested by:** backtest
