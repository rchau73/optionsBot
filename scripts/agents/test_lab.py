"""Tests of the lab step of the agents' tick (lab.sh, #876): the rules in
lab_queue.py, and the script end to end on a throwaway mvp clone — with the
real cmd/labrun (built once) and a fake cmd/compare, or a fake runner — and a
fake gh. No model call: ANTHROPIC_API_KEY is removed from the environment.
python3 -m unittest discover -s scripts/agents -p 'test_lab*.py'"""
import datetime
import json
import os
import shutil
import subprocess
import tempfile
import unittest

import lab_queue

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(os.path.dirname(HERE))
NOW = datetime.datetime(2026, 11, 22, 12, 0, tzinfo=datetime.timezone.utc)

# gh: `repo view` names the repo, `issue list` prints $FAKE_GH_OPEN (the open
# titles), `issue create` copies its title and body to $FAKE_GH_CREATED. Every
# call is logged; anything else (the board) fails.
FAKE_GH = """#!/usr/bin/env bash
echo "$*" >> "$FAKE_GH_LOG"
case "$1 $2" in
  "repo view") echo owner/repo ;;
  "issue list") cat "$FAKE_GH_OPEN" 2>/dev/null || true ;;
  "issue create")
    while [[ $# -gt 0 ]]; do
      [[ "$1" == "--title" ]] && echo "TITLE $2" >> "$FAKE_GH_CREATED"
      [[ "$1" == "--body-file" ]] && cat "$2" >> "$FAKE_GH_CREATED"
      shift
    done
    echo "https://github.com/owner/repo/issues/901" ;;
  *) exit 1 ;;
esac
"""
# A runner that only says it ran.
FAKE_RUNNER = '#!/usr/bin/env bash\necho "runner in $PWD" >> "$FAKE_RUNNER_LOG"\n'
# cmd/compare: notes the request's status while it runs, then names its report.
FAKE_COMPARE = """#!/usr/bin/env bash
grep -ho '"status": "[a-z]*"' "$PWD"/data/compare/requests/*.json >> "$FAKE_COMPARE_LOG"
echo "data/compare/20261122T000000Z/report.md, data/compare/20261122T000000Z/report.json"
"""
REAL_RUNNER = """#!/usr/bin/env bash
exec "$LABRUN_BIN" -queue "$PWD/data/compare/requests" -out "$PWD/data/compare" -base "$PWD/config_{u}.yaml" \\
  -replay none -stress none -compare-cmd "$FAKE_COMPARE"
"""


def request(rid, status="queued", requested=NOW, value=1.2):
    t = requested.strftime("%Y-%m-%dT%H:%M:%S.000Z")
    return {"id": rid, "requester": "mgr", "requested_at": t, "underlying": "BTC",
            "settings": [{"key": "gamma_flip_buffer_sd", "value": value}], "status": status, "updated_at": t}


ID1 = "20261122T090000Z-a1b2c3d4"
ID2 = "20261122T113000Z-a1b2c3d5"


class Rules(unittest.TestCase):
    def test_nothing_queued_is_no_action(self):
        reqs = [request(ID1, "done"), request(ID2, "failed")]
        self.assertEqual(lab_queue.action(reqs, False, NOW), "none")
        self.assertEqual(lab_queue.action([], False, NOW), "none")

    def test_a_queued_request_runs_unless_the_lock_is_held(self):
        reqs = [request(ID1)]
        self.assertEqual(lab_queue.action(reqs, False, NOW), "run")
        self.assertEqual(lab_queue.action(reqs, True, NOW), "busy")

    def test_a_running_request_with_the_lock_free_is_stalled_and_runs(self):
        reqs = [request(ID1, "running", NOW - datetime.timedelta(minutes=10))]
        self.assertEqual(lab_queue.stalled(reqs, False, NOW), [(ID1, "running", 10)])
        self.assertEqual(lab_queue.stalled(reqs, True, NOW), [])
        self.assertEqual(lab_queue.action(reqs, False, NOW), "run")

    def test_queued_over_two_hours_is_stalled_not_at_two_hours(self):
        old = request(ID1, requested=NOW - datetime.timedelta(minutes=121))
        edge = request(ID2, requested=NOW - datetime.timedelta(minutes=120))
        self.assertEqual(lab_queue.stalled([old, edge], True, NOW), [(ID1, "queued", 121)])

    def test_a_request_without_a_time_is_not_stalled(self):
        r = request(ID1)
        r["requested_at"] = "not a time"
        self.assertEqual(lab_queue.stalled([r], False, NOW), [])

    def test_the_issue_names_ids_and_ages_only(self):
        self.assertIsNone(lab_queue.issue([]))
        title, body = lab_queue.issue([(ID1, "queued", 190), (ID2, "running", 30)])
        self.assertEqual(title, f"[E9] Lab queue stalled: {ID1} queued 3.2 h")
        self.assertIn(f"- `{ID1}`: queued, 3.2 h since requested — queued over 2 h", body)
        self.assertIn(f"- `{ID2}`: running, 30 min since requested — left running", body)
        self.assertIn("<!-- agent:lab-stalled -->", body)
        for word in ("mgr", "gamma_flip_buffer_sd", "1.2", "$"):
            self.assertNotIn(word, body)

    def test_started_counts_the_ids_no_longer_queued(self):
        reqs = [request(ID1, "done"), request(ID2)]
        self.assertEqual(lab_queue.started([ID1, ID2, "gone"], reqs), [ID1])

    def test_count_reads_the_queue_and_window(self):
        d = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, d, True)
        log = os.path.join(d, "started.jsonl")
        with open(log, "w") as f:
            for days, queue in ((1, "/q"), (27, "/q"), (29, "/q"), (1, "/other")):
                ts = (NOW - datetime.timedelta(days=days)).strftime("%Y-%m-%dT%H:%M:%SZ")
                f.write(json.dumps({"ts": ts, "queue": queue, "id": ID1}) + "\n")
            f.write("{torn\n")
        self.assertEqual(lab_queue.count(log, "/q", 28, NOW), 2)
        self.assertEqual(lab_queue.count(os.path.join(d, "missing"), "/q", 28, NOW), 0)

    def test_unreadable_files_and_other_names_are_skipped(self):
        d = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, d, True)
        for name, text in ((ID1 + ".json", json.dumps(request(ID1))), (ID2 + ".json", "{torn"),
                           ("notes.json", json.dumps(request("notes"))),
                           ("20261122T120000Z-a1b2c3d6.json", json.dumps(request(ID1)))):  # id ≠ name
            with open(os.path.join(d, name), "w") as f:
                f.write(text)
        self.assertEqual([r["id"] for r in lab_queue.read_requests(d)], [ID1])
        self.assertEqual(lab_queue.read_requests(os.path.join(d, "missing")), [])

    def test_a_lock_older_than_a_day_is_not_held(self):
        d = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, d, True)
        now = datetime.datetime.now(datetime.timezone.utc)
        self.assertFalse(lab_queue.lock_held(d, now))
        lock = os.path.join(d, lab_queue.RUNNER_LOCK)
        os.mkdir(lock)
        self.assertTrue(lab_queue.lock_held(d, now))
        old = (now - datetime.timedelta(hours=25)).timestamp()
        os.utime(lock, (old, old))
        self.assertFalse(lab_queue.lock_held(d, now))


class Tick(unittest.TestCase):
    def test_the_tick_starts_the_lab_step_in_the_background(self):
        """tick.sh starts lab.sh through start() (detached, its own log), so a long run never delays the tick."""
        with open(os.path.join(HERE, "tick.sh")) as f:
            code = [l.split("#")[0].strip() for l in f]
        self.assertIn("start lab.sh 1", code)
        self.assertNotIn("bash \"$dir/lab.sh\"", "\n".join(code))


class Script(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.bin_dir = tempfile.mkdtemp()
        cls.labrun = os.path.join(cls.bin_dir, "labrun")
        subprocess.run(["go", "build", "-o", cls.labrun, "./cmd/labrun"], cwd=REPO, check=True)
        cls.fake_bin = os.path.join(cls.bin_dir, "fake")
        os.makedirs(cls.fake_bin)
        for name, text in (("gh", FAKE_GH), ("runner", FAKE_RUNNER), ("compare", FAKE_COMPARE),
                           ("real_runner", REAL_RUNNER)):
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
        self.clone = os.path.join(self.envs, "mvp")
        self.queue = os.path.join(self.clone, "data", "compare", "requests")
        self.state = os.path.join(self.tmp, "state")
        self.ledger = os.path.join(self.tmp, "agent_runs.jsonl")
        self.logs = {k: os.path.join(self.tmp, k + ".log") for k in ("gh", "created", "runner", "compare")}
        self.open_titles = os.path.join(self.tmp, "open.txt")
        os.makedirs(self.clone)
        with open(os.path.join(self.clone, "config_btc.yaml"), "w") as f:
            f.write("# the shipped config: cmd/compare is faked\n")

    def add(self, r):
        os.makedirs(self.queue, exist_ok=True)
        with open(os.path.join(self.queue, r["id"] + ".json"), "w") as f:
            json.dump(r, f)

    def status(self, rid):
        with open(os.path.join(self.queue, rid + ".json")) as f:
            return json.load(f)

    def run_lab(self, runner="runner"):
        env = {k: v for k, v in os.environ.items()
               if k not in ("MONITOR_URL", "MVP_MONITOR_URL", "MONITOR_TOKEN", "MVP_MONITOR_TOKEN", "ANTHROPIC_API_KEY",
                            "LAB_ENV")}
        env.update(OPTIONSBOT_ENVS=self.envs, AGENT_STATE_DIR=self.state, AGENT_RUNS_FILE=self.ledger,
                   AGENT_NOTIFY="0", LAB_RUNNER=os.path.join(self.fake_bin, runner), LABRUN_BIN=self.labrun,
                   FAKE_COMPARE=os.path.join(self.fake_bin, "compare"),
                   PATH=self.fake_bin + os.pathsep + os.environ["PATH"],
                   FAKE_GH_LOG=self.logs["gh"], FAKE_GH_CREATED=self.logs["created"], FAKE_GH_OPEN=self.open_titles,
                   FAKE_RUNNER_LOG=self.logs["runner"], FAKE_COMPARE_LOG=self.logs["compare"])
        p = subprocess.run(["bash", os.path.join(HERE, "lab.sh")], env=env, capture_output=True, text=True, timeout=120)
        self.output = p.stdout + p.stderr
        return p.returncode

    def lines(self, name):
        if not os.path.exists(self.logs[name]):
            return []
        with open(self.logs[name]) as f:
            return f.read().splitlines()

    def ledger_lines(self):
        if not os.path.exists(self.ledger):
            return []
        with open(self.ledger) as f:
            return [json.loads(l) for l in f if l.strip()]

    def test_a_queued_request_is_run_to_done_with_a_run_id(self):
        """Scenario: a queued request and the runner idle → running, then done with a run id."""
        self.add(request(ID1, requested=datetime.datetime.now(datetime.timezone.utc)))
        self.assertEqual(self.run_lab("real_runner"), 0, self.output)
        self.assertEqual(self.lines("compare"), ['"status": "running"'], self.output)
        got = self.status(ID1)
        self.assertEqual((got["status"], got["run_id"]), ("done", "20261122T000000Z"), self.output)
        self.assertIn("analysis not written", got.get("note", ""))  # no key: no model call, a note
        self.assertIn("runner pass done, 1 request(s) taken", self.output)
        runs = self.ledger_lines()
        self.assertEqual([(r["agent"], r["event"]) for r in runs], [("lab", "start"), ("lab", "end")])
        self.assertEqual((runs[1]["outcome"], runs[1]["detail"]), ("ran", "1 request(s) taken"))
        with open(os.path.join(self.state, "lab", "started.jsonl")) as f:
            started = [json.loads(l) for l in f]
        self.assertEqual([(s["id"], s["queue"]) for s in started], [(ID1, self.queue)])
        self.assertEqual(self.lines("gh"), [])

    def test_a_held_lock_starts_no_second_run_and_logs_it(self):
        """Scenario: the runner's lock is held → no second run, a log line."""
        self.add(request(ID1, requested=datetime.datetime.now(datetime.timezone.utc)))
        os.mkdir(os.path.join(self.queue, ".runner.lock"))
        self.assertEqual(self.run_lab(), 0, self.output)
        self.assertIn("the runner's lock is held (a run is in progress), skipped", self.output)
        self.assertEqual(self.lines("runner"), [])
        self.assertEqual(self.ledger_lines(), [])

    def test_no_queued_request_starts_nothing(self):
        """Scenario: no queued request → nothing starts, no model call."""
        self.add(request(ID1, "done"))
        self.assertEqual(self.run_lab(), 0, self.output)
        self.assertEqual(self.lines("runner"), [])
        self.assertEqual(self.ledger_lines(), [])
        self.assertEqual(self.output, "")

    def test_a_clone_without_a_lab_queue_is_skipped_silently(self):
        """Scenario: no data/compare/requests folder → the lab step is skipped silently."""
        self.assertEqual(self.run_lab(), 0, self.output)
        self.assertEqual(self.output, "")
        self.assertEqual(self.lines("runner"), [])
        self.assertFalse(os.path.exists(os.path.join(self.state, "lab")))

    def test_a_stalled_queue_files_one_bug_and_none_while_it_is_open(self):
        """Scenario: a request queued over 2 hours → one Triage bug with its id and age, none while one is open."""
        self.add(request(ID1, requested=datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(hours=3)))
        os.mkdir(os.path.join(self.queue, ".runner.lock"))  # a long run holds it
        self.assertEqual(self.run_lab(), 0, self.output)
        creates = [c for c in self.lines("gh") if c.startswith("issue create")]
        self.assertEqual(len(creates), 1, self.output)
        self.assertIn("--label bug --label E9", creates[0])
        created = "\n".join(self.lines("created"))
        self.assertIn(f"TITLE [E9] Lab queue stalled: {ID1} queued 3.0 h", created)
        self.assertIn(f"`{ID1}`: queued, 3.0 h since requested", created)
        for word in ("mgr", "gamma_flip_buffer_sd", "1.2"):
            self.assertNotIn(word, created)
        self.assertIn("filed but not put on the board", self.output)  # the fake gh has no board

        with open(self.open_titles, "w") as f:
            f.write(f"[E9] Lab queue stalled: {ID1} queued 3.0 h\n")
        self.assertEqual(self.run_lab(), 0, self.output)
        self.assertEqual(len([c for c in self.lines("gh") if c.startswith("issue create")]), 1)
        self.assertIn("already open, none filed", self.output)

    def test_a_running_request_the_runner_left_is_filed_and_cleared(self):
        """Scenario: a stale running request → one bug, then the runner marks it failed."""
        self.add(request(ID1, "running", datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(minutes=20)))
        self.assertEqual(self.run_lab("real_runner"), 0, self.output)
        self.assertIn(f"TITLE [E9] Lab queue stalled: {ID1} running 20 min", "\n".join(self.lines("created")))
        got = self.status(ID1)
        self.assertEqual(got["status"], "failed", self.output)
        self.assertEqual(self.lines("compare"), [])

    def test_a_failing_runner_ends_the_run_failed(self):
        self.add(request(ID1, requested=datetime.datetime.now(datetime.timezone.utc)))
        broken = os.path.join(self.tmp, "broken")
        with open(broken, "w") as f:
            f.write("#!/usr/bin/env bash\necho boom >&2\nexit 3\n")
        os.chmod(broken, 0o755)
        self.assertEqual(self.run_lab(broken), 0, self.output)
        end = self.ledger_lines()[-1]
        self.assertEqual((end["outcome"], end["detail"]), ("failed", "runner exit 3; 0 request(s) taken"))


if __name__ == "__main__":
    unittest.main()
