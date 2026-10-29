"""Tests for checkfail.py (#472): suites by what they read, touched tests,
failures and their log tails, the CI-red plan and the failure count."""
import json
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(os.path.dirname(HERE))
sys.path.insert(0, HERE)
import checkfail  # noqa: E402


def write(root, rel, text=""):
    path = os.path.join(root, rel)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        f.write(text)


class Suites(unittest.TestCase):
    def check(self, files, want):
        self.assertEqual(checkfail.suites(files), set(want), files)

    def test_a_claude_md_only_change_runs_only_the_doc_checks(self):
        self.check(["CLAUDE.md", "frontend/CLAUDE.md", "internal/strategy/CLAUDE.md"], ["docs"])

    def test_docs_no_test_reads_run_only_the_doc_checks_images_nothing(self):
        self.check(["docs/README.md", "frontend/e2e/README.md"], ["docs"])
        self.check(["docs/seq_entry.mmd", "docs/seq_entry.png"], [])

    def test_frontend_code_runs_the_frontend_and_docker_not_go(self):
        self.check(["frontend/components/StatTile.jsx"], ["frontend", "docker"])

    def test_frontend_files_the_go_tests_read_run_go_too(self):
        for f in ("frontend/lib/menu.js", "frontend/app/api/x/route.js", "frontend/content/glossary.json"):
            self.assertIn("go", checkfail.suites_for(f), f)
            self.assertIn("frontend", checkfail.suites_for(f), f)

    def test_docs_each_suite_reads(self):
        self.check(["frontend/README.md"], ["go", "frontend", "docs"])
        self.check(["docs/product/PRD.md"], ["go", "frontend", "scripts", "docs"])
        self.check(["OptionStrategy.md"], ["go", "frontend", "docs"])
        self.check(["docs/operations.md"], ["go", "docs"])

    def test_go_code_runs_go_lint_scripts_and_docker(self):
        self.check(["internal/strategy/entry.go"], ["go", "go_lint", "scripts", "docker"])

    def test_a_scripts_only_change_skips_go_vet_and_docker(self):
        self.check(["scripts/agents/tick.sh"], ["go", "scripts"])

    def test_vulnerability_scan_only_on_dependency_files(self):
        self.assertIn("vuln", checkfail.suites(["go.sum"]))
        self.assertIn("vuln", checkfail.suites(["frontend/package-lock.json"]))
        self.assertNotIn("vuln", checkfail.suites(["internal/x/a.go", "frontend/lib/a.js"]))

    def test_cli_prints_github_output_lines(self):
        with tempfile.NamedTemporaryFile("w", suffix=".txt", delete=False) as f:
            f.write("CLAUDE.md\n")
        out = subprocess.run([sys.executable, os.path.join(HERE, "checkfail.py"), "suites", f.name],
                             capture_output=True, text=True, check=True).stdout
        os.unlink(f.name)
        self.assertEqual(out.splitlines(), [f"{s}={'true' if s == 'docs' else 'false'}" for s in checkfail.SUITES])


class Honest(unittest.TestCase):
    """Every path a test opens triggers its suite: a new test that reads a doc
    cannot be skipped silently by the path filter."""

    def samples(self, read):
        path = os.path.join(REPO, read)
        if os.path.isfile(path):
            return [read]
        return [os.path.relpath(os.path.join(d, x), REPO) for d, _, xs in os.walk(path) if "node_modules" not in d for x in xs]

    def missing(self, kind, folder, reads_of, suite):
        out = set()
        for name in sorted(os.listdir(os.path.join(REPO, folder))):
            rel = os.path.join(folder, name)
            if not os.path.isfile(os.path.join(REPO, rel)):
                continue
            for read in reads_of(REPO, rel):
                for s in self.samples(read):
                    if os.path.basename(s) == checkfail.CLAUDE:
                        want = "docs" if kind == "go" else None
                    else:
                        want = suite
                    if want and want not in checkfail.suites_for(s):
                        out.add(f"{rel} opens {read} ({s})")
        return sorted(out)

    def test_every_path_the_go_tests_open_runs_them(self):
        self.assertEqual(self.missing("go", "tests", checkfail.go_reads, "go"), [])

    def test_every_path_the_frontend_tests_open_runs_them(self):
        miss = []
        for folder in ("frontend/__tests__", "frontend/test"):
            miss += self.missing("jest", folder, checkfail.js_reads, "frontend")
        self.assertEqual(miss, [])

    def test_the_scan_finds_the_known_reads(self):
        self.assertIn("frontend/lib/menu.js", checkfail.go_reads(REPO, "tests/monitor_guide_test.go"))
        self.assertIn("docs/product/assistant/golden.jsonl", checkfail.js_reads(REPO, "frontend/__tests__/assistant-guard.test.js"))
        self.assertIn("internal/api", checkfail.js_reads(REPO, "frontend/__tests__/role-matrix.test.js"))
        self.assertIn("frontend", checkfail.suites_for("internal/api/api.go"))

    def test_the_node_pins_run_the_go_tests(self):
        # tests/ci_workflow_test.go keeps the Dockerfile, ci.yml and package.json on one Node major (#522);
        # its package.json read is a local helper the scan does not follow.
        for p in ("frontend/Dockerfile", "frontend/package.json", ".github/workflows/ci.yml"):
            self.assertIn("go", checkfail.suites_for(p), p)


class Touched(unittest.TestCase):
    def setUp(self):
        self.repo = tempfile.mkdtemp()
        write(self.repo, "go.mod", "module optionsbot\n")
        write(self.repo, "internal/a/a.go", 'package a\nimport "optionsbot/internal/b"\nvar X = b.Y\n')
        write(self.repo, "internal/b/b.go", "package b\nvar Y = 1\n")
        write(self.repo, "internal/c/c.go", "package c\n")
        write(self.repo, "docs/guide.md", "# guide\n")
        write(self.repo, "tests/a_test.go",
              'package tests\nimport "optionsbot/internal/a"\nfunc TestA(t *testing.T) { _ = a.X; fixture() }\n')
        write(self.repo, "tests/doc_test.go",
              'package tests\nfunc TestDoc(t *testing.T) { os.ReadFile(filepath.Join("..", "docs", "guide.md")) }\n'
              "func TestEvidenceX(t *testing.T) {}\n")
        write(self.repo, "tests/helpers_test.go", "package tests\nfunc fixture() {}\n")
        write(self.repo, "frontend/lib/x.js", "export const x = 1;\n")
        write(self.repo, "frontend/lib/y.js", "export const y = 1;\n")
        write(self.repo, "frontend/__tests__/x.test.js", 'import { x } from "@/lib/x";\n')

    def test_imports_transitively(self):
        self.assertTrue(checkfail.touched(self.repo, "go", "TestA", ["internal/b/b.go"]))
        self.assertFalse(checkfail.touched(self.repo, "go", "TestA", ["internal/c/c.go"]))

    def test_a_doc_the_test_opens(self):
        self.assertTrue(checkfail.touched(self.repo, "go", "TestDoc", ["docs/guide.md"]))
        self.assertFalse(checkfail.touched(self.repo, "go", "TestA", ["docs/guide.md"]))

    def test_a_helper_it_names_or_its_own_file(self):
        self.assertTrue(checkfail.touched(self.repo, "go", "TestA", ["tests/helpers_test.go"]))
        self.assertFalse(checkfail.touched(self.repo, "go", "TestDoc", ["tests/helpers_test.go"]))
        self.assertTrue(checkfail.touched(self.repo, "go", "TestDoc", ["tests/doc_test.go"]))

    def test_an_unknown_test_counts_as_touched(self):
        self.assertTrue(checkfail.touched(self.repo, "go", "TestNowhere", ["scripts/x.sh"]))
        self.assertTrue(checkfail.touched(self.repo, "jest", "__tests__/gone.test.js", ["scripts/x.sh"]))

    def test_jest_follows_imports(self):
        self.assertTrue(checkfail.touched(self.repo, "jest", "__tests__/x.test.js", ["frontend/lib/x.js"]))
        self.assertFalse(checkfail.touched(self.repo, "jest", "__tests__/x.test.js", ["frontend/lib/y.js"]))

    def test_go_tests_selects_and_splits_by_the_report_regex(self):
        core, reports = checkfail.go_tests(self.repo, ["docs/guide.md"], "^(TestEvidence)")
        self.assertEqual((core, reports), (["TestDoc"], ["TestEvidenceX"]))
        self.assertEqual(checkfail.go_tests(self.repo, ["internal/b/b.go"], "")[0], ["TestA"])
        self.assertEqual(checkfail.go_tests(self.repo, ["scripts/x.sh"], ""), ([], []))
        self.assertEqual(checkfail.go_tests(self.repo, ["go.sum"], ""), "all")
        self.assertEqual(checkfail.run_regex(["TestA", "TestB"]), "^(TestA|TestB)$")
        self.assertEqual(checkfail.run_regex([]), "")

    def test_a_doc_selects_the_doc_wide_tests(self):
        write(self.repo, "tests/claude_docs_test.go", 'package tests\nfunc TestClaudeDocs_X(t *testing.T) {}\n')
        write(self.repo, "tests/docs_line_length_test.go", 'package tests\nfunc TestDocs_Y(t *testing.T) {}\n')
        self.assertEqual(checkfail.go_tests(self.repo, ["internal/b/CLAUDE.md"], ""), (["TestClaudeDocs_X", "TestDocs_Y"], []))
        self.assertEqual(checkfail.go_tests(self.repo, ["docs/other.md"], ""), (["TestDocs_Y"], []))
        self.assertRegex("TestDocs_Y", checkfail.DOC_TESTS_RUN)
        self.assertRegex("TestClaudeDocs_X", checkfail.DOC_TESTS_RUN)

    def test_jest_files_related_or_all(self):
        self.assertEqual(checkfail.jest_files(["frontend/lib/x.js", "internal/a/a.go"]), ["lib/x.js"])
        for f in ("frontend/README.md", "docs/product/PRD.md", "frontend/package.json", "frontend/content/glossary.json"):
            self.assertEqual(checkfail.jest_files(["frontend/lib/x.js", f]), "all", f)


GO_LOG = """=== RUN skipped
--- FAIL: TestFreeze (0.31s)
    strategy_run_test.go:88: want 0 working entries, got 1
    --- FAIL: TestFreeze/sub (0.00s)
        x_test.go:9: expected 2, received 3
--- PASS: TestOther (0.00s)
FAIL
FAIL\toptionsbot/tests\t12.3s
"""
JEST_LOG = """PASS __tests__/ok.test.js
FAIL __tests__/menu.test.js
  ● menu › lists every page
    Expected: 3
    Received: 2
PASS __tests__/later.test.js
Test Suites: 1 failed
"""


def ci(job, text):
    return "\n".join(f"{job}\tTests\t2026-10-29T05:00:00.1234567Z {l}" for l in text.splitlines())


class Failures(unittest.TestCase):
    def test_top_level_go_tests_and_jest_files(self):
        self.assertEqual(checkfail.failures(GO_LOG + JEST_LOG), [("go", "TestFreeze"), ("jest", "__tests__/menu.test.js")])

    def test_github_log_prefix_is_stripped(self):
        self.assertEqual(checkfail.failures(ci("Go tests: core", GO_LOG)), [("go", "TestFreeze")])
        self.assertEqual(checkfail.job_of(ci("Go tests: core", "x")), "Go tests: core")

    def test_tail_holds_the_test_s_own_lines(self):
        t = checkfail.tail_of(GO_LOG, "go", "TestFreeze")
        self.assertEqual(t[0], "--- FAIL: TestFreeze (0.31s)")
        self.assertIn("        x_test.go:9: expected 2, received 3", t)
        self.assertNotIn("--- PASS: TestOther (0.00s)", t)
        j = checkfail.tail_of(JEST_LOG, "jest", "__tests__/menu.test.js")
        self.assertEqual(j[-1], "    Received: 2")
        self.assertEqual(len(checkfail.tail_of("--- FAIL: TestX\n" + "    l\n" * 100, "go", "TestX")), 40)

    def test_tails_falls_back_to_the_log_end(self):
        self.assertIn("gofmt: a.go", checkfail.tails("ok\ngofmt: a.go\n"))
        self.assertIn("`TestFreeze`:", checkfail.tails(GO_LOG))


class Plans(unittest.TestCase):
    def setUp(self):
        self.repo = tempfile.mkdtemp()
        write(self.repo, "tests/freeze_test.go", "package tests\nfunc TestFreeze(t *testing.T) {}\n")

    def test_untouched_failure_is_retried_touched_never(self):
        log = ci("Go tests: core", GO_LOG)
        self.assertEqual(checkfail.retry_list(self.repo, ["scripts/x.sh"], GO_LOG), [("go", "TestFreeze")])
        self.assertIsNone(checkfail.retry_list(self.repo, ["tests/freeze_test.go"], GO_LOG))
        self.assertIsNone(checkfail.retry_list(self.repo, ["scripts/x.sh"], "gofmt: a.go\n"))
        checks = ["Go tests: core", checkfail.AGGREGATOR]
        self.assertEqual(checkfail.ci_plan(self.repo, ["scripts/x.sh"], 0, checks, log), ("rerun", "TestFreeze"))
        self.assertEqual(checkfail.ci_plan(self.repo, ["tests/freeze_test.go"], 0, checks, log)[0], "send_back")

    def test_three_reruns_then_it_is_a_real_failure(self):
        log = ci("Go tests: core", GO_LOG)
        self.assertEqual(checkfail.ci_plan(self.repo, ["scripts/x.sh"], 2, ["Go tests: core"], log)[0], "rerun")
        action, why = checkfail.ci_plan(self.repo, ["scripts/x.sh"], 3, ["Go tests: core"], log)
        self.assertEqual(action, "send_back")
        self.assertIn("after 3 re-runs", why)

    def test_a_job_failing_outside_a_test_is_sent_back(self):
        log = ci("Go tests: core", GO_LOG) + "\n" + ci("Go lint", "Run gofmt on: a.go")
        self.assertEqual(checkfail.ci_plan(self.repo, ["x"], 0, ["Go tests: core", "Go lint"], log),
                         ("send_back", "Go lint failed outside a test"))
        self.assertEqual(checkfail.ci_plan(self.repo, ["x"], 0, [checkfail.AGGREGATOR], "")[0], "send_back")

    def test_failed_checks_read_the_run_id(self):
        rollup = [
            {"name": "Go tests: core", "status": "COMPLETED", "conclusion": "FAILURE",
             "detailsUrl": "https://github.com/o/r/actions/runs/123/job/9"},
            {"name": "Docker build", "status": "COMPLETED", "conclusion": "SKIPPED"},
            {"name": "Frontend", "status": "IN_PROGRESS"},
            {"__typename": "StatusContext", "context": "ext", "state": "ERROR", "targetUrl": ""},
        ]
        self.assertEqual(checkfail.failed_checks(rollup), [("123", "Go tests: core"), ("", "ext")])
        self.assertEqual(checkfail.failed_checks(None), [])


def history(*bodies):
    return {"comments": [{"body": b} for b in bodies]}


class Count(unittest.TestCase):
    def test_first_failure_goes_back_to_the_agent_the_second_to_a_person(self):
        self.assertEqual(checkfail.next_step(checkfail.count(history("pickup"))), "needs-fix")
        one = history(checkfail.marker(1))
        self.assertEqual(checkfail.next_step(checkfail.count(one)), "needs-human")

    def test_a_pass_does_not_reset_but_a_person_does(self):
        h = history(checkfail.marker(1), checkfail.PASS_MARKER, checkfail.marker(2, "ci", "abc"))
        self.assertEqual(checkfail.count(h), 2)
        h = history(checkfail.marker(1), "🤖 **Dev agent stopped — a person is needed.**", "pickup")
        self.assertEqual(checkfail.count(h), 0)
        self.assertEqual(checkfail.count(history(checkfail.marker(1), "<!-- dev:blocked -->", checkfail.marker(1))), 1)

    def test_marker_parses_back(self):
        m = checkfail.marker(2, "ci", "abc1234")
        self.assertEqual(m, "<!-- dev:check-fail n=2 source=ci sha=abc1234 -->")
        self.assertRegex(m, checkfail.MARKER)

    def test_pending_is_the_last_failure_until_a_pass(self):
        fail = "checks failed\n" + checkfail.marker(1)
        self.assertEqual(checkfail.pending(history("x", fail, "pickup")), fail)
        self.assertEqual(checkfail.pending(history(fail, "opened " + checkfail.PASS_MARKER)), "")

    def test_cli_count_and_next(self):
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as f:
            json.dump(history(checkfail.marker(1)), f)
        run = lambda *a: subprocess.run([sys.executable, os.path.join(HERE, "checkfail.py"), *a],
                                        capture_output=True, text=True, check=True).stdout.strip()
        self.assertEqual(run("count", f.name), "1")
        self.assertEqual(run("next", "1"), "needs-human")
        os.unlink(f.name)


FAKE_GH = r"""#!/usr/bin/env bash
# A fake gh: logs each call; --body-file contents are appended to the log too.
echo "gh $*" >> "$FAKE/gh.log"
prev=""
for a in "$@"; do [[ "$prev" == "--body-file" ]] && { cat "$a" >> "$FAKE/gh.log"; echo >> "$FAKE/gh.log"; }; prev="$a"; done
case "$1 $2" in
  "repo view") echo "o/r" ;;
  "issue view") cat "$FAKE/history.json" ;;
  "issue list") cat "$FAKE/issues.json" 2>/dev/null || echo "[]" ;;
  "issue create") echo "https://github.com/o/r/issues/77" ;;
  "issue comment"|"issue edit"|"pr comment") ;;
  "api graphql") cat "$FAKE/graphql.json" ;;
  *) exit 1 ;;
esac
"""


class LibHelpers(unittest.TestCase):
    """lib.sh check_failed and note_flaky against a fake gh (#472)."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        write(self.tmp, "bin/gh", FAKE_GH)
        os.chmod(os.path.join(self.tmp, "bin", "gh"), 0o755)
        os.makedirs(os.path.join(self.tmp, "envs"))
        write(self.tmp, "body.md", "CI failed: TestFreeze\n")
        self.env = dict(os.environ, PATH=os.path.join(self.tmp, "bin") + os.pathsep + os.environ["PATH"], FAKE=self.tmp,
                        AGENT_STATE_DIR=os.path.join(self.tmp, "state"), AGENT_WORK_DIR=os.path.join(self.tmp, "work"),
                        OPTIONSBOT_ENVS=os.path.join(self.tmp, "envs"), AGENT_NOTIFY="0", MONITOR_TOKEN="x", MVP_MONITOR_TOKEN="x")

    def bash(self, code):
        p = subprocess.run(["bash", "-c", f'source "{HERE}/lib.sh"; {code}'], env=self.env,
                           capture_output=True, text=True, timeout=60)
        self.assertEqual(p.returncode, 0, p.stderr)
        return p.stdout.strip()

    def gh_log(self):
        with open(os.path.join(self.tmp, "gh.log")) as f:
            return f.read()

    def history(self, *bodies):
        write(self.tmp, "history.json", json.dumps(history(*bodies)))

    def test_board_json_carries_each_cards_files_and_deps_not_its_body(self):
        """#467: pm_board.py ready's overlap rule reads them; the PM agent's board.json stays small."""
        issue = {"id": "N1", "number": 446, "title": "NPS reasons", "state": "OPEN", "updatedAt": "2026-10-28T10:00:00Z",
                 "body": "Story.\n\n**Files:** `internal/usage/report.go`, `why_best`\n\nDepends on #445",
                 "labels": {"nodes": [{"name": "E10"}]}, "parent": None,
                 "projectItems": {"nodes": [{"id": "I1", "project": {"number": 5}, "status": {"name": "Ready"}, "priority": None}]}}
        write(self.tmp, "graphql.json", json.dumps({"data": {"repository": {"issues": {"nodes": [issue]}}}}))
        got = json.loads(self.bash('project_info() { echo "5 P"; }; board_json'))
        self.assertEqual(len(got), 1)
        self.assertEqual(got[0]["files"], ["internal/usage/report.go", "why_best"])
        self.assertEqual(got[0]["deps"], [445])
        self.assertNotIn("body", got[0])

    def test_first_check_failure_goes_back_to_ready_with_needs_fix_and_the_log_tail(self):
        self.history("🤖 Dev agent picked this up")
        self.assertEqual(self.bash(f'check_failed 5 "" "{self.tmp}/body.md" ci abc1234'), "needs-fix")
        log = self.gh_log()
        self.assertIn("--add-label needs-fix", log)
        self.assertNotIn("agent:needs-human", log)
        self.assertIn("CI failed: TestFreeze", log)
        self.assertIn("<!-- dev:check-fail n=1 source=ci sha=abc1234 -->", log)

    def test_second_check_failure_is_left_for_a_person(self):
        self.history(checkfail.marker(1))
        self.assertEqual(self.bash(f'check_failed 5 "" "{self.tmp}/body.md" dev'), "needs-human")
        self.assertNotIn("issue comment", self.gh_log())  # the caller blocks it, as before

    def test_a_flaky_test_is_named_on_the_pr_and_its_issue(self):
        write(self.tmp, "issues.json", json.dumps([{"number": 12, "title": "[E6] Flaky test: TestFreeze", "state": "OPEN"}]))
        self.bash('note_flaky TestFreeze 40 "CI"')
        log = self.gh_log()
        self.assertIn("gh issue comment 12", log)
        self.assertIn("gh pr comment 40", log)
        self.assertIn("flaky** on #12", log)
        self.assertNotIn("issue create", log)

    def test_a_new_flaky_test_files_its_issue(self):
        self.bash('note_flaky TestNew 40 "CI"')
        log = self.gh_log()
        self.assertIn("gh issue create -R o/r --label bug --title [E6] Flaky test: TestNew", log)
        self.assertIn("flaky** on #77", log)


class RepoWide(unittest.TestCase):
    """#467: a frontend change runs the checks that read every file before handing over."""

    def test_a_frontend_change_lists_the_repo_wide_checks(self):
        got = checkfail.repo_wide(["frontend/components/Kpi.jsx"])
        self.assertEqual(got, ["jest __tests__/empty-states.test.js", "jest __tests__/glossary.test.js",
                               "jest __tests__/menu.test.jsx", "jest __tests__/responsive-audit.test.js"])
        for t in checkfail.REPO_WIDE_JEST:
            self.assertTrue(os.path.exists(os.path.join(REPO, "frontend", t)), t)

    def test_a_help_source_needs_the_help_index(self):
        self.assertEqual(checkfail.repo_wide(["internal/api/explain.go"]), ["helpindex"])
        for f in checkfail.HELP_SOURCES:
            self.assertIn("helpindex", checkfail.repo_wide([f]), f)
            self.assertTrue(os.path.exists(os.path.join(REPO, f)), f)

    def test_a_backend_change_needs_none(self):
        self.assertEqual(checkfail.repo_wide(["internal/strategy/entry.go", "scripts/agents/lib.sh"]), [])

    def test_the_agent_prompt_lists_the_checks_and_the_last_merge(self):
        with open(os.path.join(HERE, "dev_agent.sh"), encoding="utf-8") as f:
            src = f.read()
        prompt = src[src.index('PROMPT="'):src.index('End your answer with the exact RESULT')]
        for t in checkfail.REPO_WIDE_JEST:
            self.assertIn(t, prompt)
        self.assertIn("go run ./cmd/helpindex", prompt)
        self.assertIn("git merge --no-edit origin/main", prompt)
        self.assertIn('"Bash(git merge:*)"', src)
        self.assertIn('"$CHECKFAIL" repo-wide', src)  # and the script runs them itself


class DevScript(unittest.TestCase):
    def test_related_jest_run_passes_when_no_test_relates_to_the_change(self):
        # jest exits 1 on "No tests found" (a page or globals.css no test imports):
        # without the flag a valid frontend change would fail the Dev script's check.
        with open(os.path.join(HERE, "dev_agent.sh"), encoding="utf-8") as f:
            related = [line for line in f if "--findRelatedTests" in line]
        self.assertTrue(related)
        for line in related:
            self.assertIn("--passWithNoTests", line)


if __name__ == "__main__":
    unittest.main()
