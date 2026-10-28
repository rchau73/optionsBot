"""Tests of pm_agent.sh's board-order step (#437): python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import json
import os
import re
import subprocess
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))


class BoardOrderTest(unittest.TestCase):
    """The column names reach set_position whole, "In review" included."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        t = self.tmp.name
        self.env = dict(os.environ, AGENT_STATE_DIR=os.path.join(t, "state"), OPTIONSBOT_ENVS=os.path.join(t, "envs"),
                        AGENT_WORK_DIR=os.path.join(t, "wt"), AGENT_NOTIFY="0")
        for k in ("MONITOR_URL", "MVP_MONITOR_URL", "MONITOR_TOKEN", "MVP_MONITOR_TOKEN"):
            self.env.pop(k, None)
        self.order = os.path.join(t, "order.json")
        with open(self.order, "w") as f:
            json.dump({"Triage": ["T1"], "In review": ["R1", "R2"], "Ready": []}, f)

    def tearDown(self):
        self.tmp.cleanup()

    def bash(self, script):
        p = subprocess.run(["bash", "-c", f'source "{HERE}/lib.sh"\n{script}'], env=self.env,
                           capture_output=True, text=True, timeout=60)
        self.assertEqual(p.returncode, 0, p.stderr)
        return p.stdout

    def test_json_lines_keeps_a_column_name_with_a_space(self):
        self.assertEqual(self.bash(f'json_lines "{self.order}"').splitlines(), ["Triage", "In review", "Ready"])
        self.assertEqual(self.bash(f'json_lines "{self.order}" "In review"').splitlines(), ["R1", "R2"])
        self.assertEqual(self.bash(f'json_lines "{self.order}" Ready'), "")

    def test_order_step_orders_in_review(self):
        # Run pm_agent.sh's own step 7 loop with set_position/state_set stubbed.
        with open(os.path.join(HERE, "pm_agent.sh")) as f:
            src = f.read()
        m = re.search(r"\nwhile IFS= read -r col <&3; do\n.*?\ndone 3< <\(json_lines \"\$run/order.json\"\)\n", src, re.S)
        self.assertIsNotNone(m, "step 7 must read the columns one per line with json_lines (#437)")
        run = os.path.dirname(self.order)
        out = self.bash('set -euo pipefail\n'
                        'set_position() { echo "pos $1 after=${2:-}"; }\n'
                        'state_set() { echo "state $2"; }\n'
                        'log() { echo "$*"; }\n'
                        f'run="{run}"\n' + m.group(0))
        self.assertEqual(out.splitlines(), [
            "pos T1 after=", "state Triage", "pm: ordered Triage",
            "pos R1 after=", "pos R2 after=R1", "state In review", "pm: ordered In review",
            "state Ready", "pm: ordered Ready",
        ])


if __name__ == "__main__":
    unittest.main()
