# The SME agent: where business meets engineering

Part of [Agentic development](../../AGENTIC.md).

The Dev, Reviewer and Deploy steps answer *"is it built right?"*.
The SME agent answers the two questions that come before and after them: *"are we building the right thing?"* and *"did it pay off?"*.
A fast delivery pipeline pointed at the wrong problem only ships waste faster.
That is why the SME agent is the first step of the loop and also its last.

**What makes it different from the other agents:**

- **It speaks the business's language.**
  It thinks like a portfolio manager, not like a programmer.
  Every finding is a cost in coin, USD, idle days or risk taken, and the issue reads as a decision someone can approve: *"ETH used about 2 % of a 20 % margin limit all week"*, not *"function X returns too few lots"*.
- **It connects symptoms to rules.**
  It reads the bots' data *and* the trading rules in [`CLAUDE.md`](../../CLAUDE.md) and the code, so a finding names the rule that misfired, why it exists, and where it falls short.
  The Dev agent then starts from a diagnosis, not from a symptom.
- **It judges outcomes, not output.**
  A merged PR isn't the goal.
  The next day the SME agent checks the ticket's acceptance criteria against fresh market data, and reopens the issue if the problem is still there.
- **It weighs risk before return.**
  Its persona ([option-bot-specialist](../../.claude/skills/option-bot-specialist/SKILL.md)) is a desk PM who has seen books blow up selling volatility.
  It proposes alternatives, including *do nothing*, and states the worst case before the upside.

**What it does every day:**

| Step | What happens |
|---|---|
| Collect | A script, not the agent, pulls each bot's status, positions, orders, events, trades, P&L (and its 1-week history) and account margin from the monitor API, plus the configs and the agent's previous reports. The agent itself has no network. |
| Analyse | **Risk:** margin used against the limit, MM, one-sided books, stops. **Return:** idle margin, unfilled orders, fees, P&L by slot and exit reason. Always read in market context: DVOL, GEX regime, trend. |
| Diagnose | It traces each problem through the journal (what the bot saw, decided and got, with timestamps) to the rule that caused it. |
| Write the case | One issue per **new** problem, in the [ticket template](ticket.md): cost, evidence, options, metrics, Gherkin acceptance criteria, a recommendation. A problem an open ticket already tracks (whoever filed it) is matched by root cause, not wording, and gets a comment only when there is new data. |
| Verify | It re-checks every recently closed issue against its acceptance criteria, and comments "fixed" or reopens it. |

**Its first unattended run (2026-10-15)** filed 9 issues.
By the end of the same day 8 were closed: 7 by changes built, reviewed and deployed through this loop, and 1 by the exchange listing a new expiry.

| Issue | Business problem it found | Outcome |
|---|---|---|
| [#81](https://github.com/rchau73/optionsBot/issues/81) | ETH frozen and flat for days on a volatility-band change that could not move its limit | Same diagnosis as a fix built by hand the same day ([#79](https://github.com/rchau73/optionsBot/pull/79)), found on its own |
| [#82](https://github.com/rchau73/optionsBot/issues/82) | BTC calls shed 11 min after a restart, on a guard with no warm-up | Warm-up added ([#90](https://github.com/rchau73/optionsBot/pull/90)) |
| [#83](https://github.com/rchau73/optionsBot/issues/83) | One slot per bot idle about 32 h, with no listed expiry in its window | Resolved by a new listing; the lasting fix was studied in #89 |
| [#84](https://github.com/rchau73/optionsBot/issues/84) | BTC's 50-DTE slot lost 0.04725 BTC to hedging sheds in a week | 3 of its 5 causes fixed |
| [#85](https://github.com/rchau73/optionsBot/issues/85) | A BTC top-up re-sent 6 times without a fill | Back-off on unfilled passes ([#91](https://github.com/rchau73/optionsBot/pull/91), [#94](https://github.com/rchau73/optionsBot/pull/94)) |
| [#86](https://github.com/rchau73/optionsBot/issues/86) | ETH used about 2 % of a 20 % margin limit all week | [#95](https://github.com/rchau73/optionsBot/pull/95); the root cause, orders sized up to 9× a strike's open interest, is now a product decision ([#103](https://github.com/rchau73/optionsBot/issues/103)) |
| [#87](https://github.com/rchau73/optionsBot/issues/87) | Repeated exchange connection losses on both bots | Per-connection stats in the monitor ([#100](https://github.com/rchau73/optionsBot/pull/100)) |
| [#88](https://github.com/rchau73/optionsBot/issues/88) | Is the live trial of a new hedging signal paying off? | Waiting for the human's decision |
| [#89](https://github.com/rchau73/optionsBot/issues/89) | A DTE ladder that always has a monthly expiry in the window | Ladder study tool built ([#101](https://github.com/rchau73/optionsBot/pull/101)) |
