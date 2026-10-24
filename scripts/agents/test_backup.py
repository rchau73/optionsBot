"""Tests of the nightly backup (backup_data.sh, #328), end to end on throwaway
environments: python3 -m unittest discover -s scripts/agents -p 'test_*.py'

The restore check itself (internal/backup) is tested in tests/backup_check_test.go;
here it runs as the real cmd/backupcheck, built once."""
import datetime
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


def today():
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%d")


class BackupTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.bin_dir = tempfile.mkdtemp()
        cls.check = os.path.join(cls.bin_dir, "backupcheck")
        subprocess.run(["go", "build", "-o", cls.check, "./cmd/backupcheck"], cwd=REPO, check=True)

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

    def run_backup(self):
        env = {k: v for k, v in os.environ.items()
               if k not in ("MONITOR_URL", "MVP_MONITOR_URL", "MONITOR_TOKEN", "MVP_MONITOR_TOKEN")}
        env.update(OPTIONSBOT_ENVS=self.envs, AGENT_STATE_DIR=self.state, AGENT_RUNS_FILE=self.ledger,
                   AGENT_NOTIFY="0", BACKUP_BOTS="btc eth", BACKUP_CHECK_BIN=self.check,
                   AGENTS_ENV_FILE=os.path.join(self.tmp, "agents.env"))
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
