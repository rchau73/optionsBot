"""Tests of the agent run ledger (#206): runs.py and lib.sh run_begin/run_end.
python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import io
import json
import os
import re
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stdout

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import runs  # noqa: E402

RUNS_PY = os.path.join(HERE, "runs.py")


class LineFormatTest(unittest.TestCase):
    def test_fields_in_order_on_one_line(self):
        line = runs.make_line("dev", "dev-1", "end", issue="123", pr="#456", outcome="pr_opened",
                              detail="checks\npassed", log="data/agents/dev/issue-123-x", commit="abc1234",
                              ts="2026-10-22T10:00:00Z")
        self.assertNotIn("\n", line)
        rec = json.loads(line)
        self.assertEqual(tuple(rec), runs.FIELDS)
        self.assertEqual(rec, {"ts": "2026-10-22T10:00:00Z", "agent": "dev", "run_id": "dev-1", "issue": 123,
                               "pr": 456, "event": "end", "outcome": "pr_opened", "detail": "checks passed",
                               "log": "data/agents/dev/issue-123-x", "commit": "abc1234"})

    def test_empty_values_are_null_and_ts_is_utc(self):
        rec = json.loads(runs.make_line("sme", "sme-1", "start", issue="", pr=None))
        self.assertIsNone(rec["issue"])
        self.assertIsNone(rec["pr"])
        self.assertIsNone(rec["outcome"])
        self.assertIsNone(rec["detail"])
        self.assertRegex(rec["ts"], r"^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$")

    def test_detail_is_cut(self):
        rec = json.loads(runs.make_line("pm", "pm-1", "end", outcome="triaged", detail="x" * 1000))
        self.assertEqual(len(rec["detail"]), runs.DETAIL_MAX)

    def test_bad_lines_are_refused(self):
        for kw in ({"agent": "bot"}, {"event": "middle"}, {"run_id": ""}, {"issue": "abc"}, {"pr": "1.5"},
                   {"event": "end", "outcome": ""}):
            args = dict(agent="dev", run_id="dev-1", event="start")
            args.update(kw)
            with self.assertRaises(ValueError, msg=kw):
                runs.make_line(**args)

    def test_cli_line_takes_a_detail_starting_with_a_dash(self):
        p = subprocess.run([sys.executable, RUNS_PY, "line", "--agent=review", "--run-id=r-1", "--event=end",
                            "--outcome=failed", "--detail=-1 exit", "--issue=", "--pr=9"],
                           capture_output=True, text=True)
        self.assertEqual(p.returncode, 0, p.stderr)
        rec = json.loads(p.stdout)
        self.assertEqual((rec["detail"], rec["issue"], rec["pr"]), ("-1 exit", None, 9))

    def test_cli_line_refuses_an_unknown_agent(self):
        p = subprocess.run([sys.executable, RUNS_PY, "line", "--agent=nobody", "--run-id=x", "--event=start"],
                           capture_output=True, text=True)
        self.assertEqual(p.returncode, 2)
        self.assertEqual(p.stdout, "")


def rec(ts, agent, run_id, event, issue=None, pr=None, outcome=None, log=None):
    return json.dumps({"ts": ts, "agent": agent, "run_id": run_id, "issue": issue, "pr": pr, "event": event,
                       "outcome": outcome, "detail": None, "log": log, "commit": None})


LEDGER = "\n".join([
    rec("2026-10-21T10:00:00Z", "dev", "dev-a", "start", issue=123, log="data/agents/dev/issue-123-a"),
    rec("2026-10-21T10:00:05Z", "pm", "pm-a", "start"),
    rec("2026-10-21T10:30:00Z", "dev", "dev-a", "end", issue=123, pr=456, outcome="pr_opened"),
    "not json {",
    rec("2026-10-21T10:00:06Z", "pm", "pm-a", "end", outcome="triaged"),
    rec("2026-10-21T11:00:00Z", "review", "rev-a", "start", issue=123, pr=456),
    rec("2026-10-21T11:10:00Z", "review", "rev-a", "end", issue=123, pr=456, outcome="changes_requested"),
    rec("2026-10-22T09:00:00Z", "dev", "dev-b", "start", issue=123, pr=456),
    rec("2026-10-22T09:00:00Z", "dev", "dev-c", "start", issue=77),
    rec("2026-10-22T09:30:00Z", "dev", "dev-c", "end", issue=77, pr=78, outcome="pr_opened"),
]) + "\n"


class QueryTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.path = os.path.join(self.tmp.name, "agent_runs.jsonl")
        with open(self.path, "w") as f:
            f.write(LEDGER)

    def tearDown(self):
        self.tmp.cleanup()

    def cli(self, *args):
        p = subprocess.run([sys.executable, RUNS_PY, "--file", self.path, *args], capture_output=True, text=True)
        self.assertEqual(p.returncode, 0, p.stderr)
        return p

    def test_read_skips_malformed_lines(self):
        records, bad = runs.read(self.path)
        self.assertEqual((len(records), bad), (9, 1))

    def test_missing_file_is_empty(self):
        self.assertEqual(runs.read(os.path.join(self.tmp.name, "none.jsonl")), ([], 0))

    def test_runs_pair_start_and_end(self):
        rs = {r["run_id"]: r for r in runs.runs(runs.read(self.path)[0])}
        a = rs["dev-a"]
        self.assertEqual((a["start"], a["end"], a["outcome"], a["pr"], a["log"]),
                         ("2026-10-21T10:00:00Z", "2026-10-21T10:30:00Z", "pr_opened", 456, "data/agents/dev/issue-123-a"))
        self.assertIsNone(rs["dev-b"]["end"])

    # Scenario: query — runs.py --issue 123 prints every run that touched it, in time order.
    def test_query_by_issue_in_time_order(self):
        p = self.cli("--issue", "123", "--json")
        got = [json.loads(l)["run_id"] for l in p.stdout.splitlines()]
        self.assertEqual(got, ["dev-a", "rev-a", "dev-b"])
        self.assertIn("skipped 1 line", p.stderr)

    def test_query_table_shows_open_runs(self):
        out = self.cli("--issue", "#123").stdout.splitlines()
        self.assertEqual(out[0].split("\t")[:3], ["start", "end", "agent"])
        self.assertEqual(len(out), 4)
        self.assertIn("(open)", out[3])
        self.assertIn("pr_opened", out[1])

    def test_open_runs(self):
        got = [r["run_id"] for r in runs.select(runs.runs(runs.read(self.path)[0]), open_only=True)]
        self.assertEqual(got, ["dev-b"])

    def test_filters_by_agent_pr_and_date(self):
        all_runs = runs.runs(runs.read(self.path)[0])
        self.assertEqual([r["run_id"] for r in runs.select(all_runs, agent="pm")], ["pm-a"])
        self.assertEqual([r["run_id"] for r in runs.select(all_runs, pr=456)], ["dev-a", "rev-a", "dev-b"])
        self.assertEqual([r["run_id"] for r in runs.select(all_runs, date="2026-10-22")], ["dev-b", "dev-c"])
        self.assertEqual(runs.select(all_runs, agent="deploy"), [])

    def test_no_runs(self):
        self.assertEqual(self.cli("--agent", "deploy").stdout.strip(), "no runs")

    def test_main_prints_json(self):
        buf = io.StringIO()
        with redirect_stdout(buf):
            self.assertEqual(runs.main(["--file", self.path, "--agent", "pm", "--json"]), 0)
        self.assertEqual(json.loads(buf.getvalue())["outcome"], "triaged")


class LibShTest(unittest.TestCase):
    """lib.sh's run_begin / run_end, sourced for real in a throwaway state dir."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        t = self.tmp.name
        self.ledger = os.path.join(t, "envs", "history", "agent_runs.jsonl")
        self.env = dict(os.environ, AGENT_STATE_DIR=os.path.join(t, "state"), OPTIONSBOT_ENVS=os.path.join(t, "envs"),
                        AGENT_WORK_DIR=os.path.join(t, "wt"), AGENT_NOTIFY="0")
        for k in ("AGENT_RUNS_FILE", "MONITOR_URL", "MVP_MONITOR_URL", "MONITOR_TOKEN", "MVP_MONITOR_TOKEN"):
            self.env.pop(k, None)

    def tearDown(self):
        self.tmp.cleanup()

    def bash(self, script, env=None):
        cmd = f'source "{HERE}/lib.sh"\n{script}'
        return subprocess.Popen(["bash", "-c", cmd], env=env or self.env, stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT, text=True)

    def run_bash(self, script, env=None):
        p = self.bash(script, env)
        out, _ = p.communicate(timeout=60)
        return p.returncode, out

    def lines(self):
        with open(self.ledger) as f:
            return [json.loads(l) for l in f]

    # Scenario: a Dev run is traced
    def test_dev_run_is_traced(self):
        rc, out = self.run_bash('set -euo pipefail\n'
                                'run_begin dev 123 "" "$AGENT_REPO_DIR/data/agents/dev/issue-123-x"\n'
                                'RUN_PR=456\n'
                                'run_end pr_opened "checks passed"')
        self.assertEqual(rc, 0, out)
        start, end = self.lines()
        self.assertEqual(start["run_id"], end["run_id"])
        self.assertEqual((start["event"], end["event"]), ("start", "end"))
        for r in (start, end):
            self.assertEqual((r["agent"], r["issue"], r["log"]), ("dev", 123, "data/agents/dev/issue-123-x"))
        self.assertEqual((end["pr"], end["outcome"], end["detail"]), (456, "pr_opened", "checks passed"))
        self.assertRegex(start["commit"] or "", r"^[0-9a-f]{4,}$")  # the agents' own code

    # Scenario: a crash still ends the run
    def test_crash_ends_the_run_failed(self):
        rc, out = self.run_bash('set -euo pipefail\nrun_begin dev 5\nfalse\necho never')
        self.assertEqual(rc, 1, out)
        self.assertNotIn("never", out)
        end = self.lines()[-1]
        self.assertEqual((end["event"], end["outcome"], end["detail"], end["issue"]), ("end", "failed", "exit 1", 5))

    def test_crash_with_the_script_own_exit_trap(self):
        # review_agent.sh replaces the trap and passes the exit code it saw.
        rc, out = self.run_bash('run_begin review 8 9\n'
                                'trap \'_rc=$?; true; _on_exit "$_rc"\' EXIT\n'
                                'exit 3')
        self.assertEqual(rc, 3, out)
        end = self.lines()[-1]
        self.assertEqual((end["outcome"], end["detail"], end["pr"]), ("failed", "exit 3", 9))

    def test_exit_without_an_outcome_is_done(self):
        rc, out = self.run_bash('run_begin sme\nexit 0')
        self.assertEqual(rc, 0, out)
        self.assertEqual([(l["event"], l["outcome"]) for l in self.lines()], [("start", None), ("end", "done")])

    def test_one_run_at_a_time_and_end_is_idempotent(self):
        rc, out = self.run_bash('run_begin pm\nrun_begin pm\nrun_end triaged\nrun_end triaged\n'
                                'run_begin pr_gate 1 2\nrun_end ready_to_merge')
        self.assertEqual(rc, 0, out)
        got = [(l["agent"], l["event"]) for l in self.lines()]
        self.assertEqual(got, [("pm", "start"), ("pm", "end"), ("pr_gate", "start"), ("pr_gate", "end")])
        self.assertNotEqual(self.lines()[0]["run_id"], self.lines()[2]["run_id"])

    def test_the_ledger_never_stops_an_agent(self):
        env = dict(self.env, AGENT_RUNS_FILE="/dev/null/nowhere/agent_runs.jsonl")
        rc, out = self.run_bash('set -euo pipefail\nrun_begin dev 1\nrun_end pr_opened\necho still-here', env)
        self.assertEqual(rc, 0, out)
        self.assertIn("still-here", out)
        rc, out = self.run_bash('set -euo pipefail\nrun_begin nobody\necho still-here')  # a refused line
        self.assertEqual(rc, 0, out)
        self.assertIn("run ledger: line refused", out)

    # Scenario: parallel runs — 3 Dev and 3 Reviewer runs finishing together.
    def test_parallel_runs_never_interleave(self):
        long = "y" * 190
        procs = []
        for i in range(6):
            agent = "dev" if i < 3 else "review"
            procs.append(self.bash(f'set -euo pipefail\nfor k in 1 2 3; do run_begin {agent} {100 + i} {200 + i}; '
                                   f'run_end done "{long}"; done'))
        for p in procs:
            out, _ = p.communicate(timeout=120)
            self.assertEqual(p.returncode, 0, out)
        with open(self.ledger) as f:
            raw = f.read().splitlines()
        self.assertEqual(len(raw), 6 * 3 * 2)
        recs = [json.loads(l) for l in raw]  # every line is valid JSON
        by_run = {}
        for r in recs:
            by_run.setdefault(r["run_id"], []).append(r["event"])
        self.assertEqual(len(by_run), 18)
        self.assertTrue(all(sorted(ev) == ["end", "start"] for ev in by_run.values()))
        self.assertEqual({r["agent"] for r in recs}, {"dev", "review"})


class ScriptsTest(unittest.TestCase):
    """Every agent the ticket names opens a run in the ledger."""

    def test_each_agent_script_begins_a_run(self):
        for script, agent in (("sme_review.sh", "sme"), ("pm_agent.sh", "pm"), ("pm_answer.sh", "pm_answer"),
                              ("dev_agent.sh", "dev"), ("review_agent.sh", "review"), ("pr_gate.sh", "pr_gate"),
                              ("deploy.sh", "deploy"), ("backup_data.sh", "backup")):
            with open(os.path.join(HERE, script)) as f:
                text = f.read()
            self.assertRegex(text, r"run_begin " + agent + r"\b", script)
            self.assertIn("run_end ", text, script)

    def test_dev_agent_records_pr_opened(self):
        with open(os.path.join(HERE, "dev_agent.sh")) as f:
            text = f.read()
        self.assertIn("run_end pr_opened", text)
        self.assertIn("run_end skipped_limit", text)
        self.assertTrue(re.search(r'RUN_PR="\$\{pr_url##\*/\}"', text))


if __name__ == "__main__":
    unittest.main()
