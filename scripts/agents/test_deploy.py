"""End-to-end tests of deploy.sh's failure path (#204): the real script against
a throwaway origin and mvp clone, a fake monitor, and stub docker/gh/sleep.
python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import datetime as dt
import http.server
import json
import os
import shutil
import subprocess
import tempfile
import threading
import unittest

SCRIPTS = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
TOKEN = "redactme-redactme"  # fake, low-entropy: CI's gitleaks must not take it for a key

DOCKER = """#!/usr/bin/env bash
# docker compose … up|logs — logs each call with the commit the clone is on.
state="$DEPLOY_TEST_DIR"; ver="$(cat app.txt)"
echo "$* @ $ver" >> "$state/docker.log"
case " $* " in
  *" up "*) grep -qx "$ver" "$state/compose_fails" 2>/dev/null && { echo "build error in $ver"; exit 1; }; exit 0 ;;
  *" logs "*) echo "bot-btc | panic: $ver is broken"; echo "monitor | Authorization: Bearer $(sed -n 's/^MONITOR_API_TOKEN=//p' .env)"; exit 0 ;;
esac
"""

GH = """#!/usr/bin/env bash
state="$DEPLOY_TEST_DIR"; echo "$*" >> "$state/gh.log"
case "$1 $2" in
  "repo view") echo owner/repo ;;
  "issue list") cat "$state/issues.json" 2>/dev/null || echo '[]' ;;
  "issue create")
    [[ -f "$state/gh_down" ]] && exit 1
    while [[ $# -gt 0 ]]; do
      case "$1" in --body-file) cp "$2" "$state/body.md"; shift ;; --title) echo "$2" > "$state/title.txt"; shift ;; esac
      shift
    done
    echo https://github.com/owner/repo/issues/999 ;;
  *) exit 1 ;;
esac
"""


class Monitor(http.server.BaseHTTPRequestHandler):
    """/api/bots and /api/bots/btc/status: fresh loop_at only while the mvp
    clone holds a commit listed in healthy (the bots came up)."""
    environment = "testnet"
    healthy = set()
    clone = ""

    def do_GET(self):
        if self.path == "/api/bots":
            out = {"bots": [{"name": "btc"}]}
        else:
            with open(os.path.join(self.clone, "app.txt")) as f:
                ver = f.read().strip()
            at = dt.datetime.now(dt.timezone.utc) - dt.timedelta(minutes=0 if ver in self.healthy else 30)
            out = {"environment": self.environment, "loop_at": at.isoformat()}
        body = json.dumps(out).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *a):
        pass


def git(*args, cwd=None):
    return subprocess.run(["git", *args], cwd=cwd, check=True, capture_output=True, text=True).stdout.strip()


class Deploy(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, True)
        t = lambda *p: os.path.join(self.tmp, *p)
        os.environ.update(GIT_AUTHOR_NAME="t", GIT_AUTHOR_EMAIL="t@t", GIT_COMMITTER_NAME="t", GIT_COMMITTER_EMAIL="t@t")
        # The agents' scripts, as the mvp clone would hold them.
        shutil.copytree(SCRIPTS, t("repo", "scripts"))
        # origin with A, mvp cloned at A and deployed, then main moves to B.
        git("init", "-q", "--bare", "-b", "main", t("envs", "origin.git"))
        git("clone", "-q", t("envs", "origin.git"), t("work"))
        self.a = self._commit("A")
        git("clone", "-q", t("envs", "origin.git"), t("envs", "mvp"))
        self.b = self._commit("B")
        with open(t("envs", "mvp", ".env"), "w") as f:
            f.write(f"DERIBIT_CLIENT_ID=mvpmvpmvp\nMONITOR_API_TOKEN={TOKEN}\n")
        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Monitor)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.addCleanup(self.server.shutdown)
        with open(t("envs", "mvp", "deploy.env"), "w") as f:
            f.write(f"OPTIONSBOT_ENV=mvp\nMONITOR_PORT={self.server.server_address[1]}\n")
        os.makedirs(t("envs", "history"))
        self._write(t("envs", "history", "mvp.deployed"), self.a + "\n")
        Monitor.environment, Monitor.healthy, Monitor.clone = "testnet", {"A"}, t("envs", "mvp")
        # Stubs: docker, gh, a fast sleep (wait_healthy polls every 15 s).
        os.makedirs(t("bin"))
        for name, text in (("docker", DOCKER), ("gh", GH), ("sleep", "#!/bin/sh\nexec /bin/sleep 0.05\n")):
            self._write(t("bin", name), text)
            os.chmod(t("bin", name), 0o755)
        self.env = dict(os.environ, PATH=t("bin") + os.pathsep + os.environ["PATH"], OPTIONSBOT_ENVS=t("envs"),
                                                # 3 s: wait_healthy's loop runs at least once whatever the clock
                        DEPLOY_TEST_DIR=self.tmp, AGENT_NOTIFY="0", DEPLOY_HEALTH_TIMEOUT_SEC="3")
        for k in ("MONITOR_URL", "MVP_MONITOR_URL", "MONITOR_TOKEN", "MVP_MONITOR_TOKEN", "DEPLOY_ENABLED"):
            self.env.pop(k, None)
        self.t = t

    def _write(self, path, text):
        with open(path, "w") as f:
            f.write(text)

    def _commit(self, ver):
        w = os.path.join(self.tmp, "work")
        self._write(os.path.join(w, "app.txt"), ver + "\n")
        git("add", "app.txt", cwd=w)
        git("commit", "-q", "-m", f"Version {ver}", cwd=w)
        git("push", "-q", "origin", "HEAD:main", cwd=w)
        return git("rev-parse", "HEAD", cwd=w)

    def run_deploy(self):
        p = subprocess.run(["bash", self.t("repo", "scripts", "agents", "deploy.sh")], env=self.env,
                           capture_output=True, text=True, timeout=120)
        self.output = p.stdout + p.stderr
        return p.returncode

    def read(self, *p):
        try:
            with open(self.t(*p)) as f:
                return f.read()
        except FileNotFoundError:
            return ""

    def mvp_head(self):
        return git("rev-parse", "HEAD", cwd=self.t("envs", "mvp"))

    def ups(self):
        return [l for l in self.read("docker.log").splitlines() if " up " in f" {l} "]

    def creates(self):
        return [l for l in self.read("gh.log").splitlines() if l.startswith("issue create")]

    # Scenario: health check fails
    def test_health_check_fails_rolls_back_and_files_one_bug(self):
        self.assertEqual(self.run_deploy(), 1, self.output)
        self.assertEqual(self.mvp_head(), self.a)
        self.assertEqual(git("rev-parse", "--abbrev-ref", "HEAD", cwd=self.t("envs", "mvp")), "main")
        self.assertEqual(self.read("envs", "history", "mvp.failed").strip(), self.b)
        self.assertEqual(self.read("envs", "history", "mvp.deployed").strip(), self.a)
        self.assertEqual([u.split(" @ ")[1] for u in self.ups()], ["B", "A"])  # B, then the rollback to A
        self.assertIn(f"\tmvp\t{self.a}\trollback", self.read("envs", "history", "deploys.tsv"))
        self.assertEqual(len(self.creates()), 1)
        self.assertIn("--label bug", self.creates()[0])
        self.assertEqual(self.read("title.txt").strip(), f"[E6] mvp deploy failed: {self.b[:7]} (Version B)")
        body = self.read("body.md")
        self.assertIn("panic: B is broken", body)
        self.assertIn("health check", body)
        self.assertIn(f"rolled back to `{self.a[:7]}`, healthy", body)
        self.assertIn(f"{self.b[:7]} Version B", body)  # the commit range
        # Scenario: no secret in the ticket
        self.assertNotIn(TOKEN, body)
        self.assertIn("[redacted MONITOR_API_TOKEN]", body)

    def test_compose_failure_rolls_back_too(self):
        self._write(self.t("compose_fails"), "B\n")
        self.assertEqual(self.run_deploy(), 1, self.output)
        self.assertEqual(self.mvp_head(), self.a)
        self.assertIn("docker compose up --build", self.read("body.md"))
        self.assertIn("build error in B", self.read("body.md"))
        self.assertEqual(len(self.creates()), 1)

    def test_rollback_unhealthy_too_stops_loudly(self):
        Monitor.healthy = set()
        self.assertEqual(self.run_deploy(), 1, self.output)
        self.assertIn("ROLLBACK", self.output)
        self.assertEqual(self.mvp_head(), self.a)
        self.assertEqual(self.read("envs", "history", "mvp.failed").strip(), self.b)
        self.assertNotIn("rollback", self.read("envs", "history", "deploys.tsv"))
        self.assertEqual(len(self.ups()), 2)  # no retry loop
        self.assertIn("**FAILED**", self.read("body.md"))

    # Scenario: no duplicate ticket
    def test_no_duplicate_ticket_and_no_redeploy(self):
        self.assertEqual(self.run_deploy(), 1, self.output)
        self._write(self.t("issues.json"), json.dumps([{"number": 999, "title": self.read("title.txt").strip()}]))
        self.assertEqual(self.run_deploy(), 0, self.output)
        self.assertEqual(len(self.creates()), 1)
        self.assertEqual(len(self.ups()), 2)  # B is not deployed again
        self.assertEqual(self.mvp_head(), self.a)

    def test_existing_issue_for_the_sha_is_not_filed_again(self):
        title = f"[E6] mvp deploy failed: {self.b[:7]} (Version B)"
        self._write(self.t("issues.json"), json.dumps([{"number": 5, "title": title}]))
        self.assertEqual(self.run_deploy(), 1, self.output)
        self.assertEqual(self.creates(), [])
        self.assertEqual(self.mvp_head(), self.a)

    def test_issue_is_filed_on_a_later_tick_when_github_was_down(self):
        self._write(self.t("gh_down"), "")
        self.assertEqual(self.run_deploy(), 1, self.output)
        os.remove(self.t("gh_down"))
        self.assertEqual(self.run_deploy(), 0, self.output)
        self.assertEqual(self.read("title.txt").strip(), f"[E6] mvp deploy failed: {self.b[:7]} (Version B)")
        self.assertEqual(self.run_deploy(), 0, self.output)
        self.assertEqual(len(self.creates()), 2)  # the refused one and the filed one, then nothing
        self.assertEqual(len(self.ups()), 2)

    # Scenario: live bots
    def test_live_bots_exit_before_any_deploy_or_rollback(self):
        Monitor.environment = "live"
        self.assertEqual(self.run_deploy(), 0, self.output)
        self.assertEqual(self.ups(), [])
        self.assertEqual(self.mvp_head(), self.a)
        self.assertEqual(self.read("envs", "history", "mvp.failed"), "")
        self.assertEqual(self.creates(), [])

    def test_healthy_deploy_is_unchanged(self):
        Monitor.healthy = {"A", "B"}
        self.assertEqual(self.run_deploy(), 0, self.output)
        self.assertEqual(self.mvp_head(), self.b)
        self.assertEqual(self.read("envs", "history", "mvp.deployed").strip(), self.b)
        self.assertEqual(self.read("envs", "history", "mvp.failed"), "")
        lines = self.read("envs", "history", "deploys.tsv").splitlines()
        self.assertEqual([l.split("\t")[1:] for l in lines], [["mvp", self.b]])
        self.assertEqual(self.creates(), [])


if __name__ == "__main__":
    unittest.main()
