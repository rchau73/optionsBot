"""Tests of the agents' budget (limit_lib.sh): python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import datetime
import os
import subprocess
import tempfile
import time
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))


class BudgetTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.env_file = os.path.join(self.tmp.name, "agents.env")
        self.budget_file = os.path.join(self.tmp.name, "claude_runs")

    def tearDown(self):
        self.tmp.cleanup()

    def shell(self, script, env_text="", extra_env=None):
        """Runs bash with limit_lib.sh sourced and agents.env = env_text; returns stdout."""
        open(self.env_file, "w").write(env_text)
        env = {k: v for k, v in os.environ.items()
               if k not in ("AGENTS_PAUSED", "AGENT_HOURS", "AGENT_MAX_RUNS_PER_DAY", "DEV_PARALLEL")}
        env.update(extra_env or {})
        cmd = (f'log() {{ :; }}; LIMIT_FILE="{self.tmp.name}/limit_hit"; AGENTS_ENV_FILE="{self.env_file}"; '
               f'BUDGET_FILE="{self.budget_file}"; source "{HERE}/limit_lib.sh"; {script}')
        p = subprocess.run(["bash", "-c", cmd], capture_output=True, text=True, env=env)
        self.assertEqual(p.returncode, 0, p.stderr)
        return p.stdout.strip()

    def test_no_file_runs_as_configured(self):
        """Scenario: no budget set — parallel_max returns the configured count."""
        os.remove(self.env_file) if os.path.exists(self.env_file) else None
        self.assertEqual(self.shell("parallel_max 2"), "2")

    def test_paused(self):
        """Scenario: AGENTS_PAUSED=1 — no Claude run."""
        self.assertEqual(self.shell("parallel_max 3", "AGENTS_PAUSED=1\n"), "0")
        self.assertIn("paused", self.shell("budget_reason", "AGENTS_PAUSED=1\n"))

    def test_hours(self):
        """Scenario: outside the agent hours — no run; inside (wrapping midnight too) — runs."""
        h = datetime.datetime.now().hour
        inside = f"AGENT_HOURS={h}-{(h + 1) % 24 or 24}\n"
        outside = f"AGENT_HOURS={(h + 1) % 24}-{(h + 2) % 24 or 24}\n"
        wrapping = f"AGENT_HOURS={(h + 2) % 24}-{(h + 1) % 24}\n"  # every hour but h+1
        self.assertEqual(self.shell("parallel_max 3", inside), "3")
        self.assertEqual(self.shell("parallel_max 3", outside), "0")
        self.assertEqual(self.shell("parallel_max 3", wrapping), "3")
        self.assertEqual(self.shell("parallel_max 3", "AGENT_HOURS=0-24\n"), "3")

    def test_daily_cap_counts_today_only(self):
        """Scenario: the day's runs reach the cap — no run; yesterday's runs do not count."""
        yesterday = int(time.time()) - 2 * 86400
        open(self.budget_file, "w").write(f"{yesterday} dev\n" * 5)
        self.assertEqual(self.shell("parallel_max 3", "AGENT_MAX_RUNS_PER_DAY=2\n"), "3")
        self.shell("budget_take dev; budget_take review")
        self.assertEqual(self.shell("runs_today"), "2")
        self.assertEqual(self.shell("parallel_max 3", "AGENT_MAX_RUNS_PER_DAY=2\n"), "0")
        self.assertIn("2 of 2", self.shell("budget_reason", "AGENT_MAX_RUNS_PER_DAY=2\n"))

    def test_file_sets_parallelism_environment_wins(self):
        """Scenario: agents.env sets DEV_PARALLEL; a value in the environment wins; unknown keys are ignored."""
        text = "DEV_PARALLEL=2 # two at once\nPATH=/nowhere\n"
        self.assertEqual(self.shell('echo "$DEV_PARALLEL"', text), "2")
        self.assertEqual(self.shell('echo "$DEV_PARALLEL"', text, {"DEV_PARALLEL": "1"}), "1")
        self.assertNotEqual(self.shell('echo "$PATH"', text), "/nowhere")


if __name__ == "__main__":
    unittest.main()
