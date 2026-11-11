"""Tests of the daily recording check (recording_check.sh, #716): the message
built by recording_day.py, and the script end to end on throwaway environments
with the real cmd/datacoverage (built once) and a fake gh and osascript.
The Go check itself is tested in tests/evidence_daily_test.go.
python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import datetime
import gzip
import json
import os
import shutil
import subprocess
import tempfile
import unittest

import recording_day

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(os.path.dirname(HERE))

# gh: `repo view` names the repo, `issue list` the E2 epic (#12), `issue
# comment` copies its body to $FAKE_GH_COMMENTS (fails while $FAKE_GH_FAIL
# exists). Every call is logged.
FAKE_GH = """#!/usr/bin/env bash
echo "$*" >> "$FAKE_GH_LOG"
case "$1 $2" in
  "repo view") echo owner/repo ;;
  "issue list") echo 12 ;;
  "issue comment")
    [[ -e "$FAKE_GH_FAIL" ]] && exit 1
    while [[ $# -gt 0 ]]; do [[ "$1" == "--body-file" ]] && cat "$2" >> "$FAKE_GH_COMMENTS"; shift; done ;;
esac
"""
FAKE_OSASCRIPT = '#!/usr/bin/env bash\necho "$*" >> "$FAKE_NOTIFY_LOG"\n'


def yesterday():
    return (datetime.datetime.now(datetime.timezone.utc).date() - datetime.timedelta(days=1)).isoformat()


def check(day, datasets):
    return {"day": day, "dir": "x", "floor_pct": 90, "long_gap_hours": 3, "datasets": datasets,
            "short": any(d.get("short") for d in datasets)}


def ds(name, pct, covered, gap_hours=0, gap_from="2026-11-07T03:00:00Z", gap_to="2026-11-07T08:30:00Z", status="recorded"):
    gap = {"from": gap_from, "to": gap_to, "hours": gap_hours} if gap_hours else {"from": "0001-01-01T00:00:00Z", "to": "0001-01-01T00:00:00Z", "hours": 0}
    return {"dataset": name, "status": status, "market_hours": 24, "covered_hours": covered, "covered_pct": pct,
            "records": 1, "longest_gap": gap, "long_gaps": [gap] if gap_hours > 3 else [],
            "short": status == "recorded" and (pct < 90 or gap_hours > 3)}


class Message(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.dir, True)

    def write(self, name, obj):
        with open(os.path.join(self.dir, name), "w") as f:
            json.dump(obj, f) if not isinstance(obj, str) else f.write(obj)

    def test_a_complete_day_has_no_message(self):
        """Scenario: A complete day is silent."""
        self.write("stable-btc.json", check("2026-11-07", [ds("market", 100, 24), ds("market_gex", 95.8, 23, 1),
                                                           ds("market_usdc", 0, 0, status="not recorded")]))
        self.assertEqual(recording_day.short_lines(recording_day.load(self.dir)), [])
        self.assertEqual(recording_day.notify_text(self.dir, "2026-11-07"), "")

    def test_a_short_day_names_bot_dataset_day_share_and_gap(self):
        """Scenario: A short day is reported the next morning."""
        self.write("stable-btc.json", check("2026-11-07", [ds("market", 100, 24), ds("market_gex", 77.1, 18.5, 5.5)]))
        self.write("stable-eth.json", check("2026-11-07", [ds("market", 0, 0, 24, "2026-11-07T00:00:00Z", "2026-11-08T00:00:00Z")]))
        text = recording_day.notify_text(self.dir, "2026-11-07")
        self.assertEqual(text, "stable btc market_gex 2026-11-07: 77.1 % covered (18.5 of 24 h, floor 90 %), "
                               "longest gap 5.5 h from 03:00 UTC (+1 more)")
        body = recording_day.comment("2026-11-07", self.dir)
        self.assertTrue(body.startswith("🤖 **Recording check** — 2026-11-07 UTC"))
        self.assertIn("<!-- agent:recording day=2026-11-07 -->", body)
        self.assertIn("- stable eth market 2026-11-07: 0.0 % covered", body)
        self.assertIn("market_gex 03:00 → 08:30 (5.5 h)", body)
        self.assertIn("| stable | btc | market_gex | 77.1 % **short** | 5.5 h from 03:00 |", body)
        self.assertNotIn("$", body)  # hours and shares only

    def test_a_failed_check_is_named(self):
        self.write("failures.tsv", "stable\teth\n")
        self.assertIn("stable eth 2026-11-07: not checked", recording_day.notify_text(self.dir, "2026-11-07"))
        self.assertIn("stable eth 2026-11-07: not checked", recording_day.comment("2026-11-07", self.dir))

    def test_a_file_that_does_not_parse_is_skipped(self):
        self.write("stable-btc.json", "{torn")
        self.assertEqual(recording_day.load(self.dir), [])

    def test_yesterday_and_recorders(self):
        self.assertEqual(recording_day.yesterday("2026-11-01"), "2026-10-31")
        for env, rec in (("stable", "1"), ("mvp", "0")):
            os.makedirs(os.path.join(self.dir, env))
            self.write(f"{env}/deploy.env", f"OPTIONSBOT_ENV={env}\nBOT_RECORD_MARKET={rec}\n")
        os.makedirs(os.path.join(self.dir, "other"))  # no deploy.env: the recorder's default is on
        self.assertEqual(recording_day.recorders(self.dir, ["stable", "mvp", "other", "gone"]), ["stable", "other"])


class Script(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.bin_dir = tempfile.mkdtemp()
        cls.check = os.path.join(cls.bin_dir, "datacoverage")
        subprocess.run(["go", "build", "-o", cls.check, "./cmd/datacoverage"], cwd=REPO, check=True)
        cls.fake_bin = os.path.join(cls.bin_dir, "fake")
        os.makedirs(cls.fake_bin)
        for name, text in (("gh", FAKE_GH), ("osascript", FAKE_OSASCRIPT)):
            path = os.path.join(cls.fake_bin, name)
            with open(path, "w") as f:
                f.write(text)
            os.chmod(path, 0o755)

    @classmethod
    def tearDownClass(cls):
        shutil.rmtree(cls.bin_dir, True)

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self.envs = os.path.join(self.tmp, "envs")
        self.state = os.path.join(self.tmp, "state")
        self.ledger = os.path.join(self.tmp, "agent_runs.jsonl")
        self.log = {k: os.path.join(self.tmp, k + ".log") for k in ("gh", "notify", "comments")}
        self.fail_flag = os.path.join(self.tmp, "gh-fail")
        self.day = yesterday()
        for env, rec in (("stable", "1"), ("mvp", "0")):
            self.write(f"{env}/deploy.env", f"OPTIONSBOT_ENV={env}\nBOT_RECORD_MARKET={rec}\n")
        # mvp records nothing: its empty folder would read 0 % if it were checked.
        os.makedirs(os.path.join(self.envs, "mvp", "data", "btc", "data", "market_gex"))

    def write(self, rel, text):
        path = os.path.join(self.envs, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as f:
            f.write(text)

    def gex_day(self, skip=range(0)):
        """stable btc's GEX file for yesterday, a record a minute but the minutes in skip."""
        d = os.path.join(self.envs, "stable", "data", "btc", "data", "market_gex")
        os.makedirs(d, exist_ok=True)
        start = datetime.datetime.fromisoformat(self.day + "T00:00:05+00:00")
        lines = "".join(json.dumps({"computed_at": (start + datetime.timedelta(minutes=m)).strftime("%Y-%m-%dT%H:%M:%SZ"),
                                    "spot": 100000, "regime": "POSITIVE"}) + "\n"
                        for m in range(24 * 60) if m not in skip)
        with gzip.open(os.path.join(d, self.day + ".jsonl.gz"), "wt") as f:
            f.write(lines)

    def run_check(self):
        env = {k: v for k, v in os.environ.items()
               if k not in ("MONITOR_URL", "MVP_MONITOR_URL", "MONITOR_TOKEN", "MVP_MONITOR_TOKEN")}
        env.update(OPTIONSBOT_ENVS=self.envs, AGENT_STATE_DIR=self.state, AGENT_RUNS_FILE=self.ledger,
                   AGENT_NOTIFY="1", RECORDING_BOTS="btc eth", RECORDING_CHECK_BIN=self.check,
                   PATH=self.fake_bin + os.pathsep + os.environ["PATH"],
                   FAKE_GH_LOG=self.log["gh"], FAKE_NOTIFY_LOG=self.log["notify"],
                   FAKE_GH_COMMENTS=self.log["comments"], FAKE_GH_FAIL=self.fail_flag)
        p = subprocess.run(["bash", os.path.join(HERE, "recording_check.sh")], env=env,
                           capture_output=True, text=True, timeout=120)
        self.output = p.stdout + p.stderr
        return p.returncode

    def lines(self, name):
        if not os.path.exists(self.log[name]):
            return []
        with open(self.log[name]) as f:
            return f.read().splitlines()

    def outcomes(self):
        with open(self.ledger) as f:
            return [json.loads(l).get("outcome") for l in f if l.strip() and json.loads(l)["event"] == "end"]

    def done(self):
        return os.path.exists(os.path.join(self.state, "recording", self.day + ".done"))

    def test_a_complete_day_is_silent(self):
        """Scenario: A complete day is silent."""
        self.gex_day()
        self.assertEqual(self.run_check(), 0, self.output)
        with open(os.path.join(self.state, "recording", self.day, "stable-btc.json")) as f:
            got = json.load(f)
        gex = [d for d in got["datasets"] if d["dataset"] == "market_gex"][0]
        self.assertEqual((gex["covered_pct"], gex["short"]), (100, False))
        self.assertIn("every market recording meets the floor", self.output)
        self.assertEqual(self.lines("notify"), [])
        self.assertFalse([c for c in self.lines("gh") if c.startswith("issue comment")])
        self.assertFalse(os.path.exists(os.path.join(self.state, "recording", self.day, "mvp-btc.json")))
        self.assertTrue(self.done())
        self.assertEqual(self.outcomes(), ["complete"])

    def test_a_short_day_notifies_once_and_comments_once(self):
        """Scenario: A short day is reported the next morning — a 5.5 h GEX gap."""
        self.gex_day(skip=range(3 * 60, 8 * 60 + 30))
        self.assertEqual(self.run_check(), 0, self.output)
        notes = self.lines("notify")
        self.assertEqual(len(notes), 1, notes)
        self.assertIn(f"stable btc market_gex {self.day}: 77.1 % covered", notes[0])
        self.assertIn("longest gap 5.5 h from 03:00 UTC", notes[0])
        comments = "\n".join(self.lines("comments"))
        self.assertIn(f"<!-- agent:recording day={self.day} -->", comments)
        self.assertIn("issue comment 12", "\n".join(self.lines("gh")))
        self.assertTrue(self.done())
        self.assertEqual(self.run_check(), 0, self.output)  # the day is done: nothing again
        self.assertEqual(len(self.lines("notify")), 1)
        self.assertEqual(comments.count("agent:recording"), 1)
        self.assertEqual(self.outcomes(), ["posted"])

    def test_a_day_with_no_file_is_zero(self):
        """Scenario: A day with no file — the folder exists, yesterday's file does not."""
        os.makedirs(os.path.join(self.envs, "stable", "data", "btc", "data", "market_gex"))
        self.assertEqual(self.run_check(), 0, self.output)
        notes = self.lines("notify")
        self.assertEqual(len(notes), 1, self.output)
        self.assertIn(f"stable btc market_gex {self.day}: 0.0 % covered (0.0 of 24 h", notes[0])

    def test_a_comment_that_fails_is_retried_without_a_second_notification(self):
        self.gex_day(skip=range(0, 6 * 60))
        open(self.fail_flag, "w").close()
        self.assertEqual(self.run_check(), 0, self.output)
        self.assertFalse(self.done())
        os.remove(self.fail_flag)
        self.assertEqual(self.run_check(), 0, self.output)
        self.assertTrue(self.done())
        self.assertEqual(len(self.lines("notify")), 1)
        self.assertEqual("\n".join(self.lines("comments")).count("agent:recording"), 1)
        self.assertEqual(self.outcomes(), ["short", "posted"])

    def test_tick_starts_it(self):
        with open(os.path.join(HERE, "tick.sh")) as f:
            self.assertIn("start recording_check.sh 1", f.read())


if __name__ == "__main__":
    unittest.main()
