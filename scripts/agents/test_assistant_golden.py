"""Tests of the weekly live golden-set run (#536): assistant_golden.sh end to
end against throwaway env clones with stub node/gh (no model is called).
python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import datetime as dt
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
sys.path.insert(0, os.path.join(os.path.dirname(HERE), "product"))
import usage_week  # noqa: E402
from test_assistant_grade import oracle  # noqa: E402
from test_usage import GH, write  # noqa: E402

SCRIPTS = os.path.dirname(HERE)
ROOT = os.path.dirname(SCRIPTS)

# node frontend/scripts/assistant-run.mjs --out O --log L → O = answers.jsonl
# ($GOLDEN_TEST_DIR/answers.jsonl), L = one test_run line; FAIL file → exit 3.
NODE = """#!/usr/bin/env bash
echo "$* key=${ANTHROPIC_API_KEY:+set}" >> "$GOLDEN_TEST_DIR/node.log"
[[ -f "$GOLDEN_TEST_DIR/node_fails" ]] && { echo "assistant-run: broken" >&2; exit 3; }
while [[ $# -gt 0 ]]; do
  case "$1" in --out) out="$2"; shift ;; --log) runlog="$2"; shift ;; esac
  shift
done
cp "$GOLDEN_TEST_DIR/answers.jsonl" "$out"
echo '{"kind":"answer","id":"x","test_run":true}' > "$runlog"
"""


class GoldenScript(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, True)
        t = self.t = lambda *p: os.path.join(self.tmp, *p)
        shutil.copytree(SCRIPTS, t("repo", "scripts"))
        shutil.copytree(os.path.join(ROOT, "docs", "product", "assistant"), t("repo", "docs", "product", "assistant"))
        os.makedirs(t("repo", "frontend", "content"))
        shutil.copy(os.path.join(ROOT, "frontend", "content", "help-index.json"), t("repo", "frontend", "content"))
        for env, port in (("stable", 3000), ("mvp", 3100)):
            write(t("envs", env, ".env"), f"DERIBIT_CLIENT_ID={env}id\nDERIBIT_ENV=testnet\n")
            write(t("envs", env, "deploy.env"), f"OPTIONSBOT_ENV={env}\nMONITOR_PORT={port}\n")
        with open(os.path.join(ROOT, "docs", "product", "assistant", "golden.jsonl")) as f:
            self.golden = [json.loads(l) for l in f if l.strip()]
        self.answers()
        for name, text in (("node", NODE), ("gh", GH)):
            write(t("bin", name), text)
            os.chmod(t("bin", name), 0o755)
        self.env = dict(os.environ, PATH=t("bin") + os.pathsep + os.environ["PATH"], OPTIONSBOT_ENVS=t("envs"),
                        US_TEST_DIR=self.tmp, GOLDEN_TEST_DIR=self.tmp, AGENT_NOTIFY="0")
        for k in ("ANTHROPIC_API_KEY", "ASSISTANT_GOLDEN_ENV", "AGENT_STATE_DIR", "AGENT_RUNS_FILE"):
            self.env.pop(k, None)
        self.week = usage_week.previous_week(dt.datetime.now(dt.timezone.utc).date())

    def answers(self, wrong_category=None):
        with open(self.t("answers.jsonl"), "w") as f:
            for q in self.golden:
                a = oracle(q)
                if q["category"] == wrong_category:
                    a = {"id": q["id"], "text": "Rules are checked.", "citations": [], "outcome": "answered"}
                f.write(json.dumps(a) + "\n")

    def key(self, env="mvp"):
        with open(self.t("envs", env, ".env"), "a") as f:
            f.write("ANTHROPIC_API_KEY=sk-test-not-a-key\n")

    def run_script(self):
        p = subprocess.run(["bash", self.t("repo", "scripts", "agents", "assistant_golden.sh")], env=self.env,
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
        return os.path.exists(self.t("repo", "data", "agents", "assistant_golden", self.week + ".done"))

    def ledger(self):
        return [json.loads(l) for l in self.read("envs", "history", "agent_runs.jsonl").splitlines() if l.strip()]

    def reviews(self, *p):
        return self.t("envs", "mvp", "data", "monitor", "assistant_reviews", *p)

    def test_scenario_the_weekly_run_grades_and_posts_counts_on_the_e10_epic(self):
        self.key()
        self.run_script()
        node = self.read("node.log").splitlines()
        self.assertEqual(len(node), 1, self.output)
        self.assertIn("key=set", node[0])
        self.assertNotIn("sk-test-not-a-key", node[0] + self.output)
        self.assertTrue(os.path.exists(self.reviews(self.week + ".golden.summary.json")))
        self.assertTrue(os.path.exists(self.reviews("golden_baseline.json")))
        [c] = self.comments()
        self.assertIn(f"Assistant golden-set run** — week {self.week}", c)
        self.assertIn("| all | ", c)
        self.assertIn("First run: this score is the baseline.", c)
        self.assertIn("issue comment 164", self.read("gh.log"))
        self.assertTrue(self.done())
        self.assertEqual([(r["agent"], r["event"], r.get("outcome")) for r in self.ledger()][-1], ("assistant_golden", "end", "posted"))
        self.run_script()  # done: the model is not asked again
        self.assertEqual(len(self.read("node.log").splitlines()), 1)

    def test_scenario_a_regression_is_named_in_the_weekly_summary(self):
        self.key()
        write(self.reviews("golden_baseline.json"), json.dumps(
            {"week": "2026-W40", "categories": {"jargon": {"passed": 5, "total": 5, "pct": 100.0}}}))
        self.answers(wrong_category="jargon")
        self.run_script()
        [c] = self.comments()
        self.assertIn("**jargon** ⚠️", c)
        self.assertIn("Regression: jargon 0 % (baseline 100 %, −100 pts).", c)
        for q in self.golden:
            self.assertNotIn(q["question"], c)
        self.assertIn("regression in", self.output)
        self.assertTrue(self.done())

    def test_scenario_no_key_runs_nothing(self):
        self.run_script()
        self.assertEqual(self.read("node.log"), "")
        self.assertEqual(self.read("gh.log"), "")
        self.assertFalse(self.done())
        self.assertEqual(self.ledger(), [])

    def test_a_failed_run_is_tried_once_more_then_the_week_is_done(self):
        self.key()
        write(self.t("node_fails"), "")
        self.run_script()
        self.assertIn("the run failed", self.output)
        self.assertFalse(self.done())
        self.assertFalse(os.path.exists(self.reviews(self.week + ".golden.summary.json")))
        self.run_script()
        self.assertTrue(self.done())
        self.assertEqual(self.comments(), [])
        self.assertEqual(len(self.read("node.log").splitlines()), 2)

    def test_a_failed_post_is_posted_again_without_asking_the_model(self):
        self.key()
        write(self.t("gh_down"), "")
        self.run_script()
        self.assertFalse(self.done())
        os.remove(self.t("gh_down"))
        self.run_script()
        self.assertEqual(len(self.read("node.log").splitlines()), 1)
        self.assertEqual(len(self.comments()), 1)
        self.assertTrue(self.done())

    def test_usage_runs_it_after_the_quality_review(self):
        with open(os.path.join(HERE, "usage.sh")) as f:
            text = f.read()
        self.assertLess(text.index("assistant_review.sh\" ||"), text.index("assistant_golden.sh\" ||"))


if __name__ == "__main__":
    unittest.main()
