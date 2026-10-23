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
        "hold": {"known": True, "hold_return_usd_pct": 4.2, "strategy_known": True, "excess_return_pct": -1.3},
        "missing": {"history_days": ["2026-10-20"], "book_days": [], "gex_days": [
            "2026-10-19", "2026-10-20", "2026-10-21", "2026-10-22", "2026-10-23", "2026-10-24", "2026-10-25"]},
    }
    r.update(over)
    return r


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
        self.assertIn("| -25.60 % |", row)                     # equity not explained by the bot (#277)
        self.assertNotIn("0.5123", row)                        # never the residual's amount
        self.assertIn("-1.30 % (hold +4.20 %)", row)           # excess vs hold
        self.assertIn("-0.0040 BTC (-0.20 %)", row)            # worst 7 days
        self.assertIn("+0.0020 BTC (+0.10 %)", row)            # replay gap, % of equity
        self.assertIn("worst crash -12.50 %", row)             # failed shocks left out
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
        r["bridge"] = {"known": False, "note": "no equity recorded on the P&L history in the week"}
        self.put("stable", "btc", r)
        row = self.row(self.build(envs=("stable",)), "stable", "btc")
        for want in ("— (no P&L history)", "— (no equity recorded on the P&L history in the week)", "— (no spot in the history)", "— (under 7 days of history)",
                     "— (no recorded book)", "— (no book in data/stress/btc)", "history none"):
            self.assertIn(want, row)

    def test_missing_report_and_failures_are_listed(self):
        write(self.fail_tsv, "mvp\t-\tclone\tno clone at /x/mvp\nstable\teth\tcmd/evidence\texit status 1\n")
        self.put("stable", "btc", report())
        text = self.build(("btc", "eth"))
        self.assertIn("| stable | eth | no report |", text)
        self.assertIn("- mvp: clone — `no clone at /x/mvp`", text)
        self.assertIn("- stable eth: cmd/evidence — `exit status 1`", text)


GO = """#!/usr/bin/env bash
# go run ./cmd/evidence -env E -root R [-books-root B] -bot b -week W → R/data/evidence/W/b.{md,json}
echo "$*" >> "$EV_TEST_DIR/go.log"
while [[ $# -gt 0 ]]; do
  case "$1" in -env) env="$2"; shift ;; -root) root="$2"; shift ;; -bot) bot="$2"; shift ;; -week) week="$2"; shift ;; esac
  shift
done
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
  "issue list") [[ -f "$EV_TEST_DIR/no_epic" ]] || echo 31 ;;
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
        go = self.read("go.log").splitlines()
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
        self.assertEqual(len(self.read("go.log").splitlines()), 8)

    def test_tick_starts_evidence(self):
        with open(os.path.join(SCRIPTS, "agents", "tick.sh")) as f:
            self.assertIn("start evidence.sh 1", f.read())


if __name__ == "__main__":
    unittest.main()
