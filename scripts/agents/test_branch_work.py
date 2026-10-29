"""Tests of branch_work.sh on a throwaway clone (#517: a blocked run's commits were
never pushed, and a run resuming them was blocked for making none):
python3 -m unittest scripts/agents/test_branch_work.py"""
import os
import subprocess
import tempfile
import unittest

SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "branch_work.sh")
ENV = {**os.environ, "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@x", "GIT_COMMITTER_NAME": "t",
       "GIT_COMMITTER_EMAIL": "t@x", "GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1"}
BRANCH = "agent/issue-7"


class BranchWork(unittest.TestCase):
    """An origin with main, and a run's worktree on agent/issue-7 started from main (dev_agent.sh)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        d = self.tmp.name
        self.origin, self.seed = os.path.join(d, "origin.git"), os.path.join(d, "seed")
        self.git(d, "init", "-q", "--bare", "-b", "main", self.origin)
        self.git(d, "clone", "-q", self.origin, self.seed)
        self.git(self.seed, "checkout", "-q", "-b", "main")
        self.commit(self.seed, "a.txt", "one\n", "initial")
        self.git(self.seed, "push", "-q", "origin", "HEAD:main")
        self.wt = self.new_run("run1", "origin/main")

    def tearDown(self):
        self.tmp.cleanup()

    def git(self, cwd, *args):
        return subprocess.run(["git", *args], cwd=cwd, env=ENV, check=True, capture_output=True, text=True).stdout.strip()

    def commit(self, repo, name, text, msg):
        with open(os.path.join(repo, name), "w") as f:
            f.write(text)
        self.git(repo, "add", name)
        self.git(repo, "commit", "-q", "-m", msg)

    def new_run(self, name, start):
        """A run's worktree: a fresh clone on the branch, from <start> (dev_agent.sh's worktree add -B)."""
        wt = os.path.join(self.tmp.name, name)
        self.git(self.tmp.name, "clone", "-q", self.origin, wt)
        self.git(wt, "checkout", "-q", "-B", BRANCH, start)
        return wt

    def run_script(self, *args):
        return subprocess.run(["bash", SCRIPT, *args], env=ENV, capture_output=True, text=True)

    def remote_head(self):
        out = self.git(self.tmp.name, "ls-remote", "--heads", self.origin, BRANCH)
        return out.split()[0] if out else ""

    def count(self, wt, base, pr=""):
        r = self.run_script("count", wt, base, pr)
        self.assertEqual(r.returncode, 0, r.stderr)
        return int(r.stdout)

    def test_a_blocked_run_with_commits_leaves_the_branch_at_its_head(self):
        self.commit(self.wt, "b.txt", "split\n", "the split")
        r = self.run_script("keep", self.wt, BRANCH)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.remote_head(), self.git(self.wt, "rev-parse", "HEAD"))
        self.assertIn("1 commit(s) pushed", r.stderr)

    def test_a_blocked_run_with_no_commit_pushes_nothing(self):
        r = self.run_script("keep", self.wt, BRANCH)
        self.assertEqual(r.returncode, 3)
        self.assertEqual(self.remote_head(), "")

    def test_the_next_run_starts_from_the_pushed_branch(self):
        self.commit(self.wt, "b.txt", "split\n", "the split")
        self.run_script("keep", self.wt, BRANCH)
        pushed = self.git(self.wt, "rev-parse", "HEAD")
        nxt = self.new_run("run2", "origin/" + BRANCH)
        self.assertEqual(self.git(nxt, "rev-parse", "HEAD"), pushed)
        # A second blocked run on top pushes fast-forward.
        self.commit(nxt, "c.txt", "more\n", "more work")
        self.assertEqual(self.run_script("keep", nxt, BRANCH).returncode, 0)
        self.assertEqual(self.remote_head(), self.git(nxt, "rev-parse", "HEAD"))

    def test_a_resumed_branch_with_no_new_commit_and_no_pr_counts_its_commits_ahead_of_main(self):
        self.commit(self.wt, "b.txt", "split\n", "the split")
        self.run_script("keep", self.wt, BRANCH)
        nxt = self.new_run("run2", "origin/" + BRANCH)
        base = self.git(nxt, "rev-parse", "HEAD")
        self.assertEqual(self.count(nxt, base), 1)  # goes on to the checks and the PR

    def test_a_fresh_run_with_no_commit_counts_zero(self):
        self.assertEqual(self.count(self.wt, self.git(self.wt, "rev-parse", "HEAD")), 0)

    def test_with_an_open_pr_only_the_runs_own_commits_count(self):
        self.commit(self.wt, "b.txt", "pr\n", "the PR's change")
        base = self.git(self.wt, "rev-parse", "HEAD")
        self.assertEqual(self.count(self.wt, base, "42"), 0)
        self.commit(self.wt, "b.txt", "pr\nfix\n", "review fix")
        self.assertEqual(self.count(self.wt, base, "42"), 1)

    def test_bad_usage_exits_2(self):
        self.assertEqual(self.run_script("count", self.wt).returncode, 2)
        self.assertEqual(self.run_script("push", self.wt, BRANCH).returncode, 2)


if __name__ == "__main__":
    unittest.main()
