"""Tests of the nightly backup (backup_data.sh, #328), end to end on throwaway
environments: python3 -m unittest discover -s scripts/agents -p 'test_*.py'

The restore check itself (internal/backup) is tested in tests/backup_check_test.go;
here it runs as the real cmd/backupcheck, built once."""
import datetime
import hashlib
import json
import os
import shutil
import subprocess
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(os.path.dirname(HERE))
SECRET = "s3cr3t-client-id-value"
LINE = '{"timestamp":"2026-10-23T00:00:00Z","event":"skip","detail":"x"}\n'
# A chained audit trail as the monitor writes it (#619), shared with
# tests/backup_check_test.go and frontend __tests__/audit-chain.test.js.
AUDIT_FIXTURE = os.path.join(REPO, "tests", "testdata", "audit_chain")
AUDIT_ARCHIVE = "audit.20261104T031433123Z.jsonl"
# The monitor files the backup may copy (#650): a change to it is a change to this list.
MONITOR_ALLOW_LIST = ["assistant.jsonl", "audit.jsonl", AUDIT_ARCHIVE, "auth.db", "events.jsonl",
                      "requests.jsonl", "requests_status.jsonl"]

# A fake docker on PATH (#650): `compose … ps -q --status running monitor`
# prints a container id while <env dir>/.fake-volume exists (the monitor-auth
# volume, the cwd being the environment's clone), and `compose … cp
# monitor:/app/data/monitor/. <dest>` copies it out. Every call is logged.
FAKE_DOCKER = """#!/usr/bin/env bash
echo "$*" >> "$FAKE_DOCKER_LOG"
vol="$PWD/.fake-volume"
for a in "$@"; do
  case "$a" in
    ps) [[ -d "$vol" ]] && echo 0f1e2d3c; exit 0 ;;
    cp) [[ -d "$vol" ]] || { echo "service monitor is not running" >&2; exit 1; }
        cp -R "$vol/." "${!#}"; exit 0 ;;
  esac
done
exit 1
"""
FAKE_OSASCRIPT = '#!/usr/bin/env bash\necho "$*" >> "$FAKE_NOTIFY_LOG"\n'


def today():
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%d")


class BackupTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.bin_dir = tempfile.mkdtemp()
        cls.check = os.path.join(cls.bin_dir, "backupcheck")
        subprocess.run(["go", "build", "-o", cls.check, "./cmd/backupcheck"], cwd=REPO, check=True)
        cls.fake_bin = os.path.join(cls.bin_dir, "fake")
        os.makedirs(cls.fake_bin)
        for name, text in (("docker", FAKE_DOCKER), ("osascript", FAKE_OSASCRIPT)):
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
        self.state = os.path.join(self.tmp, "state")
        self.ledger = os.path.join(self.tmp, "agent_runs.jsonl")
        self.backups = os.path.join(self.envs, "history", "backups")
        for env in ("stable", "mvp"):
            self.write(f"{env}/.env", f"DERIBIT_CLIENT_ID={SECRET}\nMONITOR_API_TOKEN={SECRET}\n")
            self.bot(env, "btc", 3)

    def write(self, rel, text):
        path = os.path.join(self.envs, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as f:
            f.write(text)

    def bot(self, env, bot, events):
        self.write(f"{env}/data/{bot}/orders.log", LINE * events)
        for name in ("pnl_history", "regime_history", "direction_history"):
            self.write(f"{env}/data/{bot}/data/{name}.jsonl", '{"t":1}\n')

    def audit_trail(self, rel):
        """The shared chained trail (an archive, then audit.jsonl) into envs/<rel>."""
        for name in (AUDIT_ARCHIVE, "audit.jsonl"):
            with open(os.path.join(AUDIT_FIXTURE, name)) as f:
                self.write(f"{rel}/{name}", f.read())

    def assertSameAsFixture(self, dst):
        for name in (AUDIT_ARCHIVE, "audit.jsonl"):
            with open(os.path.join(AUDIT_FIXTURE, name), "rb") as a, open(os.path.join(dst, name), "rb") as b:
                self.assertEqual(a.read(), b.read(), name)

    def deployed(self, env):
        """A deployed stack: deploy.env beside .env, and a running monitor whose volume is <env>/.fake-volume."""
        self.write(f"{env}/deploy.env", f"OPTIONSBOT_ENV={env}\nMONITOR_PORT=3100\n")
        os.makedirs(os.path.join(self.envs, env, ".fake-volume"), exist_ok=True)
        return f"{env}/.fake-volume"

    def docker_calls(self):
        if not os.path.exists(self.docker_log):
            return []
        with open(self.docker_log) as f:
            return f.read().splitlines()

    def run_backup(self, notify=False):
        env = {k: v for k, v in os.environ.items()
               if k not in ("MONITOR_URL", "MVP_MONITOR_URL", "MONITOR_TOKEN", "MVP_MONITOR_TOKEN")}
        self.docker_log = os.path.join(self.tmp, "docker.log")
        self.notify_log = os.path.join(self.tmp, "notify.log")
        env.update(OPTIONSBOT_ENVS=self.envs, AGENT_STATE_DIR=self.state, AGENT_RUNS_FILE=self.ledger,
                   AGENT_NOTIFY="1" if notify else "0", BACKUP_BOTS="btc eth", BACKUP_CHECK_BIN=self.check,
                   AGENTS_ENV_FILE=os.path.join(self.tmp, "agents.env"),
                   PATH=self.fake_bin + os.pathsep + os.environ["PATH"],
                   FAKE_DOCKER_LOG=self.docker_log, FAKE_NOTIFY_LOG=self.notify_log)
        p = subprocess.run(["bash", os.path.join(HERE, "backup_data.sh")], env=env,
                           capture_output=True, text=True, timeout=120)
        self.output = p.stdout + p.stderr
        return p.returncode

    def ledger_lines(self):
        if not os.path.exists(self.ledger):
            return []
        with open(self.ledger) as f:
            return [json.loads(l) for l in f if l.strip()]

    def dated(self):
        return sorted(d for d in os.listdir(self.backups) if len(d) == 10) if os.path.isdir(self.backups) else []

    def test_nightly_backup_is_made(self):
        """Scenario: A nightly backup is made."""
        self.assertEqual(self.run_backup(), 0, self.output)
        day = today()
        for env in ("stable", "mvp"):
            dst = os.path.join(self.backups, day, env, "btc")
            for f in ("orders.log", "pnl_history.jsonl", "regime_history.jsonl", "direction_history.jsonl", "check.json"):
                self.assertTrue(os.path.isfile(os.path.join(dst, f)), f"{env} {f}\n{self.output}")
            self.assertEqual(json.load(open(os.path.join(dst, "check.json")))["events"], 3)
        self.assertFalse(os.path.exists(os.path.join(self.backups, day, "stable", "eth")))  # no eth folder: skipped
        self.assertTrue(os.path.isfile(os.path.join(self.state, "backup", f"{day}.done")))
        ends = [r for r in self.ledger_lines() if r["event"] == "end"]
        self.assertEqual([(r["agent"], r["outcome"]) for r in ends], [("backup", "backed_up")])
        # A second tick the same day makes no new copy (and no ledger line).
        self.write("stable/data/btc/orders.log", LINE * 9)
        self.assertEqual(self.run_backup(), 0, self.output)
        self.assertEqual(json.load(open(os.path.join(self.backups, day, "stable", "btc", "check.json")))["events"], 3)
        self.assertEqual(len(self.ledger_lines()), 2)
        # The live files are untouched.
        with open(os.path.join(self.envs, "stable", "data", "btc", "orders.log")) as f:
            self.assertEqual(f.read(), LINE * 9)

    def test_archive_is_copied(self):
        """--reset-history's data/archive/ is part of the copy."""
        self.write("stable/data/btc/data/archive/20261001T000000Z/orders.log", LINE * 2)
        self.assertEqual(self.run_backup(), 0, self.output)
        check = json.load(open(os.path.join(self.backups, today(), "stable", "btc", "check.json")))
        self.assertEqual(check["archived_events"], 2)

    def test_monitor_request_queue_is_copied(self):
        """The assistant's private request queue (#409) and the account store (#650) are in the backup; nothing off the allow-list."""
        self.write("mvp/data/monitor/requests.jsonl", '{"id":"R-0001"}\n')
        self.write("mvp/data/monitor/requests_status.jsonl", '{"id":"R-0001","status":"filed"}\n')
        self.write("mvp/data/monitor/auth.db", "users")
        self.write("mvp/data/monitor/notes.txt", "not on the list")
        self.assertEqual(self.run_backup(), 0, self.output)
        dst = os.path.join(self.backups, today(), "mvp", "monitor")
        self.assertEqual(sorted(os.listdir(dst)), ["auth.db", "requests.jsonl", "requests_status.jsonl"])
        self.assertFalse(os.path.exists(os.path.join(self.backups, today(), "stable", "monitor")))

    def test_audit_trail_and_its_archives_are_copied(self):
        """#619: audit.jsonl and every rotated archive audit.<UTC time>.jsonl are in the backup, as they are."""
        self.audit_trail("mvp/data/monitor")
        self.assertEqual(self.run_backup(), 0, self.output)
        dst = os.path.join(self.backups, today(), "mvp", "monitor")
        self.assertEqual(sorted(os.listdir(dst)), [AUDIT_ARCHIVE, "audit.jsonl"])
        self.assertSameAsFixture(dst)
        self.assertIn("mvp audit chain intact — checked=4", self.output)
        self.assertFalse(os.path.exists(os.path.join(self.backups, today(), "stable", "monitor")))

    def test_scenario_the_assistant_log_is_backed_up(self):
        """#438: data/monitor/assistant.jsonl is copied to <date>/<env>/monitor/ and its lines checked."""
        self.write("mvp/data/monitor/assistant.jsonl", '{"kind":"answer","id":"a"}\n{"kind":"flag","id":"a"}\n')
        self.assertEqual(self.run_backup(), 0, self.output)
        dst = os.path.join(self.backups, today(), "mvp", "monitor")
        self.assertEqual(os.listdir(dst), ["assistant.jsonl"])
        self.assertIn("mvp assistant.jsonl ok — lines=2 skipped=0", self.output)

    def test_scenario_the_review_closures_are_backed_up_from_the_clone(self):
        """#704: closures.jsonl, a person's record in the clone, joins the env's monitor files and its lines are checked."""
        self.write("mvp/data/monitor/assistant_reviews/closures.jsonl", '{"answer_id":"a","how":"help_fix"}\n')
        self.write("mvp/data/monitor/assistant_reviews/2026-W44.md", "private question text\n")
        self.assertEqual(self.run_backup(), 0, self.output)
        dst = os.path.join(self.backups, today(), "mvp", "monitor")
        self.assertEqual(os.listdir(dst), ["closures.jsonl"])
        self.assertIn("mvp closures.jsonl ok — lines=1 skipped=0", self.output)

    def test_scenario_an_unparseable_assistant_log_fails_the_check(self):
        self.write("stable/data/monitor/assistant.jsonl", '{"kind":"answer","id":"a"}\nnot json\n{"kind":"flag","id":"a"}\n')
        self.assertNotEqual(self.run_backup(), 0, self.output)
        self.assertIn("stable monitor failed: assistant.jsonl check", self.output)
        self.assertTrue(os.path.isdir(os.path.join(self.backups, f"{today()}.failed")))

    def test_scenario_a_file_exists_only_in_the_volume(self):
        """#650: the monitor container holds audit.jsonl in its volume and the clone's data/monitor has none."""
        vol = self.deployed("mvp")
        # audit.jsonl alone (the fixture's links to its archive): a trail of one line.
        self.write(f"{vol}/audit.jsonl", self.chain_of_one())
        self.assertEqual(self.run_backup(), 0, self.output)
        dst = os.path.join(self.backups, today(), "mvp", "monitor")
        self.assertEqual(os.listdir(dst), ["audit.jsonl"])
        self.assertIn("mvp monitor files from the monitor-auth volume", self.output)
        self.assertIn("mvp audit chain intact — checked=1", self.output)
        self.assertFalse(os.path.exists(os.path.join(self.envs, "mvp", "data", "monitor")))
        ends = [r for r in self.ledger_lines() if r["event"] == "end"]
        self.assertEqual([r["outcome"] for r in ends], ["backed_up"])

    def chain_of_one(self):
        """A one-line trail sealed as frontend/lib/auth/auditchain.js sealLine does."""
        body = json.dumps({"time": "2026-11-05T00:00:00.000Z", "action": "signed_in", "prev": ""}, separators=(",", ":"))
        return body[:-1] + ',"line_hash":"' + hashlib.sha256(body.encode()).hexdigest() + '"}\n'

    def test_scenario_rotated_audit_archives_are_included(self):
        """#650: the volume holds audit.jsonl and audit.20261104T031433123Z.jsonl → both copied unchanged, the chain check passes."""
        vol = self.deployed("stable")
        self.audit_trail(vol)
        self.write("stable/data/monitor/audit.jsonl", '{"stale":"clone copy"}\n')  # the clone's folder is not read
        self.assertEqual(self.run_backup(), 0, self.output)
        dst = os.path.join(self.backups, today(), "stable", "monitor")
        self.assertEqual(sorted(os.listdir(dst)), [AUDIT_ARCHIVE, "audit.jsonl"])
        self.assertSameAsFixture(dst)
        self.assertIn("stable audit chain intact — checked=4", self.output)
        # Read-only on the volume: only `ps` and a copy out of the container, and the volume is unchanged.
        calls = self.docker_calls()
        self.assertTrue(calls, self.output)
        for call in calls:
            self.assertRegex(call, r"^compose --env-file \.env --env-file deploy\.env (ps -q --status running monitor|cp monitor:/app/data/monitor/\. \S+)$")
        self.assertSameAsFixture(os.path.join(self.envs, vol))
        self.assertFalse(os.path.exists(os.path.join(self.state, "backup", ".volume-stable")))  # the temporary copy is gone

    def test_scenario_a_secret_never_leaves_the_volume(self):
        """#650: a file of the volume that is not on the allow-list is not in the backup."""
        vol = self.deployed("mvp")
        self.audit_trail(vol)
        for name in ("auth.db", "assistant.jsonl", "events.jsonl", "requests.jsonl", "requests_status.jsonl"):
            self.write(f"{vol}/{name}", '{"ok":1}\n')
        for name in (".env", "session.key", "tokens.json", "auth.db-journal", "audit.old.jsonl", "secrets/x.jsonl"):
            self.write(f"{vol}/{name}", f"AUTH_SECRET={SECRET}\n")
        os.symlink(os.path.join(self.envs, "mvp", ".env"), os.path.join(self.envs, vol, "events.jsonl.link"))
        self.assertEqual(self.run_backup(), 0, self.output)
        dst = os.path.join(self.backups, today(), "mvp", "monitor")
        self.assertEqual(sorted(os.listdir(dst)), sorted(MONITOR_ALLOW_LIST))
        for root, _, files in os.walk(self.backups):
            for f in files:
                with open(os.path.join(root, f), errors="replace") as fh:
                    self.assertNotIn(SECRET, fh.read(), os.path.join(root, f))

    def test_scenario_a_damaged_audit_chain_fails_the_backup(self):
        """#650: the volume's audit.jsonl has an altered line → <date>.failed, a notification, earlier backups untouched."""
        good = os.path.join(self.backups, "2026-01-01", "mvp", "monitor")
        os.makedirs(good)
        with open(os.path.join(good, "audit.jsonl"), "w") as f:
            f.write("earlier\n")
        vol = self.deployed("mvp")
        self.audit_trail(vol)
        path = os.path.join(self.envs, vol, "audit.jsonl")
        with open(path) as f:
            text = f.read()
        with open(path, "w") as f:
            f.write(text.replace('"username":"bob"', '"username":"eve"'))
        self.assertNotEqual(self.run_backup(notify=True), 0, self.output)
        self.assertIn("mvp monitor failed: audit chain check", self.output)
        with open(os.path.join(self.state, "backup", f"{today()}.log")) as f:
            self.assertIn("audit.jsonl line 2: edited", f.read())
        self.assertTrue(os.path.isfile(os.path.join(self.backups, f"{today()}.failed", "mvp", "monitor", "audit.jsonl")))
        self.assertFalse(os.path.exists(os.path.join(self.backups, today())))
        with open(os.path.join(good, "audit.jsonl")) as f:
            self.assertEqual(f.read(), "earlier\n")
        with open(self.notify_log) as f:
            self.assertIn("optionsBot backup failed", f.read())
        ends = [r for r in self.ledger_lines() if r["event"] == "end"]
        self.assertEqual([r["outcome"] for r in ends], ["failed"])

    def test_scenario_the_monitor_container_is_down(self):
        """#650: no running monitor → the clone's data/monitor is used, the log names it, the backup does not fail for that."""
        self.deployed("mvp")
        os.rmdir(os.path.join(self.envs, "mvp", ".fake-volume"))  # stack set up, container not running
        self.audit_trail("mvp/data/monitor")
        self.assertEqual(self.run_backup(), 0, self.output)
        self.assertIn("mvp monitor not running, monitor files from the clone's", self.output)
        dst = os.path.join(self.backups, today(), "mvp", "monitor")
        self.assertSameAsFixture(dst)
        self.assertTrue(any(" ps " in f" {c} " for c in self.docker_calls()))
        self.assertFalse(any(" cp " in f" {c} " for c in self.docker_calls()))
        ends = [r for r in self.ledger_lines() if r["event"] == "end"]
        self.assertEqual([r["outcome"] for r in ends], ["backed_up"])

    def test_corrupt_journal_is_caught(self):
        """Scenario: A corrupt journal is caught."""
        good = os.path.join(self.backups, "2026-01-01", "stable", "btc")
        os.makedirs(good)
        with open(os.path.join(good, "orders.log"), "w") as f:
            f.write(LINE * 3)
        self.write("stable/data/btc/orders.log", LINE + "not json at all\n" + LINE)
        self.assertNotEqual(self.run_backup(), 0, self.output)
        self.assertIn("restore check failed", self.output)
        day = today()
        self.assertFalse(os.path.exists(os.path.join(self.backups, day)))
        self.assertTrue(os.path.isdir(os.path.join(self.backups, f"{day}.failed")))
        self.assertTrue(os.path.isfile(os.path.join(good, "orders.log")))  # the previous good backup is kept
        ends = [r for r in self.ledger_lines() if r["event"] == "end"]
        self.assertEqual([r["outcome"] for r in ends], ["failed"])
        # Not done yet: retried once on a later tick, then done whatever happened.
        self.assertFalse(os.path.exists(os.path.join(self.state, "backup", f"{day}.done")))
        self.assertNotEqual(self.run_backup(), 0, self.output)
        self.assertTrue(os.path.exists(os.path.join(self.state, "backup", f"{day}.done")))

    def test_fewer_events_than_the_day_before_fails(self):
        """Scenario: A corrupt journal is caught — the journal shrank since the previous backup."""
        prev = os.path.join(self.backups, "2026-01-01", "stable", "btc")
        os.makedirs(prev)
        with open(os.path.join(prev, "check.json"), "w") as f:
            json.dump({"events": 50, "archived_events": 0, "skipped": 0}, f)
        self.assertNotEqual(self.run_backup(), 0, self.output)
        self.assertIn("shrank", open(os.path.join(self.state, "backup", f"{today()}.log")).read())

    def test_vanished_bot_folder_fails(self):
        """A bot the previous good backup had, whose folder is now gone, fails the check."""
        os.makedirs(os.path.join(self.backups, "2026-01-01", "mvp", "eth"))
        self.assertNotEqual(self.run_backup(), 0, self.output)
        self.assertIn("mvp eth", open(os.path.join(self.state, "backup", f"{today()}.log")).read())

    def test_old_backups_are_pruned(self):
        """Scenario: Old backups are pruned — 15 daily backups → the newest 14 remain."""
        start = datetime.date(2026, 1, 1)
        for i in range(14):
            os.makedirs(os.path.join(self.backups, (start + datetime.timedelta(days=i)).isoformat()))
        self.assertEqual(self.run_backup(), 0, self.output)  # today's is the 15th
        kept = self.dated()
        self.assertEqual(len(kept), 14)
        self.assertNotIn("2026-01-01", kept)
        self.assertEqual(kept[-1], today())

    def test_no_secrets(self):
        """Scenario: No secrets — no .env file or secret value is copied."""
        self.write("stable/data/btc/data/archive/x/.env", f"DERIBIT_CLIENT_SECRET={SECRET}\n")
        self.write("stable/data/btc/.env", f"DERIBIT_CLIENT_SECRET={SECRET}\n")
        self.assertEqual(self.run_backup(), 0, self.output)
        for root, _, files in os.walk(self.backups):
            for f in files:
                self.assertFalse(f.startswith(".env"), os.path.join(root, f))
                with open(os.path.join(root, f), errors="replace") as fh:
                    self.assertNotIn(SECRET, fh.read(), os.path.join(root, f))

    def test_nothing_to_back_up_writes_no_run(self):
        """No bot folder anywhere: no copy and no ledger line."""
        shutil.rmtree(os.path.join(self.envs, "stable", "data"))
        shutil.rmtree(os.path.join(self.envs, "mvp", "data"))
        self.assertEqual(self.run_backup(), 0, self.output)
        self.assertEqual(self.ledger_lines(), [])
        self.assertEqual(self.dated(), [])


if __name__ == "__main__":
    unittest.main()
