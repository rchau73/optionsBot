"""Wiring of the delivery report (#470): dora.sh runs from the tick, and the
regression hooks of the Dev and SME agents write the "Caused by #N" line cmd/dora
reads. The report itself is tested in Go (tests/delivery_test.go).
python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import os
import re
import subprocess
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))


def read(name):
    with open(os.path.join(HERE, name)) as f:
        return f.read()


class DoraWiring(unittest.TestCase):
    def test_tick_starts_dora(self):
        self.assertIn("start dora.sh 1", read("tick.sh"))

    def test_dora_runs_cmd_dora_and_posts_on_e6(self):
        text = read("dora.sh")
        self.assertRegex(text, r"run_begin dora\b")
        self.assertIn("go run ./cmd/dora", text)
        self.assertIn('startswith("Epic E6 ")', text)
        # The week is marked posted only after the comment went out.
        self.assertLess(text.index("gh issue comment"), text.index('> "$posted_marker"'))

    def test_regression_label_exists(self):
        self.assertRegex(read("setup_github.sh"), r'label "regression"')

    def test_sme_adds_the_label_and_the_caused_by_line(self):
        text = read("sme_review.sh")
        self.assertIn("--label regression", text)
        self.assertIn('echo "Caused by #$caused"', text)
        self.assertIn("caused_by", text)

    def test_dev_agent_reads_caused_by_from_the_pr_body(self):
        text = read("dev_agent.sh")
        self.assertIn("--add-label regression", text)
        # The same extraction the script runs, on a PR body.
        m = re.search(r"caused=\"\$\((grep .*?\n.*?)\)\" \|\| caused=\"\"", text, re.S)
        self.assertIsNotNone(m)
        cmd = m.group(1).replace('"$run_dir/pr-body.md"', '"$1"')
        with tempfile.NamedTemporaryFile("w", suffix=".md", delete=False) as f:
            f.write("## Plan\n- fix the stop\n- Caused by #412 (its deploy)\n- Caused by #9\n")
        self.addCleanup(os.remove, f.name)
        out = subprocess.run(["bash", "-c", cmd, "bash", f.name], capture_output=True, text=True).stdout.strip()
        self.assertEqual(out, "412")


if __name__ == "__main__":
    unittest.main()
