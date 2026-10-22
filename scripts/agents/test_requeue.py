"""Tests of requeue.py and limit_lib.sh (#237): python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import json
import os
import subprocess
import sys
import tempfile
import time
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import requeue  # noqa: E402

FIXTURE = os.path.join(HERE, "testdata", "agent_weekly_limit.md")
WEEKLY = open(FIXTURE, encoding="utf-8").read()

PICKUP = "🤖 **Dev agent** picked this up on branch `agent/issue-88` — **attempt 1**."
STOP = ("🤖 **Dev agent stopped — a person is needed.**\n\nStopped because the agent did not finish "
        "(exit 1, result 'none').\n\nLast lines:\n\n    " + WEEKLY)
QUESTIONS = "🤖 **Dev agent stopped — a person is needed.**\n\n## Questions\nWhich threshold should apply?"
LIMIT = "🤖 Dev agent hit a usage/rate limit (exit 1 after 3s). The card goes back to **Ready**."


def c(body, at="2026-10-19T23:03:00Z"):
    return {"author": {"login": "rchau73"}, "createdAt": at, "body": body}


def card(n, status="Blocked", labels=("agent:needs-human",), state="OPEN"):
    return {"item": f"I{n}", "number": n, "status": status, "state": state, "labels": list(labels)}


def shell(script, limit_file):
    """Runs bash with limit_lib.sh sourced; returns (exit code, stdout)."""
    cmd = f'log() {{ :; }}; LIMIT_FILE="{limit_file}"; source "{HERE}/limit_lib.sh"; {script}'
    p = subprocess.run(["bash", "-c", cmd], capture_output=True, text=True)
    return p.returncode, p.stdout.strip()


class LimitPatternTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.limit_file = os.path.join(self.tmp.name, "limit_hit")

    def tearDown(self):
        self.tmp.cleanup()

    def write(self, text):
        path = os.path.join(self.tmp.name, "agent.md")
        open(path, "w", encoding="utf-8").write(text)
        return path

    def test_weekly_limit_is_a_limit(self):
        """Scenario: weekly limit is a limit — note_limit succeeds and writes the limit time."""
        before = int(time.time())
        rc, _ = shell(f'note_limit "{FIXTURE}"', self.limit_file)
        self.assertEqual(rc, 0)
        self.assertGreaterEqual(int(open(self.limit_file).read()), before - 1)
        rc, out = shell("parallel_max 3", self.limit_file)
        self.assertEqual(out, "0")  # inside the 30-minute pause

    def test_python_pattern_matches_the_shell_one(self):
        p = requeue.limit_pattern()
        for text in (WEEKLY, "Claude AI usage limit reached", "You have hit your session limit",
                     "API Error: 429 rate limit", "limit · resets 3am", "Overloaded"):
            with self.subTest(text=text):
                self.assertTrue(p.search(text))
                rc, _ = shell(f'note_limit "{self.write(text)}"', self.limit_file)
                self.assertEqual(rc, 0)

    def test_ordinary_failure_is_not_a_limit(self):
        path = self.write("go test failed: TestStrategy_Entry\nRESULT: needs_human\n")
        self.assertFalse(requeue.limit_pattern().search(open(path).read()))
        rc, _ = shell(f'note_limit "{path}"', self.limit_file)
        self.assertEqual(rc, 1)
        self.assertFalse(os.path.exists(self.limit_file))

    def test_fast_failure_without_result_is_infrastructure(self):
        """Scenario: a fast failure never asks a person — exit 1 after 3 s, no RESULT line."""
        rc, _ = shell(f'note_stop 1 3 "{self.write("Error: something unexpected")}"', self.limit_file)
        self.assertEqual(rc, 0)
        self.assertTrue(os.path.exists(self.limit_file))

    def test_slow_failure_or_an_answer_is_the_card(self):
        cases = {
            "slow": ("1", "600", "Error: tests kept failing"),
            "answer": ("1", "5", "RESULT: needs_human\n<!-- PR-BODY q -->"),
            "review block": ("1", "5", "<!-- REVIEW-JSON {} -->"),
            "pm block": ("1", "5", "<!-- PM-STORIES {} -->"),
            "success": ("0", "5", "done"),
        }
        for name, (code, secs, text) in cases.items():
            with self.subTest(name):
                rc, _ = shell(f'note_stop {code} {secs} "{self.write(text)}"', self.limit_file)
                self.assertEqual(rc, 1)
                self.assertFalse(os.path.exists(self.limit_file))


class LimitNotedTest(unittest.TestCase):
    """At most one limit comment per episode."""

    def test_first_limit_gets_a_comment(self):
        self.assertFalse(requeue.limit_noted([c(PICKUP)]))
        self.assertFalse(requeue.limit_noted([]))

    def test_repeated_limit_after_pickups_does_not(self):
        self.assertTrue(requeue.limit_noted([c(PICKUP), c(LIMIT), c(PICKUP)]))

    def test_a_person_in_between_starts_a_new_episode(self):
        self.assertFalse(requeue.limit_noted([c(PICKUP), c(LIMIT), c("please retry"), c(PICKUP)]))

    def test_cli_reads_gh_issue_json(self):
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as f:
            json.dump({"comments": [c(PICKUP), c(LIMIT), c(PICKUP)]}, f)
        try:
            p = subprocess.run([sys.executable, os.path.join(HERE, "requeue.py"), "limit-noted", f.name])
            self.assertEqual(p.returncode, 0)
        finally:
            os.unlink(f.name)


class SelectTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.runs = self.tmp.name
        self.pattern = requeue.limit_pattern()

    def tearDown(self):
        self.tmp.cleanup()

    def run_log(self, n, when, text):
        d = os.path.join(self.runs, f"issue-{n}-{when}")
        os.makedirs(d)
        open(os.path.join(d, "agent.md"), "w", encoding="utf-8").write(text)

    def pick(self, board, comments):
        outputs = {b["number"]: requeue.newest_run_output(self.runs, b["number"]) for b in board}
        return requeue.select(board, comments, outputs, self.pattern)

    def test_cards_blocked_by_the_old_bug_are_requeued(self):
        """Scenario: cards blocked by the old bug are re-queued."""
        self.run_log(88, "20261021-211000", WEEKLY)
        got = self.pick([card(88)], {88: [c(PICKUP), c(STOP)]})
        self.assertEqual(got, [("I88", 88)])

    def test_an_agent_comment_after_the_stop_still_qualifies(self):
        self.run_log(88, "20261021-211000", WEEKLY)
        got = self.pick([card(88)], {88: [c(PICKUP), c(STOP), c("🧭 **PM agent** placed this in E6.")]})
        self.assertEqual(got, [("I88", 88)])

    def test_real_questions_stay_with_the_person(self):
        """Scenario: real questions stay with the person — each non-qualifying case."""
        self.run_log(1, "20261019-230300", WEEKLY)
        cases = {
            "agent's questions": (card(1), [c(PICKUP), c(QUESTIONS)]),
            "person after the stop": (card(1), [c(PICKUP), c(STOP), c("Use 0.25, then retry")]),
            "pm:waiting": (card(1, labels=("agent:needs-human", "pm:waiting")), [c(PICKUP), c(STOP)]),
            "no needs-human": (card(1, labels=()), [c(PICKUP), c(STOP)]),
            "not Blocked": (card(1, status="Ready"), [c(PICKUP), c(STOP)]),
            "closed": (card(1, state="CLOSED"), [c(PICKUP), c(STOP)]),
            "no stop at all": (card(1), [c(PICKUP)]),
        }
        for name, (b, comments) in cases.items():
            with self.subTest(name):
                self.assertEqual(self.pick([b], {1: comments}), [])

    def test_newest_run_must_show_the_limit(self):
        # An older run hit the limit, the newest failed for a real reason.
        self.run_log(5, "20261019-230300", WEEKLY)
        self.run_log(5, "20261021-100000", "panic: nil map\nexit status 2")
        self.assertEqual(self.pick([card(5)], {5: [c(PICKUP), c(STOP)]}), [])
        # No run log at all.
        self.assertEqual(self.pick([card(6)], {6: [c(PICKUP), c(STOP)]}), [])

    def test_run_dirs_of_other_issues_do_not_count(self):
        self.run_log(237, "20261021-230656", WEEKLY)  # issue-237-… is not issue-23-…
        self.assertIsNone(requeue.newest_run_output(self.runs, 23))
        self.assertEqual(self.pick([card(23)], {23: [c(PICKUP), c(STOP)]}), [])

    def test_candidates_skip_reading_comments_of_other_cards(self):
        self.run_log(88, "20261021-211000", WEEKLY)
        self.run_log(98, "20261021-211000", "RESULT: needs_human")
        board = [card(88), card(98), card(99, status="Ready")]
        outputs = {b["number"]: requeue.newest_run_output(self.runs, b["number"]) for b in board}
        self.assertEqual(requeue.candidates(board, outputs, self.pattern), [88])

    def test_cli_select(self):
        self.run_log(88, "20261021-211000", WEEKLY)
        board = os.path.join(self.runs, "board.json")
        comments = os.path.join(self.runs, "comments.json")
        json.dump([card(88), card(98)], open(board, "w"))
        json.dump({"88": [c(PICKUP), c(STOP)]}, open(comments, "w"))
        rq = os.path.join(HERE, "requeue.py")
        out = subprocess.run([sys.executable, rq, "select", board, comments, self.runs],
                             capture_output=True, text=True, check=True).stdout
        self.assertEqual(out.split(), ["I88", "88"])
        out = subprocess.run([sys.executable, rq, "candidates", board, self.runs],
                             capture_output=True, text=True, check=True).stdout
        self.assertEqual(out.split(), ["88"])


if __name__ == "__main__":
    unittest.main()
