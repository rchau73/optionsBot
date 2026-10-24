"""Tests of the weekly usage job (#340, #341, #252): usage_week.py's pure parts,
and usage.sh end to end against throwaway env clones with stub go/gh.
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
import usage_week  # noqa: E402

SCRIPTS = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
NOW = dt.datetime(2026, 10, 26, 0, 10, tzinfo=dt.timezone.utc)
WEEK = "2026-W43"


def report(**over):
    """A cmd/usage .json with the figures the comment reads."""
    r = {
        "week": WEEK, "events_in_week": 120,
        "active": {"week": {"users": 20, "by_role": {"user": 20}}},
        "cohorts": [
            {"week": "2026-W41", "users": 12, "retained": [{"offset": 1, "users": 9, "pct": 75.0}, {"offset": 2, "users": 6, "pct": 50.0}]},
            {"week": "2026-W42", "users": 10, "retained": [{"offset": 1, "users": 4, "pct": 40.0}]},
            {"week": "2026-W43", "users": 3, "retained": []},
        ],
        "first_answers": {"first_sessions": 3, "with_answers": 2, "pct": 66.666},
        "help": {"tips": {"tip.gamma_flip": 5}, "videos_played": {"video.read_monitor": 2}, "answers": 4, "helped": 3, "helped_pct": 75.0},
    }
    r.update(over)
    return r


def drops(*ds, missing=(), prev_active=40):
    return {"week": WEEK, "weeks": [{"week": "2026-W42", "active_users": prev_active, "missing": prev_active is None},
                                    {"week": WEEK, "active_users": 20, "missing": False}],
            "missing": list(missing), "drops": list(ds), "skipped": []}


ACTIVE_DROP = {"metric": "active_users", "kind": "count", "current": 20, "base": 40, "change_pct": -50}
CLOSE_DROP = {"metric": "manual_close", "kind": "funnel", "current": 50, "base": 80, "change_pts": -30, "starts": 10, "base_starts": 30}


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(text)


class Pure(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self.envs = os.path.join(self.tmp, "envs")
        self.state = os.path.join(self.tmp, "state")

    def put(self, env, r, d=None, dotenv="DERIBIT_ENV=testnet\n"):
        write(os.path.join(self.envs, env, ".env"), dotenv)
        write(os.path.join(self.envs, env, "data", "monitor", "reports", f"{WEEK}.json"), json.dumps(r))
        if d is not None:
            write(os.path.join(self.state, f"{env}.drops.json"), json.dumps(d))

    def comment(self, envs=("stable", "mvp")):
        return usage_week.comment(WEEK, self.envs, self.state, envs)

    def test_week_is_the_last_complete_iso_week(self):
        self.assertEqual(usage_week.previous_week(dt.date(2026, 10, 26)), WEEK)

    def test_comment_has_counts_and_percentages(self):
        self.put("mvp", report(), drops())
        text = self.comment()
        self.assertTrue(text.startswith("🤖 **Usage report** — week 2026-W43"))
        row = next(l for l in text.splitlines() if l.startswith("| mvp"))
        # 20 active, −50 % vs 40, W42's cohort +1 = 4 of 10, 2 of 3 first answers, 5 tips + 2 videos, 4 answers.
        self.assertEqual(row, "| mvp | 20 | -50 % | 4 of 10 (40 %, 2026-W42) | 2 of 3 (67 %) | 7 | 4 (75 %) | 0 | `mvp/data/monitor/reports/2026-W43.md` |")
        self.assertNotIn("| stable", text)  # no report: left out

    def test_no_events_in_the_week_leaves_the_env_out(self):
        self.put("stable", report(events_in_week=0), drops())
        self.put("mvp", report(), drops())
        text = self.comment()
        self.assertNotIn("| stable", text)
        self.assertIn("| mvp", text)

    def test_scenario_user_session_and_amount_fields_are_not_in_the_comment(self):
        r = report(user="alice", user_id="a0a0a0a0a0a0a0a0", session_id="b" * 32, amount=1234.5678)
        r["active"]["week"]["user"] = "bob"
        r["cohorts"][1]["session"] = "c" * 32
        r["help"]["amount"] = 98765.4321
        d = drops(dict(ACTIVE_DROP, user="carol", amount=5555.55))
        self.put("mvp", r, d)
        text = self.comment()
        title_body = usage_week.issue(WEEK, self.envs, self.state, [], NOW)[1]
        for out in (text, title_body[0], title_body[1]):
            for leak in ("alice", "bob", "carol", "a0a0a0a0a0a0a0a0", "b" * 32, "c" * 32, "1234.5678", "98765.4321", "5555.55",
                         "user_id", "session_id", "amount"):
                self.assertNotIn(leak, out)

    def test_live_env_shows_percentages_only(self):
        self.put("stable", report(), drops(ACTIVE_DROP, CLOSE_DROP), dotenv="DERIBIT_ENV=live\n")
        self.put("mvp", report(), drops())
        text = self.comment()
        row = next(l for l in text.splitlines() if l.startswith("| stable (live)"))
        self.assertEqual(row, "| stable (live) | — | -50 % | 40 % (2026-W42) | 67 % | — | 75 % | 2 | `stable/data/monitor/reports/2026-W43.md` |")
        live_drops = [l for l in text.splitlines() if l.startswith("- stable:")]
        self.assertEqual(live_drops, ["- stable: weekly active users: -50 % against the mean of the base weeks",
                                      "- stable: manual-close dialogs confirmed: 50 % of starts, was 80 % (-30 pts)"])
        self.assertIn("live account — percentages only", text)
        body = usage_week.issue(WEEK, self.envs, self.state, [], NOW)[1][1]
        self.assertNotIn("starts this week", body)
        self.assertNotIn("mean of the base weeks 40", body)

    def test_missing_week_is_noted(self):
        self.put("mvp", report(), drops(missing=["2026-W41"], prev_active=None))
        text = self.comment()
        self.assertIn("- mvp: no events in 2026-W41 — left out of the base, never counted as zero", text)
        self.assertIn("| mvp | 20 | — (no events the week before) |", text)

    def test_drop_files_one_issue_for_the_week_with_every_metric(self):
        self.put("stable", report(), drops(ACTIVE_DROP))
        self.put("mvp", report(), drops(ACTIVE_DROP, CLOSE_DROP))
        line, filing = usage_week.issue(WEEK, self.envs, self.state, [], NOW)
        title, body = filing
        self.assertEqual(title, "[E13] Engagement drop: active_users, manual_close 2026-W43")
        self.assertIn("- mvp: weekly active users: 20, mean of the base weeks 40.0 (-50 %)", body)
        self.assertIn("- mvp: manual-close dialogs confirmed: 50 % of starts, was 80 % (-30 pts) — 10 starts this week, 30 in the base weeks", body)
        self.assertIn("**stable**", body)
        self.assertIn("filing", line)

    def test_scenario_the_same_drop_already_filed_this_week_files_nothing(self):
        self.put("mvp", report(), drops(ACTIVE_DROP))
        for issues in ([{"number": 7, "title": "[E13] Engagement drop: active_users 2026-W43", "state": "OPEN"}],
                       [{"number": 8, "title": "[E13] Engagement drop: help_used 2026-W43", "state": "CLOSED",
                         "closedAt": "2026-10-26T00:00:00Z"}]):
            line, filing = usage_week.issue(WEEK, self.envs, self.state, issues, NOW)
            self.assertIsNone(filing)
            self.assertIn("already reports it", line)
        # Another week's issue, or one closed long ago, is no duplicate.
        old = [{"number": 9, "title": "[E13] Engagement drop: active_users 2026-W42", "state": "OPEN"},
               {"number": 10, "title": "[E13] Engagement drop: active_users 2026-W43", "state": "CLOSED", "closedAt": "2026-09-01T00:00:00Z"}]
        self.assertIsNotNone(usage_week.issue(WEEK, self.envs, self.state, old, NOW)[1])

    def test_no_drop_files_nothing(self):
        self.put("mvp", report(), drops())
        self.assertEqual(usage_week.issue(WEEK, self.envs, self.state, [], NOW), ("no engagement drop", None))


GO = """#!/usr/bin/env bash
# go run ./cmd/usage -events E -week W -out O -alert → O/W.{md,json}, a DROPS line
echo "$*" >> "$US_TEST_DIR/go.log"
while [[ $# -gt 0 ]]; do
  case "$1" in -events) events="$2"; shift ;; -week) week="$2"; shift ;; -out) out="$2"; shift ;; esac
  shift
done
grep -q FAIL "$events" && { echo "usage: broken"; exit 1; }
mkdir -p "$out"
if grep -q OLD "$events"; then echo '{"events_in_week": 0}' > "$out/$week.json"; else cp "$US_TEST_DIR/report.json" "$out/$week.json"; fi
echo "# usage $week" > "$out/$week.md"
echo "$week: some events"
echo "DROPS $(cat "$US_TEST_DIR/drops.json")"
"""

GH = """#!/usr/bin/env bash
echo "$*" >> "$US_TEST_DIR/gh.log"
case "$1 $2" in
  "repo view") echo owner/repo ;;
  "issue list")
    if [[ "$*" == *"--label epic"* ]]; then echo 164; else cat "$US_TEST_DIR/issues.json" 2>/dev/null || echo '[]'; fi ;;
  "issue create")
    n="$(ls "$US_TEST_DIR" | grep -c '^title-' || true)"
    while [[ $# -gt 0 ]]; do
      case "$1" in --body-file) cp "$2" "$US_TEST_DIR/body-$n.md"; shift ;; --title) echo "$2" > "$US_TEST_DIR/title-$n.txt"; shift ;; esac
      shift
    done
    python3 - "$US_TEST_DIR/issues.json" "$US_TEST_DIR/title-$n.txt" "$((900 + n))" <<'PY'
import json, sys
try:
    issues = json.load(open(sys.argv[1]))
except OSError:
    issues = []
issues.append({"number": int(sys.argv[3]), "title": open(sys.argv[2]).read().strip(), "state": "OPEN"})
json.dump(issues, open(sys.argv[1], "w"))
PY
    echo "https://github.com/owner/repo/issues/$((900 + n))" ;;
  "issue comment")
    [[ -f "$US_TEST_DIR/gh_down" ]] && exit 1
    n="$(ls "$US_TEST_DIR" | grep -c '^comment-' || true)"
    while [[ $# -gt 0 ]]; do [[ "$1" == --body-file ]] && cp "$2" "$US_TEST_DIR/comment-$n.md"; shift; done ;;
  *) exit 1 ;;
esac
"""


class UsageScript(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, True)
        t = lambda *p: os.path.join(self.tmp, *p)
        shutil.copytree(SCRIPTS, t("repo", "scripts"))
        for env, port in (("stable", 3000), ("mvp", 3100)):
            write(t("envs", env, ".env"), f"DERIBIT_CLIENT_ID={env}id\nDERIBIT_ENV=testnet\n")
            write(t("envs", env, "deploy.env"), f"OPTIONSBOT_ENV={env}\nMONITOR_PORT={port}\n")
        write(t("report.json"), json.dumps(report()))
        write(t("drops.json"), json.dumps(drops(ACTIVE_DROP)))
        for name, text in (("go", GO), ("gh", GH)):
            write(t("bin", name), text)
            os.chmod(t("bin", name), 0o755)
        self.env = dict(os.environ, PATH=t("bin") + os.pathsep + os.environ["PATH"], OPTIONSBOT_ENVS=t("envs"),
                        US_TEST_DIR=self.tmp, AGENT_NOTIFY="0")
        for k in ("MONITOR_URL", "MVP_MONITOR_URL", "MONITOR_TOKEN", "MVP_MONITOR_TOKEN", "AGENT_STATE_DIR", "AGENT_RUNS_FILE"):
            self.env.pop(k, None)
        self.t = t
        self.week = usage_week.previous_week(dt.datetime.now(dt.timezone.utc).date())

    def events(self, env, text="{}\n"):
        write(self.t("envs", env, "data", "monitor", "events.jsonl"), text)

    def run_script(self):
        p = subprocess.run(["bash", self.t("repo", "scripts", "agents", "usage.sh")], env=self.env,
                           capture_output=True, text=True, timeout=120)
        self.output = p.stdout + p.stderr
        self.assertEqual(p.returncode, 0, self.output)

    def read(self, *p):
        try:
            with open(self.t(*p)) as f:
                return f.read()
        except FileNotFoundError:
            return ""

    def files(self, prefix):
        return [self.read(n) for n in sorted(os.listdir(self.tmp)) if n.startswith(prefix)]

    def done(self):
        return os.path.exists(self.t("repo", "data", "agents", "usage", self.week + ".done"))

    def ledger(self):
        return [json.loads(l) for l in self.read("envs", "history", "agent_runs.jsonl").splitlines() if l.strip()]

    def test_scenario_first_monday_tick_reports_comments_files_and_marks_done(self):
        self.events("mvp")
        self.run_script()
        go = self.read("go.log").splitlines()
        self.assertEqual(len(go), 1, self.output)  # stable has no events file
        self.assertIn(f"-week {self.week}", go[0])
        self.assertIn("-alert", go[0])
        self.assertIn("-events " + self.t("envs", "mvp", "data", "monitor", "events.jsonl"), go[0])
        self.assertTrue(os.path.exists(self.t("envs", "mvp", "data", "monitor", "reports", self.week + ".md")))
        comments = self.files("comment-")
        self.assertEqual(len(comments), 1)
        self.assertIn(f"week {self.week}", comments[0])
        self.assertIn("| mvp | 20 |", comments[0])
        self.assertIn("issue comment 164", self.read("gh.log"))
        self.assertEqual(self.files("title-"), [f"[E13] Engagement drop: active_users {self.week}\n"])
        self.assertTrue(self.done())
        runs = self.ledger()
        self.assertEqual([r["event"] for r in runs], ["start", "end"])
        self.assertEqual(runs[0]["agent"], "usage")
        self.assertEqual(runs[1]["outcome"], "posted")

    def test_scenario_the_marker_exists_nothing_runs_nothing_posted(self):
        self.events("mvp")
        write(self.t("repo", "data", "agents", "usage", self.week + ".done"), "x\n")
        self.run_script()
        self.assertEqual(self.read("go.log"), "")
        self.assertEqual(self.read("gh.log"), "")

    def test_scenario_missing_or_empty_events_file_posts_nothing_no_marker(self):
        self.events("stable", "")  # empty; mvp has none
        self.run_script()
        self.assertIn("nothing to report", self.output)
        self.assertEqual(self.read("go.log"), "")
        self.assertEqual(self.read("gh.log"), "")
        self.assertFalse(self.done())

    def test_scenario_no_events_in_the_week_posts_nothing_no_marker(self):
        self.events("mvp", "OLD\n")
        self.run_script()
        self.assertIn("no event in", self.output)
        self.assertEqual(self.files("comment-"), [])
        self.assertEqual(self.files("title-"), [])
        self.assertFalse(self.done())
        self.assertEqual(self.ledger()[-1]["outcome"], "skipped")
        self.run_script()  # the next tick, within the hour: not looked at again
        self.assertEqual(len(self.read("go.log").splitlines()), 1)

    def test_scenario_drop_already_filed_this_week_no_second_issue(self):
        self.events("mvp")
        write(self.t("issues.json"), json.dumps([{"number": 5, "title": f"[E13] Engagement drop: active_users {self.week}", "state": "OPEN"}]))
        self.run_script()
        self.assertEqual(self.files("title-"), [])
        self.assertIn("#5 already reports it", self.output)
        self.assertEqual(len(self.files("comment-")), 1)

    def test_one_env_failing_keeps_going(self):
        self.events("stable", "FAIL\n")
        self.events("mvp")
        self.run_script()
        self.assertIn("stable: cmd/usage failed", self.output)
        self.assertIn("| mvp |", self.files("comment-")[0])
        self.assertTrue(self.done())

    def test_a_failed_post_is_retried_once_then_the_week_is_done(self):
        self.events("mvp")
        write(self.t("gh_down"), "")
        self.run_script()
        self.assertFalse(self.done())
        self.run_script()
        self.assertTrue(self.done())
        self.run_script()  # a third tick: nothing more
        self.assertEqual(len(self.read("go.log").splitlines()), 2)
        self.assertEqual(len(self.files("title-")), 1)  # the retry finds the first attempt's issue

    def test_tick_starts_usage(self):
        with open(os.path.join(SCRIPTS, "agents", "tick.sh")) as f:
            self.assertIn("start usage.sh 1", f.read())


if __name__ == "__main__":
    unittest.main()
