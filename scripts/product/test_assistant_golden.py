"""Tests of the live golden-set run's scoring (#536): assistant_golden.py and
assistant_run.sh's stop without a key.
python3 -m unittest scripts/product/test_assistant_golden.py"""
import contextlib
import io
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import assistant_golden as gold  # noqa: E402
import assistant_grade as ag  # noqa: E402
from test_assistant_grade import oracle  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
GOLDEN = ag.load_jsonl(ag.GOLDEN)


class GoldenScoreTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self.out = os.path.join(self.tmp, "assistant_reviews")

    def answers(self, wrong=()):
        """The set's own correct answers, "I don't know" for the ids in wrong."""
        path = os.path.join(self.tmp, "answers.jsonl")
        with open(path, "w") as f:
            for q in GOLDEN:
                a = oracle(q) if q["id"] not in wrong else {"id": q["id"], "text": ag.DONT_KNOW, "citations": [], "outcome": "dont_know"}
                f.write(json.dumps(a) + "\n")
        return path

    def score(self, *extra):
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            code = gold.main(["score", self.answers(*extra[:1]), "--out-dir", self.out, "--week", "2026-W44", *extra[1:]])
        return code, buf.getvalue()

    def read(self, name):
        with open(os.path.join(self.out, name)) as f:
            return json.load(f)

    def test_first_run_sets_the_baseline(self):
        code, text = self.score()
        self.assertEqual(code, 0)
        self.assertIn("First run: this score is the baseline.", text)
        base = self.read(gold.BASELINE)
        self.assertEqual(base["categories"]["all"], {"passed": len(GOLDEN), "total": len(GOLDEN), "pct": 100.0})
        s = self.read("2026-W44.golden.summary.json")
        self.assertEqual((s["status"], s["regressions"], s["questions"]), ("baseline_set", [], len(GOLDEN)))
        self.assertEqual(oct(os.stat(os.path.join(self.out, gold.BASELINE)).st_mode & 0o777), "0o600")

    # Scenario: a regression is reported.
    def test_a_category_falling_more_than_the_threshold_is_named_and_exits_non_zero(self):
        self.score()
        jargon = [q["id"] for q in GOLDEN if q["category"] == "jargon"]
        code, text = self.score(jargon[:2])
        self.assertEqual(code, 1)
        s = self.read("2026-W44.golden.summary.json")
        self.assertIn("jargon", s["regressions"])
        row = next(r for r in s["categories"] if r["category"] == "jargon")
        self.assertEqual(row["baseline_pct"], 100.0)
        self.assertGreater(row["drop_pts"], gold.THRESHOLD_PTS)
        self.assertIn("Regression: jargon", text)
        c = gold.comment(s)
        self.assertIn("**jargon** ⚠️", c)
        self.assertIn(f"−{row['drop_pts']:g} pts", c)
        # The baseline stays the last accepted score.
        self.assertEqual(self.read(gold.BASELINE)["categories"]["jargon"]["pct"], 100.0)

    def test_a_drop_at_the_threshold_is_not_a_regression(self):
        rows = gold.compare({"x": {"passed": 9, "total": 10}, "y": {"passed": 8, "total": 10}},
                            {"x": {"pct": 100.0}, "y": {"pct": 90.0}, "z": {"pct": 50}})
        self.assertEqual([(r["category"], r["drop_pts"], r["regression"]) for r in rows], [("x", 10.0, False), ("y", 10.0, False)])
        rows = gold.compare({"x": {"passed": 8, "total": 10}, "new": {"passed": 1, "total": 2}}, {"x": {"pct": 90.1}})
        self.assertEqual([(r["category"], r["regression"]) for r in rows], [("x", True), ("new", False)])

    def test_accept_makes_this_run_the_baseline(self):
        self.score()
        wrong = [q["id"] for q in GOLDEN if q["category"] == "risk"]
        code, text = self.score(wrong, "--accept")
        self.assertEqual(code, 0)
        self.assertIn("accepted as the new baseline", text)
        self.assertEqual(self.read(gold.BASELINE)["categories"]["risk"]["pct"], 0.0)
        self.assertEqual(self.score(wrong)[0], 0)

    # Scenario: privacy.
    def test_the_summary_and_comment_hold_counts_and_percentages_only(self):
        self.score()
        self.score([q["id"] for q in GOLDEN[:20]])
        s = self.read("2026-W44.golden.summary.json")
        text = json.dumps(s, ensure_ascii=False) + gold.comment(s)
        for q in GOLDEN:
            self.assertNotIn(q["question"], text, q["id"])
            self.assertNotIn(q["id"], text)
        self.assertNotIn(ag.DONT_KNOW, text)
        # A category name from anywhere else is never printed.
        s["categories"].append({"category": "What is my account worth?", "passed": 1, "total": 1, "pct": 100.0,
                                "baseline_pct": None, "drop_pts": None, "regression": False})
        self.assertNotIn("account worth", gold.comment(s))

    def test_model_errors_are_counted_and_fail_their_question(self):
        path = os.path.join(self.tmp, "answers.jsonl")
        with open(path, "w") as f:
            for q in GOLDEN:
                a = {"id": q["id"], "text": "", "citations": [], "outcome": "model_error"} if q["id"] == "how-01" else oracle(q)
                f.write(json.dumps(a) + "\n")
        s = gold.score(path, self.out, "2026-W44")
        self.assertEqual(s["model_errors"], 1)
        row = next(r for r in s["categories"] if r["category"] == "how_it_works")
        self.assertEqual(row["passed"], row["total"] - 1)

    def test_bad_input_exits_2_and_writes_no_score(self):
        bad = os.path.join(self.tmp, "bad.jsonl")
        with open(bad, "w") as f:
            f.write("not json\n")
        with contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(gold.main(["score", bad, "--out-dir", self.out, "--week", "2026-W44"]), 2)
        self.assertFalse(os.path.exists(self.out))


class RunScriptTest(unittest.TestCase):
    """Scenario: no key — assistant_run.sh stops with one clear message and writes no score."""

    def test_no_key_stops_before_anything(self):
        tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, tmp, True)
        os.makedirs(os.path.join(tmp, "mvp"))
        with open(os.path.join(tmp, "mvp", ".env"), "w") as f:
            f.write("DERIBIT_CLIENT_ID=x\nANTHROPIC_API_KEY=\n")
        env = {k: v for k, v in os.environ.items() if k != "ANTHROPIC_API_KEY"}
        env["OPTIONSBOT_ENVS"] = tmp
        p = subprocess.run(["bash", os.path.join(HERE, "assistant_run.sh"), "--env", "mvp"], env=env,
                           capture_output=True, text=True, timeout=60)
        self.assertEqual(p.returncode, 2)
        self.assertEqual(p.stderr.strip().splitlines(), [
            f"assistant_run: ANTHROPIC_API_KEY is not set (in the shell or {tmp}/mvp/.env): nothing was asked and no score was written"])
        self.assertEqual(os.listdir(os.path.join(tmp, "mvp")), [".env"])

    def test_unknown_env_is_refused(self):
        p = subprocess.run(["bash", os.path.join(HERE, "assistant_run.sh"), "--env", "live"], capture_output=True, text=True, timeout=60,
                           env=dict(os.environ, ANTHROPIC_API_KEY="x"))
        self.assertEqual(p.returncode, 2)
        self.assertIn("unknown environment 'live'", p.stderr)


if __name__ == "__main__":
    unittest.main()
