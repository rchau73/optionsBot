# How-to video storyboards (E11)

Scripts, shot lists and captions for the monitor's how-to videos, one task each, 30–90 s (#337, #189). Content only: nothing here is recorded, published or shown in the product yet. **Any public video or text with performance figures needs legal review first** (`docs/legal/`).

## Rules for every video

- **Facts only.** No forecast, no promised return, no advice. Losses are shown as large and as clear as gains. `frontend/__tests__/video-storyboards.test.js` runs the glossary's no-promise words, the forecast and advice words of the empty states and the 25-word sentence limit over every spoken line.
- **Testnet or live is always on screen.** Record on testnet. If a bot runs live, show no amount: no coin size, no price, no balance, no P&L figure. The spoken lines never contain an amount (the test refuses a number followed by a coin, USD or %).
- **Words follow the glossary** (`frontend/content/glossary.json`). A jargon word is said only if it has its own glossary entry, and the entry is listed in the section's *Terms*.
- **Each section** holds: *Task* (one sentence), *Length* (30–90 s), *Screen* (what must exist to record it), *Terms* (glossary keys), *Risk sentence* (shown on screen before any action that can change trading state; for a read-only video it says so), *Source* (the `OptionStrategy.md` or `frontend/README.md` anchor the facts come from), numbered *Shots* (what is on screen, what is said) and the full *Captions* (the spoken text, with times).
- **At most 200 spoken words** per video (#189). The test also checks the pace stays between 1.5 and 3 words a second, so the caption times can be met.
- **Screens that do not exist yet** are marked *waiting for the E11 prototype*; the shot list then describes what the prototype must show. The links that put each video where its task happens (and the player) also wait for that prototype. Video ids are the analytics catalogue ids of `video_played` / `video_completed` (`docs/product/analytics/event-plan.md`): `video.` then lowercase letters and `_` only.
- **Not covered here:** the comprehension test of these videos is `docs/product/education/comprehension-test.md`; the text for empty panels is `docs/product/education/empty-states.md`.

---

## video.read_summary

- **Task:** Read the monitor's summary card and know what the bot holds, what it earned and what the worst case is.
- **Length:** 60 s
- **Screen:** the investor summary card (exists: `components/InvestorSummary.jsx`). The video link on the card waits for the E11 prototype.
- **Terms:** `strangle`, `leg`, `income over months`, `realized p&l`, `unrealized p&l`, `exchange fees`
- **Risk sentence:** Nothing in this video changes trading. Selling options can lose money, and a month can end in a loss.
- **Source:** `OptionStrategy.md#1-the-idea-in-one-paragraph`

### Shots

1. On screen: one bot's summary card; the TESTNET or LIVE badge is highlighted. Say: "This is the summary card of one bot. The badge says whether it trades on testnet, which is practice money, or live."
2. On screen: the block that says what the bot holds is highlighted. Say: "First, what it holds. A strangle is a pair of sold options, one call and one put. Each one is a leg. The card counts them and shows the nearest expiry."
3. On screen: the income per month rows are highlighted; one loss month, one month with no closes. Say: "Second, income per month, after fees. A loss has a minus sign and the same size as a gain. A month with no closes says so."
4. On screen: the worst case answer is highlighted, with its model label. Say: "Third, the worst case now. It estimates the loss if the price moves a lot in one day. It is a model. A real day can be worse."
5. On screen: the last action line is highlighted, then the detail panels below the card. Say: "Last, the latest action, in plain words. Everything below this card is detail. Open it when you want to know more."

### Captions

- [00:00-00:11] This is the summary card of one bot. The badge says whether it trades on testnet, which is practice money, or live.
- [00:11-00:25] First, what it holds. A strangle is a pair of sold options, one call and one put. Each one is a leg. The card counts them and shows the nearest expiry.
- [00:25-00:38] Second, income per month, after fees. A loss has a minus sign and the same size as a gain. A month with no closes says so.
- [00:38-00:50] Third, the worst case now. It estimates the loss if the price moves a lot in one day. It is a model. A real day can be worse.
- [00:50-01:00] Last, the latest action, in plain words. Everything below this card is detail. Open it when you want to know more.

---

## video.position_worst_case

- **Task:** Understand one open position and the worst-case page.
- **Length:** 75 s
- **Screen:** the open positions table (exists: `components/PositionsTable.jsx`) and the worst-case page (exists: `/worst-case?bot=<name>`, `components/WorstCaseView.jsx`). The links to the video wait for the E11 prototype.
- **Terms:** `position`, `strangle`, `leg`, `premium`, `mark`, `unrealized p&l`, `p&l %`, `stop-loss`, `dte`
- **Risk sentence:** Nothing in this video changes trading. The worst-case figures are a model's estimate, not a limit: a loss can be larger.
- **Source:** `OptionStrategy.md#4-how-a-trade-can-end`

### Shots

1. On screen: one row of the open positions table; the strike, the type and the DTE columns are highlighted. Say: "Each row is one leg of a strangle. You see its type, its strike and its days to expiry, called DTE."
2. On screen: the premium and the mark columns are highlighted. Say: "The premium is what the bot was paid when it sold the option. The mark is what the option is worth now. If the mark is lower, the leg is ahead."
3. On screen: the unrealized P&L and P&L % columns are highlighted, one positive and one negative row. Say: "Unrealized P&L is the result if the leg were closed now. P&L percent compares it with the premium. A negative number is a loss."
4. On screen: the stop-loss column is highlighted. Say: "The stop-loss is a mark at which the bot buys the leg back to limit the loss. In a fast move, it can fill at a worse price."
5. On screen: the worst-case page opens; the headline and the table of different days are highlighted. Say: "The worst-case page shows what different days would do to the account. A bad day is a loss in coin, in dollars and as a share of the account."
6. On screen: the sentence "what this estimate can't promise" is highlighted. Say: "The page does not say how often such a day happens. A model understates crypto's biggest moves, so read it as a guide, not a limit."

### Captions

- [00:00-00:11] Each row is one leg of a strangle. You see its type, its strike and its days to expiry, called DTE.
- [00:11-00:24] The premium is what the bot was paid when it sold the option. The mark is what the option is worth now. If the mark is lower, the leg is ahead.
- [00:24-00:38] Unrealized P&L is the result if the leg were closed now. P&L percent compares it with the premium. A negative number is a loss.
- [00:38-00:50] The stop-loss is a mark at which the bot buys the leg back to limit the loss. In a fast move, it can fill at a worse price.
- [00:50-01:03] The worst-case page shows what different days would do to the account. A bad day is a loss in coin, in dollars and as a share of the account.
- [01:03-01:15] The page does not say how often such a day happens. A model understates crypto's biggest moves, so read it as a guide, not a limit.

---

## video.last_trade

- **Task:** Find out why the bot made its last trade.
- **Length:** 60 s
- **Screen:** the activity feed and the trade history (exist: `components/ActivityFeed.jsx`, `components/TradeHistory.jsx`; the plain-words sentence and the **details** toggle exist since #287).
- **Terms:** `close`, `stop-loss`, `take-profit`, `time roll`, `skipped`, `entry`, `reason`
- **Risk sentence:** Nothing in this video changes trading. A close can book a loss; the reason line says which rule closed the leg.
- **Source:** `OptionStrategy.md#4-how-a-trade-can-end`

### Shots

1. On screen: the activity feed; the newest line is highlighted. Say: "To see why the bot did something, open the activity feed. The first line of each item says what happened, in plain words."
2. On screen: the details toggle on a close is opened; the rule and its numbers appear. Say: "Open details to see the rule that fired and its numbers. The rule is the real reason; the sentence above it is a summary."
3. On screen: three closes are shown in turn, labelled stop-loss, take-profit and time roll. Say: "A leg can end by a stop-loss, after a loss. It can end by take-profit, after most of the premium was kept. Or by a time roll, near its expiry."
4. On screen: a skipped entry in the feed is highlighted. Say: "Skipped means the bot chose not to act, and the reason is written. A skipped entry is not an error."
5. On screen: the trade history row for the same close, with the result column highlighted. Say: "The trade history shows the result of each close, after fees. Wins and losses look the same, except for the sign."

### Captions

- [00:00-00:11] To see why the bot did something, open the activity feed. The first line of each item says what happened, in plain words.
- [00:11-00:24] Open details to see the rule that fired and its numbers. The rule is the real reason; the sentence above it is a summary.
- [00:24-00:38] A leg can end by a stop-loss, after a loss. It can end by take-profit, after most of the premium was kept. Or by a time roll, near its expiry.
- [00:38-00:48] Skipped means the bot chose not to act, and the reason is written. A skipped entry is not an error.
- [00:48-01:00] The trade history shows the result of each close, after fees. Wins and losses look the same, except for the sign.

---

## video.margin_bar

- **Task:** Read the margin bar and know what the limit and the colours mean.
- **Length:** 60 s
- **Screen:** the account panel with the IM and MM bars and the margin policy per bot (exist: `components/AccountPanel.jsx`, `components/MarginBar.jsx`).
- **Terms:** `margin`, `margin balance`, `im`, `mm`, `im limit`, `mm limit`, `im over limit`, `dvol`
- **Risk sentence:** Nothing in this video changes trading. When the maintenance margin reaches its end of the bar, the exchange can close positions.
- **Source:** `OptionStrategy.md#8-risk-controls`

### Shots

1. On screen: the account panel; the IM bar is highlighted. Say: "Margin is what the exchange holds to cover open positions. The IM bar shows initial margin as a share of the margin balance."
2. On screen: the tick on the IM bar that marks the limit is highlighted. Say: "The tick on the bar is the IM limit. The bot sizes new positions so initial margin stays under it. The limit follows the volatility band, called DVOL."
3. On screen: the line "why this limit" is highlighted. Say: "The monitor says why the limit is what it is now. In a confirmed negative regime it is the lowest one."
4. On screen: the MM bar, first amber, then red. Say: "The MM bar shows maintenance margin. It turns amber, then red. At the end of the bar the exchange can close positions."
5. On screen: the IM over limit badge is highlighted. Say: "If initial margin goes over the limit, a badge says so. The bot only warns; it does not resize by itself."

### Captions

- [00:00-00:12] Margin is what the exchange holds to cover open positions. The IM bar shows initial margin as a share of the margin balance.
- [00:12-00:26] The tick on the bar is the IM limit. The bot sizes new positions so initial margin stays under it. The limit follows the volatility band, called DVOL.
- [00:26-00:36] The monitor says why the limit is what it is now. In a confirmed negative regime it is the lowest one.
- [00:36-00:48] The MM bar shows maintenance margin. It turns amber, then red. At the end of the bar the exchange can close positions.
- [00:48-01:00] If initial margin goes over the limit, a badge says so. The bot only warns; it does not resize by itself.

---

## video.freeze

- **Task:** Understand why new entries are frozen and what still runs.
- **Length:** 45 s
- **Screen:** the margin policy block of the account panel, with the FROZEN state and its reason (exists: `components/AccountPanel.jsx`).
- **Terms:** `freeze`, `gamma regime`, `dvol`, `im limit`, `entry`
- **Risk sentence:** Nothing in this video changes trading. A freeze stops new risk, but positions already open stay open.
- **Source:** `OptionStrategy.md#9-market-regimes-and-what-the-bot-does`

### Shots

1. On screen: the margin policy block shows "Entries frozen" and the countdown of daily closes. Say: "When entries are frozen, the bot opens nothing new. The block says why and how many daily closes are still needed."
2. On screen: the reason line, a change of volatility band or of regime. Say: "A freeze starts when a change of the volatility band or the gamma regime could lower the margin limit. It waits for the change to hold."
3. On screen: the activity feed shows an exit and a roll during the freeze. Say: "During a freeze exits and rolls go on. Open positions stay open, so a freeze does not remove risk."
4. On screen: the badge goes back to allowed. Say: "If the change goes away, or is confirmed, the freeze ends and new entries are allowed again."

### Captions

- [00:00-00:11] When entries are frozen, the bot opens nothing new. The block says why and how many daily closes are still needed.
- [00:11-00:23] A freeze starts when a change of the volatility band or the gamma regime could lower the margin limit. It waits for the change to hold.
- [00:23-00:35] During a freeze exits and rolls go on. Open positions stay open, so a freeze does not remove risk.
- [00:35-00:45] If the change goes away, or is confirmed, the freeze ends and new entries are allowed again.

---

## video.manual_close

- **Task:** Close one leg by hand and know what follows.
- **Length:** 75 s
- **Screen:** the Close button and its confirmation window (exist: `components/ManualClose.jsx`; the risk note in `frontend/content/confirm-notes.json` is not shown in the window yet, it waits for the E11 prototype). A manager or an admin only.
- **Terms:** `manual close`, `regime side`, `gamma regime`, `realized p&l`, `stop hold`, `repair`, `slot`, `entry`, `buy back`
- **Risk sentence:** A manual close buys the leg back at market now, pays the asking price and a fee, and the result so far becomes final.
- **Source:** `OptionStrategy.md#9-market-regimes-and-what-the-bot-does`

### Shots

1. On screen: the Close button on a row, grey; the reason shows on hover. Say: "The Close button buys a leg back at market. It is grey when the bot's rule does not allow it, and the hover says why."
2. On screen: a row where the button is active; the regime side block is highlighted. Say: "It is active only on the side the bot no longer sells. That is, in a confirmed negative regime: puts in a bear trend, or calls in a bull trend."
3. On screen: the confirmation window opens; the cost at the ask is highlighted, with the risk sentence. Say: "Before you confirm, read the window. A market buy pays the asking price, the spread and a fee. The result so far becomes final."
4. On screen: the line "what follows" is highlighted. Say: "It also says what follows. The side is held like after a stop-loss, so the bot does not sell it again at once."
5. On screen: the red note for a last leg. Say: "If it is the last leg of the strangle, the note is red. The slot becomes empty and the bot opens a new entry there."
6. On screen: the Cancel button is highlighted. Say: "If you are unsure, cancel. Nothing is sent until you confirm, and every close is recorded with who made it."

### Captions

- [00:00-00:11] The Close button buys a leg back at market. It is grey when the bot's rule does not allow it, and the hover says why.
- [00:11-00:24] It is active only on the side the bot no longer sells. That is, in a confirmed negative regime: puts in a bear trend, or calls in a bull trend.
- [00:24-00:38] Before you confirm, read the window. A market buy pays the asking price, the spread and a fee. The result so far becomes final.
- [00:38-00:50] It also says what follows. The side is held like after a stop-loss, so the bot does not sell it again at once.
- [00:50-01:02] If it is the last leg of the strangle, the note is red. The slot becomes empty and the bot opens a new entry there.
- [01:02-01:15] If you are unsure, cancel. Nothing is sent until you confirm, and every close is recorded with who made it.

---

## video.rebalance

- **Task:** Preview and run a rebalance and know what follows.
- **Length:** 75 s
- **Screen:** the Rebalance button, its dry-run preview and its confirmation window (exist: `components/Rebalance.jsx`; the risk note in `frontend/content/confirm-notes.json` waits for the E11 prototype). A manager or an admin only.
- **Terms:** `rebalance`, `im limit`, `slot`, `liquidity cap`, `max_leg_size_multiple`, `fill timeout`, `stop hold`, `repair`
- **Risk sentence:** A rebalance can sell more, which adds premium and the risk of a big move, or buy back, which pays the asking price, the spread and fees.
- **Source:** `OptionStrategy.md#9-market-regimes-and-what-the-bot-does`

### Shots

1. On screen: the Rebalance button beside the margin policy block. Say: "A rebalance resizes each slot toward its share of the current IM limit. It sells more where there is room and buys back where the book is too large."
2. On screen: the dry run is started and the list of actions appears. Say: "Start with the dry run. It lists what the pass would do, slot by slot. It sends nothing and records no change."
3. On screen: the risk sentence is shown above the confirm button. Say: "Read the risk before you confirm. Selling more adds premium and the risk of a big move. Buying back pays the asking price, the spread and fees."
4. On screen: a slot marked short of its share, with the reason. Say: "A slot can stay short of its share. The liquidity cap, a stop hold or the size cap can limit it, and the list says which."
5. On screen: the real pass runs; one order is cancelled at its fill timeout. Say: "The real pass reads fresh margin, so its orders can differ from the preview. An order that does not fill in time is cancelled, and the rest is tried later."

### Captions

- [00:00-00:14] A rebalance resizes each slot toward its share of the current IM limit. It sells more where there is room and buys back where the book is too large.
- [00:14-00:27] Start with the dry run. It lists what the pass would do, slot by slot. It sends nothing and records no change.
- [00:27-00:42] Read the risk before you confirm. Selling more adds premium and the risk of a big move. Buying back pays the asking price, the spread and fees.
- [00:42-00:56] A slot can stay short of its share. The liquidity cap, a stop hold or the size cap can limit it, and the list says which.
- [00:56-01:15] The real pass reads fresh margin, so its orders can differ from the preview. An order that does not fill in time is cancelled, and the rest is tried later.

---

## video.income_month

- **Task:** Read the income page, month by month.
- **Length:** 60 s
- **Screen:** the income page (exists: `/income?bot=<name>`, `components/IncomePage.jsx`).
- **Terms:** `income over months`, `worst month`, `worst week`, `holding alone`, `exchange fees`, `buy back`, `premium`
- **Risk sentence:** Nothing in this video changes trading. Past months say nothing about the next one, and a month can end in a loss.
- **Source:** `frontend/README.md#income-page`

### Shots

1. On screen: the income page header with the coin and USD toggle and the TESTNET or LIVE badge. Say: "The income page shows the result month by month, after fees. Use the toggle to see it in coin or in dollars. The badge shows testnet or live."
2. On screen: the table row of one month, with the columns highlighted in turn. Say: "Each row starts with what the bot was paid. Then what it paid to buy back, and the exchange fees. The result is what is left."
3. On screen: a loss month, tinted and marked as loss. Say: "A loss month is tinted and marked as a loss. It has the same size as a month with a gain."
4. On screen: the holding alone column is highlighted. Say: "Holding alone is what the same account would have done by just holding the coin that month. It shows how much of a dollar result is the coin's own move."
5. On screen: the worst month and worst week tiles are highlighted. Say: "Read the average with the worst month and the worst week. One good average can hide a bad month."

### Captions

- [00:00-00:12] The income page shows the result month by month, after fees. Use the toggle to see it in coin or in dollars. The badge shows testnet or live.
- [00:12-00:24] Each row starts with what the bot was paid. Then what it paid to buy back, and the exchange fees. The result is what is left.
- [00:24-00:33] A loss month is tinted and marked as a loss. It has the same size as a month with a gain.
- [00:33-00:48] Holding alone is what the same account would have done by just holding the coin that month. It shows how much of a dollar result is the coin's own move.
- [00:48-01:00] Read the average with the worst month and the worst week. One good average can hide a bad month.

---

## video.change_setting

- **Task:** Change one setting safely and see what it changed.
- **Length:** 60 s
- **Screen:** *waiting for the E11 prototype.* The monitor is read-only and has no settings screen: today a setting is changed in the bot's config file by the person who runs it. The prototype must show the setting's tip (what it is, how it moves risk and income), the current value and the journal line that records the change.
- **Terms:** `dte_delta_matrix`, `stop_loss_multiplier`, `max_leg_size_multiple`, `repair_cooldown_hours`, `im limit`, `freeze`
- **Risk sentence:** A setting changes how much risk the bot takes and how much income it can earn; a change can lead to larger losses, and past tests do not promise what happens next.
- **Source:** `OptionStrategy.md#8-risk-controls`

### Shots

1. On screen: a setting's tip opens; its short text and the line on risk and income are highlighted. Say: "Every setting has a tip. It says what the setting is, and how it moves risk and income. Read it before you change anything."
2. On screen: the current value and the allowed range are highlighted. Say: "See the current value and the range the bot accepts. A value outside the range is refused when the bot starts."
3. On screen: the risk sentence is shown beside the confirm step. Say: "Read the risk sentence. A setting can raise the income and raise the possible loss in the same step."
4. On screen: one setting is changed, then the journal line that records it. Say: "Change one setting at a time. When the bot starts, it records the settings that changed, so you can see what you changed and when."
5. On screen: the margin policy block after the restart. Say: "Then watch the margin bar and the activity feed. If the result is not what you wanted, set the old value back."

### Captions

- [00:00-00:13] Every setting has a tip. It says what the setting is, and how it moves risk and income. Read it before you change anything.
- [00:13-00:25] See the current value and the range the bot accepts. A value outside the range is refused when the bot starts.
- [00:25-00:37] Read the risk sentence. A setting can raise the income and raise the possible loss in the same step.
- [00:37-00:50] Change one setting at a time. When the bot starts, it records the settings that changed, so you can see what you changed and when.
- [00:50-01:00] Then watch the margin bar and the activity feed. If the result is not what you wanted, set the old value back.
