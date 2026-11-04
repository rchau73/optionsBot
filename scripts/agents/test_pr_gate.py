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

    def test_working_docs_in_docs_product_are_not_policy(self):
        # #596: they waited for a person only because they sit beside the PRD.
        for path in ["docs/product/assistant/README.md", "docs/product/lab/README.md",
                     "docs/product/competitors/README.md", "docs/product/releases/README.md"]:
            self.assertEqual(auto(files=[path])[0], "merge", path)
        for path in ["docs/product/PRD.md", "docs/product/decisions/x.md", "docs/product/analytics/event-plan.md",
                     "docs/product/assistant/contract.md", "docs/product/education/tips.json", "docs/product/new.md"]:
            self.assertEqual(auto(files=[path])[0], "ready", path)

    def test_golden_questions_added_only_are_not_policy(self):
        golden = "docs/product/assistant/golden.jsonl"
        got = pr_gate.decide(pr(), CLEAN_FREE, [], (), [golden], 0, True, "", ())
        self.assertEqual(got[0], "merge")
        a, why = pr_gate.decide(pr(), CLEAN_FREE, [], (), [golden], 0, True, "", [golden])
        self.assertEqual(a, "ready")
        self.assertIn("the PRD and product policies", why)

    def test_blocking_or_ci_still_stop_a_merge(self):
        self.assertEqual(auto(markers=["sha=abc verdict=blocking policy=no"])[0], "wait")
        red = [{"__typename": "CheckRun", "name": "ci", "status": "COMPLETED", "conclusion": "FAILURE"}]
        self.assertEqual(auto(pr(statusCheckRollup=red))[0], "wait")
        self.assertEqual(auto(pr(labels=[{"name": "agent:needs-human"}]))[0], "wait")


RED = [{"__typename": "CheckRun", "name": "Go tests: core", "status": "COMPLETED", "conclusion": "FAILURE",
        "detailsUrl": "https://github.com/o/r/actions/runs/7/job/1"}]
RUNNING = [{"__typename": "CheckRun", "name": "ci", "status": "IN_PROGRESS"}]


def gate(p, card="In review", markers=CLEAN_FREE):
    return pr_gate.decide(p, markers, [], (), ["frontend/x.jsx"], 0, True, card)


class CIFirst(unittest.TestCase):
    """#472: the Dev agent opens drafts and CI runs the full suite."""

    def test_a_failing_suite_never_reaches_ready_to_merge_or_merge(self):
        # Given red CI on an In review PR's head, then it goes back (ci_red),
        # reviewed clean or not, draft or not.
        for p in (pr(statusCheckRollup=RED), pr(statusCheckRollup=RED, isDraft=True)):
            for markers in (CLEAN_FREE, ["sha=abc verdict=clean policy=yes"], []):
                a, why = gate(p, markers=markers)
                self.assertEqual(a, "ci_red", (p, markers))
                self.assertIn("Go tests: core", why)

    def test_red_ci_elsewhere_only_waits(self):
        for card in ("Ready", "Blocked", "In progress", ""):
            self.assertEqual(gate(pr(statusCheckRollup=RED), card)[0], "wait", card)
        self.assertEqual(gate(pr(statusCheckRollup=RED, labels=[{"name": "agent:needs-human"}]))[0], "wait")

    def test_a_green_draft_is_marked_ready_a_running_one_waits(self):
        self.assertEqual(gate(pr(isDraft=True)), ("undraft", "CI green on the draft"))
        a, why = gate(pr(isDraft=True, statusCheckRollup=RUNNING))
        self.assertEqual(a, "wait")
        self.assertIn("still running", why)

    def test_a_draft_off_the_review_column_is_left_alone(self):
        for card in ("Blocked", "Ready", ""):
            self.assertEqual(gate(pr(isDraft=True, mergeable="CONFLICTING"), card)[0], "wait", card)

    def test_the_reviewer_starts_on_a_green_ready_head_only(self):
        self.assertTrue(pr_gate.green(pr()))
        for p in (pr(isDraft=True), pr(statusCheckRollup=RED), pr(statusCheckRollup=RUNNING), pr(statusCheckRollup=[]),
                  pr(state="MERGED")):
            self.assertFalse(pr_gate.green(p), p)
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as f:
            json.dump(pr(isDraft=True), f)
        self.addCleanup(os.unlink, f.name)
        cli = lambda cmd: subprocess.run([sys.executable, os.path.join(os.path.dirname(__file__), "pr_gate.py"), cmd, f.name]).returncode
        self.assertEqual(cli("green"), 1)     # a draft: not for the Reviewer yet
        self.assertEqual(cli("ci-green"), 0)  # but its CI is green


def commit(sha, parents=("abc", "m1"), committer="GitHub", subject="Merge branch 'main' into agent/issue-1"):
    return {"sha": sha, "parents": [{"sha": p} for p in parents],
            "commit": {"committer": {"name": committer}, "message": subject + "\n\nbody"}}


def main_commits():
    """What main brought in: another PR's commit, its merge, a GitHub merge of main into that PR."""
    return [commit("m0", parents=("x",), committer="Roberto Chau", subject="Fix something (#9)"),
            commit("m1", parents=("y", "m0"), subject="Merge pull request #9 from rchau73/agent/issue-9"),
            commit("m2", parents=("m1", "z"), subject="Merge branch 'main' into agent/issue-9")]


class Equiv(unittest.TestCase):
    def test_only_github_update_merges_carry_the_review(self):
        cmp = {"base_commit": {"sha": "abc"}, "commits": [commit("u1", ("abc", "m1")), commit("u2", ("u1", "m2"))]}
        self.assertEqual(pr_gate.equiv(cmp), "abc")

    def test_the_commits_main_brought_in_do_not_count(self):
        # #596, PR #609: one update-branch merge, with main's own commits in the compare.
        cmp = {"base_commit": {"sha": "abc"}, "commits": main_commits() + [commit("u1", ("abc", "m2"))]}
        self.assertEqual(pr_gate.equiv(cmp), "abc")

    def test_anything_else_does_not(self):
        for c in [commit("u2", parents=("u1",), subject="Fix review"), commit("u2", ("u1", "m1"), committer="Roberto Chau"),
                  commit("u2", ("u1", "m1"), subject="Merge remote-tracking branch 'origin/main'")]:
            cmp = {"base_commit": {"sha": "abc"}, "commits": main_commits() + [commit("u1", ("abc", "m2")), c]}
            self.assertIsNone(pr_gate.equiv(cmp))
        self.assertIsNone(pr_gate.equiv({"base_commit": {"sha": "abc"}, "commits": []}))

    def test_a_line_that_never_reaches_base_does_not(self):
        cmp = {"base_commit": {"sha": "abc"}, "commits": [commit("u1", ("other", "m1"))]}
        self.assertIsNone(pr_gate.equiv(cmp))


GATE_MERGE = dict(committer="Roberto Chau", subject="Merge remote-tracking branch 'origin/main' into HEAD")


class GateRegenMerge(unittest.TestCase):
    """#634: the gate's own regenerated-files merge keeps merge:approved and the clean review."""

    def cmp(self, *extra):
        return {"base_commit": {"sha": "abc"}, "commits": main_commits() + [commit("b1", ("abc", "m2"), **GATE_MERGE), *extra]}

    def test_given_an_approved_reviewed_pr_when_the_gate_regenerates_then_b_is_approved_and_reviewed(self):
        self.assertEqual(pr_gate.equiv(self.cmp(), regen=["b1"]), "abc")
        # Both checks read that sha: the clean review on A counts for B, and so does the approval.
        equiv = [pr_gate.equiv(self.cmp(), regen=["b1"])]
        p = pr(headRefOid="b1")
        self.assertEqual(pr_gate.decide(p, ["sha=abc verdict=clean policy=yes"], [], equiv, ["internal/strategy/x.go"],
                                        0, False, "In review", (), True)[0], "merge")
        # Mixed with GitHub's own update merges, in either order.
        self.assertEqual(pr_gate.equiv(self.cmp(commit("u2", ("b1", "m2"))), regen=["b1"]), "abc")

    def test_an_unrecorded_merge_still_voids(self):
        # A person's or an agent's own merge of main: not recorded by the gate.
        self.assertIsNone(pr_gate.equiv(self.cmp()))
        self.assertIsNone(pr_gate.equiv(self.cmp(), regen=["other"]))

    def test_a_recorded_sha_must_still_be_a_merge(self):
        cmp = {"base_commit": {"sha": "abc"}, "commits": [commit("b1", ("abc",), **GATE_MERGE)]}
        self.assertIsNone(pr_gate.equiv(cmp, regen=["b1"]))

    def test_a_commit_after_it_still_voids(self):
        self.assertIsNone(pr_gate.equiv(self.cmp(commit("c1", ("b1",), committer="t", subject="Fix")), regen=["b1"]))

    def test_cli_reads_the_gate_state(self):
        with tempfile.TemporaryDirectory() as d:
            paths = {"cmp": os.path.join(d, "cmp.json"), "state": os.path.join(d, "pr_gate.json")}
            for name, data in (("cmp", self.cmp()), ("state", {"7:regen": "old b1"})):
                with open(paths[name], "w") as f:
                    json.dump(data, f)
            run = lambda *a: subprocess.run([sys.executable, os.path.join(os.path.dirname(__file__), "pr_gate.py"), "equiv", *a],
                                            capture_output=True, text=True).stdout.strip()
            self.assertEqual(run(paths["cmp"], paths["state"], "7"), "abc")
            self.assertEqual(run(paths["cmp"], paths["state"], "8"), "")
            self.assertEqual(run(paths["cmp"]), "")
            self.assertEqual(run(paths["cmp"], os.path.join(d, "missing.json"), "7"), "")

    def test_only_generated_files_and_sorted_lists_are_settled(self):
        gen = ["docs/parameters.md", "frontend/content/help-index.json"]
        self.assertTrue(pr_gate.only_settled([], gen))
        self.assertTrue(pr_gate.only_settled(["docs/parameters.md", "docs/product/assistant/golden.jsonl"], gen))
        self.assertFalse(pr_gate.only_settled(["docs/parameters.md", "frontend/content/glossary.json"], gen))

    def test_sorted_lists_agree_with_regen_sh(self):
        root = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
        with open(os.path.join(root, "scripts", "regen.sh")) as f:
            text = f.read()
        line = next(l for l in text.splitlines() if l.startswith("SORTED_LISTS="))
        self.assertEqual(tuple(line.split("=", 1)[1].strip('"').split()), pr_gate.SORTED_LISTS)


GIT_ENV = {**os.environ, "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@x", "GIT_COMMITTER_NAME": "t",
           "GIT_COMMITTER_EMAIL": "t@x", "GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1"}


class Settled(unittest.TestCase):
    """pr_gate.py settled on a throwaway repo: a merge whose conflict was only in a generated file."""

    def git(self, *args, check=True):
        return subprocess.run(["git", *args], cwd=self.repo, env=GIT_ENV, check=check, capture_output=True, text=True)

    def write(self, path, text):
        with open(os.path.join(self.repo, path), "w") as f:
            f.write(text)

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.repo = self.tmp.name
        self.git("init", "-q", "-b", "main")
        self.write(".gitattributes", "gen.json linguist-generated=true\n")
        for f in ("gen.json", "src.txt", "other.txt"):
            self.write(f, "base\n")
        self.git("add", "-A")
        self.git("commit", "-q", "-m", "base")
        self.git("checkout", "-q", "-b", "pr")
        self.write("gen.json", "pr\n")
        self.write("src.txt", "pr\n")
        self.git("commit", "-q", "-am", "pr change")
        self.git("checkout", "-q", "main")
        self.write("gen.json", "main\n")
        self.write("other.txt", "main\n")
        self.git("commit", "-q", "-am", "main change")
        self.git("checkout", "-q", "pr")
        self.assertNotEqual(self.git("merge", "--no-edit", "-q", "main", check=False).returncode, 0)

    def settled(self):
        return subprocess.run([sys.executable, os.path.join(os.path.dirname(__file__), "pr_gate.py"), "settled", self.repo],
                              env=GIT_ENV).returncode

    def test_a_merge_that_settles_only_the_generated_file_is_settled(self):
        self.write("gen.json", "regenerated\n")
        self.git("add", "gen.json")
        self.git("commit", "-q", "--no-edit")
        self.assertEqual(self.settled(), 0)

    def test_a_merge_that_changes_any_other_file_is_not(self):
        self.write("gen.json", "regenerated\n")
        self.write("src.txt", "edited during the merge\n")
        self.git("add", "gen.json", "src.txt")
        self.git("commit", "-q", "--no-edit")
        self.assertEqual(self.settled(), 1)

    def test_a_plain_commit_is_not(self):
        self.git("merge", "--abort")
        self.write("src.txt", "plain\n")
        self.git("commit", "-q", "-am", "plain")
        self.assertEqual(self.settled(), 1)


def approved(p=None, markers=CLEAN, files=("internal/strategy/open.go",), behind=0, on=False):
    return pr_gate.decide(p or pr(), markers, [], (), list(files), behind, on, "In review", (), True)


class Approved(unittest.TestCase):
    """#596: a person labels a ready policy PR merge:approved; the gate merges it in turn."""

    def test_an_approved_policy_pr_merges_even_with_auto_merge_off(self):
        a, why = approved()
        self.assertEqual(a, "merge")
        self.assertIn("merge:approved", why)

    def test_behind_main_it_is_updated_first(self):
        self.assertEqual(approved(behind=2)[0], "update")

    def test_approval_never_skips_review_ci_or_a_conflict(self):
        self.assertEqual(approved(markers=["sha=abc verdict=blocking policy=yes"])[0], "wait")
        self.assertEqual(approved(pr(headRefOid="new"))[0], "wait")
        running = [{"__typename": "CheckRun", "name": "ci", "status": "IN_PROGRESS"}]
        self.assertEqual(approved(pr(statusCheckRollup=running))[0], "wait")
        self.assertEqual(approved(pr(mergeable="CONFLICTING", mergeStateStatus="DIRTY"))[0], "conflict")
        self.assertEqual(approved(pr(labels=[{"name": "agent:needs-human"}]))[0], "wait")

    def test_main_reads_removed_paths_and_the_approval(self):
        with tempfile.TemporaryDirectory() as d:
            paths = {}
            for name, text in {"pr": json.dumps(pr()), "markers": CLEAN_FREE[0], "deps": "", "equiv": "",
                               "files": "docs/product/assistant/golden.jsonl", "removed": "docs/product/assistant/golden.jsonl"}.items():
                paths[name] = os.path.join(d, name)
                open(paths[name], "w").write(text)
            args = [sys.executable, os.path.join(os.path.dirname(__file__), "pr_gate.py"), "decide",
                    paths["pr"], paths["markers"], paths["deps"], paths["equiv"], paths["files"], "0", "1", "In review", paths["removed"]]
            run = lambda *extra: subprocess.run(args + list(extra), capture_output=True, text=True, check=True).stdout.strip()
            self.assertTrue(run("0").startswith("ready"))
            self.assertTrue(run("1").startswith("merge"))
