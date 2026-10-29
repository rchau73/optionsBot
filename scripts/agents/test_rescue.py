"""Tests of rescue_commits.sh on a throwaway clone (#478: review fixes pushed to
a PR that merged meanwhile were stranded): python3 -m unittest scripts/agents/test_rescue.py"""
import os
import subprocess
import tempfile
import unittest

SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "rescue_commits.sh")
ENV = {**os.environ, "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@x", "GIT_COMMITTER_NAME": "t",
       "GIT_COMMITTER_EMAIL": "t@x", "GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1"}


class MergedPR(unittest.TestCase):
    """An origin with main, a PR branch the agent works on, and the PR merged while it works."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        d = self.tmp.name
        self.origin, self.seed, self.wt = (os.path.join(d, n) for n in ("origin.git", "seed", "wt"))
        self.git(d, "init", "-q", "--bare", "-b", "main", self.origin)
        self.git(d, "clone", "-q", self.origin, self.seed)
        self.git(self.seed, "checkout", "-q", "-b", "main")
        self.commit(self.seed, "a.txt", "one\n", "initial")
        self.git(self.seed, "push", "-q", "origin", "HEAD:main")
        # The PR: its first round of work, pushed.
        self.git(self.seed, "checkout", "-q", "-b", "agent/issue-7")
        self.commit(self.seed, "b.txt", "pr\n", "the PR's change")
        self.git(self.seed, "push", "-q", "origin", "agent/issue-7")
        # The agent's worktree starts from the PR head (dev_agent.sh's base) and
        # merges main, which moved meanwhile.
        self.git(self.seed, "checkout", "-q", "main")
        self.commit(self.seed, "c.txt", "main moved\n", "another PR")
        self.git(self.seed, "push", "-q", "origin", "main")
        self.git(d, "clone", "-q", "-b", "agent/issue-7", self.origin, self.wt)
        self.base = self.git(self.wt, "rev-parse", "HEAD")
        self.git(self.wt, "merge", "-q", "--no-edit", "origin/main")

    def tearDown(self):
        self.tmp.cleanup()

    def git(self, cwd, *args):
        return subprocess.run(["git", *args], cwd=cwd, env=ENV, check=True, capture_output=True, text=True).stdout.strip()

    def commit(self, repo, name, text, msg):
        with open(os.path.join(repo, name), "w") as f:
            f.write(text)
        self.git(repo, "add", name)
        self.git(repo, "commit", "-q", "-m", msg)

    def merge_pr_on_origin(self, extra=None):
        """A person merges the PR (at its first head) on main, as PR #474 merged."""
        self.git(self.seed, "fetch", "-q", "origin")
        self.git(self.seed, "checkout", "-q", "main")
        self.git(self.seed, "merge", "-q", "--no-ff", "--no-edit", "origin/agent/issue-7")
        if extra:
            self.commit(self.seed, *extra)
        self.git(self.seed, "push", "-q", "origin", "main")
        self.git(self.wt, "fetch", "-q", "origin")

    def rescue(self):
        return subprocess.run(["bash", SCRIPT, self.wt, self.base, "agent/issue-7-r1"], env=ENV, capture_output=True, text=True)

    def test_the_new_commits_land_on_a_new_branch_on_top_of_main(self):
        self.commit(self.wt, "b.txt", "pr\nreview fix 1\n", "fix finding 1")
        self.commit(self.wt, "d.txt", "fix 2\n", "fix finding 2")
        self.merge_pr_on_origin()
        r = self.rescue()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.git(self.wt, "symbolic-ref", "--short", "HEAD"), "agent/issue-7-r1")
        # On top of main, exactly the two fixes, in order: no merge, nothing main holds.
        self.assertEqual(self.git(self.wt, "rev-parse", "HEAD~2"), self.git(self.wt, "rev-parse", "origin/main"))
        self.assertEqual(self.git(self.wt, "log", "--format=%s", "origin/main..HEAD").splitlines(), ["fix finding 2", "fix finding 1"])
        self.assertEqual(open(os.path.join(self.wt, "b.txt")).read(), "pr\nreview fix 1\n")
        self.assertIn("2 commit(s)", r.stderr)

    def test_a_fix_main_already_holds_is_dropped(self):
        self.commit(self.wt, "d.txt", "fix 2\n", "fix finding 2")
        self.commit(self.wt, "e.txt", "fix 3\n", "fix finding 3")
        self.merge_pr_on_origin(extra=("d.txt", "fix 2\n", "a person fixed it by hand"))
        r = self.rescue()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.git(self.wt, "log", "--format=%s", "origin/main..HEAD").splitlines(), ["fix finding 3"])
        self.assertIn("already in main, dropped", r.stderr)

    def test_nothing_main_lacks_exits_3(self):
        self.commit(self.wt, "d.txt", "fix 2\n", "fix finding 2")
        self.merge_pr_on_origin(extra=("d.txt", "fix 2\n", "a person fixed it by hand"))
        r = self.rescue()
        self.assertEqual(r.returncode, 3, r.stderr)
        self.assertEqual(self.git(self.wt, "symbolic-ref", "--short", "HEAD"), "agent/issue-7")

    def test_no_new_commit_exits_3(self):
        self.merge_pr_on_origin()
        self.assertEqual(self.rescue().returncode, 3)

    def test_a_conflict_aborts_and_keeps_the_old_branch(self):
        self.commit(self.wt, "c.txt", "agent's version\n", "fix touching c")
        head = self.git(self.wt, "rev-parse", "HEAD")
        self.merge_pr_on_origin(extra=("c.txt", "main's version\n", "main changed c"))
        r = self.rescue()
        self.assertEqual(r.returncode, 1)
        self.assertIn("conflicts with main in: c.txt", r.stderr)
        self.assertEqual(self.git(self.wt, "symbolic-ref", "--short", "HEAD"), "agent/issue-7")
        self.assertEqual(self.git(self.wt, "rev-parse", "HEAD"), head)
        self.assertEqual(self.git(self.wt, "status", "--porcelain"), "")


if __name__ == "__main__":
    unittest.main()
