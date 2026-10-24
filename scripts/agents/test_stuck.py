"""Tests of stuck.py and stuck.sh (#329): python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import datetime as dt
import json
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import stuck  # noqa: E402

NOW = dt.datetime(2026, 10, 24, 20, 0, 0, tzinfo=dt.timezone.utc)


def at(hours_ago, now=NOW):
    return (now - dt.timedelta(hours=hours_ago)).strftime("%Y-%m-%dT%H:%M:%SZ")


def card(number, status, updated_h, labels=(), state="OPEN"):
    return {"item": f"I{number}", "number": number, "title": "t", "state": state, "status": status,
            "labels": list(labels), "updatedAt": at(updated_h), "priority": None, "parent": None}


def run(issue, start_h, end_h=None, agent="dev", outcome="pr_opened", run_id=None):
    return {"run_id": run_id or f"{agent}-{issue}-{start_h}", "agent": agent, "issue": issue, "pr": None,
            "start": at(start_h), "end": at(end_h) if end_h is not None else None,
            "outcome": outcome if end_h is not None else None, "detail": None,
            "log": f"data/agents/{agent}/issue-{issue}-x", "commit": None}


class StuckRules(unittest.TestCase):
    # Scenario: a stalled card is reported — it names the card, the idle hours and the last run
    def test_a_stalled_card_is_reported(self):
        got = stuck.stuck([card(12, "In progress", 8)], [run(12, 10, 9)], NOW)
        self.assertEqual(len(got), 1)
        it = got[0]
        self.assertEqual((it["issue"], it["column"], it["hours_idle"], it["reason"], it["fault"]),
                         (12, "In progress", 8.0, "no agent run", True))
        self.assertEqual(it["last_run"], f"dev pr_opened {at(9)}")
        body = stuck.comment(it)
        self.assertTrue(body.startswith("🤖 **Stuck card** — #12 has sat in **In progress** for **8.0 h**"))
        self.assertIn(f"Last run on it: dev pr_opened {at(9)} — log `data/agents/dev/issue-12-x`", body)
        self.assertIn(f"<!-- agent:stuck key={it['key']} -->", body)

    def test_idle_since_the_newest_activity(self):
        # the newest run (ended 2 h ago) is newer than the card's update: not idle long enough
        self.assertEqual(stuck.stuck([card(12, "In progress", 8)], [run(12, 3, 2)], NOW), [])

    def test_thresholds_per_column(self):
        board = [card(1, "In progress", 5), card(2, "In progress", 7), card(3, "In review", 20),
                 card(4, "In review", 30), card(5, "Ready", 100), card(6, "Blocked", 100)]
        self.assertEqual([it["issue"] for it in stuck.stuck(board, [], NOW)], [2, 4])
        self.assertEqual(stuck.stuck(board, [], NOW)[0]["last_run"], "none")

    def test_an_open_run_means_it_is_worked_on(self):
        self.assertEqual(stuck.stuck([card(12, "In progress", 8)], [run(12, 9)], NOW), [])

    # Scenario: cards waiting for a person are left alone
    def test_cards_waiting_for_a_person_are_left_alone(self):
        board = [card(1, "In progress", 30, ["agent:needs-human"]), card(2, "In review", 30, ["ready-to-merge"]),
                 card(3, "In progress", 30, ["pm:waiting"]), card(4, "In review", 30, state="CLOSED")]
        self.assertEqual(stuck.stuck(board, [], NOW), [])

    # Scenario: a budget pause explains the idle time
    def test_a_budget_pause_explains_the_idle_time(self):
        it = stuck.stuck([card(12, "In progress", 8)], [], NOW, budget="paused (AGENTS_PAUSED=1)")[0]
        self.assertEqual((it["reason"], it["fault"]), ("paused", False))
        it = stuck.stuck([card(12, "In progress", 8)], [], NOW, budget="outside agent hours 20-8")[0]
        self.assertEqual((it["reason"], it["fault"]), ("budget: outside agent hours 20-8", False))

    def test_a_recent_limit_explains_the_idle_time(self):
        it = stuck.stuck([card(12, "In progress", 8)], [], NOW, limit_at=NOW - dt.timedelta(hours=2))[0]
        self.assertEqual((it["reason"], it["fault"]), ("usage limit 2.0 h ago", False))
        it = stuck.stuck([card(12, "In progress", 8)], [], NOW, limit_at=NOW - dt.timedelta(hours=7))[0]
        self.assertTrue(it["fault"])


class Check(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.state = self.tmp.name

    def tearDown(self):
        self.tmp.cleanup()

    def test_check_records_and_dedups(self):
        board = [card(12, "In progress", 8)]
        self.assertTrue(stuck.due(self.state, NOW))
        got = stuck.check(board, [], self.state, NOW)
        self.assertEqual([it["issue"] for it in got], [12])
        # a second check within 6 h adds nothing
        self.assertFalse(stuck.due(self.state, NOW + dt.timedelta(hours=5)))
        self.assertTrue(stuck.due(self.state, NOW + dt.timedelta(hours=6)))
        open(os.path.join(self.state, "marks", got[0]["key"]), "w").close()  # stuck.sh, after its comment
        self.assertEqual(stuck.check(board, [], self.state, NOW + dt.timedelta(hours=6)), [])
        with open(os.path.join(self.state, "reports.jsonl")) as f:
            recs = [json.loads(l) for l in f]
        self.assertEqual([(r["issue"], r["fault"]) for r in recs], [(12, True), (12, True)])

    def test_explained_cards_are_listed_not_commented(self):
        got = stuck.check([card(12, "In progress", 8)], [], self.state, NOW, budget="paused (AGENTS_PAUSED=1)")
        self.assertEqual(got, [])
        with open(os.path.join(self.state, "reports.jsonl")) as f:
            self.assertEqual(json.loads(f.readline())["reason"], "paused")


GH_STUB = r'''#!{python}
import json, sys
a = sys.argv[1:]
if a[:2] == ["repo", "view"]:
    print("o/r")
elif a[:2] == ["api", "graphql"]:
    sys.stdout.write(open({board!r}).read())
elif a[:2] == ["issue", "comment"]:
    body = open(a[a.index("--body-file") + 1]).read()
    with open({log!r}, "a") as f:
        f.write(json.dumps({{"issue": a[2], "body": body}}) + "\n")
else:
    sys.exit("gh stub: unexpected " + " ".join(a))
'''


class StuckScript(unittest.TestCase):
    """stuck.sh end to end with a stub gh: the comment, the 6 h gate, the marker, the pause."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        t = self.tmp.name
        self.state = os.path.join(t, "state")
        envs = os.path.join(t, "envs")
        os.makedirs(os.path.join(envs, "history"))
        os.makedirs(self.state)
        self.ledger = os.path.join(envs, "history", "agent_runs.jsonl")
        self.comments = os.path.join(t, "comments.jsonl")
        board_file = os.path.join(t, "graphql.json")
        now = dt.datetime.now(dt.timezone.utc)
        node = {"id": "N12", "number": 12, "title": "t", "state": "OPEN", "updatedAt": at(8, now),
                "labels": {"nodes": []}, "parent": None,
                "projectItems": {"nodes": [{"id": "I12", "project": {"number": 7}, "status": {"name": "In progress"},
                                            "priority": {"name": "P2"}}]}}
        with open(board_file, "w") as f:
            json.dump({"data": {"repository": {"issues": {"nodes": [node]}}}}, f)
        with open(self.ledger, "w") as f:
            for ev, h in (("start", 10), ("end", 9)):
                f.write(json.dumps({"ts": at(h, now), "agent": "dev", "run_id": "dev-20261024T100000Z-1-1",
                                    "issue": 12, "pr": None, "event": ev, "outcome": "pr_opened" if ev == "end" else None,
                                    "detail": None, "log": "data/agents/dev/issue-12-x", "commit": None}) + "\n")
        with open(os.path.join(self.state, "project.json"), "w") as f:
            json.dump({"number": 7, "id": "P", "status_field": "S", "options": {}, "priority_field": "F",
                       "priority_options": {}}, f)
        bin_dir = os.path.join(t, "bin")
        os.makedirs(bin_dir)
        gh = os.path.join(bin_dir, "gh")
        with open(gh, "w") as f:
            f.write(GH_STUB.format(python=sys.executable, board=board_file, log=self.comments))
        os.chmod(gh, 0o755)
        self.env = dict(os.environ, AGENT_STATE_DIR=self.state, OPTIONSBOT_ENVS=envs,
                        AGENT_WORK_DIR=os.path.join(t, "wt"), AGENT_NOTIFY="0",
                        PATH=bin_dir + os.pathsep + os.environ.get("PATH", ""))
        for k in ("AGENT_RUNS_FILE", "MONITOR_URL", "MVP_MONITOR_URL", "MONITOR_TOKEN", "MVP_MONITOR_TOKEN",
                  "AGENTS_PAUSED", "AGENT_HOURS", "AGENT_MAX_RUNS_PER_DAY", "AGENTS_ENV_FILE", "BUDGET_FILE"):
            self.env.pop(k, None)

    def tearDown(self):
        self.tmp.cleanup()

    def run_sh(self, **extra):
        p = subprocess.run(["bash", os.path.join(HERE, "stuck.sh")], env=dict(self.env, **extra),
                           capture_output=True, text=True, timeout=60)
        self.assertEqual(p.returncode, 0, p.stdout + p.stderr)
        return p.stdout

    def posted(self):
        if not os.path.exists(self.comments):
            return []
        with open(self.comments) as f:
            return [json.loads(l) for l in f]

    # Scenario: a stalled card is reported, and a second check within 6 h adds nothing
    def test_a_stalled_card_is_reported_once(self):
        out = self.run_sh()
        posted = self.posted()
        self.assertEqual(len(posted), 1, out)
        self.assertEqual(posted[0]["issue"], "12")
        self.assertIn("#12 has sat in **In progress** for **8.0 h**", posted[0]["body"])
        self.assertIn("dev pr_opened", posted[0]["body"])
        self.run_sh()  # within 6 h: the gate
        self.assertEqual(len(self.posted()), 1)
        os.remove(os.path.join(self.state, "stuck", "last_check"))
        self.run_sh()  # past the gate: the marker
        self.assertEqual(len(self.posted()), 1)

    # Scenario: a budget pause explains the idle time — listed, not commented on
    def test_paused(self):
        self.run_sh(AGENTS_PAUSED="1")
        self.assertEqual(self.posted(), [])
        with open(os.path.join(self.state, "stuck", "reports.jsonl")) as f:
            rec = json.loads(f.readline())
        self.assertEqual((rec["issue"], rec["reason"], rec["fault"]), (12, "paused", False))


class Wiring(unittest.TestCase):
    def test_tick_runs_the_sweep_and_the_stuck_check_before_the_agents(self):
        with open(os.path.join(HERE, "tick.sh")) as f:
            text = f.read()
        sweep, check, agents = text.index("sweep_runs.sh\""), text.index("stuck.sh\""), text.index("start dev_agent.sh")
        self.assertLess(sweep, agents)
        self.assertLess(check, agents)


if __name__ == "__main__":
    unittest.main()
