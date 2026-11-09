# Tip placement map (E11, #728)

Which tip and which video belongs on which monitor page, so the first tip screen can be built the day its prototype is approved.
No screen uses it yet.

How to read it:
- One `##` section per page of the menu (`frontend/lib/menu.js`), named by the menu item's key, in menu order.
- Each row is `| spot | kind | key | video |`, in alphabetical (byte) order of the key.
  - `kind` is `term` (the key is a glossary term in `frontend/content/glossary.json`, which is its own tip) or `setting` (the key is a tip in `tips.json`).
  - `spot` is where the tip sits: a header, a tile, a column, a value, a chart or an empty state.
  - `video` is a `video.<id>` of `videos.json`, or `-` when none fits.
- `Roles:` lists who sees the page. Roles are user, manager and admin (`frontend/docs/users-and-roles.md`).
- The last section lists every term and setting that is on no page, with the reason, so none is dropped silently.
- A term shown on several pages has a row on each.
- A glossary entry named like a setting (`delta_exit_threshold`) is covered by that setting's row.
- `tests/tip_placement_test.go` fails, naming the page and the key, on a menu page with no section, a term or setting with no tip, a video id missing from `videos.json`, a key out of order, and a term or setting that is neither on a page nor in the last section.

## overview

Roles: user, manager, admin.

| spot | kind | key | video |
| --- | --- | --- | --- |
| tile "Net delta", value | term | delta | video.read_summary |
| strangle row, days left | term | dte | - |
| band header, DVOL chip | term | dvol | video.read_summary |
| strangle row, expiry | term | expiry | - |
| band header, flip band on the chart | term | flip buffer | - |
| status line, held-back line | term | freeze | video.freeze |
| hedge line under the tiles | term | futures hedge | - |
| band header, regime chip | term | gamma regime | video.read_summary |
| band header, "GEX n s ago" | term | gex | - |
| hedge line, "hedge state unavailable" | term | hedge unavailable | - |
| tile "Margin used" | term | im limit | video.margin_bar |
| tile "Margin used", bar | term | margin | video.margin_bar |
| hedge line | term | perpetual | - |
| strangle row, "x % of the premium kept" | term | premium kept | video.read_summary |
| tile "Coins earned this month" | term | realized p&l | video.read_summary |
| strangle row, slot | term | slot | - |
| band header, trend chip | term | sma trend | - |
| band header, spot | term | spot | - |
| strangle row, title | term | strangle | video.read_summary |
| tile "Open now" | term | unrealized p&l | video.read_summary |
| tiles, "≈ USD" footer | term | usd | - |
| tile "Worst case" | term | worst case | video.position_worst_case |

## details

Roles: user, manager, admin (the Close… button is for a manager or an admin).

| spot | kind | key | video |
| --- | --- | --- | --- |
| Trades summary line | term | avg loss | - |
| Trades summary line | term | avg win | - |
| Trades log, reason detail | term | balance legs | - |
| Missing legs, "the market now" | term | bias | - |
| Trades log, spread note | term | bid / ask | - |
| Trades log, reason chip | term | buy back | - |
| Open positions, call chip | term | call | - |
| Working orders, state | term | cancelled | - |
| Trades log, open/close chip | term | close | - |
| Futures hedge row | term | coin delta | - |
| Open positions, Position Δ column | term | delta | - |
| By exit reason, "Roll (delta drift)" | term | delta drift | - |
| Open positions, delta-per-contract column | term | delta exit | video.change_setting |
| Open positions, delta-per-contract bar | setting | delta_exit_threshold | video.change_setting |
| Trades log, open chip | term | entry | - |
| Working orders, time left | term | fill timeout | - |
| Working orders, state | term | filled | - |
| Futures hedge section | term | futures hedge | - |
| Open positions, Γ column | term | gamma | - |
| Missing legs, "spot vs flip + buffer" | term | gamma flip | - |
| Missing legs, "spot vs flip + buffer" | setting | gamma_flip_buffer_sd | video.change_setting |
| Missing legs, held-by chip | term | gex anchor | - |
| Open positions, Θ / Vega / Γ columns | term | greeks | - |
| Open positions, filter | term | group by | - |
| Futures hedge row, band | term | hedge band | - |
| Futures hedge row, pause | term | hedge unavailable | - |
| Futures hedge row, switch link | term | hedge vs shed | - |
| Missing legs, held-by chip | term | held | - |
| Missing legs, held-by chip | term | hold rule | - |
| Open positions, instrument title | term | instrument | - |
| Working orders, limit column | term | limit price | - |
| Open positions, Close… button | term | manual close | video.manual_close |
| Open positions, "entry → mark" column | term | mark | - |
| Missing legs, empty state | term | missing leg | - |
| Open positions, filter | term | moneyness | - |
| Open positions, title | term | option | - |
| Working orders, title | term | order sent | - |
| Trades log, P&L column | term | p&l | - |
| Trades log, P&L % column | term | p&l % | - |
| Open positions, title | term | position | - |
| Open positions, "premium kept" bar | term | premium kept | - |
| Open positions, call/put chip | term | put | - |
| Open positions, qty column | term | quantity | - |
| Working orders, price steps | term | re-priced | - |
| Trades summary line | term | realized p&l | - |
| Trades log, reason chip | term | reason | video.last_trade |
| Missing legs, held-by chip | term | regime side | - |
| Missing legs, title | term | repair | - |
| Missing legs, release hours | setting | repair_cooldown_hours | video.change_setting |
| Trades log, open chip | term | side | - |
| Open positions, group header | term | slot | - |
| Trades log, spot · DVOL | term | spot | - |
| Missing legs, held-by chip | term | stop hold | - |
| Open positions, "stop at" column | term | stop-loss | - |
| Open positions, "stop at" column | setting | stop_loss_multiplier | video.change_setting |
| Open positions, group header | term | strangle | - |
| Open positions, Θ/day column | term | theta | - |
| Trades log, reason chip | term | time roll | - |
| Trades log, reason detail | term | unknown order | - |
| Open positions, Vega column | term | vega | - |
| Trades summary line | term | win / loss size | - |
| Trades summary line | term | win rate | - |
| Working orders, title | term | working order | - |

## positions

Roles: user, manager, admin.

| spot | kind | key | video |
| --- | --- | --- | --- |
| strangle card, delta box | term | delta | video.position_worst_case |
| strangle card, exit line | term | delta exit | video.position_worst_case |
| strangle card, exit line | setting | delta_exit_threshold | video.change_setting |
| strangle card, title | term | expiry | - |
| book panel and hedge card | term | futures hedge | - |
| strangle card, "premium earned so far" bar | term | premium kept | video.position_worst_case |
| strangle card, "missing side" note | term | repair | - |
| strangle card, stop-loss level | term | stop-loss | video.position_worst_case |
| strangle card, stop-loss level | setting | stop_loss_multiplier | video.change_setting |
| strangle card, title | term | strangle | video.position_worst_case |

## income

Roles: user, manager, admin.

| spot | kind | key | video |
| --- | --- | --- | --- |
| "% of start" view, tiles | term | % of start | video.income_month |
| month table, fees column | term | exchange fees | video.income_month |
| month table, "holding alone" column | term | holding alone | video.income_month |
| page title and card | term | income over months | video.income_month |
| month table, month column | term | month | video.income_month |
| "% of start" view, start strip | term | start balance | video.income_month |
| tile "Worst month" | term | worst month | video.income_month |
| tile "Worst week" | term | worst week | video.income_month |

## worst-case

Roles: user, manager, admin.

| spot | kind | key | video |
| --- | --- | --- | --- |
| "How it's worked out" | term | dvol | video.position_worst_case |
| "What the bot does on a bad day" | term | stop-loss | video.position_worst_case |
| "What the bot does on a bad day" | setting | stop_loss_multiplier | video.change_setting |
| headline | term | worst case | video.position_worst_case |

## activity

Roles: user, manager, admin.

| spot | kind | key | video |
| --- | --- | --- | --- |
| "Protection" entries | term | churn pause | - |
| "Waiting" entries | term | freeze | video.freeze |
| "Waiting" entries | term | gex wait | - |
| "Waiting" entries | term | hold rule | - |
| "Protection" entries | term | kill switch | - |
| "Waiting" entries | term | liquidity cap | - |
| "Protection" entries | term | manual close | video.manual_close |
| "Money in or out" entries | term | rebalance | video.rebalance |
| entries, bold first line | term | reconcile | - |
| "Waiting" entries | term | regime side | - |
| "Waiting" entries | term | repair | - |
| "Protection" entries | term | shed | - |
| "Waiting" entries | term | skipped | video.last_trade |
| "Waiting" entries | term | stop waiting | - |
| entries, time | term | time | - |

## account

Roles: user, manager, admin (the Rebalance button and the futures hedge switch are for a manager or an admin).

| spot | kind | key | video |
| --- | --- | --- | --- |
| per-asset table, column | term | asset | - |
| per-asset table, column | term | available | - |
| per-asset table, column | term | balance | - |
| per-asset table, column | term | equity | - |
| margin policy, entries frozen | term | freeze | video.freeze |
| futures hedge switch | term | futures hedge | - |
| margin policy, IM bar | term | im | video.margin_bar |
| margin policy, IM bar tick | term | im limit | video.margin_bar |
| margin policy, amber badge | term | im over limit | video.margin_bar |
| futures hedge switch | term | inverse perpetual | - |
| margin policy, band reason | term | iv percentile | video.margin_bar |
| margin policy, band | setting | iv_margin_bands[].max_im_pct | video.margin_bar |
| margin policy, band | setting | iv_margin_bands[].min_iv_pct | video.margin_bar |
| futures hedge switch, kill switch note | term | kill switch perpetual | - |
| USD totals | term | margin balance | video.margin_bar |
| margin policy, MM bar | setting | max_mm_pct | video.margin_bar |
| margin policy, MM bar | term | max_mm_pct | video.margin_bar |
| margin policy, MM bar | term | mm | video.margin_bar |
| margin policy, MM bar tick | term | mm limit | video.margin_bar |
| futures hedge switch | term | perpetual | - |
| Rebalance button | term | rebalance | video.rebalance |
| per-asset table, column | term | reserved | - |
| per-asset table, column | term | withdrawable | - |

## how-it-works

Roles: user, manager, admin.

| spot | kind | key | video |
| --- | --- | --- | --- |
| card 1, the call | term | call | video.read_summary |
| card 4, the hedge | term | coin delta | - |
| card 3, "exit line" | term | delta | - |
| card 3, "exit line" | term | delta exit | video.change_setting |
| card 3, "exit line" | setting | delta_exit_threshold | video.change_setting |
| card 1, expiry | term | expiry | - |
| card 4, the hedge | term | futures hedge | - |
| card 4, the band | term | hedge band | - |
| card 1, leg | term | leg | - |
| card 1, "margin" word | term | margin | video.margin_bar |
| card 4, the perpetual | term | perpetual | - |
| card 1, "paid now" | term | premium | video.read_summary |
| card 1, the put | term | put | video.read_summary |
| card 3, "wait then repair" | term | repair | - |
| card 3, "wait then repair" | setting | repair_cooldown_hours | video.change_setting |
| card 3, "take profit" | setting | roi_take_profit | video.change_setting |
| card 3, "stop-loss" | term | stop-loss | - |
| card 3, "stop-loss" | setting | stop_loss_multiplier | video.change_setting |
| card 1, strike | term | strike | - |
| card 3, "take profit" | term | take-profit | - |

## Not on any page

Each row is `| kind | key | reason |`, in alphabetical (byte) order of the key.
A term or setting leaves this list when a page shows it.

| kind | key | reason |
| --- | --- | --- |
| term | action funnel | Analytics admin page, not in the menu bar. |
| term | actions | Users admin page, not in the menu bar. |
| term | activation | Analytics admin page, not in the menu bar. |
| term | active people | Analytics admin page, not in the menu bar. |
| term | age | Shown only in a sentence of the activity feed, no label of its own. |
| term | backtest | Backtest tool only; no monitor page shows a backtest. |
| setting | backtest.commission_per_contract | Backtest only; no monitor page shows it. |
| setting | backtest.fill_model | Backtest only; no monitor page shows it. |
| setting | backtest.limit_fill_rule | Backtest only; no monitor page shows it. |
| setting | backtest.margin_call_liquidation | Backtest only; no monitor page shows it. |
| setting | backtest.slippage_pct | Backtest only; no monitor page shows it. |
| term | bot | Page chooser (BTC / ETH switch) and table headers; a plain word, no tip needed on a page yet. |
| term | change failure rate | Analytics admin page (delivery), not in the menu bar. |
| term | created | Users admin page, not in the menu bar. |
| term | daily swing | Shown only in the Worst case method text, not as a label. |
| setting | delta_drift_threshold | Set in the config; no monitor page shows its value. |
| term | deployment frequency | Analytics admin page (delivery), not in the menu bar. |
| term | detail | Audit admin page, not in the menu bar. |
| setting | dte_delta_matrix[].deltas[] | Set in the config; no monitor page shows it. |
| setting | dte_delta_matrix[].dte | Set in the config; no monitor page shows it. |
| term | email | Users admin page, not in the menu bar. |
| term | first answers | Analytics admin page, not in the menu bar. |
| setting | gex_direction | Set in the config; no monitor page shows it. |
| term | help used | Analytics admin page, not in the menu bar. |
| term | implied volatility | Shown only in sentences (payoff text), no label of its own. |
| term | lapsed | Analytics admin page, not in the menu bar. |
| term | last sign-in | Users admin page, not in the menu bar. |
| term | lead time | Analytics admin page (delivery), not in the menu bar. |
| setting | max_leg_size_multiple | Set in the config; no monitor page shows it. |
| setting | min_premium | Set in the config; no monitor page shows it. |
| term | nps | Analytics admin page and the monthly survey card, not a menu page. |
| term | person | Audit admin page, not in the menu bar. |
| term | price | Shown only in sentences; the columns say entry, mark or limit. |
| term | release train | Analytics admin page (delivery), not in the menu bar. |
| term | request draft | Assistant panel, not a menu page. |
| term | retention | Analytics admin page, not in the menu bar. |
| term | role | Users admin page, not in the menu bar. |
| setting | rollout_dte | Set in the config; no monitor page shows its value. |
| term | soak | Analytics admin page (delivery), not in the menu bar. |
| term | strategy | Shown only in the Details (pro) filter's name, no label of its own. |
| setting | strategy_id | Set in the config; no monitor page shows it. |
| term | target / bot | Audit admin page, not in the menu bar. |
| term | time to restore | Analytics admin page (delivery), not in the menu bar. |
| setting | underlying | Set in the config; the pages show the coin, not this key. |
| term | underlying | Set in the config; the pages show the coin, not this key. |
| term | username | Users admin page, not in the menu bar. |
| term | what went live | Analytics admin page (delivery), not in the menu bar. |
