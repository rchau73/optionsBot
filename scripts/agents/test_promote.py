"""End-to-end tests of promote.sh's release pick (#360): the real script against
a throwaway origin and stable clone, a fake deploys.tsv, a fake monitor, and
stub docker/gh/sleep.
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
FMT = "%Y-%m-%dT%H:%M:%SZ"


class Monitor(http.server.BaseHTTPRequestHandler):
    """Every bot's decision loop reports now: the health check passes."""

    def do_GET(self):
        if self.path == "/api/bots":
            out = {"bots": [{"name": "btc"}]}
        else:
            out = {"loop_at": dt.datetime.now(dt.timezone.utc).isoformat()}
        body = json.dumps(out).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *a):
        pass


def git(*args, cwd=None):
    return subprocess.run(["git", *args], cwd=cwd, check=True, capture_output=True, text=True).stdout.strip()


class Promote(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, True)
        t = lambda *p: os.path.join(self.tmp, *p)
        os.environ.update(GIT_AUTHOR_NAME="t", GIT_AUTHOR_EMAIL="t@t", GIT_COMMITTER_NAME="t", GIT_COMMITTER_EMAIL="t@t")
        # The dev checkout's scripts (no deploy.env beside them).
        shutil.copytree(SCRIPTS, t("repo", "scripts"))
        # origin: A (v0.1.0, stable's release), then B, C, D on main.
        self.origin = t("envs", "origin.git")
        git("init", "-q", "--bare", "-b", "main", self.origin)
        git("clone", "-q", self.origin, t("work"))
        self.a = self._commit("A")
        git("tag", "-a", "v0.1.0", "-m", "release v0.1.0", cwd=t("work"))
        git("push", "-q", "origin", "v0.1.0", cwd=t("work"))
        git("clone", "-q", self.origin, t("envs", "stable"))
        git("checkout", "-q", "--detach", "v0.1.0", cwd=t("envs", "stable"))
        self.b, self.c, self.d = self._commit("B"), self._commit("C"), self._commit("D")
        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Monitor)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.addCleanup(self.server.shutdown)
        self._write(t("envs", "stable", "deploy.env"), f"OPTIONSBOT_ENV=stable\nMONITOR_PORT={self.server.server_address[1]}\n")
        self._write(t("envs", "stable", ".env"), "DERIBIT_CLIENT_ID=stablestable\nDERIBIT_ENV=testnet\n")
        os.makedirs(t("envs", "history"))
        self._write(t("envs", "history", "promotions.tsv"), f"2026-10-20T00:00:00Z\tv0.1.0\t{self.a}\n")
        # Stubs: docker, gh (no sme:not-fixed issue), a fast sleep.
        os.makedirs(t("bin"))
        for name, text in (("docker", "#!/bin/sh\nexit 0\n"), ("gh", "#!/bin/sh\nexit 0\n"),
                           ("sleep", "#!/bin/sh\nexec /bin/sleep 0.05\n")):
            self._write(t("bin", name), text)
            os.chmod(t("bin", name), 0o755)
        self.env = dict(os.environ, PATH=t("bin") + os.pathsep + os.environ["PATH"], OPTIONSBOT_ENVS=t("envs"),
                        PROMOTE_SOAK_DAYS="3", PROMOTE_HEALTH_TIMEOUT_SEC="3")
        self.now = dt.datetime.now(dt.timezone.utc).replace(microsecond=0)
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

    def _deploys(self, *lines):
        """lines: (days ago, commit[, "rollback"]) → deploys.tsv, oldest first."""
        out = ""
        for days, commit, *kind in lines:
            at = (self.now - dt.timedelta(days=days)).strftime(FMT)
            out += "\t".join([at, "mvp", commit, *kind]) + "\n"
        self._write(self.t("envs", "history", "deploys.tsv"), out)

    def _since(self, days):
        return (self.now - dt.timedelta(days=days)).strftime("%Y-%m-%d %H:%M") + " UTC"

    def _promote(self, *args, script=None):
        script = script or self.t("repo", "scripts", "promote.sh")
        return subprocess.run(["bash", script, *args, "--yes"], env=self.env, capture_output=True, text=True, timeout=60)

    def _origin_tag(self, tag):
        r = subprocess.run(["git", "rev-parse", "-q", "--verify", f"refs/tags/{tag}^{{commit}}"],
                           cwd=self.origin, capture_output=True, text=True)
        return r.stdout.strip() or None

    def _stable_head(self):
        return git("rev-parse", "HEAD", cwd=self.t("envs", "stable"))

    def test_promote_picks_the_newest_soaked_mvp_build(self):
        # B 6 days ago, C 3.5 days ago (newest soaked), D a day ago (not soaked).
        self._deploys((6, self.b), (3.5, self.c), (1, self.d))
        r = self._promote("v0.2.0")
        self.assertEqual(r.returncode, 0, r.stderr + r.stdout)
        self.assertIn(f"newest mvp build soaked 3 days: {self.c[:7]}", r.stderr)
        self.assertIn(f"in mvp since {self._since(3.5)}", r.stdout)
        self.assertIn("(3.5 days)", r.stdout)
        self.assertIn(f"v0.1.0 → v0.2.0 ({self.c[:7]}, new tag)", r.stdout)
        self.assertIn("Version C", r.stdout)  # the commits since stable's release
        self.assertEqual(self._origin_tag("v0.2.0"), self.c)
        self.assertEqual(self._stable_head(), self.c)
        with open(self.t("envs", "history", "promotions.tsv")) as f:
            self.assertTrue(f.read().splitlines()[-1].endswith(f"\tv0.2.0\t{self.c}"))
        # #470: the release is also a stable line in deploys.tsv (cmd/dora).
        with open(self.t("envs", "history", "deploys.tsv")) as f:
            self.assertTrue(f.read().splitlines()[-1].endswith(f"\tstable\t{self.c}\trelease\tv0.2.0"))

    def test_a_stable_release_line_is_never_an_mvp_soak(self):
        # A stable line on D (4 days old) must not make D soaked: only mvp lines count.
        self._deploys((6, self.b), (1, self.d))
        with open(self.t("envs", "history", "deploys.tsv"), "a") as f:
            at = (self.now - dt.timedelta(days=4)).strftime(FMT)
            f.write(f"{at}\tstable\t{self.d}\trelease\tv0.1.9\n")
        r = self._promote("v0.2.0")
        self.assertEqual(r.returncode, 0, r.stderr + r.stdout)
        self.assertEqual(self._origin_tag("v0.2.0"), self.b)

    def test_a_rollback_line_never_counts(self):
        # C deployed 5 days ago; a rollback to B 4 days ago is newer, but never a candidate.
        self._deploys((6, self.b), (5, self.c), (4, self.b, "rollback"), (1, self.d))
        r = self._promote("v0.2.0")
        self.assertEqual(r.returncode, 0, r.stderr + r.stdout)
        self.assertEqual(self._origin_tag("v0.2.0"), self.c)

    def test_nothing_soaked_is_refused_with_the_date_it_will_have(self):
        self._deploys((2, self.b), (1, self.c), (0.5, self.b, "rollback"))
        r = self._promote("v0.3.0")
        self.assertNotEqual(r.returncode, 0)
        ready = (self.now - dt.timedelta(days=2) + dt.timedelta(days=3)).strftime("%Y-%m-%d %H:%M")
        self.assertIn(f"the oldest deploy ({self.b[:7]}, {self._since(2)}) will have soaked at {ready} UTC", r.stderr)
        self.assertIsNone(self._origin_tag("v0.3.0"))
        self.assertEqual(self._stable_head(), self.a)

    def test_no_deploy_at_all_is_refused(self):
        r = self._promote("v0.3.0")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("no commit deployed to mvp yet", r.stderr)

    def test_a_named_commit_that_ran_in_mvp(self):
        self._deploys((6, self.b), (4, self.c), (1, self.d))
        r = self._promote("v0.2.1", "--commit", self.b[:9])
        self.assertEqual(r.returncode, 0, r.stderr + r.stdout)
        self.assertIn(f"in mvp since {self._since(6)}", r.stdout)
        self.assertEqual(self._origin_tag("v0.2.1"), self.b)
        self.assertEqual(self._stable_head(), self.b)

    def test_a_named_commit_that_never_ran_in_mvp_is_refused(self):
        # C is on main and contained in D's deploy, but never ran in mvp itself.
        self._deploys((6, self.b), (4, self.d))
        r = self._promote("v0.2.1", "--commit", self.c)
        self.assertNotEqual(r.returncode, 0)
        self.assertIn(f"{self.c[:7]} never ran in mvp", r.stderr)
        self.assertIsNone(self._origin_tag("v0.2.1"))
        self.assertEqual(self._stable_head(), self.a)

    def test_a_named_commit_still_needs_the_soak(self):
        self._deploys((6, self.b), (1, self.d))
        r = self._promote("v0.2.1", "--commit", self.d)
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("the 3-day soak ends", r.stderr)
        self.assertIsNone(self._origin_tag("v0.2.1"))

    def test_an_existing_tag_is_unchanged(self):
        # A tag made by hand on B; mvp ran C (a descendant) 4 days ago. The newest
        # soaked deploy (C) does not move it.
        w = self.t("work")
        git("tag", "-a", "v0.2.0", self.b, "-m", "by hand", cwd=w)
        git("push", "-q", "origin", "v0.2.0", cwd=w)
        self._deploys((4, self.c), (3.5, self.d))
        r = self._promote("v0.2.0")
        self.assertEqual(r.returncode, 0, r.stderr + r.stdout)
        self.assertIn(f"in mvp since {self._since(4)}", r.stdout)
        self.assertNotIn("new tag", r.stdout)
        self.assertEqual(self._origin_tag("v0.2.0"), self.b)
        self.assertEqual(self._stable_head(), self.b)
        # --commit is for a new tag only.
        r = self._promote("v0.2.0", "--commit", self.c)
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("--commit is only for a new tag", r.stderr)

    def test_a_rollback_tag_skips_the_soak(self):
        r = self._promote("v0.1.0")
        self.assertEqual(r.returncode, 0, r.stderr + r.stdout)
        self.assertIn("rollback, no soak check", r.stdout)
        self.assertEqual(self._stable_head(), self.a)

    def test_refused_from_inside_an_environments_clone(self):
        # stable's own copy of the scripts: its clone has a deploy.env.
        clone = self.t("envs", "stable")
        shutil.copytree(SCRIPTS, os.path.join(clone, "scripts"), dirs_exist_ok=True)
        self._deploys((6, self.b), (3.5, self.c))
        env = dict(self.env)
        env.pop("OPTIONSBOT_ENVS")
        self.env = env
        r = self._promote("v0.2.0", script=os.path.join(clone, "scripts", "promote.sh"))
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("run it from the dev checkout", r.stderr)
        self.assertIsNone(self._origin_tag("v0.2.0"))
        r = subprocess.run(["bash", os.path.join(clone, "scripts", "envctl.sh"), "mvp", "init"],
                           env=env, capture_output=True, text=True, timeout=60)
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("run it from the dev checkout", r.stderr)
        self.assertFalse(os.path.exists(self.t("envs", "mvp")))


if __name__ == "__main__":
    unittest.main()
