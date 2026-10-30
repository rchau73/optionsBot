# internal/strategy

Folder notes for `CLAUDE.md`.
The root file's **Trading rules** are the rules; this file adds each rule's history, numbers and mechanics, which did not fit in the root file's 25 KB.
Read both before changing a rule.

- **`internal/strategy`** — the decision loop; depends only on the interfaces in `deps.go`.
  - `strategy.go` Run/evaluate/heartbeat · `limits.go` margin policy (status, MM reduction, simulation sizing) · `open.go` entry · `pending.go` fill tracking ·
    `close.go` `buyToClose`, rollouts, GEX closes · `repair.go` · `reconcile.go` · `rebalance.go` · `killswitch.go`.
  - Pure rules shared with the backtest: `entry.go` (`SelectExpiry(…, now, …)`, `SelectSlotExpiry`, `SelectStrike`, `SelectStrikePair`), `rollout.go` (`EvaluateLeg(pos, now, …)`),
    `gamma.go`, `margin.go`, `pricing.go` (`StepDownPrice`, `FloorPrice`).

## Trading rules: details

- **Margin policy** — the IM alerts:
  - IM more than `rebalance_startup_band_pct` over the limit with no rebalance pass this cycle (a transfer out, a price move) is **alert only** (`imalert.go`, pure `IMOverLimitNext`; the product owner's option B on #166):
    one `im_over_limit` risk line + WARN per episode (IM, limit, `margin_balance` and `ref_margin_balance` = the balance at the last rebalance or confirmed limit change),
    `risk.im_over_limit(_since)` in `/api/status` and a badge beside the monitor's Rebalance button, `im_over_limit_cleared` once IM is at or under the limit;
    never an order, never a rebalance (auto-downsize, option C, was rejected without a stress study); in memory, so a restart mid-episode journals it once more.
  - The other direction (`underuse.go`, pure `IMUnderUseNext`/`UnderUseCause`, #369):
    entries allowed and IM under 60 % of the limit for `IMUnderUseWindowMinutes` (360, fixed; a reading gap > `history.MaxSampleGap` restarts the window, margin unknown pauses it, entries not allowed ends it)
    journals one `im_under_use` risk line + WARN (IM, limit, `utilisation_pct`, `minutes`, `cause` — the vacant slot's last `Skip*` reason or `orders.Idle*`, the vocabulary `cmd/idlemargin` shares),
    `risk.im_under_use(_since)` in `/api/status`, `im_under_use_cleared` + WARN at ≥ 60 % or when entries stop being allowed; alert only, in memory.
  - The drift band on downsizes (`limits.go`, pure `DownsizeDue`, #555, the issue's option A): `limitChanged` is set by a confirmed limit change and cleared once its pass is `Done`;
    until then the pass cuts to the exact limit, any other pass (a top-up's retry, startup, the Rebalance button, its dry run) only above limit + `rebalance_startup_band_pct`.
    A strangle left over its target is reported `kept` and the pass's `rebalance` line ends "drift, no leg bought back"; top-ups still need IM under the limit, the MM cut never waits.
    2026-10-29 stable BTC: a retry 15 min after a top-up read 20.4 % of 20 % and cut 0.2 of four legs (−0.0016 BTC + fees); 10-28 the same with four legs.
- **Size cap:** Portfolio margin can price a leg almost free when it offsets the book; the offset disappears when the rest of the book is closed.
- **Liquidity cap:**
  - A strike missing from the mainnet book has no open interest (nothing sent); a wired source without a snapshot sends nothing.
  - When it binds it is journaled as a `liquidity_cap` skip with the margin size, the capped size and each leg's OI — once per capped order sent and per capped top-up, once per slot for an entry it blocks;
    the rebalance report shows the capped size; under one lot nothing is sent (a top-up is not re-armed: the slot stays short, no re-send of the rest).
  - 2026-10-15: ETH sent 1,454 on a strike whose OI was 164 and filled 152; mvp's 2–12 % of OI filled whole.
  - `cmd/stress -replay … -liqcap N` studies it.
- **Sizing:** The backtest has no simulator: `backtest.ApproxIMLimitPct` (premium as margin, DVOL band only) is a labelled approximation.
- **Exit priority** — resting closes:
  - **Take-profit, delta drift and the DTE roll rest first** (`restingclose.go`, #239, the product owner's option B):
    a GTC buy limit at the mid rounded down a tick (`RestingClosePrice`; no bid or no tick of room under the ask → IOC at the ask at once) for `order_fill_timeout_sec`,
    polled each cycle after `checkPendingOrders` (fills booked as they come, `exit_step: mid`);
    when the window ends it is cancelled, its fill read back, and if the rule still applies that cycle the rest is an IOC at the then-current ask (`exit_step: ask`, `askStep`)
    — retried next cycle as before, cleared when the rule stops applying.
  - Any other close of that leg (`buyToCloseNote`: stop, delta exit, MM cut, shed, manual, kill switch, downsize) cancels the resting order and books its fill first,
    and so does a shutdown (`settleRestingCloses` when Run's context ends, on a detached 5 s context; `main` keeps the trading gateway open until Run returns);
    a failed cancel or read-back goes to the unknown-outcome path (cancel by label).
  - While it rests, leg balancing skips the strangle and the position check does not adopt the exchange being less short on that instrument.
- **Even legs** — a leg an exit closed in part (`closinglegs.go`, #554): a piece of a stop-loss, DTE roll, delta exit, drift or take-profit that leaves a remainder marks the leg closing (`bookClose` → `noteClosingPiece`).
  - Leg balancing skips the strangle while either leg is marked; the mark holds while the leg's rule fires and `ClosingHold` (30 min, `config.FixedClosingHoldMinutes`) after the last piece once it stops, and goes with the position.
  - In memory: a restart forgets it (the strangle is balanced at once if still uneven).
  - 2026-10-29/30 stable ETH: |Δ| on the drift (0.100) or exit (0.30) threshold ticked back between two IOC pieces and balancing trimmed the other leg four times, once 100 calls sold the same second.
- **Position safety:** Churn breaker: a slot that buys back and re-sells `churn_max_round_trips` (3) times within `churn_window_minutes` (60) is paused for the window
  — no entries, repairs, upsizes or leg balancing; exits run.
- **No re-selling into the move:**
  - The rebalance top-up obeys the same record:
    it sells nothing into a slot while one of its strangles' stopped (or hand-closed) legs is held (`slotStopHold`, `repair_held: top-up held`),
    and it tops up only strangles whose two legs are both in the book (`liveLegs`)
    — a closed leg stays linked to its strangle with its last size, and reading the link topped up a strangle whose put had been closed by hand (2026-10-11 BTC).
  - The records live in memory and are rebuilt at startup from the journal (`StopsFromTrades` → `RestoreStops`, applied after reconcile;
    a replayed close carries the journal's `close_reason` label — `stop_loss`, not `stop_loss_200pct` — so `closeReasons` matches both: until 2026-10-14 a stop-loss was never restored):
    a side whose latest close on an expiry was a stop-loss, delta exit, manual close or MM cut, with no repair re-sale since, is held again with its original time,
    so the cooldown runs on (2026-10-11: a deploy forgot BTC's two hand-closed puts).
- **Confirmed negative regime, side by trend:**
  - Every cycle `cancelRegimeBlockedSides` cancels the working entries, repairs and top-ups of a blocked side whole (`cancelSide`, as `cancelShedSide`; journaled with `TriggerRegimeSide`, fills read back and booked).
  - The SMA trend reads the price against SMA9 with a band of `sma9_band_sd` (0.10, fixed) daily σ:
    `SMA9Side` changes side only past SMA9 × (1 ± band), within it the side it was on (`GammaMonitor.sma9Side`, updated in `PushPriceAt`; `SMALevelsBand`; `cmd/stress -sma9band`)
    — #243: spot on SMA9 flipped the trend every cycle and a put repair sent in a one-cycle blip filled 100 lots.
  - Its confirmation lifted the freeze and ETH topped up puts in a bear trend (2026-10-09).
  - Stress: blocking all new risk instead cost ETH 21–27 %; this rule +0.4–1.8 % ETH, +20–24 % BTC.
- **GEX gating:**
  - The buffer scales with the asset's volatility (stress 2026-10-14, 1.5 vs 1: BTC on bias +21.5 % today / −1.2 % general, ETH on sma +8.1 % / −0.8 %, fewer sheds in every setup; live replay BTC 7 → 3 sheds);
    without a DVOL reading it is 0 (shed as before, never less).
  - A shed leg is anchored (`shed.go`, `ShedAnchor`: the flip and buffer at the shed) and repaired only when `ShedRepairBlockReason` is empty
    — spot above that flip × (1 + buffer) on `gex_repair_confirm_snapshots` (2) consecutive snapshots, or the live regime non-negative for `gex_repair_release_hours` (24)
    — never because the live flip jumped under spot (2026-10-06 ETH: flip 2697 → 2440 in a minute, puts shed and re-sold).
  - Anchors are in memory like `stopped` and rebuilt from the journal the same way
    (`ShedsFromTrades` → `applyJournalSheds`: a side whose latest close on an expiry was a GEX shed, with no repair since, is anchored again at the shed's time, `market.gamma_flip` and `market.flip_buffer_pct`
    — older records without the buffer take today's sd at the shed's DVOL); 2026-10-14 a deploy 20 min after a bias shed re-sold BTC's put 24 h early.
  - The backtest (daily) sheds on `Action`.
  - The GEX regime and flip themselves are unchanged (they match the reference script).
  - When a shed starts, every pending entry/repair with a working order of the shed side is cancelled whole (`cancelShedSide`) and finalized at once,
    so a late fill is booked and shed in the same cycle instead of filling into the shed.
- **Direction source:**
  - The bias in use is `StableBiasDir`: `BiasDir` must agree against the live flip and the median flip of the last `gex_bias_flip_window_minutes` (60; `GammaMonitor.recordFlip`, once per snapshot), else neutral;
    until the record spans the whole window (startup, or a GEX gap longer than it) the bias sheds nothing
    — `GammaDecision.WarmupHeld`, journaled once as a `bias_warmup` skip, `Action` still skips the side for entries and repairs (2026-10-14: a flip jump 10 min after a restart was the whole window and shed BTC's calls)
    — before daily expiries the script's flip jumped for 3–20 min (BTC 2026-10-12 → 72,000, 10-14 → 79,500) and the live bias shed every call with spot flat.
  - Under `bias` the shed is `ResolveBiasAction` (bias down → puts, up → calls, confirmed on 2 snapshots), the regime-side rule, entries, top-ups and manual close read the bias,
    and a shed leg is re-sold by `BiasRepairBlockReason` — `gex_bias_repair_cooldown_hours` after the shed, then the bias off its side on `gex_repair_confirm_snapshots` (anchors count every snapshot in `repairIncompleteStrangles`).
  - Both sources are always computed: journal `portfolio.sma_trend`/`gex_bias`,
    and `data/direction_history.jsonl` (`history.DirectionLog`, one line per change, with `other_action` = what the unused source would shed).
  - Stress 2026-10-12: bias −16…−139 % vs sma (3–5× the sheds); live trial on BTC since 2026-10-12, ETH on sma as the control;
    since 2026-10-18 only stable's BTC runs bias, mvp's BTC runs sma (#168).
- **Manual rebalance** (`manualrebalance.go`): `RequestRebalance` hands the request to the Run goroutine (`rebalanceCh`), which reads fresh margin and applies `RebalanceBlockReason`
  — the same pure gate `applyMarginPolicy` uses, so the two never drift — then runs `runRebalance`; it skips only "limit unchanged" and `rebalanceRetryAt`.
  - `rebalancePositions(…, dryRun)` reports every decision (`RebalanceReport`/`RebalanceAction`); with `dryRun` it works on copies of the strangles, sends nothing, journals nothing and does not re-arm.
  - Cooldowns: 5 min per real pass (`ManualRebalanceWait`), 30 s per dry run; a refused request does not start the cooldown.
  - Journaled `rebalance_manual`.
- **Manual close:**
  - Each leg's view says whether it is allowed and what follows (`rebuilt` when it is its strangle's last leg: the slot is re-entered).
  - The request runs on the Run goroutine (`manualCh`), re-checks the rule, records the leg like a stop (repair held by `RepairBlockReason`) and closes with `TriggerManualClose`;
    a request the loop has not taken within 25 s is dropped unexecuted.
- **Kill switch:** 2026-10-09: pending entries were dropped unread and 100 filled lots stayed short.
- **Startup:** reconcile from the exchange (cancel this currency's orders via `private/cancel_all_by_currency`, load positions, regroup with `GroupPositions`
  - — an expiry may hold several strangles: calls and puts paired by size, then rank from the money;
  - leftovers one-legged, except that an uneven pair's excess is split onto a lone leg of the other type on the same expiry,
    and two uneven pairs whose excesses offset (one a call too many, the other a put) are split into one more strangle
    (Deribit reports a strike shared by two strangles as one position; without the split, balancing bought it back and repair re-sold it at every restart
    — 2026-10-17 BTC 27NOV calls 2.1 + 0.2 = puts 1.8 + 0.5, #156);
  - every position in exactly one strangle, checked with an ERROR log
  - — and give each strangle a slot with `assignSlots` (`slotassign.go`):
    the slot the journal says opened its legs (`OpenSlotsFromTrades`, handed over by `RestoreStops`; a preference, never a position),
    else the assignment with the lowest **total** `slotScore` — closest pair first swapped ETH's two slots on a tie (2026-10-17, #156)
    — that never puts a strangle the journal does not place there beyond a slot's window (DTE > target + `max_dte_deviation`) nor one expiry in two slots;
  - strangles on one expiry share a slot, and a slot may stay vacant (`VacantSlots`, its reason in the `reconciled` detail;
    2026-10-21 BTC: the empty 25-DTE slot was handed the 50-DTE slot's 36-DTE strangle, both slots on 27NOV, IM 24 % of 20 %, #242);
  - the `reconciled` lines' `detail` gives the pairing and why the slot)
  - → margin policy (rebalance only when the limit rests on confirmed data and nothing is frozen) → entries.
