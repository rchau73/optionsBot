"""Tests of the weekly evidence job (#244): evidence_week.py's pure parts, and
evidence.sh end to end against throwaway env clones with stub go/gh/curl.
python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import datetime as dt
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import evidence_week  # noqa: E402

SCRIPTS = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def report(**over):
    """A cmd/evidence .json with every section known (testnet amounts)."""
    r = {
        "env": "stable", "bot": "btc", "week": "2026-W43", "equity": 2.0,
        "live": {"perf": {"Points": 600, "NetPnL": 0.0123, "EquityKnown": True, "ReturnPct": 0.62,
                          "WorstWeekKnown": True, "WorstWeek": -0.004, "WorstWeekPct": -0.2}},
        "bridge": {"known": True, "unit": "BTC", "equity_start": 2.0, "equity_end": 1.5,
                   "bot_net": 0.0123, "residual": -0.5123, "residual_pct": -25.6},
        "replay": {"ran": True},
        "gap": {"known": True, "gap": 0.002, "replay": 0.0143},
        "stress": {"ran": True, "stale": False, "outcomes": [
            {"shock": "crash", "return_pct": -12.5}, {"shock": "rally", "return_pct": -3.1},
            {"shock": "chop", "return_pct": 1.0, "error": ""}, {"shock": "crash40", "return_pct": -99, "error": "boom"}]},
        "return_on_margin": {"live": {"Known": True, "ReturnPct": 6.2, "Return30dPct": 26.57},
                             "replay": {"Known": True, "ReturnPct": 7.15, "Return30dPct": 30.64}},
        "hold": {"known": True, "hold_return_usd_pct": 4.2, "strategy_known": True, "excess_return_pct": -1.3},
        "e1_reliability": {
            "target_pct": 60,
            "margin_use": {"known": True, "mean_pct": 48.21, "allowed_share": 0.9, "target_met": False},
            "idle_margin": {"known": True, "causes": [{"cause": "no_free_expiry", "hours": 30, "share": 0.75},
                                                      {"cause": "frozen", "hours": 10, "share": 0.25}]},
            "restarts": {"known": True, "startups": 3, "restart_made": 1, "restart_made_lots": 0.35}},
        "missing": {"history_days": ["2026-10-20"], "book_days": [], "gex_days": [
            "2026-10-19", "2026-10-20", "2026-10-21", "2026-10-22", "2026-10-23", "2026-10-24", "2026-10-25"]},
    }
    r.update(over)
    return r


def scorecard(bias=0.5, sma=0.8, bias_sheds=0, sma_sheds=0, live_sheds=(0, 0), gaps=(0.1, -0.3), book_days=()):
    """A cmd/evidence -compare-direction .json (percentages and counts only)."""
    return {
        "bot": "btc", "week": "2026-W43", "units": "percentages",
        "replay": [{"source": "bias", "ran": bias is not None, "net_pct": bias, "max_drawdown_pct": 1.2, "sheds": bias_sheds},
                   {"source": "sma", "ran": sma is not None, "net_pct": sma, "max_drawdown_pct": 0.9, "sheds": sma_sheds}],
        "live": [{"source": "bias", "env": "stable", "sheds": live_sheds[0], "live_minus_replay_pp": gaps[0]},
                 {"source": "sma", "env": "mvp", "sheds": live_sheds[1], "live_minus_replay_pp": gaps[1]}],
        "missing": {"book_days": list(book_days), "gex_days": []},
    }


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(text)


class Week(unittest.TestCase):
    def test_monday_of_w44_reports_w43(self):
        self.assertEqual(evidence_week.previous_week(dt.date(2026, 10, 26)), "2026-W43")

    def test_sunday_still_reports_the_week_before(self):
        self.assertEqual(evidence_week.previous_week(dt.date(2026, 11, 1)), "2026-W43")

    def test_year_boundary(self):
        self.assertEqual(evidence_week.previous_week(dt.date(2027, 1, 4)), "2026-W53")


class ReconCell(unittest.TestCase):
    """The account reconciliation's cell (#566): % only, flagged over the tolerance."""

    def test_within_tolerance(self):
        r = report(reconciliation={"known": True, "gap_pct": 0.0, "gap": 0.0, "flagged": False, "tolerance_pct": 1})
        self.assertEqual(evidence_week.recon_cell(r), "+0.00 % (within tolerance)")

    def test_flagged_never_shows_the_amount(self):
        r = report(reconciliation={"known": True, "gap_pct": -25.5, "gap": -0.51, "flagged": True, "tolerance_pct": 1})
        cell = evidence_week.recon_cell(r)
        self.assertEqual(cell, "⚠ -25.50 % over 1 %")
        self.assertNotIn("0.51", cell)

    def test_not_enough_history_and_older_reports(self):
        r = report(reconciliation={"known": False, "note": "not enough history: 1 margin-balance point(s) in the week"})
        self.assertEqual(evidence_week.recon_cell(r), "— (not enough history: 1 margin-balance point(s) in the week)")
        self.assertEqual(evidence_week.recon_cell(report()), "— (not measured)")


class Comment(unittest.TestCase):
    def setUp(self):
        self.envs = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.envs, True)
        self.fail_tsv = os.path.join(self.envs, "failures.tsv")
        write(self.fail_tsv, "")

    def put(self, env, bot, r, dotenv="DERIBIT_ENV=testnet\n"):
        write(os.path.join(self.envs, env, ".env"), dotenv)
        write(os.path.join(self.envs, env, "data", "evidence", "2026-W43", bot + ".json"), json.dumps(r))

    def build(self, bots=("btc",), envs=("stable", "mvp")):
        return evidence_week.comment("2026-W43", self.envs, self.fail_tsv, list(bots), envs)

    def row(self, text, env, bot):
        return next(l for l in text.splitlines() if l.startswith(f"| {env}") and f"| {bot} |" in l)

    def test_every_env_and_bot_has_a_row_with_its_sections(self):
        for env in ("stable", "mvp"):
            for bot in ("btc", "eth"):
                self.put(env, bot, report())
        text = self.build(("btc", "eth"))
        self.assertTrue(text.startswith("🤖 **Evidence report** — week 2026-W43"))
        row = self.row(text, "stable", "btc")
        self.assertIn("+0.0123 BTC (+0.62 %)", row)           # net P&L
        self.assertIn("live +6.20 % (+26.57 % / 30 d) · replay +7.15 % (+30.64 % / 30 d)", row)  # return on margin
        self.assertIn("| -25.60 % |", row)                     # equity not explained by the bot (#277)
        self.assertNotIn("0.5123", row)                        # never the residual's amount
        self.assertIn("| — (not measured) |", row)             # account vs booked P&L: a report before #566
        self.assertIn("-1.30 % (hold +4.20 %)", row)           # excess vs hold
        self.assertIn("-0.0040 BTC (-0.20 %)", row)            # worst 7 days
        self.assertIn("+0.0020 BTC (+0.10 %)", row)            # replay gap, % of equity
        self.assertIn("worst crash -12.50 %", row)             # failed shocks left out
        self.assertIn("IM 48.2 % of limit (target 60 %: not met), entries allowed 90 % · idle top: no_free_expiry 75 %"
                      " · startups 3, restart-made 1 (0.35 lots)", row)  # E1 reliability (#361)
        self.assertIn("history 10-20 · books none · GEX all 7", row)
        self.assertIn("`stable/data/evidence/2026-W43/btc.md`", row)
        self.assertIn("+0.0123 ETH", self.row(text, "mvp", "eth"))
        self.assertEqual(sum(l.startswith("| stable") or l.startswith("| mvp") for l in text.splitlines()), 4)

    def test_live_env_shows_no_coin_or_usd_amount(self):
        self.put("stable", "btc", report(), "DERIBIT_CLIENT_ID=x\nDERIBIT_ENV='live'\n")
        self.put("mvp", "btc", report())
        write(self.fail_tsv, "stable\tbtc\tstress inputs\tspot 108123.5 USD\n")
        text = self.build()
        row = self.row(text, "stable", "btc")
        self.assertIn("stable (live)", row)
        self.assertIn("+0.62 %", row)
        self.assertIn("live +6.20 %", row)                     # return on margin is a %: allowed live
        self.assertIn("IM 48.2 % of limit", row)               # E1: percentages and counts only
        self.assertIn("startups 3, restart-made 1 |", row)
        self.assertNotIn("lots", row)
        self.assertNotIn("BTC", row)
        self.assertNotIn("USD", text)
        self.assertNotIn("108123", text)                       # failure details hidden for a live env
        self.assertIn("- stable btc: stress inputs", text)
        self.assertIn("+0.0123 BTC", self.row(text, "mvp", "btc"))  # the testnet env keeps its amounts

    def test_unknown_sections_say_why(self):
        r = report(equity=0)
        r["live"]["perf"] = {"Points": 0}
        r["gap"] = {"known": False}
        r["replay"] = {"ran": False, "note": "no recorded book"}
        r["stress"] = {"ran": False, "note": "no book in data/stress/btc"}
        r["hold"] = {"known": False}
        r["missing"] = {}
        r["return_on_margin"] = {"live": {"Known": False, "Reason": "no point carries im_pct"}, "replay": None}
        r["bridge"] = {"known": False, "note": "no equity recorded on the P&L history in the week"}
        r["e1_reliability"] = {"target_pct": 60, "margin_use": {"known": False, "note": "P&L history not read"},
                               "idle_margin": {"known": False, "note": "journal not read"},
                               "restarts": {"known": False, "note": "journal not read"}}
        self.put("stable", "btc", r)
        row = self.row(self.build(envs=("stable",)), "stable", "btc")
        for want in ("margin use not measured (P&L history not read) · idle not measured (journal not read)"
                     " · startups not measured (journal not read)",
                     "live — (no point carries im_pct) · replay — (replay did not run)", "— (no P&L history)",
                     "— (no equity recorded on the P&L history in the week)", "— (no spot in the history)", "— (under 7 days of history)",
                     "— (no recorded book)", "— (no book in data/stress/btc)", "history none"):
            self.assertIn(want, row)

    def test_report_written_before_the_e1_section_says_so(self):
        r = report()
        del r["e1_reliability"]
        self.put("stable", "btc", r)
        self.assertIn("— (report without the E1 section)", self.row(self.build(envs=("stable",)), "stable", "btc"))

    def test_complete_week_has_no_gap_notice(self):
        r = report(coverage={"known": True, "market_pct": 100.0, "partial": False, "long_gap_hours": 3, "long_gaps": []})
        self.put("stable", "btc", r)
        text = self.build(envs=("stable",))
        self.assertIn("· market hours 100.0 % |", self.row(text, "stable", "btc"))
        self.assertNotIn("Recorder gaps", text)
        self.assertEqual(evidence_week.notices("2026-W43", self.envs, ["btc"], ("stable",)), [])

    def test_a_recorder_gap_is_one_line_naming_the_dataset_and_the_gap(self):
        gaps = [{"dataset": "market", "from": "2026-10-21T03:00:00Z", "to": "2026-10-21T09:00:00Z", "hours": 6},
                {"dataset": "market_gex", "from": "2026-10-20T01:00:00Z", "to": "2026-10-20T05:00:00Z", "hours": 4},
                {"dataset": "market_gex", "from": "2026-10-22T01:00:00Z", "to": "2026-10-22T06:30:00Z", "hours": 5.5}]
        r = report(coverage={"known": True, "market_pct": 52.4, "partial": True, "long_gap_hours": 3, "long_gaps": gaps})
        self.put("stable", "btc", r, "DERIBIT_ENV=live\n")  # times and hours only: the same for a live env
        text = self.build(envs=("stable",))
        self.assertIn("· market hours 52.4 % (replay partial) |", self.row(text, "stable", "btc"))
        self.assertIn("**Recorder gaps**", text)
        self.assertIn("- stable btc: market gap 6.0 h from 2026-10-21 03:00 UTC", text)
        self.assertIn("- stable btc: market_gex 2 gaps over 3 h, longest 5.5 h from 2026-10-22 01:00 UTC", text)
        self.assertEqual(evidence_week.notices("2026-W43", self.envs, ["btc", "eth"], ("stable", "mvp")),
                         ["stable btc: market gap 6.0 h from 2026-10-21 03:00 UTC",
                          "stable btc: market_gex 2 gaps over 3 h, longest 5.5 h from 2026-10-22 01:00 UTC"])

    def test_market_not_recorded_is_said(self):
        self.put("stable", "btc", report(coverage={"known": True, "market_pct": None, "partial": True, "long_gaps": []}))
        self.assertIn("market hours not recorded (replay partial)", self.row(self.build(envs=("stable",)), "stable", "btc"))

    def test_missing_report_and_failures_are_listed(self):
        write(self.fail_tsv, "mvp\t-\tclone\tno clone at /x/mvp\nstable\teth\tcmd/evidence\texit status 1\n")
        self.put("stable", "btc", report())
        text = self.build(("btc", "eth"))
        self.assertIn("| stable | eth | no report |", text)
        self.assertIn("- mvp: clone — `no clone at /x/mvp`", text)
        self.assertIn("- stable eth: cmd/evidence — `exit status 1`", text)


class DirectionTrial(unittest.TestCase):
    """The direction trial block and its decision tickets (#440)."""

    def setUp(self):
        self.envs = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.envs, True)
        self.stable = os.path.join(self.envs, "stable")
        self.fail_tsv = os.path.join(self.envs, "failures.tsv")
        write(self.fail_tsv, "")

    def card(self, week, **over):
        write(evidence_week.scorecard_path(self.stable, week), json.dumps(scorecard(**over)))

    def comment(self, week):
        return evidence_week.comment(week, self.envs, self.fail_tsv, ["btc"], ("stable", "mvp"))

    def test_trial_weeks_start_at_w43(self):
        self.assertEqual(evidence_week.trial_weeks("2026-W42"), [])
        self.assertEqual(evidence_week.trial_weeks("2026-W45"), ["2026-W43", "2026-W44", "2026-W45"])
        self.assertEqual(evidence_week.trial_weeks("2027-W01")[-2:], ["2026-W53", "2027-W01"])

    def test_back_fill_lists_the_missing_weeks_and_always_the_last(self):
        self.card("2026-W44")
        self.card("2026-W45")
        self.assertEqual(evidence_week.direction_weeks("2026-W45", self.stable), ["2026-W43", "2026-W45"])
        self.assertEqual(evidence_week.direction_weeks("2026-W42", self.stable), [])

    def test_weekly_scorecard_posted_with_percentages_and_counts_only(self):
        self.card("2026-W43", gaps=(0.1, -0.3))
        self.card("2026-W44", bias=-0.2, sma=0.4, book_days=("2026-10-26", "2026-10-27", "2026-10-28"))
        text = self.comment("2026-W44")
        self.assertIn("**Direction trial** — bias (stable BTC) vs sma (mvp BTC), `stable/data/evidence/direction/2026-W44.md`", text)
        self.assertIn("| bias | -0.20 % | +1.20 % | 0 | 0 | +0.10 pp |", text)
        self.assertIn("| sma | +0.40 % | +0.90 % | 0 | 0 | -0.30 pp |", text)
        self.assertIn("- Weeks counted toward 6: 1 (2026-W43) · excluded, books missing over 2 days: 2026-W44", text)
        self.assertIn("bias − sma, replay net (mean of the counted weeks): -0.30 pp · noise (mean live − replay): +0.20 pp", text)
        self.assertIn("Early-stop streak (bias's replay net under sma's by more than the noise): 1 of 3", text)
        self.assertIn("- First GEX shed: none yet", text)
        self.assertIn("- Review: 2026-12-07", text)
        self.assertNotIn("BTC |", text.split("**Direction trial**")[1])  # no amount column
        self.assertNotIn("🚩", text)

    def test_no_scorecard_is_said_and_before_the_trial_no_block(self):
        self.assertIn("No scorecard for 2026-W43 (see the failed steps).", self.comment("2026-W43"))
        self.assertNotIn("Direction trial", self.comment("2026-W42"))

    def test_a_replay_that_did_not_run_counts_toward_neither(self):
        self.card("2026-W43", bias=None)
        text = self.comment("2026-W43")
        self.assertIn("| bias | — (replay did not run) | — | — | 0 |", text)
        self.assertIn("Weeks counted toward 6: 0 (none)", text)
        self.assertIn("no scorecard or replay: 2026-W43", text)

    def test_first_shed_on_the_comments_first_line_and_one_ticket(self):
        self.card("2026-W43")
        self.card("2026-W44", live_sheds=(2, 0))
        text = self.comment("2026-W44")
        self.assertTrue(text.splitlines()[0].startswith(evidence_week.HEADER))
        self.assertIn("🚩 **first GEX shed of the direction trial", text.splitlines()[0])
        self.assertIn("- First GEX shed: 2026-W44", text)
        tickets = evidence_week.trial_tickets("2026-W44", self.stable, [])
        self.assertEqual([(k, t) for k, t, _ in tickets],
                         [("first-shed", "[E2] Direction trial: first GEX shed in 2026-W44, review bias vs sma")])
        body = tickets[0][2]
        self.assertIn("| 2026-W44 | counted | +0.50 % | +0.80 % | -0.30 pp | 0 / 0 · 2 / 0 |", body)
        self.assertIn("<!-- direction-trial: first-shed 2026-W44 -->", body)
        self.assertIn("no agent", body)

    def test_first_shed_filed_once(self):
        self.card("2026-W43", bias_sheds=1)
        issues = [{"number": 7, "title": "[E2] Direction trial: first GEX shed in 2026-W43, review bias vs sma", "state": "CLOSED"}]
        self.assertEqual(evidence_week.trial_tickets("2026-W43", self.stable, issues), [])
        # a later week with sheds only counts in the comment
        self.card("2026-W44", sma_sheds=3)
        self.assertEqual(evidence_week.trial_tickets("2026-W44", self.stable, issues), [])
        text = self.comment("2026-W44")
        self.assertNotIn("🚩", text)
        self.assertIn("| sma | +0.80 % | +0.90 % | 3 |", text)
        self.assertIn("- First GEX shed: 2026-W43", text)

    def test_early_stop_after_three_weeks_under_by_more_than_noise(self):
        for w in ("2026-W43", "2026-W44", "2026-W45"):
            self.card(w, bias=-1.0, sma=1.0, gaps=(0.5, 0.5))
        s = evidence_week.trial_state(self.stable, "2026-W45")
        self.assertEqual(s["streak"], 3)
        self.assertTrue(s["early_stop"])
        kinds = [k for k, _, _ in evidence_week.trial_tickets("2026-W45", self.stable, [])]
        self.assertEqual(kinds, ["early-stop"])
        self.assertIn("**early stop: decision ticket filed**", self.comment("2026-W45"))

    def test_a_week_inside_the_noise_breaks_the_streak(self):
        self.card("2026-W43", bias=-1.0, sma=1.0, gaps=(1.0, 1.0))
        self.card("2026-W44", bias=-1.0, sma=1.0, gaps=(1.0, 1.0))
        self.card("2026-W45", bias=0.0, sma=0.5, gaps=(1.0, 1.0))  # d −0.5 within noise 1.0
        self.assertEqual(evidence_week.trial_state(self.stable, "2026-W45")["streak"], 0)

    def test_no_gap_no_streak(self):
        self.card("2026-W43", bias=-1.0, sma=1.0, gaps=(None, None))
        self.assertIn("not measured (no live − replay gap yet)", self.comment("2026-W43"))

    def test_review_due_after_six_counted_weeks_or_the_review_date(self):
        for w in evidence_week.trial_weeks("2026-W48"):
            self.card(w, bias=0.5, sma=0.5)  # no difference: no early stop
        self.assertEqual(evidence_week.trial_state(self.stable, "2026-W47")["review_reason"], None)
        s = evidence_week.trial_state(self.stable, "2026-W48")
        self.assertIn("6 weeks counted", s["review_reason"])
        self.assertEqual([k for k, _, _ in evidence_week.trial_tickets("2026-W48", self.stable, [])], ["review"])
        self.assertIn("**due** — 6 weeks counted", self.comment("2026-W48"))
        # Two weeks short of books: 5 counted at W49, reported on 2026-12-07, the review date.
        self.card("2026-W47", book_days=("a", "b", "c"))
        self.card("2026-W48", book_days=("a", "b", "c"))
        self.assertIsNone(evidence_week.trial_state(self.stable, "2026-W48")["review_reason"])
        self.card("2026-W49", bias=0.5, sma=0.5)
        self.assertIn("review date 2026-12-07 is reached with 5 of 6",
                      evidence_week.trial_state(self.stable, "2026-W49")["review_reason"])
        issues = [{"title": "[E2] Direction trial: review due in 2026-W48, decide bias vs sma"}]
        self.assertEqual(evidence_week.trial_tickets("2026-W49", self.stable, issues), [])

    def test_unreadable_issue_list_files_nothing(self):
        self.card("2026-W43", bias_sheds=1)
        out = os.path.join(self.envs, "tickets")
        with self.assertRaises(SystemExit):
            evidence_week.write_tickets("2026-W43", self.stable, os.path.join(self.envs, "nope.json"), out)
        self.assertFalse(os.path.exists(out))


GO = """#!/usr/bin/env bash
# go run ./cmd/evidence -env E -root R [-books-root B] -bot b -week W → R/data/evidence/W/b.{md,json}
# go run ./cmd/evidence -compare-direction -bot btc -week W -stable-root S -mvp-root M → S/data/evidence/direction/W.{md,json}
echo "$*" >> "$EV_TEST_DIR/go.log"
compare=""
while [[ $# -gt 0 ]]; do
  case "$1" in -env) env="$2"; shift ;; -root) root="$2"; shift ;; -bot) bot="$2"; shift ;; -week) week="$2"; shift ;;
    -compare-direction) compare=1 ;; -stable-root) root="$2"; shift ;; esac
  shift
done
if [[ -n "$compare" ]]; then
  [[ -f "$EV_TEST_DIR/direction_fails" ]] && { echo "direction: no recorded book"; exit 1; }
  mkdir -p "$root/data/evidence/direction"
  src="$EV_TEST_DIR/scorecard-$week.json"; [[ -f "$src" ]] || src="$EV_TEST_DIR/scorecard.json"
  cp "$src" "$root/data/evidence/direction/$week.json"
  echo "# direction $week" > "$root/data/evidence/direction/$week.md"
  exit 0
fi
grep -qx "$env $bot" "$EV_TEST_DIR/go_fails" 2>/dev/null && { echo "evidence: broken"; exit 1; }
mkdir -p "$root/data/evidence/$week"
cp "$EV_TEST_DIR/report.json" "$root/data/evidence/$week/$bot.json"
echo "# $env $bot" > "$root/data/evidence/$week/$bot.md"
"""

CURL = """#!/usr/bin/env bash
# The monitor (-H @-: token on stdin, -o file) and Deribit's public market data.
url=""; out=""; tok=""
while [[ $# -gt 0 ]]; do
  case "$1" in -o) out="$2"; shift ;; -H) [[ "$2" == "@-" ]] && tok="$(cat)"; shift ;; http*) url="$1" ;; esac
  shift
done
echo "$url ${tok#Authorization: Bearer }" >> "$EV_TEST_DIR/curl.log"
while read -r bad; do [[ -n "$bad" && "$url" == *"$bad"* ]] && exit 22; done < <(cat "$EV_TEST_DIR/curl_fails" 2>/dev/null)
case "$url" in
  *get_volatility_index_data*) echo '{"result": {"data": [[1, 0, 0, 0, 50]]}}' ;;
  *get_tradingview_chart_data*) echo '{"result": {"ticks": [1], "close": [100000]}}' ;;
  *) echo '{}' > "$out" ;;
esac
"""

GH = """#!/usr/bin/env bash
echo "$*" >> "$EV_TEST_DIR/gh.log"
case "$1 $2" in
  "repo view") echo owner/repo ;;
  "issue list")
    if [[ "$*" == *"Direction trial"* ]]; then
      [[ -f "$EV_TEST_DIR/issues_down" ]] && exit 1
      cat "$EV_TEST_DIR/trial_issues.json" 2>/dev/null || echo '[]'
    else
      [[ -f "$EV_TEST_DIR/no_epic" ]] || echo 31
    fi ;;
  "issue create")
    while [[ $# -gt 0 ]]; do
      case "$1" in
        --title) printf '%s\\n' "$2" >> "$EV_TEST_DIR/created.txt"
                 python3 -c 'import json,sys; p=sys.argv[1]; t=sys.argv[2]
try: l=json.load(open(p))
except Exception: l=[]
l.append({"number": 900+len(l), "title": t, "state": "OPEN"}); json.dump(l, open(p, "w"))' "$EV_TEST_DIR/trial_issues.json" "$2" ;;
        --body-file) cat "$2" >> "$EV_TEST_DIR/created_bodies.txt" ;;
        --label) echo "$2" >> "$EV_TEST_DIR/created_labels.txt" ;;
      esac
      shift
    done
    echo "https://github.com/owner/repo/issues/900" ;;
  "issue comment")
    [[ -f "$EV_TEST_DIR/gh_down" ]] && exit 1
    n="$(ls "$EV_TEST_DIR" | grep -c '^comment-' || true)"
    while [[ $# -gt 0 ]]; do [[ "$1" == --body-file ]] && cp "$2" "$EV_TEST_DIR/comment-$n.md"; shift; done ;;
  *) exit 1 ;;
esac
"""


class EvidenceScript(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, True)
        t = lambda *p: os.path.join(self.tmp, *p)
        shutil.copytree(SCRIPTS, t("repo", "scripts"))
        for env, port, tok in (("stable", 3000, "stabletoken"), ("mvp", 3100, "mvptoken")):
            write(t("envs", env, ".env"), f"DERIBIT_CLIENT_ID={env}id\nMONITOR_API_TOKEN={tok}\nDERIBIT_ENV=testnet\n")
            write(t("envs", env, "deploy.env"), f"OPTIONSBOT_ENV={env}\nMONITOR_PORT={port}\n")
        write(t("report.json"), json.dumps(report()))
        write(t("scorecard.json"), json.dumps(scorecard()))
        for name, text in (("go", GO), ("curl", CURL), ("gh", GH)):
            write(t("bin", name), text)
            os.chmod(t("bin", name), 0o755)
        self.env = dict(os.environ, PATH=t("bin") + os.pathsep + os.environ["PATH"], OPTIONSBOT_ENVS=t("envs"),
                        EV_TEST_DIR=self.tmp, AGENT_NOTIFY="0")
        for k in ("MONITOR_URL", "MVP_MONITOR_URL", "MONITOR_TOKEN", "MVP_MONITOR_TOKEN", "EVIDENCE_BOTS"):
            self.env.pop(k, None)
        self.t = t
        self.week = evidence_week.previous_week(dt.datetime.now(dt.timezone.utc).date())

    def run_script(self):
        p = subprocess.run(["bash", self.t("repo", "scripts", "agents", "evidence.sh")], env=self.env,
                           capture_output=True, text=True, timeout=120)
        self.output = p.stdout + p.stderr
        self.assertEqual(p.returncode, 0, self.output)

    def read(self, *p):
        try:
            with open(self.t(*p)) as f:
                return f.read()
        except FileNotFoundError:
            return ""

    def comments(self):
        return [self.read(n) for n in sorted(os.listdir(self.tmp)) if n.startswith("comment-")]

    def done(self):
        return os.path.exists(self.t("repo", "data", "agents", "evidence", self.week + ".done"))

    def test_first_monday_tick_writes_every_report_comments_and_marks_the_week_done(self):
        self.run_script()
        for env in ("stable", "mvp"):
            for bot in ("btc", "eth"):
                for ext in ("md", "json"):
                    self.assertTrue(os.path.exists(self.t("envs", env, "data", "evidence", self.week, f"{bot}.{ext}")),
                                    f"{env} {bot}.{ext}\n{self.output}")
        go = [l for l in self.read("go.log").splitlines() if "-compare-direction" not in l]
        self.assertEqual(len(go), 4)
        self.assertTrue(all(f"-week {self.week}" in l for l in go))
        self.assertTrue(all("-books-root " + self.t("envs", "stable") in l for l in go if "-env mvp" in l))
        self.assertFalse(any("-books-root" in l for l in go if "-env stable" in l))
        curl = self.read("curl.log")
        self.assertIn("127.0.0.1:3100/api/bots/btc/positions mvptoken", curl)   # each env's own monitor and token
        self.assertIn("127.0.0.1:3000/api/bots/eth/account stabletoken", curl)
        self.assertTrue(os.path.exists(self.t("envs", "mvp", "data", "stress", "eth", "market.json")))
        comments = self.comments()
        self.assertEqual(len(comments), 1)
        self.assertIn(f"week {self.week}", comments[0])
        self.assertIn("issue comment 31", self.read("gh.log"))
        for env in ("stable", "mvp"):
            for bot in ("btc", "eth"):
                self.assertIn(f"| {env} | {bot} | +0.0123 {bot.upper()}", comments[0])
        self.assertIn("GEX all 7", comments[0])
        self.assertTrue(self.done())

    def test_a_recorder_gap_is_posted_and_notified(self):
        r = report(coverage={"known": True, "market_pct": 96.4, "partial": False, "long_gap_hours": 3, "long_gaps": [
            {"dataset": "market", "from": "2026-10-21T03:00:00Z", "to": "2026-10-21T09:00:00Z", "hours": 6}]})
        write(self.t("report.json"), json.dumps(r))
        self.run_script()
        comment = self.comments()[0]
        self.assertIn("- stable btc: market gap 6.0 h from 2026-10-21 03:00 UTC", comment)
        self.assertIn("- mvp eth: market gap 6.0 h from 2026-10-21 03:00 UTC", comment)
        self.assertIn("evidence: recorder gaps: stable btc: market gap 6.0 h", self.output)

    def test_no_gap_no_notice(self):
        self.run_script()
        self.assertNotIn("Recorder gaps", self.comments()[0])
        self.assertNotIn("recorder gaps", self.output)

    def test_a_later_tick_after_the_done_marker_does_nothing(self):
        write(self.t("repo", "data", "agents", "evidence", self.week + ".done"), "x\n")
        self.run_script()
        self.assertEqual(self.read("go.log"), "")
        self.assertEqual(self.read("curl.log"), "")
        self.assertEqual(self.read("gh.log"), "")

    def test_stress_inputs_of_one_bot_fail_the_others_are_written_and_named(self):
        write(self.t("curl_fails"), "127.0.0.1:3000/api/bots/eth/\n")
        self.run_script()
        for env in ("stable", "mvp"):
            for bot in ("btc", "eth"):
                self.assertTrue(os.path.exists(self.t("envs", env, "data", "evidence", self.week, f"{bot}.json")))
        comment = self.comments()[0]
        self.assertIn("- stable eth: stress inputs", comment)
        self.assertNotIn("- stable btc", comment)
        self.assertNotIn("- mvp", comment)
        self.assertTrue(self.done())

    def test_one_report_failing_keeps_going(self):
        write(self.t("go_fails"), "stable btc\n")
        self.run_script()
        self.assertTrue(os.path.exists(self.t("envs", "mvp", "data", "evidence", self.week, "eth.json")))
        comment = self.comments()[0]
        self.assertIn("| stable | btc | no report |", comment)
        self.assertIn("- stable btc: cmd/evidence — `evidence: broken`", comment)

    def test_live_env_comment_has_no_amount_for_it(self):
        write(self.t("envs", "stable", ".env"), "DERIBIT_CLIENT_ID=stableid\nMONITOR_API_TOKEN=stabletoken\nDERIBIT_ENV=live\n")
        self.run_script()
        comment = self.comments()[0]
        for line in comment.splitlines():
            if line.startswith("| stable"):
                self.assertNotIn("BTC", line)
                self.assertNotIn("ETH", line)
        self.assertIn("+0.0123 BTC", comment)  # mvp, testnet

    def test_a_failed_post_is_retried_once_then_the_week_is_done(self):
        write(self.t("gh_down"), "")
        self.run_script()
        self.assertFalse(self.done())
        self.run_script()
        self.assertTrue(self.done())
        self.run_script()  # a third tick: nothing more
        go = [l for l in self.read("go.log").splitlines() if "-compare-direction" not in l]
        self.assertEqual(len(go), 8)

    def created(self):
        return self.read("created.txt").splitlines()

    def test_weekly_scorecard_posted(self):
        self.run_script()
        for ext in ("md", "json"):
            self.assertTrue(os.path.exists(self.t("envs", "stable", "data", "evidence", "direction", f"{self.week}.{ext}")))
        compare = [l for l in self.read("go.log").splitlines() if "-compare-direction" in l]
        self.assertEqual(len(compare), len(evidence_week.trial_weeks(self.week)))  # last week + back-fill
        self.assertTrue(all("-stable-root " + self.t("envs", "stable") in l and "-mvp-root " + self.t("envs", "mvp") in l
                            for l in compare))
        self.assertIn(f"-week {self.week} ", compare[-1] + " ")
        self.assertIn("**Direction trial**", self.comments()[0])
        self.assertFalse(any("first GEX shed" in c for c in self.created()))

    def test_first_shed_flagged_once(self):
        write(self.t(f"scorecard-{self.week}.json"), json.dumps(scorecard(live_sheds=(0, 1))))
        self.run_script()
        title = f"[E2] Direction trial: first GEX shed in {self.week}, review bias vs sma"
        self.assertIn(title, self.created())
        self.assertIn("agent:needs-human", self.read("created_labels.txt"))
        self.assertIn("🚩", self.comments()[0].splitlines()[0])
        os.remove(self.t("repo", "data", "agents", "evidence", self.week + ".done"))
        self.run_script()  # a later run for the same week
        self.assertEqual(self.created().count(title), 1)

    def test_scorecard_fails(self):
        write(self.t("direction_fails"), "")
        self.run_script()
        comment = self.comments()[0]
        for env in ("stable", "mvp"):
            for bot in ("btc", "eth"):
                self.assertIn(f"| {env} | {bot} | +0.0123 {bot.upper()}", comment)
        self.assertIn(f"- stable btc: direction scorecard {self.week} — `direction: no recorded book`", comment)
        self.assertIn(f"No scorecard for {self.week}", comment)
        self.assertTrue(self.done())

    def test_an_unreadable_issue_list_files_no_ticket(self):
        write(self.t(f"scorecard-{self.week}.json"), json.dumps(scorecard(bias_sheds=2)))
        write(self.t("issues_down"), "")
        self.run_script()
        self.assertEqual(self.created(), [])
        self.assertIn("direction trial issues not read", self.output)

    def test_nothing_changes_in_trading(self):
        # The job reads and reports; a decision is a person's config PR.
        for name in ("evidence.sh", "evidence_week.py"):
            with open(os.path.join(SCRIPTS, "agents", name)) as f:
                text = f.read()
            for touched in ("config_btc.yaml", "config_btc.mvp.yaml", "gamma.go"):
                self.assertNotIn(touched, text, name)

    def test_tick_starts_evidence(self):
        with open(os.path.join(SCRIPTS, "agents", "tick.sh")) as f:
            self.assertIn("start evidence.sh 1", f.read())


if __name__ == "__main__":
    unittest.main()
