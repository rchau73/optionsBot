"""Tests of the security digest and alerts job (security.sh, #717), end to end
on throwaway environments: python3 -m unittest discover -s scripts/agents -p 'test_*.py'

The rules themselves (internal/security) are tested in tests/security_digest_test.go;
here cmd/security runs as the real binary, built once, with a fake docker and osascript."""
import datetime as dt
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(os.path.dirname(HERE))
sys.path.insert(0, HERE)
import usage_week  # noqa: E402  the same "last complete week"

# The same fakes as test_backup.py: a running monitor's volume is <env>/.fake-volume.
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
USER, IP = "alice", "203.0.113.5"


def seal(entry, prev):
    """frontend/lib/auth/auditchain.js sealLine."""
    body = json.dumps({**entry, "prev": prev}, separators=(",", ":"))
    return body[:-1] + ',"line_hash":"' + hashlib.sha256(body.encode()).hexdigest() + '"}'


def trail(entries):
    out, prev = [], ""
    for e in entries:
        line = seal(e, prev)
        out.append(line)
        prev = hashlib.sha256(line.encode()).hexdigest()
    return "\n".join(out) + "\n"


def at(t):
    return t.strftime("%Y-%m-%dT%H:%M:%S.000Z")


def failures(n, start, minutes):
    return [{"time": at(start + dt.timedelta(minutes=i * minutes)), "action": "sign_in_failed",
             "username": USER, "ip": IP, "reason": "password"} for i in range(n)]


class SecurityTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.bin_dir = tempfile.mkdtemp()
        cls.bin = os.path.join(cls.bin_dir, "security")
        subprocess.run(["go", "build", "-o", cls.bin, "./cmd/security"], cwd=REPO, check=True)
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
        self.now = dt.datetime.now(dt.timezone.utc)
        self.week = usage_week.previous_week(self.now.date())
        self.write("mvp/.env", "DERIBIT_ENV=testnet\n")

    def write(self, rel, text):
        path = os.path.join(self.envs, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as f:
            f.write(text)

    def read(self, path):
        try:
            with open(path) as f:
                return f.read()
        except FileNotFoundError:
            return ""

    def run_job(self, alert_min="0"):
        env = {k: v for k, v in os.environ.items()
               if k not in ("MONITOR_URL", "MVP_MONITOR_URL", "MONITOR_TOKEN", "MVP_MONITOR_TOKEN")}
        self.notify_log = os.path.join(self.tmp, "notify.log")
        env.update(OPTIONSBOT_ENVS=self.envs, AGENT_STATE_DIR=self.state, AGENT_RUNS_FILE=os.path.join(self.tmp, "runs.jsonl"),
                   AGENTS_ENV_FILE=os.path.join(self.tmp, "agents.env"), AGENT_NOTIFY="1", SECURITY_BIN=self.bin,
                   SECURITY_ALERT_MIN=alert_min, PATH=self.fake_bin + os.pathsep + os.environ["PATH"],
                   FAKE_DOCKER_LOG=os.path.join(self.tmp, "docker.log"), FAKE_NOTIFY_LOG=self.notify_log)
        p = subprocess.run(["bash", os.path.join(HERE, "security.sh")], env=env, capture_output=True, text=True, timeout=120)
        self.output = p.stdout + p.stderr
        self.assertEqual(p.returncode, 0, self.output)

    def notifications(self):
        return [l for l in self.read(self.notify_log).splitlines() if l.strip()]

    def runs(self):
        return [json.loads(l) for l in self.read(os.path.join(self.tmp, "runs.jsonl")).splitlines() if l.strip()]

    def test_scenario_password_guessing_alerts_the_same_day_once(self):
        self.write("mvp/data/monitor/audit.jsonl", trail(failures(10, self.now - dt.timedelta(minutes=50), 5)))
        self.run_job()
        alerts = [n for n in self.notifications() if "security alert" in n]
        self.assertEqual(len(alerts), 1, self.output)
        self.assertIn("10 failed sign-ins for one username within an hour", alerts[0])
        self.assertIn("rule: 10 or more in any hour", alerts[0])
        for leak in (USER, IP):
            self.assertNotIn(leak, self.read(self.notify_log))
            self.assertNotIn(leak, self.read(os.path.join(self.state, "security", "mvp.alerts")))
        self.run_job()  # the next check: the same pattern never alerts twice
        self.assertEqual(len([n for n in self.notifications() if "security alert" in n]), 1, self.output)
        ends = [r for r in self.runs() if r["event"] == "end"]
        self.assertIn(("security", "notified", "1 alert(s)"), [(r["agent"], r["outcome"], r["detail"]) for r in ends])

    def test_the_alert_check_waits_its_interval(self):
        self.write("mvp/data/monitor/audit.jsonl", trail(failures(10, self.now - dt.timedelta(minutes=50), 5)))
        self.run_job(alert_min="30")
        self.write("mvp/data/monitor/audit.jsonl", trail(failures(10, self.now - dt.timedelta(minutes=20), 1)))
        self.run_job(alert_min="30")  # within 30 min of the last check: not checked again
        self.assertEqual(len([n for n in self.notifications() if "security alert" in n]), 1, self.output)

    def test_the_running_monitors_trail_is_read_and_only_audit_files_kept(self):
        self.write("mvp/deploy.env", "OPTIONSBOT_ENV=mvp\nMONITOR_PORT=3100\n")
        promo = {"time": at(self.now - dt.timedelta(minutes=5)), "action": "role_changed", "username": "root",
                 "role": "admin", "target": "dave", "from": "user", "to": "admin"}
        self.write("mvp/.fake-volume/audit.jsonl", trail([promo]))
        self.write("mvp/.fake-volume/auth.db", "users")
        self.write("mvp/data/monitor/audit.jsonl", trail([]))  # the clone's copy is stale
        self.run_job()
        alerts = [n for n in self.notifications() if "security alert" in n]
        self.assertEqual(len(alerts), 1, self.output)
        self.assertIn("a role change to admin on", alerts[0])
        self.assertNotIn("dave", alerts[0])
        self.assertFalse(os.path.exists(os.path.join(self.state, "security", ".trail-mvp")), "the scratch copy is removed")

    def test_scenario_weekly_digest_notifies_and_becomes_the_usage_reports_section(self):
        monday = self.now.date() - dt.timedelta(days=self.now.weekday() + 7)  # the digest's week starts then
        last_week = dt.datetime.combine(monday + dt.timedelta(days=1), dt.time(9), dt.timezone.utc)
        self.write("mvp/data/monitor/audit.jsonl", trail(failures(12, last_week, 30)))
        report = os.path.join(self.envs, "mvp", "data", "monitor", "reports", self.week + ".md")
        self.write(f"mvp/data/monitor/reports/{self.week}.md", "# usage\n")
        self.run_job()
        digest = self.read(os.path.join(self.state, "security", self.week, "mvp", "digest.md"))
        self.assertIn("| Failed sign-ins | 12 | 0 |", digest)
        self.assertIn("Audit chain intact — 12 lines checked.", digest)
        heads = [n for n in self.notifications() if "security digest" in n]
        self.assertEqual(len(heads), 1, self.output)
        self.assertIn(f"Security {self.week}: chain intact; 12 failed sign-ins (0 the week before)", heads[0])
        self.assertTrue(os.path.exists(os.path.join(self.state, "security", self.week + ".done")))
        text = self.read(report)
        self.assertTrue(text.startswith("# usage\n"))
        self.assertEqual(text.count("## Security"), 1)
        self.run_job()  # done: no second digest, no second section
        self.assertEqual(len([n for n in self.notifications() if "security digest" in n]), 1)
        self.assertEqual(self.read(report).count("## Security"), 1)
        for leak in (USER, IP):
            self.assertNotIn(leak, digest + self.read(self.notify_log))

    def test_an_empty_trail_is_no_data_yet(self):
        self.run_job()
        self.assertIn("No data yet", self.read(os.path.join(self.state, "security", self.week, "mvp", "digest.md")))
        self.assertIn("no data yet", self.read(self.notify_log))

    def test_tick_starts_security(self):
        with open(os.path.join(HERE, "tick.sh")) as f:
            self.assertIn("start security.sh 1", f.read())


if __name__ == "__main__":
    unittest.main()
