# Monitor guide

How to read each page of the monitor. It is written for the person who owns the account, not for developers. Every page has one section below, with the same name as its item in the menu bar. The menu bar shows Overview and Details (pro) directly. Positions, Income, Worst case, Activity and Account are under **Operational**, and How it works is under **Help**.

Three rules hold on every page. Amounts are in the bot's coin first (BTC for the BTC bot, ETH for the ETH bot) and in US dollars second, as a smaller "about" figure. Different coins are never added together. A figure the monitor does not have says "unknown" or "not yet" and gives the reason; it never shows a zero in its place.

Every figure is a fact from the bot's journal or from Deribit, or a labelled estimate. Nothing here is a forecast or advice.

## Overview

The first page. It answers one question: is the bot running, and how is it doing this month?

**The status line** at the top says whether each bot is trading ("Both trading · updated 14:02"). Under it, an amber line appears for each bot that holds something back, in plain words. Example: "ETH sells no new puts: negative gamma regime in a bear trend". Amber means the bot is waiting on purpose, not that it is broken.

**The four tiles**:
- *Coins earned this month*: the money kept after closes, net of Deribit fees. It counts only positions already closed.
- *Open now*: the profit or loss of positions still open. It is not realised until they close and can change every minute.
- *Worst case*: the loss of one very bad day, as an estimate. The Worst case page explains it.
- *Margin used*: how much of the margin limit the bot has used. The bar shows the limit; the footer shows the room left.

**The profit and loss chart** has two switches: Coin or USD, and 1 day, 1 week or 1 month. The solid line is total profit and loss, including open positions. The dashed grey line is realised only. A loss is shown in red and with a minus sign.

**One band per bot** shows a header with the coin, the price, the gamma regime (positive or negative), the trend (bull, bear or neutral), how old the last GEX reading is, and the margin bar. Below it, a price chart with 60 daily candles: green for up days, grey for down days. Red is kept for losses only. The shaded corridor lies between the lowest and the highest strike the bot has sold. The dashed lines are the sold strikes, SMA9 and SMA21, the swing high and low, and the gamma flip with its shed band. Each can be switched off with its button. On the right, one row for each strangle: its slot, expiry, days left, profit and loss in coin, and how much of the premium has been kept. A click opens its payoff chart.

**Colours and marks**:
- A dimmed band means the bot's data is stale; the age of the last answer is shown.
- "GEX 40s ago" turns amber after five minutes. Until the GEX reading is fresh again, the bot opens nothing new.
- A bot that is down says so on its band.

**When something looks wrong**:
- Stale data or a bot down: look at the header chips (STALE, OFFLINE, LOOP STALLED, HALTED). LOOP STALLED means the page answers but the bot's decision loop has not run for three intervals, so exits are not being checked.
- A held leg: the amber line names the rule. Open Positions or Details (pro), Missing legs, to see when it is released.
- A loss: open Activity for the bot's own sentence on each close, then Details (pro) for the rule and numbers.

Learn more: glossary terms *gamma regime*, *trend*, *freeze*, *margin*; OptionStrategy.md sections 3, 8 and 9.

## Income

Month by month, where the money came from. It reads the journal's closes, after fees.

**Four tiles**: *Last 6 months, after fees*; *Average month* (with how many months were up and down); *Worst month*; *Worst week* (as a share of the account). A switch changes the amounts between the coin and USD.

**The table** has one row per UTC month, newest first:
- *Paid to you*: the premium received when the options were sold.
- *Paid to buy back*: what it cost to close them.
- *Exchange fees*: Deribit's fees, shown as a negative number.
- *Result*: paid to you, minus buy-back, minus fees.
- *Holding alone*: what the same coin would have gained or lost if it had only been held. In the coin it is 0 by definition; in USD it is the price change.

A month with a loss is tinted and marked "loss". A month without closes says "no closes". The current month says "so far".

**When something looks wrong**: a loss month is normal data, not an error; the table keeps it in the open. "Unknown" in *holding alone* means the price for that month was not recorded. Testnet income is practice money, not real income.

Learn more: glossary terms *income over months*, *worst month*, *worst week*, *premium*, *exchange fees*; OptionStrategy.md section 4.

## Worst case

An estimate of what one very bad day would cost the positions held right now. It is a model, not a measurement and not a forecast.

**The headline** is the worse of the two big-move days (down and up), as a loss in coin, in USD and as a share of the account.

**The five rows** show what different days would do: a very bad fall (−3 daily standard deviations), a bad fall (−2), a quiet day (0), a strong rise (+2) and a very strong rise (+3). Each row has the price move in percent, how big that is in typical daily swings, and the result with its sign, in coin and USD. A typical daily swing comes from DVOL, the market's own measure of expected volatility.

No row says how often such a day happens. The model understates how large crypto moves can be, and there is no measured figure.

**What the bot does on a bad day** is shown beside it: it closes a leg when its price reaches the stop-loss multiple of what it was sold for. A stop is a trigger, and a fast move can fill it at a worse price than the stop level.

**When something looks wrong**: "unknown" means the model could not run, for example no position is held or DVOL is missing; the reason is written next to it. A stale page keeps the last figures and says "Not updating".

Learn more: glossary terms *worst case*, *stop-loss*, *DVOL*, *delta exit*; OptionStrategy.md sections 6 and 8.

## Positions

One card for each strangle the bot holds: a put and a call sold on the same expiry. Cards are ordered by slot, shortest first.

**The card** has a title (coin, slot in days, expiry), the time of its data and the **premium earned so far**: a bar and words such as "62 % of the premium earned". A negative share means the position is at a loss.

**The payoff chart**: the horizontal axis is the coin's price; the vertical axis is profit and loss in coin.
- The dashed curve is the result on the expiry date.
- The solid curve is the result if the position were bought back now, from a model.
- Green fill is a gain. Hatched fill is a loss. Hatching is used so the loss does not rely on colour alone.
- The "now" marker shows today's price and the position's result at that price.
- Vertical lines mark the strikes (the *floor* is the put, the *ceiling* is the call), the break-evens at expiry, each leg's exit line (where its delta would reach the delta-exit threshold) and each leg's stop-loss level. Each kind has a button to hide it.

**The delta box** gives the strangle's net delta in coin and in USD for a 1 % move, then each leg's. A negative delta means the position gains if the price falls.

**When something looks wrong**:
- A card that says a side is missing ("no call held: repair waiting") has one leg only. The reason in brackets is the rule that holds the repair: a stop cooldown, a GEX shed, the confirmed regime side, or the margin limit.
- "Unknown" in the today curve means an implied volatility was missing for a leg. The expiry curve is still drawn.

Learn more: glossary terms *strangle*, *break-even*, *exit line*, *delta*, *stop-loss*, *missing leg*, *repair*; OptionStrategy.md sections 3, 4 and 5.

## How it works

The bot's method in three pictures, using the strangle you hold. If none is held, an example is shown and is labelled as an example.

**1. One position, two legs**: the put (the floor) and the call (the ceiling), each with its strike, what it commits to on the expiry date and the premium paid now, in coin.

**2. What you end with on the expiry date**: a chart of the result at expiry. Between the strikes, all the premium is kept. Beyond a break-even, a loss grows; that zone is hatched. The dashed line is today's price.

**3. How the bot defends the position**: four cards, in the order the rules are applied:
- *Take profit*: buy back when enough of the premium has been kept.
- *Exit line*: buy back when a leg's delta reaches the delta-exit threshold, because the move looks real.
- *Stop-loss*: buy back at market when the price of the option reaches the stop multiple.
- *Wait, then repair*: after a stop, the bot waits the repair cooldown before it sells that side again.

**When something looks wrong**: "exit lines move with the market (unknown …)" means the model has no volatility figure at the moment. The explanations are the same words as the glossary.

Learn more: glossary terms *premium*, *delta*, *strike*, *expiry*, *margin*; OptionStrategy.md sections 1 to 4.

## Activity

What the bot did and why, in plain sentences, from its own journal. Entries are grouped by day (Today, Yesterday, then dates), and the time is in 24-hour format.

**The filters**:
- *Everything*.
- *Money in or out*: opens and closes.
- *Protection*: stop-loss, delta exit, GEX shed, maintenance-margin cut, kill switch, manual close, freezes and every action by a person.
- *Waiting*: skips and holds, such as waiting for fresh GEX, a repair held after a stop, or no free expiry.

**Each entry** has a time, an icon with a text name, a bold first line and the rest of the sentence. An arrow down is money in (an option sold). An arrow up is a buy-back. A shield is protection. A clock is waiting. A person is an action by a person, with their name and role. Opens show *Paid to you*; closes show *Result*, both in coin, signed.

**When something looks wrong**: a long run of Waiting entries means a rule is holding the bot back; the sentence names it. "Show earlier days" adds three more days. An empty journal says so. The raw entries, with all their numbers, are under Details (pro).

Learn more: glossary terms *skipped*, *hold*, *freeze*, *shed*, *kill switch*; OptionStrategy.md section 9.

## Account

The account as Deribit reports it, plus the margin policy each bot applies. Figures here are Deribit's; the monitor does not recompute them.

**Account totals**: the margin model, whether cross collateral is on, and equity, margin balance, initial margin (IM) and maintenance margin (MM) in USD, with IM and MM bars. A bar turns amber from half of its limit and red from four fifths. Deribit liquidates positions when MM reaches 100 %. Then one block per asset: balance, equity, available, withdrawable and reserved.

**Margin policy per bot**:
- IM used against the active limit, and why that limit applies (the DVOL band, or a negative gamma regime).
- MM used against its limit.
- Whether new entries are allowed or frozen, with the reason.
- Any pending limit change with its countdown, for example "1 of 2 daily closes".

**The Rebalance button** resizes the positions towards the current limit. A dry run shows the plan and sends nothing. A real run asks for confirmation, and only a manager or an admin may run it.

**Not managed by the bot** lists any other position on the account, for example one bought by hand. No bot trades these, but they use margin, so they are shown.

**Privacy** has your switch for the monthly question in the assistant. It is off after you choose "Don't ask me again"; turn it back on here.

**When something looks wrong**:
- "IM over limit since …" means margin used is above the limit. The bot only warns; it sells and buys nothing to correct it by itself.
- "Frozen" means new risk waits for a limit change to be confirmed on daily closes. Exits and repairs continue.
- STALE or a failed poll means the figures are old.

Learn more: glossary terms *margin*, *IM limit*, *MM limit*, *freeze*, *rebalance*, *IM over limit*; OptionStrategy.md section 8.

## Details (pro)

The numbers page for an active trader. Figures come first and graphics are small. All figures are in coin first, with USD smaller beside them.

**The top line** counts bots online, orders sent and filled, closes and entries skipped, since a date. The **section bar** jumps to Positions, Missing legs, Trades, Working orders and Not managed. A section is amber when it needs attention.

**Open positions**: one line per leg, grouped by strangle. Columns include quantity, entry and mark prices, *premium kept*, profit and loss, |Δ| against the exit line (amber from 80 % of the threshold), distance out of the money, the stop level and the greeks. *Close…* appears only where the manual-close rule allows it: a leg on the side the confirmed regime blocks.

**Missing legs**: one row for each one-legged strangle. It shows who holds the repair (a stop, a GEX shed, the regime side), the market now and when the hold ends.

**Trades and results**: realised profit and loss per coin, the win rate, average win against average loss, results by exit reason and by slot, and one line per trade. A caret opens the rule and its numbers. *Why?* on a row asks the assistant to explain it.

**Working orders**: orders still resting, with fill progress and time left before the fill timeout.

**When something looks wrong**: a working order past its fill timeout, or a position no bot manages, turns its section amber. A loss is red and is never hidden.

Learn more: glossary terms *premium kept*, *delta exit*, *stop hold*, *working order*, *win rate*, *avg win*, *avg loss*; OptionStrategy.md sections 4, 6 and 9.

## The assistant

The **Ask** button at the bottom right of every page opens a panel. You write a question; it answers from the published help (the strategy guide, this guide, the settings reference and the glossary) and from the bot's journal of the last seven days.

**How to read an answer**: each answer lists its sources as chips. A chip opens the document section, the glossary term or the trade in Details (pro). Check the source: answers can be wrong. The line under the answer says whether it came from the help, from your journal, or was refused.

**What it will not do**: it gives no advice and no forecasts. It says "I don't know" when the sources do not cover the question. A question about money or timing, such as "should I close this?" or "where will the price go?", is refused.

**Why?** buttons on trade rows (Details and Activity) ask for the reason behind one trade, using the facts the journal recorded for it.

**A question from the team**: about once a month, a "1" on the Ask button means one optional question is waiting: how likely you are to recommend the monitor, from 0 to 10. A score of 9 or 10 asks what we do best; 0 to 8 asks what to improve first. A comment is optional and stays private with your account. "Not now" or × closes it until next month; "Don't ask me again" turns it off (Account, Privacy turns it back on). It appears only if you said yes to usage analytics, and never in your first 14 days. The assistant works the same whether you answer or not.

**Limits and flags**: each person has a number of questions an hour, shown under the box. *Flag* marks an answer for review. Questions and answers are logged for review, never the account's keys.

**When something looks wrong**: a dimmed Ask button means the assistant is off or not set up; hover for the reason. "Retry" appears when a request failed.

Learn more: OptionStrategy.md section 12; the glossary.
