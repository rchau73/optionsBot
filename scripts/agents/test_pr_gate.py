"""Tests of pr_gate.py: python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import json
import os
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import pr_gate  # noqa: E402

GREEN = [{"__typename": "CheckRun", "name": "ci", "status": "COMPLETED", "conclusion": "SUCCESS"}]


def pr(**kw):
    p = {"state": "OPEN", "isDraft": False, "labels": [{"name": "agent:dev"}], "headRefOid": "abc",
         "mergeable": "MERGEABLE", "mergeStateStatus": "CLEAN", "statusCheckRollup": GREEN}
    p.update(kw)
    return p


CLEAN = ["sha=abc verdict=clean"]


class Decide(unittest.TestCase):
    def test_ready(self):
        self.assertEqual(pr_gate.decide(pr(), CLEAN, [])[0], "ready")

    def test_conflict_first(self):
        self.assertEqual(pr_gate.decide(pr(mergeable="CONFLICTING", mergeStateStatus="DIRTY"), CLEAN, [])[0], "conflict")

    def test_behind_updates(self):
        self.assertEqual(pr_gate.decide(pr(mergeStateStatus="BEHIND"), CLEAN, [])[0], "update")

    def test_review_of_an_older_commit_is_not_enough(self):
        a, why = pr_gate.decide(pr(headRefOid="new"), CLEAN, [])
        self.assertEqual(a, "wait")
        self.assertIn("current commit", why)

    def test_latest_verdict_on_head_counts(self):
        self.assertEqual(pr_gate.decide(pr(), ["sha=abc verdict=blocking", "sha=abc verdict=clean"], [])[0], "ready")
        self.assertEqual(pr_gate.decide(pr(), ["sha=abc verdict=blocking"], [])[0], "wait")

    def test_open_dependency_waits(self):
        a, why = pr_gate.decide(pr(), CLEAN, [170])
        self.assertEqual((a, why), ("wait", "its ticket waits for #170"))

    def test_ci_running_waits(self):
        running = [{"__typename": "CheckRun", "name": "ci", "status": "IN_PROGRESS"}]
        self.assertEqual(pr_gate.decide(pr(statusCheckRollup=running), CLEAN, [])[0], "wait")

    def test_needs_human_and_draft_wait(self):
        self.assertEqual(pr_gate.decide(pr(labels=[{"name": "agent:needs-human"}]), CLEAN, [])[0], "wait")
        self.assertEqual(pr_gate.decide(pr(isDraft=True), CLEAN, [])[0], "wait")


CLEAN_FREE = ["sha=abc verdict=clean policy=no"]


def auto(p=None, markers=CLEAN_FREE, files=("frontend/components/X.jsx",), behind=0, on=True, equiv=()):
    return pr_gate.decide(p or pr(), markers, [], equiv, list(files), behind, on)


class AutoMerge(unittest.TestCase):
    """#468: ready PRs merge on their own unless they change a business rule or policy."""

    def test_policy_free_up_to_date_merges(self):
        self.assertEqual(auto(), ("merge", "reviewed clean, CI green, up to date with main, no policy change"))

    def test_a_policy_path_leaves_it_to_a_person_and_names_why(self):
        a, why = auto(files=("internal/strategy/open.go", "internal/risk/x.go", "internal/strategy/close.go"))
        self.assertEqual(a, "ready")
        self.assertIn("it touches trading rules; it touches margin policy", why)
        self.assertEqual(why.count("trading rules"), 1)

    def test_every_policy_area_is_caught(self):
        for path in ["config_btc.yaml", "OptionStrategy.md", "internal/orders/journal.go", "cmd/bot/main.go",
                     "docs/product/PRD.md", ".github/workflows/ci.yml", ".claude/skills/developer/SKILL.md",
                     "scripts/agents/pr_gate.sh", "scripts/promote.sh", "frontend/lib/auth/store.js"]:
            self.assertEqual(auto(files=[path])[0], "ready", path)

    def test_near_misses_are_not_policy(self):
        for path in ["internal/strategyx.go", "frontend/lib/authz.js", "docs/productivity.md", "scripts/agents/pr_gatekeeper_notes.md"]:
            # "scripts/agents/pr_gate." is a filename prefix: pr_gatekeeper does not match it.
            self.assertEqual(auto(files=[path])[0], "merge", path)

    def test_the_reviewer_saying_yes_or_nothing_leaves_it_to_a_person(self):
        a, why = auto(markers=["sha=abc verdict=clean policy=yes"])
        self.assertEqual((a, why.endswith("the Reviewer marked it a policy change")), ("ready", True))
        a, why = auto(markers=CLEAN)  # an older review, before the declaration existed
        self.assertEqual((a, why.endswith("did not say whether it changes a policy")), ("ready", True))

    def test_auto_off_never_merges(self):
        self.assertEqual(auto(on=False)[0], "ready")

    def test_behind_main_is_updated_before_it_merges(self):
        self.assertEqual(auto(behind=3), ("update", "behind main by 3: updated before it merges"))

    def test_a_policy_pr_behind_main_is_not_updated_for_merging(self):
        self.assertEqual(auto(files=["internal/strategy/open.go"], behind=3)[0], "ready")

    def test_a_review_carries_over_an_update_merge(self):
        moved = pr(headRefOid="new")
        self.assertEqual(auto(moved)[0], "wait")
        self.assertEqual(auto(moved, equiv=["abc"])[0], "merge")

    def test_an_incomplete_review_is_not_reviewed(self):
        # #476: a changed file left unread → wait "not fully reviewed", never ready or merge.
        for markers in (["sha=abc verdict=incomplete policy=no"],
                        ["sha=abc verdict=clean policy=no", "sha=abc verdict=incomplete policy=no"]):
            a, why = auto(markers=markers)
            self.assertEqual(a, "wait", markers)
            self.assertIn("not fully reviewed", why)
        self.assertEqual(auto(pr(headRefOid="new"), markers=["sha=abc verdict=incomplete policy=no"], equiv=["abc"])[0], "wait")

    def test_a_card_in_progress_waits_for_the_dev_agent(self):
        # #478: a PR merged while the Dev agent fixed its review findings. In
        # progress stops every action — merge, ready, update and conflict alike.
        for p, behind in ((pr(), 0), (pr(), 3), (pr(mergeable="CONFLICTING", mergeStateStatus="DIRTY"), 0)):
            got = pr_gate.decide(p, CLEAN_FREE, [], (), ["frontend/x.jsx"], behind, True, "In progress")
            self.assertEqual(got, ("wait", "a Dev agent is working on it"))
        for card in ("In review", "", "Ready"):
            self.assertEqual(pr_gate.decide(pr(), CLEAN_FREE, [], (), ["frontend/x.jsx"], 0, True, card)[0], "merge", card)

    def test_main_reads_the_card_status(self):
        with tempfile.TemporaryDirectory() as d:
            paths = {}
            for name, text in {"pr": json.dumps(pr()), "markers": CLEAN_FREE[0], "deps": "", "equiv": "", "files": "frontend/x.jsx"}.items():
                paths[name] = os.path.join(d, name)
                open(paths[name], "w").write(text)
            args = [sys.executable, os.path.join(os.path.dirname(__file__), "pr_gate.py"), "decide",
                    paths["pr"], paths["markers"], paths["deps"], paths["equiv"], paths["files"], "0", "1"]
            run = lambda *extra: subprocess.run(args + list(extra), capture_output=True, text=True, check=True).stdout.strip()
            self.assertEqual(run("In progress"), "wait a Dev agent is working on it")
            self.assertTrue(run().startswith("merge"))

    def test_blocking_or_ci_still_stop_a_merge(self):
        self.assertEqual(auto(markers=["sha=abc verdict=blocking policy=no"])[0], "wait")
        red = [{"__typename": "CheckRun", "name": "ci", "status": "COMPLETED", "conclusion": "FAILURE"}]
        self.assertEqual(auto(pr(statusCheckRollup=red))[0], "wait")
        self.assertEqual(auto(pr(labels=[{"name": "agent:needs-human"}]))[0], "wait")


def commit(parents=2, committer="GitHub", subject="Merge branch 'main' into agent/issue-1"):
    return {"parents": [{}] * parents, "commit": {"committer": {"name": committer}, "message": subject + "\n\nbody"}}


class Equiv(unittest.TestCase):
    def test_only_github_update_merges_carry_the_review(self):
        cmp = {"base_commit": {"sha": "abc"}, "commits": [commit(), commit()]}
        self.assertEqual(pr_gate.equiv(cmp), "abc")

    def test_anything_else_does_not(self):
        for c in [commit(parents=1, subject="Fix review"), commit(committer="Roberto Chau"),
                  commit(subject="Merge remote-tracking branch 'origin/main'")]:
            self.assertIsNone(pr_gate.equiv({"base_commit": {"sha": "abc"}, "commits": [commit(), c]}))
        self.assertIsNone(pr_gate.equiv({"base_commit": {"sha": "abc"}, "commits": []}))
