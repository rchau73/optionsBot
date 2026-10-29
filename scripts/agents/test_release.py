"""Tests of the automatic release (#471): release.py's pure rules, and
release.sh end to end (dry run and real) on a throwaway origin and stable
clone (test_promote.PromoteEnv) with a stub gh.
python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import datetime as dt
import json
import os
import subprocess
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import release  # noqa: E402
from test_promote import PromoteEnv, git  # noqa: E402

NOW = dt.datetime(2026, 11, 2, 0, 10, tzinfo=dt.timezone.utc)  # a Monday
CHAIN = ["a", "b", "c", "d", "e"]  # main, oldest first: each contains the earlier ones


def contains(x, y):
    return CHAIN.index(x) <= CHAIN.index(y)


def ago(days=0, hours=0):
    return NOW - dt.timedelta(days=days, hours=hours)


def decide(deploys, weekly=True, hotfixes=(), not_fixed=(), stable=("v0.1.0", "a"), tags=("v0.1.0",)):
    return release.decide(sorted(deploys), stable, list(tags), list(not_fixed), weekly, list(hotfixes), NOW, contains)


class Rules(unittest.TestCase):
    def test_a_build_3_days_in_mvp_newer_than_stable_is_released_weekly_with_the_next_version(self):
        got = decide([(ago(6), "b"), (ago(3.5), "c"), (ago(1), "d")], tags=["v0.1.0", "v0.0.9", "v0.1.1", "x"])
        self.assertEqual((got["action"], got["trigger"], got["commit"], got["tag"]), ("release", "weekly", "c", "v0.1.2"))

    def test_nothing_new_is_no_release_and_the_note_says_why(self):
        got = decide([(ago(6), "a")])
        self.assertEqual(got["action"], "none")
        self.assertIn("nothing new on mvp since v0.1.0", got["note"])
        # A soaked build stable already contains is not new either.
        got = decide([(ago(6), "b")], stable=("v0.1.1", "c"))
        self.assertEqual(got["action"], "none")

    def test_the_newest_build_under_3_days_with_no_hotfix_is_not_released(self):
        got = decide([(ago(2), "b"), (ago(1), "c")])
        self.assertEqual(got["action"], "none")
        self.assertIn("nothing soaked 3 days on mvp since v0.1.0", got["note"])
        self.assertIn("soaks at 2026-11-03 00:10 UTC", got["note"])
        self.assertIn("no hotfix ready", got["note"])

    def test_the_weekly_check_runs_only_when_due(self):
        got = decide([(ago(6), "b")], weekly=False)
        self.assertEqual((got["action"], got["note"]), ("none", ""))

    def test_a_hotfix_healthy_on_mvp_for_1_hour_is_released_early(self):
        # Its merge commit d first ran in the deploy of e (d itself never deployed).
        got = decide([(ago(2), "b"), (ago(hours=2), "e")], weekly=False, hotfixes=[(9, "d")])
        self.assertEqual((got["action"], got["trigger"], got["commit"], got["ref"]), ("release", "hotfix", "e", "#9"))

    def test_a_hotfix_under_1_hour_or_never_on_mvp_is_refused(self):
        got = decide([(ago(2), "b"), (ago(hours=0.5), "d")], weekly=False, hotfixes=[(9, "d")])
        self.assertEqual(got["action"], "none")
        self.assertIn("hotfix #9 live on mvp since", got["note"])
        got = decide([(ago(2), "b")], weekly=False, hotfixes=[(9, "d")])
        self.assertEqual(got["action"], "none")
        self.assertIn("hotfix #9 has not run in mvp yet", got["note"])

    def test_a_released_hotfix_is_not_released_again(self):
        got = decide([(ago(hours=2), "d")], weekly=False, hotfixes=[(9, "d")], stable=("v0.1.1", "d"))
        self.assertEqual(got["action"], "none")

    def test_the_newer_of_hotfix_and_weekly_goes(self):
        got = decide([(ago(4), "b"), (ago(hours=3), "d")], hotfixes=[(9, "d")])
        self.assertEqual((got["trigger"], got["commit"]), ("hotfix", "d"))
        # A hotfix older than the soaked build (already in it) goes as weekly.
        got = decide([(ago(5), "b"), (ago(4), "c")], hotfixes=[(9, "b")])
        self.assertEqual((got["trigger"], got["commit"]), ("weekly", "c"))

    def test_an_open_sme_not_fixed_blocks_every_path(self):
        for weekly, hot in ((True, []), (False, [(9, "d")]), (True, [(9, "d")])):
            got = decide([(ago(4), "b"), (ago(hours=3), "d")], weekly=weekly, hotfixes=hot, not_fixed=["#77 not fixed"])
            self.assertEqual(got["action"], "none", (weekly, hot))
            self.assertIn("#77 not fixed", got["note"])

    def test_no_stable_release_or_tag_is_left_to_a_person(self):
        self.assertIn("--first", decide([(ago(6), "b")], stable=None)["note"])
        self.assertIn("--first", decide([(ago(6), "b")], tags=[])["note"])

    def test_rollback_and_stable_lines_are_never_candidates(self):
        text = ("2026-10-25T00:00:00Z\tmvp\tb\n"
                "2026-10-26T00:00:00Z\tmvp\tc\trollback\n"
                "2026-10-26T01:00:00Z\tstable\td\trelease\tv0.1.1\tweekly\tagent:release\t-\n"
                "garbage\n")
        self.assertEqual(release.mvp_deploys(text), [(release.parse_time("2026-10-25T00:00:00Z"), "b")])

    def test_hotfix_prs_need_both_labels(self):
        issues = [{"number": 8, "labels": [{"name": "bug"}, {"name": "release:hotfix"}]},
                  {"number": 5, "labels": [{"name": "bug"}]}]
        prs = [{"number": 9, "body": "Fixes #8", "labels": [], "mergeCommit": {"oid": "d"}},
               {"number": 10, "body": "Closes #5", "labels": [], "mergeCommit": {"oid": "c"}},
               {"number": 11, "body": "", "labels": [{"name": "bug"}, {"name": "release:hotfix"}], "mergeCommit": {"oid": "e"}},
               {"number": 12, "body": "Closes #8", "labels": [], "mergeCommit": None}]
        self.assertEqual(release.hotfix_prs(prs, issues), [(9, "d"), (11, "e")])

    def test_week_and_next_patch(self):
        self.assertEqual(release.iso_week(NOW), "2026-W45")
        self.assertEqual(release.next_patch(["v0.2.0", "v0.10.1", "v0.9.9"]), "v0.10.2")
        self.assertIsNone(release.next_patch(["latest"]))


class ReleaseScript(PromoteEnv):
    """release.sh on a throwaway clone: stub gh, fake monitor (both envs)."""

    def setUp(self):
        super().setUp()
        t = self.t
        mvp = t("envs", "mvp")
        os.makedirs(mvp)
        self._write(os.path.join(mvp, "deploy.env"), f"OPTIONSBOT_ENV=mvp\nMONITOR_PORT={self.server.server_address[1]}\n")
        self.gh = t("gh")
        os.makedirs(self.gh)
        for name, text in (("not_fixed", ""), ("hotfix_issues.json", "[]"), ("prs.json", "[]")):
            self._write(os.path.join(self.gh, name), text)
        # The stub: answers by what is asked, records every comment.
        self._write(t("bin", "gh"), f"""#!/usr/bin/env python3
import sys
a = sys.argv[1:]
d = {self.gh!r}
def out(name):
    sys.stdout.write(open(d + "/" + name).read())
if a[:2] == ["repo", "view"]:
    print("o/r")
elif a[:2] == ["issue", "comment"]:
    open(d + "/comments", "a").write(a[a.index("--body") + 1] + "\\n")
elif "sme:not-fixed" in a:
    out("not_fixed")
elif "release:hotfix" in a:
    out("hotfix_issues.json")
elif a[:2] == ["pr", "list"]:
    out("prs.json")
elif "epic" in a:
    print("6")
""")
        self.env.update(AGENT_STATE_DIR=t("state"), AGENT_WORK_DIR=t("work-trees"), AGENT_NOTIFY="0",
                        AGENTS_ENV_FILE=t("envs", "agents.env"), RELEASE_MVP_HEALTH_SEC="3")
        self.script = t("repo", "scripts", "agents", "release.sh")
        self.week = release.iso_week(dt.datetime.now(dt.timezone.utc))

    def _release(self, **env):
        return subprocess.run(["bash", self.script], env=dict(self.env, **env), capture_output=True, text=True, timeout=120)

    def _comments(self):
        try:
            with open(os.path.join(self.gh, "comments")) as f:
                return f.read()
        except FileNotFoundError:
            return ""

    def _marker(self):
        return os.path.exists(self.t("state", "release", f"{self.week}.done"))

    def test_weekly_release_once_with_the_next_version_and_a_weekly_line(self):
        self._deploys((6, self.b), (3.5, self.c), (1, self.d))
        r = self._release()
        self.assertEqual(r.returncode, 0, r.stderr + r.stdout)
        self.assertEqual(self._origin_tag("v0.1.1"), self.c)
        self.assertEqual(self._stable_head(), self.c)
        self.assertEqual(self._last_deploy_line()[1:],
                         ["stable", self.c, "release", "v0.1.1", "weekly", "agent:release", "-"])
        self.assertIn("Release v0.1.1", self._comments())
        self.assertTrue(self._marker())
        # Once a week: the next tick releases nothing, not even D later.
        r = self._release()
        self.assertEqual(r.returncode, 0, r.stderr + r.stdout)
        self.assertIsNone(self._origin_tag("v0.1.2"))

    def test_nothing_new_posts_one_note_and_marks_the_week(self):
        self._deploys((2, self.b))
        r = self._release()
        self.assertEqual(r.returncode, 0, r.stderr + r.stdout)
        self.assertIsNone(self._origin_tag("v0.1.1"))
        self.assertIn("no release — nothing soaked 3 days", self._comments())
        self.assertTrue(self._marker())
        self.assertEqual(self._stable_head(), self.a)

    def test_a_hotfix_goes_early_on_any_tick(self):
        os.makedirs(self.t("state", "release"))
        self._write(self.t("state", "release", f"{self.week}.done"), "x\n")  # weekly already done
        self._deploys_hours((100, self.b), (2, self.d))
        self._write(os.path.join(self.gh, "hotfix_issues.json"),
                    json.dumps([{"number": 8, "labels": [{"name": "bug"}, {"name": "release:hotfix"}]}]))
        self._write(os.path.join(self.gh, "prs.json"),
                    json.dumps([{"number": 9, "title": "Fix it", "body": "Closes #8", "labels": [], "mergeCommit": {"oid": self.d}}]))
        r = self._release()
        self.assertEqual(r.returncode, 0, r.stderr + r.stdout)
        self.assertEqual(self._origin_tag("v0.1.1"), self.d)
        self.assertEqual(self._last_deploy_line()[5:], ["hotfix", "agent:release", "#9"])

    def test_an_open_sme_not_fixed_blocks_the_weekly_release(self):
        self._deploys((6, self.b), (3.5, self.c))
        self._write(os.path.join(self.gh, "not_fixed"), "#77 the fix did not work\n")
        r = self._release()
        self.assertEqual(r.returncode, 0, r.stderr + r.stdout)
        self.assertIsNone(self._origin_tag("v0.1.1"))
        self.assertIn("#77 the fix did not work", self._comments())

    def test_dry_run_changes_nothing(self):
        self._deploys((6, self.b), (3.5, self.c))
        r = self._release(RELEASE_DRY_RUN="1")
        self.assertEqual(r.returncode, 0, r.stderr + r.stdout)
        self.assertIn(f"would run promote.sh v0.1.1 --commit {self.c} --yes --weekly", r.stdout)
        self.assertIsNone(self._origin_tag("v0.1.1"))
        self.assertFalse(self._marker())
        self.assertEqual(self._comments(), "")

    def test_auto_release_off(self):
        self._deploys((6, self.b), (3.5, self.c))
        self._write(self.t("envs", "agents.env"), "AUTO_RELEASE=0\n")
        r = self._release()
        self.assertEqual(r.returncode, 0, r.stderr + r.stdout)
        self.assertIsNone(self._origin_tag("v0.1.1"))
        self.assertFalse(self._marker())

    def test_tick_starts_it_only_after_a_healthy_deploy(self):
        with open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "tick.sh")) as f:
            tick = f.read()
        self.assertIn('if [[ $deploy_ok -eq 1 ]]; then start release.sh 1', tick)


if __name__ == "__main__":
    unittest.main()
