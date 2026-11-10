# Monitor guide

How to read each page of the monitor.
It is written for the person who owns the account, not for developers.
Every page has one section below, with the same name as its item in the menu bar.
The menu bar shows Overview and Details (pro) directly.
Positions, Income, Worst case, Activity and Account are under **Operational**, and How it works is under **Help**.

Three rules hold on every page.
Amounts are in the bot's coin first (BTC for the BTC bot, ETH for the ETH bot) and in US dollars second, as a smaller "about" figure.
Different coins are never added together.
A figure the monitor does not have says "unknown" or "not yet" and gives the reason; it never shows a zero in its place.

Every figure is a fact from the bot's journal or from Deribit, or a labelled estimate.
Nothing here is a forecast or advice.

## Overview

The first page.
It answers one question: is the bot running, and how is it doing this month?

**The status line** at the top says whether each bot is trading ("Both trading · updated 14:02").
Under it, an amber line appears for each bot that holds something back, in plain words.
Example: "ETH sells no new puts: negative gamma regime in a bear trend".
Amber means the bot is waiting on purpose, not that it is broken.

**The five tiles**:
- *Coins earned this month*: the money kept after closes, net of Deribit fees.
  It counts only positions already closed.
- *Open now*: the profit or loss of positions still open.
  It is not realised until they close and can change every minute.
- *Worst case*: the loss of one very bad day, as an estimate.
  The Worst case page explains it.
- *Margin used*: how much of the margin limit the bot has used.
  The bar shows the limit; the footer shows the room left.
- *Net delta*: the book's direction, in coin.
  With a futures hedge it is options plus perpetual, with both under it; without one it says "options only".
  If the hedge cannot be read, it shows the options with "hedge state unavailable".

**The futures hedge line** appears under the tiles, one per bot, while a hedge is on or a position is left open.
It shows the perpetual's size and delta and its profit and loss net of fees, and links to Details (pro).
"Futures hedge paused" is amber and gives the reason; the GEX shed protects the book meanwhile.
With the hedge off and flat, nothing is shown.
On a phone the line sits under the headline and also gives the net delta.

**The profit and loss chart** has two switches: Coin or USD, and 1 day, 1 week or 1 month.
The solid line is total profit and loss, including open positions.
The dashed grey line is realised only.
A loss is shown in red and with a minus sign.

**One band per bot** shows a header with the coin, the price, the gamma regime (positive or negative), the trend (bull, bear or neutral), how old the last GEX reading is, and the margin bar.
Below it, a price chart with 60 daily candles: green for up days, grey for down days.
Red is kept for losses only.
The shaded corridor lies between the lowest and the highest strike the bot has sold.
The dashed lines are the sold strikes, SMA9 and SMA21, the swing high and low, and the gamma flip with its shed band.
Each can be switched off with its button.
On the right, one row for each strangle: its slot, expiry, days left, profit and loss in coin, and how much of the premium has been kept.
A click opens its payoff chart.

**The last trade** sits under each band: when it happened and what the bot sold or bought back.
One click on *Why?* opens the reason the bot recorded in its journal when it traded.
For a buy-back it also shows that trade's past result, net of fees, in coin and USD.
A past result is no promise of future results.
*All actions, explained* opens the Activity page.

**Since you last looked** sits under the bands when you come back.
It compares your last visit in this browser with now, for each bot: the result of the positions closed since then, after fees, in coin and USD;
each close with its reason in plain words (take profit, time roll, exit line, stop-loss), up to five, the rest on the Activity page;
and the worst case of one very bad day then and now, said as larger, smaller or about the same.
A warning in the header that began while you were away (HALTED, OFFLINE, STALE, IM over limit) gets one line pointing at it.
With a futures hedge, the hedge's lines are listed in the same plain words as the Activity page, up to five; a line that changes the book's protection comes first with ⚠, such as a hedge that cannot trade, so the GEX shed is back on.
They come from the last 500 journal lines the bot keeps, so an older one may be missing after a long time away.
When nothing was closed or sold and the hedge did nothing it says "Nothing happened since" and the time.
The time of your visit stays in this browser only: it is not sent anywhere, so another browser or phone keeps its own.
It counts after the page has been open for 10 seconds, so a quick reload keeps the summary.
Away more than 30 days, it covers the last 30 days.
On your first visit there is no card, only a line saying what it will show next time.
*Hide until my next visit* hides it until then.

**When the bot holds nothing**, the band says "Nothing held" and the *Open now* and *Worst case* tiles say "nothing open" instead of a zero. On a phone, the *Open now* figure of the *Making or losing* card says "nothing open" and the *Worst case right now* card says "Nothing held: a bad day costs nothing."

**A loss looks like a gain, with a minus sign**: on every page that shows a result (Overview, Income, Details (pro), Worst case, Positions) a loss has the same size and weight as a gain, a minus sign and its unit, and is never folded away. Red is only a second cue.

**Colours and marks**:
- A dimmed band means the bot's data is stale; the age of the last answer is shown.
- "GEX 40s ago" turns amber after five minutes.
  Until the GEX reading is fresh again, the bot opens nothing new.
- A bot that is down says so on its band.

**When something looks wrong**:
- Stale data or a bot down: look at the header chips (STALE, OFFLINE, LOOP STALLED, HALTED).
  LOOP STALLED means the page answers but the bot's decision loop has not run for three intervals, so exits are not being checked.
- A held leg: the amber line names the rule.
  Open Positions or Details (pro), Missing legs, to see when it is released.
- A loss: open Activity for the bot's own sentence on each close, then Details (pro) for the rule and numbers.

Learn more: glossary terms *gamma regime*, *trend*, *freeze*, *margin*; OptionStrategy.md sections 3, 8 and 9.

## Income

Month by month, where the money came from.
It reads the journal's closes, after fees.

**Four tiles**: *Last 6 months, after fees*; *Average month* (with how many months were up and down); *Worst month*; *Worst week* (as a share of the account).
A switch changes the amounts between the coin and USD.

**The table** has one row per UTC month, newest first:
- *Paid to you*: the premium received when the options were sold.
- *Paid to buy back*: what it cost to close them.
- *Exchange fees*: Deribit's fees, shown as a negative number.
- *Futures hedge*: shown only while a month has one: the perpetual's realised result, minus its fees, with its funding.
  Its open gain or loss counts when it closes; funding comes from Deribit's reading of the position, so treat it as an estimate.
- *Result*: paid to you, minus buy-back, minus fees, plus the futures hedge.
- *Holding alone*: what the same coin would have gained or lost if it had only been held.
  In the coin it is 0 by definition; in USD it is the price change.

A month with a loss is tinted and marked "loss".
A month without closes says "no closes".
The current month says "so far".
Before the first close, every tile says "not yet", never a zero.

**% of start**: the switch's third choice shows every result as a % of the start balance.
The start balance is the account's balance on Deribit when the period began, with the time it was recorded.
Four tiles: *Bot result*, *Holding alone* (in USD; 0 % in the coin), *Bot + holding* and *Worst week*, all measured from that same time.
The table shows each month from its own first balance.
A month whose balance was recorded after it began says "partial: from" that date; a month with no balance says why and shows no %.
Because each result is a share of its own balance, two bots of different size compare directly.
To start two environments at the same time, add `&from=` and a time to the address, for example `/income?bot=btc&from=2026-10-28T11:45Z`.
The worst week needs 7 days after the start balance; before that it says "not yet" and when it will be known.

**When something looks wrong**: a loss month is normal data, not an error; the table keeps it in the open.
"Unknown" in *holding alone* means the price for that month was not recorded.
Testnet income is practice money, not real income.

Learn more: glossary terms *income over months*, *worst month*, *worst week*, *start balance*, *% of start*, *premium*, *exchange fees*; OptionStrategy.md section 4.

## Worst case

An estimate of what one very bad day would cost the positions held right now.
It is a model, not a measurement and not a forecast.

**The headline** is the worse of the two big-move days (down and up), as a loss in coin, in USD and as a share of the account.

**The five rows** show what different days would do: a very bad fall (−3 daily standard deviations), a bad fall (−2), a quiet day (0), a strong rise (+2) and a very strong rise (+3).
Each row has the price move in percent, how big that is in typical daily swings, and the result with its sign, in coin and USD.
A typical daily swing comes from DVOL, the market's own measure of expected volatility.

**With the futures hedge open**, every figure includes the perpetual's gain or loss at the moved price, counted from its average price, so a row can include gain or loss the perpetual already holds.
A second line under the headline gives the options alone.

No row says how often such a day happens.
The model understates how large crypto moves can be, and there is no measured figure.

**What the bot does on a bad day** is shown beside it: it closes a leg when its price reaches the stop-loss multiple of what it was sold for.
A stop is a trigger, and a fast move can fill it at a worse price than the stop level.

**When something looks wrong**: "unknown" means the model could not run, for example no position is held or DVOL is missing; the reason is written next to it.
A stale page keeps the last figures and says "Not updating".

Learn more: glossary terms *worst case*, *stop-loss*, *DVOL*, *delta exit*; OptionStrategy.md sections 6 and 8.

## Positions

One card for each strangle the bot holds: a put and a call sold on the same expiry.
Cards are ordered by slot, shortest first.

**The card** has a title (coin, slot in days, expiry), the time of its data and the **premium earned so far**: a bar and words such as "62 % of the premium earned".
A negative share means the position is at a loss.

**The payoff chart**: the horizontal axis is the coin's price; the vertical axis is profit and loss in coin.
- The dashed curve is the result on the expiry date.
- The solid curve is the result if the position were bought back now, from a model.
- Green fill is a gain.
  Hatched fill is a loss.
  Hatching is used so the loss does not rely on colour alone.
- The "now" marker shows today's price and the position's result at that price.
- Vertical lines mark the strikes (the *floor* is the put, the *ceiling* is the call), the break-evens at expiry, each leg's exit line (where its delta would reach the delta-exit threshold) and each leg's stop-loss level.
  Each kind has a button to hide it.

**The delta box** gives the strangle's net delta in coin and in USD for a 1 % move, then each leg's.
A negative delta means the position gains if the price falls.

**With a futures hedge**, two more blocks come first, for that bot only:
- *The whole book, for a one-day move*: a chart and a table of the book's result at −10, −5, +5 and +10 %, with the hedge (solid) and the options alone (dashed), coin first.
  It is a model, not a quote.
  The hedge takes out the slope (delta), not the curve (gamma): the book still loses on a big move either way.
- *The hedge card*: the perpetual's size, delta and net delta, average and mark, unrealised and realised results, and funding.

**Each strangle's hedge share**: while the bot holds a perpetual, every payoff chart includes that strangle's part of it, here, on the overview cards and in the window that opens from a leg.
A strangle's share is the perpetual × its part of the options' net delta, so the shares add up to the whole perpetual.
- The chart opens with its hedge share: the solid blue curve is bought back today, the dashed light blue curve is at expiry, and the faint line is the options alone today.
- Two buttons switch between *With its hedge share* and *Options only*; the compact overview card has no buttons.
- The delta box adds this strangle's delta, its hedge share and the net.
- A table gives the result at −20, −10, −5, +5, +10 and +20 %, options only and with its hedge share, coin first, USD at the moved price.
- Break-evens, exit lines and stop levels stay the options' own, because they are option rules; the legend lists where the hedged curve at expiry crosses zero.
The hedge is drawn as held now.
The bot re-hedges each time the book's delta leaves its band, so a real move lands near the solid blue curve, minus fees.

When the options' net delta is inside the hedge band, the shares would blow up, so the hedge is not split.
Each card then shows the options alone with the line "Hedge counted on the whole book" and a link to the whole-book block.
With the hedge off and no perpetual, the card is the same as without a hedge.

**When something looks wrong**:
- A card that says a side is missing ("no call held: repair waiting") has one leg only.
  The reason in brackets is the rule that holds the repair: a stop cooldown, a GEX shed, the confirmed regime side, or the margin limit.
- "Unknown" in the today curve means an implied volatility was missing for a leg.
  The expiry curve is still drawn.

Learn more: glossary terms *strangle*, *break-even*, *exit line*, *delta*, *stop-loss*, *missing leg*, *repair*; OptionStrategy.md sections 3, 4 and 5.

## How it works

The bot's method in four pictures, using the strangle you hold.
If none is held, an example is shown and is labelled as an example.
While a futures hedge is on or holds a position, the introduction says so in one sentence.

**1. One position, two legs**: the put (the floor) and the call (the ceiling), each with its strike, what it commits to on the expiry date and the premium paid now, in coin.

**2. What you end with on the expiry date**: a chart of the result at expiry.
Between the strikes, all the premium is kept.
Beyond a break-even, a loss grows; that zone is hatched.
The dashed line is today's price.

**3. How the bot defends the position**: five cards, the first four in the order the rules are applied:
- *Take profit*: buy back when enough of the premium has been kept.
- *Exit line*: buy back when a leg's delta reaches the delta-exit threshold, because the move looks real.
- *Stop-loss*: buy back at market when the price of the option reaches the stop multiple.
- *Wait, then repair*: after a stop, the bot waits the repair cooldown before it sells that side again.
- *Market turns*: this card follows the futures hedge.
  On, the bot keeps both legs and the perpetual cancels the lean.
  Paused or not holding, the GEX shed protects the book until the hedge can trade.
  Off, it is the GEX shed, and the hedge can be turned on in Account.

**4. The safety net: a futures hedge**: a testnet experiment, with its state beside the title: ON, PAUSED, NOT HOLDING or OFF.
A picture shows the options' delta and the perpetual's delta as bars, the net as a dot and the hedge band shaded, from the bot's live figures, with the same numbers in words under it.
With the hedge off and flat there is no perpetual bar.
Four cards say what it does, what it replaces, what it does not do and where you see it.
The hedge removes the lean, not the curve: a big move still costs money.
If the hedge cannot be read, the page says the live figures are unavailable and keeps the explanation without numbers.

**When something looks wrong**: "exit lines move with the market (unknown …)" means the model has no volatility figure at the moment.
The explanations are the same words as the glossary.

Learn more: glossary terms *premium*, *delta*, *strike*, *expiry*, *margin*, *perpetual*, *hedge band*, *coin delta*; OptionStrategy.md sections 1 to 4.

## Activity

What the bot did and why, in plain sentences, from its own journal.
Entries are grouped by day (Today, Yesterday, then dates), and the time is in 24-hour format.

**The filters**:
- *Everything*.
- *Money in or out*: opens and closes, and the futures hedge's trades.
- *Protection*: stop-loss, delta exit, GEX shed, maintenance-margin cut, kill switch, manual close, freezes and every action by a person; also a futures hedge trade, and a hedge that stopped holding the book.
- *Waiting*: skips and holds, such as waiting for fresh GEX, a repair held after a stop, no free expiry, or a futures hedge order that waits or found nothing to fill.

**Each entry** has a time, an icon with a text name, a bold first line and the rest of the sentence.
An arrow down is money in (an option sold).
An arrow up is a buy-back.
A shield is protection.
A clock is waiting.
A person is an action by a person, with their name and role.
Opens show *Paid to you*; closes show *Result*, both in coin, signed.

**The futures hedge's lines** say what the hedge did and why, with their numbers.
A trade names its side and size in USD of the perpetual, and the book's coin delta before and after against its band: "Sold 30,000 USD of the perpetual to bring the book's coin delta back inside its band (it was +0.4200 BTC, about +0.0200 BTC after; the band is ±0.3000 BTC)."
The delta after is an estimate from the fill's size and price.
An order that filled in part or not at all says so.
Closing the hedge shows its *Result* net of its fee, a loss with its minus sign.
When the hedge cannot trade, the line says so and that the GEX shed is back on.
A leg the shed would have bought back and the hedge kept has its own line.

**When something looks wrong**: a long run of Waiting entries means a rule is holding the bot back; the sentence names it.
"Show earlier days" adds three more days.
An empty journal says so.
The raw entries, with all their numbers, are under Details (pro).

Learn more: glossary terms *skipped*, *hold*, *freeze*, *shed*, *kill switch*; OptionStrategy.md section 9.

## Account

The account as Deribit reports it, plus the margin policy each bot applies.
Figures here are Deribit's; the monitor does not recompute them.

**Account totals**: the margin model, whether cross collateral is on, and equity, margin balance, initial margin (IM) and maintenance margin (MM) in USD, with IM and MM bars.
A bar turns amber from half of its limit and red from four fifths.
Deribit liquidates positions when MM reaches 100 %.
Then one block per asset: balance, equity, available, withdrawable and reserved.

**Margin policy per bot**:
- IM used against the active limit, and why that limit applies (the DVOL band, or a negative gamma regime).
- MM used against its limit.
- Whether new entries are allowed or frozen, with the reason.
- Any pending limit change with its countdown, for example "1 of 2 daily closes".

**The Rebalance button** resizes the positions towards the current limit.
A dry run shows the plan and sends nothing.
A real run asks for confirmation, and only a manager or an admin may run it.
When IM is over the limit by 1.5 points or less and the limit has not changed, the plan buys nothing back: that is market drift, and the positions are shown as kept.
Further over, it buys back toward the limit as before.
<!-- guide: route:POST /api/bots/[bot]/rebalance -->

**Futures hedge (experiment, testnet)**: one card per bot.
Off by default; while it is on, the GEX shed keeps its legs and the bot trades the coin's perpetual future to hold the book's direction (delta) near zero.
The card shows the options' delta, the perpetual and the net delta, how many hedge orders went out in the last hour, and the comparison with the shed since the experiment started: positive when the hedge did better, negative (in red, with its minus sign) when the shed did.
The comparison is an estimate: the side that is not trading is followed on paper.
On testnet the comparison is priced at the index: fills and marks at the index price, funding left out, because the testnet perpetual trades about 3.5 % from the index; the card says "benchmark priced at the index (testnet)".
If the hedge cannot trade, the GEX shed works as usual until it can, so the book is never left with neither; the card shows "Not holding" with the reason.
When the exchange will not take its orders for a reason a retry cannot cure, such as scheduled maintenance or too few funds, the card shows "Paused" with the reason: no hedge orders are sent, the bot looks again every 10 minutes and resumes by itself.
Only an admin may switch it.
Switching off first shows what it would do: the perpetual bought back or sold at market with its result and fee, the delta and the margin before and after, and what a 5 % or 10 % move would cost the options without the hedge.
If that preview cannot be read, the window says so and the admin can still switch off.
If the perpetual cannot be closed, the experiment stays on.
The kill switch closes the perpetual too, but only the size the bot itself holds: a perpetual beyond that, such as one held by hand, stays open for you to close, and the journal says so (`perp_left_open`).
Without a hedge position the kill switch sends no futures order.
The hedge's perpetual is the bot's own position, so "Not managed by the bot" does not list it: only the part beyond what the bot holds.
The hedge also shows on Overview, Positions and Details (pro).
The terms on the card have a plain-words tip each, in the help: coin delta, perpetual, inverse perpetual, hedge band, hedge vs shed, hedge unavailable and kill switch perpetual.
A hedge band is how far the coin delta may drift from zero before the hedge trades: 5 % of the margin balance.
<!-- guide: route:POST /api/bots/[bot]/hedge -->

**Not managed by the bot** lists any other position on the account, for example one bought by hand.
No bot trades these, but they use margin, so they are shown.

**Privacy** has your usage analytics switch, showing your answer (Yes, No or Not asked yet).
Switching it off stops collection at once, and events already stored stay until you delete your account.
It also has your switch for the monthly question in the assistant.
It is off after you choose "Don't ask me again"; turn it back on here.

**When something looks wrong**:
- "IM over limit since …" means margin used is above the limit.
  The bot only warns; it sells and buys nothing to correct it by itself.
- "Frozen" means new risk waits for a limit change to be confirmed on daily closes.
  Exits and repairs continue.
- STALE or a failed poll means the figures are old.

Learn more: glossary terms *margin*, *IM limit*, *MM limit*, *freeze*, *rebalance*, *IM over limit*; OptionStrategy.md section 8.

## Details (pro)

The numbers page for an active trader.
Figures come first and graphics are small.
All figures are in coin first, with USD smaller beside them.

**The top line** counts bots online, orders sent and filled, closes and entries skipped, since a date.
The **section bar** jumps to Positions, Missing legs, Trades, Working orders and Not managed, and to Futures hedge while one is shown.
A section is amber when it needs attention.

**Futures hedge**, under Open positions, while a hedge is on or a position is left open: one row per bot with the perpetual's position, its delta and the net delta, average and mark, unrealised, realised less fees, and funding ("left out on testnet" when the benchmark is priced at the index).
The row says how close the hedge holds the delta and how many orders went out in the last hour, and links to the switch on Account.
A row whose perpetual was not read for two cycles is dimmed with its age.

**Open positions**: one line per leg, grouped by strangle.
Columns include quantity, entry and mark prices, *premium kept*, profit and loss, *Position Δ*, *|Δ| per contract → exit* (amber from 80 % of the threshold), distance out of the money, the stop level and the greeks.
*Position Δ*: the delta of what you hold (one contract's Δ × the size, sign of the short), in coin, then in USD for a +1 % move.
The legs add up to the strangle, the strangles to the book; the foot rows add the futures hedge.
Example: a put at Δ −0.227, 0.2 sold, is +0.045 BTC; with a call at Δ +0.088, 0.2 sold (−0.018 BTC), the strangle's net is +0.028 BTC.
*|Δ| per contract → exit* is one contract's delta, the number the delta exit checks — don't add it up.
Under the last strangle of each bot, *All options* sums its position Δ, and while a futures hedge is shown, *Futures hedge* gives the perpetual's delta and *Book* the two together.
The Book row's note gives the coin delta the hedge holds flat, which also counts the options' own value moving with the price, and whether it is inside the hedge band.
*Close…* appears only where the manual-close rule allows it: a leg on the side the confirmed regime blocks.

*Opened* is when the bot sold the leg, with its age.
A roll or a repair opens a new leg on another strike, so a leg that showed a large gain can be replaced by one that starts a little negative: the opening fees and the spread come first, and time decay earns them back.
<!-- guide: route:POST /api/bots/[bot]/close -->

**Missing legs**: one row for each one-legged strangle.
It shows who holds the repair (a stop, a GEX shed, the regime side), the market now and when the hold ends.

**Trades and results**: realised profit and loss per coin, the win rate, average win against average loss, results by exit reason and by slot, and one line per trade.
A caret opens the rule and its numbers.
*Why?* on a row asks the assistant to explain it.

**Working orders**: orders still resting, with fill progress and time left before the fill timeout.

**When something looks wrong**: a working order past its fill timeout, or a position no bot manages, turns its section amber.
A loss is red and is never hidden.

Learn more: glossary terms *premium kept*, *delta exit*, *stop hold*, *working order*, *win rate*, *avg win*, *avg loss*; OptionStrategy.md sections 4, 6 and 9.

## The assistant

The **Ask** button at the bottom right of every page opens a panel.
You write a question; it answers from the published help (the strategy guide, this guide, the settings reference, the settings' tips and the glossary) and from the bot's journal of the last seven days.

**How to read an answer**: each answer lists its sources as chips.
A chip opens the document section, the glossary term or the trade in Details (pro).
Check the source: answers can be wrong.
The line under the answer says whether it came from the help, from your journal, or was refused.
Ask what a setting does, such as "what does the repair cooldown do?", and the answer uses the words of that setting's tip, with a chip named "Tip: repair cooldown".

**What it will not do**: it gives no advice and no forecasts.
It says "I don't know" when the sources do not cover the question.
A question about money or timing, such as "should I close this?" or "where will the price go?", is refused.
It answers in its own words and never just sends you to read a document.
When it cannot back an answer with the bot's records and the help, it says it couldn't confirm an answer and won't guess.
It answers in English, whatever language you write in: its checks for advice and forecasts read English only, so an answer in another language is never shown.

**How did I do this week or this month?**: ask about your results, such as "how did I do this month?", "what was my worst week?" or "did I beat just holding my coin?", and the answer reads the bot's figures for the last 7 and 30 days.
They are net P&L after fees in coin and in USD, the return on the account, what holding the coin did over the same days, the largest fall and the worst week with its dates.
Its chip names the window, and the answer says whether the figures are testnet or live.
A loss is shown with its minus sign, in the same words as a gain.
A figure the bot could not measure, such as the return when no balance was recorded, is called unknown with the reason; days with no records are named, never filled in.
These are past results: a question about future results, such as "will I make money next month?", is refused.

**Why is nothing open, or why is my margin unused?**: ask, such as "why is nothing open right now?" or "why isn't it opening more trades?", and the answer reads the bot's book as it is now.
It says whether the bot may open trades and, if not, why: waiting for a fresh reading of the market makers' positioning (GEX), new trades frozen by the margin policy, or the kill switch.
It gives the margin in use against its limit as percentages, how many slots hold a strangle, the latest reason each empty slot recorded, and a repair held for its cooldown.
When every slot is full and the margin is near its limit, it says so; whether to change a limit is your decision, and the assistant does not advise it.
When the bot does not answer, the reason is called unknown, never guessed.

**Why?** buttons on trade rows (Details and Activity) ask for the reason behind one trade, using the facts the journal recorded for it: the rule, its numbers, the instrument, the time and the market then.

**A question from the team**: about once a month, a "1" on the Ask button means one optional question is waiting: how likely you are to recommend the monitor, from 0 to 10.
A score of 9 or 10 asks what we do best; 0 to 8 asks what to improve first.
A comment is optional and stays private with your account.
"Not now" or × closes it until next month; "Don't ask me again" turns it off (Account, Privacy turns it back on).
It appears only if you said yes to usage analytics, and never in your first 14 days.
The assistant works the same whether you answer or not.

**Limits and flags**: each person has a number of questions an hour, shown under the box.
*Flag* marks an answer for review.
Questions and answers are logged for review, never the account's keys.
<!-- guide: route:POST /api/assistant, route:POST /api/assistant/flag -->

**Did this help?**: under an answer, *Helped* and *Didn't help* tell the team whether it solved your question, in one click and without writing anything.
The choice can be changed once.
*Didn't help* also offers *Report it as wrong*, which flags the answer; nothing is flagged unless you press it.
The buttons appear only under an answer, not under "I don't know", a refusal, the safe fallback reply or a request draft.
Your choice is your opinion of the answer, not a check of whether it was correct; the weekly review counts the choices, never your question.
<!-- guide: route:POST /api/assistant/feedback -->

**How to report a bug or ask for a feature**: describe it in the Ask box with what you saw, where (the page, the bot and the time) and the evidence (a number, a journal entry or the steps to see it again).
When something is missing, the assistant lists what to add.
When it has enough, it shows a draft request, a bug or an enhancement.
Nothing is sent until you press *Send for review*; *Edit* changes any field and *Discard* drops it.
A sent request goes to a private review queue, not to the public repository, and amounts, account details and personal data are removed before anything is made public.
Each person may send 10 requests a day.

**My requests**: the tab in the Ask panel lists the requests you sent, newest first, with their status (In review, Filed, Fixed, Not planned, Not a bug, Duplicate or Needs info) and the team's note.
You see only your own.
<!-- guide: route:POST /api/assistant/requests -->

**When something looks wrong**: a dimmed Ask button means the assistant is off or not set up; hover for the reason.
"Retry" appears when a request failed.

Learn more: OptionStrategy.md section 12; the glossary.
Features that are not menu pages (the survey, sign-in, consent, roles, environments, videos and the admin pages) are described in the sections below.

## The monthly survey

<!-- guide: survey:nps, route:POST /api/survey, route:POST /api/survey/nps -->

The survey is one optional question in the Ask panel: how likely you are to recommend the monitor, from 0 to 10.
This is the NPS survey.

**How to answer the NPS survey**: when it is due, a "1" shows on the Ask button.
Open Ask and the question is the first card.
Pick a number from 0 to 10.
A score of 9 or 10 then asks what we do best.
A score of 0 to 8 asks what to improve first.
Choose from the short list of reasons and add a comment if you want; the comment is optional and stays private with your account.
The answer never includes amounts.

**How to skip the survey**: "Not now" or × closes it until next month.
"Don't ask me again" turns it off.
Account, Privacy has a switch that turns it back on.
The assistant works the same whether you answer or not.

**Is my survey answer in the audit trail?** Only the fact that you answered, and which survey, with your username and the time.
Your score, reasons and comment are not in it.
Turning the survey off or back on is recorded the same way.

**When it shows**: at most once every 30 days, counted from the day it was shown, answered or not.
It shows only when you said yes to usage analytics, only after your account is 14 days old and you have signed in at least 3 times, and not while a bot's last 7 days is a net loss or cannot be read.

**Can I open the survey myself?** No. You cannot trigger it on demand: it appears once every 30 days when you have agreed to analytics.
When it is due you can still answer it in the Ask panel, or skip it.

Learn more: "Analytics and your consent" and "The assistant" in this guide.

## The four answers questions

<!-- guide: survey:answers, route:POST /api/survey/answers -->

The four answers questions are two optional questions in the Ask panel: what you came to check today, and what was hardest to find.
Each offers the same five choices: what it holds, making or losing, the worst case, why the last trade, or none of these.

**How to answer the four answers questions**: when they are due, a "1" shows on the Ask button.
Open Ask and the questions are the first card.
Pick one choice for either question, or for both, and press Send.
You can leave one blank.
There is no text box, and the answer never includes amounts.

**How to skip the four answers questions**: "Not now" or × closes the card.
Closing the Ask panel without answering skips it too.
It is not asked again for 30 days.

**When the four answers questions show**: at most once every 30 days, counted from the day they were shown, answered or not.
They show only when you said yes to usage analytics, only after your account is 14 days old and you have signed in at least 3 times.
They never show in the same session as the monthly survey, nor within 7 days after it.
Once shown on one device or tab, they do not show on another.

Learn more: "The monthly survey" and "Analytics and your consent" in this guide.

## Sign-in and password

<!-- guide: screen:login, route:POST /api/auth/login, route:POST /api/auth/logout, route:POST /api/auth/refresh, route:POST /api/auth/password, route:POST /api/auth/reset -->

You sign in at the sign-in page with a username and password.
Every page and every part of the monitor needs a signed-in session; without one you are sent to the sign-in page.
A session is kept alive while you use the monitor and ends after 14 days or when you sign out.

**How to change your password**: choose Change password in the account menu.
Enter the current password and the new one twice.
The new one must differ from the current one and be at least 12 characters.
Your sessions on other devices end when you change it.

**Which passwords are refused**: besides the 12 characters, a new password is refused when it is too common or contains your username, and the form says which.
Too common means it is on a list of about 1,000 common passwords, even with capitals, with look-alikes such as p@ssw0rd, or with digits or symbols added before or after.
The rule applies to a new user, a password change and both kinds of reset.
A password you already have keeps working, and a refused password on a reset link leaves the link usable.

**How to reset a forgotten password**: ask an admin.
An admin issues a single-use reset link on the Users page.
Open the link, choose a new password and sign in with it.
The link works once and then stops working.

**Signed out everywhere**: a password change, a password reset, a role change or the deletion of your account signs you out on every device.
Sign in again to continue.

Learn more: "Roles" in this guide.

## Active sessions

<!-- guide: route:DELETE /api/auth/sessions/[id] -->

Active sessions lists every browser where you are signed in to the monitor, whatever your role. You can sign out the ones you don't recognise, such as a lost phone or a shared computer, without changing your password.

**How to see where I am signed in**: choose Active sessions in the account menu. Each row shows the browser, when it signed in and when it was last used. The browser you are using is marked This browser. No address is kept or shown.

**How to sign out another device**: choose Sign out on its row. From that moment the browser can no longer change anything, and it goes to the sign-in page within 15 minutes, when its sign-in renews and fails. Until then it can still read pages.

**Signing out this browser**: use Sign out in the account menu. Active sessions does not sign out the browser you are using.

Learn more: "Sign-in and password" in this guide.

## Deleting your account

<!-- guide: survey:exit, route:DELETE /api/auth/account -->

You can delete your own monitor account at any time, whatever your role.

**How to delete my account**: choose Delete my account in the account menu.
The first screen says what is deleted: your sign-in, your usage analytics events and your survey answers, with any text you wrote.
It cannot be undone.
Press Continue, then Delete my account.

**What deleting my account does not touch**: your exchange accounts and funds.
The bots keep trading, and deposits and withdrawals stay on the exchange.

**The question before you go**: below the delete button, one optional question asks the main reason you leave.
Pick a reason, or Other with a few words if you want.
Skip and delete, or ×, deletes the account at once without an answer; so does Delete my account with nothing picked.
The answer is kept only if you said yes to usage analytics, and it is deleted with your account.
Escape or Back returns to the first screen without deleting.

**Why can't I delete my account as an admin?** The monitor always keeps one admin.
If you are the only admin, make another person an admin on the Users page first, then delete your account.

Learn more: "Analytics and your consent" in this guide.

## Roles

There are three roles.
A **user** may see every page.
A **manager** may do what a user does and also close a leg by hand and rebalance by hand, including the dry run.
An **admin** may do what a manager does and also use the admin pages: Users, Audit and Analytics.
Only an admin may export the audit trail.

**Who can rebalance?** Managers and admins.
Closing a leg by hand is the same.
A user may still see the buttons, but the monitor refuses the action and the bot receives nothing.
The role is checked each time you act, so a role change takes effect at once.

**Is every action checked and recorded?** Yes.
Every action that changes something is checked on the server against your role, whatever the page shows: the wrong role is refused and nothing changes.
An allowed action is recorded in the audit trail with your username, the action and the time.

**Can I check a setting value before a comparison run?** Managers and admins can.
The monitor says whether the value is inside the range the bot accepts and, if not, which rule it breaks in plain words.
Rules that depend on a whole list, such as the margin bands' "the lowest band starts at 0", are listed and said not checked, because one value does not show which band it replaces.
It also says whether the setting is a core policy, where a new default needs the product owner, what the setting does to risk and income, and its current value.
The check changes nothing: no config file or bot setting is touched.
No screen shows it yet.

<!-- guide: route:POST /api/lab/validate -->

**Can I ask for a comparison run of a setting I am thinking of changing?** Managers and admins can.
You send one or more proposed values, and each one is checked first in the same way.
If a value breaks a rule, the monitor gives the reason and nothing is queued.
A valid request waits in a queue with the status queued, and the audit trail records your username.
A runner takes one request at a time and compares the bot's current settings with yours on the last seven days of past data, and also tries your number a little higher and a little lower.
The status then goes to running, and then to done, or to failed with a short reason.
A request that has not run within a day is marked expired.
You may have three requests waiting or running at a time.
A finished run appears in the list of runs, in percentages only, labelled "backtested, not a forecast".
The request changes nothing: no config file or bot setting is touched, and a change takes effect only after a manager confirms it.
No screen shows it yet.

<!-- guide: route:POST /api/lab/requests -->

**Can I see what confirming a comparison run would change?** Managers and admins can ask for a preview of a finished run.
It shows each setting with its current value and the proposed one, the account, your username and the run's numbers, labelled "backtested, not a forecast", with any fragile spots the run found.
If the setting is a core policy, the preview says a product owner must approve it.
If the run is more than seven days old, or the value it started from is no longer the account's, the preview says the run is stale.
In both cases, or when the run has no numbers for the proposed value, it cannot be confirmed, and the preview says why.
The audit trail records one line with your username, the account, the run and both values.
The preview changes nothing: no config file or bot setting is touched, and confirming a change is not built yet.
No screen shows it yet.

**Can the assistant tell me what a comparison run showed?** Managers and admins can ask it, for example "what did my last comparison show?".
It answers from the lab's latest runs: the return, the worst week, the drawdown and the fees of the current and the proposed settings, in percentages only, labelled backtested, with the worst week and the drawdown beside each return, and it names the run it read.
A request still queued or running, or one that failed or expired, is answered with its status and its reason; with no run at all the answer is that it is unknown.
The written analysis of a run is not available yet, so the assistant does not quote it.
It does not advise whether to apply a change.
A user who asks gets no run figure.

Each environment has its own people and roles.

Learn more: "Sign-in and password" and "For admins" in this guide.

## Analytics and your consent

<!-- guide: route:POST /api/consent, route:POST /api/events -->

The monitor collects usage analytics only if you say yes.
Your answer is stored with your account.
Until you answer, or if you say no, nothing is collected.

**What the monitor collects about me**: which pages and features are used and how (for example that a help video was played), under a random session code.
Events outside a fixed list are refused.
**What it does not collect**: no keys, no passwords, no amounts, no positions, no orders and no text you type in the assistant.

**The consent question**: the first time you sign in, a card above the page asks once whether the monitor may record how you use it.
Yes and No look the same and nothing is chosen for you.
The card has no close button: an answer closes it, and it does not come back.
It never covers the page, and nothing is sent until you say yes.
If your answer could not be saved, the card says "Not saved" and offers Try again.

**How to say yes or no**: on that card, or at any time in Account, Privacy, where the usage analytics switch shows your answer (Yes, No or Not asked yet).
Saying no stops collection at once.
Every change of your answer is recorded in the audit trail.

**What happens to events already stored**: they stay until you ask for their deletion.
Deleting your account deletes them (Delete my account in the account menu); an admin can also delete them on request.

Learn more: "The monthly survey" in this guide.

## Which environment you are looking at

Each environment is a separate monitor with its own bot, Deribit account, people and passwords.
The header shows which one you are in.
**Stable** is the release track, where a tagged version runs. **mvp** runs the newest version of the code, where changes are tried first.
A **testnet** label means the bot trades on Deribit's test exchange with test money, not real capital.
Numbers from the two environments are never added together.

Learn more: OptionStrategy.md section 12.

## How-to videos

Short how-to videos explain one task each: reading the summary card, one position and the worst case, why the last trade, the margin bar, why entries are frozen, closing a leg by hand, previewing a rebalance, and income month by month.
They run 45 to 75 seconds and have captions.
They are planned: none plays in the monitor yet.

Learn more: "The assistant" in this guide.

## For admins

These pages are for the admin role only.
A manager or a user who opens one is refused.

**Users**: lists the people of this environment with role, email, when created and last sign-in.
An admin creates a user (a username of 3 to 64 letters, digits, dots, underscores or hyphens, and a password of at least 12 characters), changes a role, resets a password, issues a single-use reset link, signs a person out everywhere, or deletes a user.
Every change signs that person out everywhere.
There is always at least one admin, and you cannot delete yourself.
<!-- guide: page:/admin/users, route:POST /api/admin/users, route:PATCH /api/admin/users/[id], route:DELETE /api/admin/users/[id], route:POST /api/admin/users/[id]/password, route:POST /api/admin/users/[id]/reset-link, route:POST /api/admin/users/[id]/signout -->

**Audit**: the trail of who did what: sign-ins, user changes, role changes, consent changes, closes and rebalances by hand, and survey answers (which survey only, never the answer).
It is read-only.
<!-- guide: page:/admin/audit -->

**Audit export**: Export CSV, next to the filters, downloads the lines that match the filters you applied, newest first, for example one person's lines for one week.
The columns are time (UTC), person, role, action, target and detail; the chain's fingerprints are left out, as on the page.
A value a spreadsheet would run as a formula starts with an apostrophe, so it is read as text.
One file holds the newest 50,000 lines at most; when there are more, the page says the export was cut, and narrower filters get the older ones.
Only an admin may export, and the file is a copy of personal data, so keep it as carefully as the trail.
Each export is recorded in the trail itself as audit_exported, with your username, the filters and the number of lines, never the lines.
When the monitor keeps its users in memory, the button is disabled because the trail is off.
<!-- guide: route:GET /api/admin/audit/export -->

**Audit chain**: each line of the audit trail carries a fingerprint of the line before it, so a line changed or removed by hand shows.
Under the filters the Audit page says "Chain intact" with how many lines were checked, or "Chain broken at line N" with its time and why: a line before it was removed, reordered or edited, or this line was edited or is damaged.
Check again runs the check once more.
A broken chain hides no line: the trail below still lists every line.
Lines written before the chain began are legacy and are named as not covered.
When the trail grows past its size limit it continues in a new file, and the check and the pages read across both.
Nothing is ever deleted.
The check shows tampering; it cannot stop someone who can rewrite the whole trail.
<!-- guide: route:GET /api/admin/audit/verify -->

**Analytics**: the weekly usage report (Usage), built only from the events of people who said yes.
A Delivery view is not on the page yet.
<!-- guide: page:/admin/analytics, screen:analytics -->

**Stopped coming back** (on Analytics): how many people visited in the 4 weeks before and did not visit this week, by weeks since their last visit and by role.
Counts only, never a name.
When their share rises 20 points or more over the 3 weeks before, the card shows DROP and sits under "Needs your attention".
Under 10 people it gives a small-sample note and no share.
Nobody gone shows "No one stopped coming back".

**Do they come back** (Q2 on Analytics): each group of people who first came in the same week, and the share of them back in each of the 12 weeks after.
A week that has not ended yet shows —, never 0 %.
The Curve column says whether the group's return has levelled off: **flattening** when each of its last 3 weeks is within 5 points of the week before, **falling** when one dropped by more, **rising** when one rose by more.
Under 10 people, or before 4 weeks have ended, it says **too few to judge** and makes no claim.
On a phone the table scrolls sideways inside its card.

**Activation in the first day** (on Analytics): of the people whose first visit fell in a week, how many opened the Overview and then the worst case, a trade's reason, a tip or the assistant within 24 hours of that first visit.
It shows "8 of 12", the share and the median minutes to that answer, for the last 12 weeks.
Under 10 new people it says **too few to judge**, gives no share and raises no alert.
A week with no events is named under the table, never counted as zero.
Only people who said yes to analytics are counted.

Learn more: "Roles" and "Analytics and your consent" in this guide.

## Notifications

<!-- guide: route:POST /api/notifications -->

The **bell** in the menu bar, before your name, lists what happened to your book and your requests, and what waits for you.
On a phone it opens the Notifications page.

**Needs you** (counted on the bell): a short question from the team is waiting, or a request you sent needs more from you.
Their buttons open the question or My requests in the assistant.

**Updates** (listed, never counted): what the bot did to your book on the page's bot (a stop-loss, a delta exit, the kill switch, a sell cancelled because new risk is frozen, initial margin over the limit), each with the journal's plain sentence and *Why?*; the status of your requests (accepted as a public issue, declined with its reason, shipped); and one What's new note per release.
Fills and skips are never notifications.

**Reading**: an unread item has a dot and bold text.
Following an item marks it read; "Mark all read" marks the whole list.
Read marks are kept with your account, so your other devices agree.
Items are kept 90 days.

**Settings**: the Notifications page turns each kind of update off or on.
What needs you cannot be turned off; the question from the team has its own "Don't ask me again".
Nothing is sent by e-mail or to your phone.

**When something looks wrong**: if the bot does not answer, the list says its events could not be read and still shows the rest.
