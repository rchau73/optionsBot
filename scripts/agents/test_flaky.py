"""Tests of the nightly flaky-test hunt (#248): flaky.py's pure part, and
flaky.sh end to end against a throwaway origin with stub go/npm/gh.
python3 -m unittest discover -s scripts/agents -p 'test_*.py'"""
import datetime as dt
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import flaky  # noqa: E402

SCRIPTS = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SHA = "c" * 40
NOW = dt.datetime(2026, 10, 23, 4, 0, tzinfo=dt.timezone.utc)
PKG = "github.com/rchau73/optionsBot/tests"
# Fake values, low-entropy on purpose: CI's gitleaks must not take them for keys.
ENV = "DERIBIT_CLIENT_SECRET=secretsecret\nMONITOR_API_TOKEN=tokentoken\nDERIBIT_ENV=testnet\n"


def go_run(test, ok, output="", pkg=PKG):
    """The go test -json lines of one run of a test."""
    lines = [{"Action": "run", "Package": pkg, "Test": test}]
    lines += [{"Action": "output", "Package": pkg, "Test": test, "Output": o + "\n"} for o in output.splitlines()]
    lines.append({"Action": "pass" if ok else "fail", "Package": pkg, "Test": test, "Elapsed": 0.01})
    return [json.dumps(l) for l in lines]


def go_night(outcomes):
    """outcomes: test → list of (ok, output) per run, interleaved like -count."""
    lines = [json.dumps({"Action": "start", "Package": PKG})]
    for i in range(max(len(v) for v in outcomes.values())):
        for test, runs in outcomes.items():
            if i < len(runs):
                lines += go_run(test, *runs[i])
    lines.append(json.dumps({"Action": "fail", "Package": PKG, "Elapsed": 9}))
    return lines


def jest_report(results, suite_status=None, message=""):
    """results: fullName → "passed" | "failed"."""
    status = suite_status or ("failed" if "failed" in results.values() else "passed")
    return {"testResults": [{
        "name": "/tmp/wt/frontend/__tests__/api.test.js", "status": status, "message": message,
        "assertionResults": [{"fullName": n, "status": s, "failureMessages": ["expected 1, got 2"] if s == "failed" else []}
                             for n, s in results.items()]}]}


FLAKY_GO = {
    "TestFlaky": [(True, "")] * 3 + [(False, "flaky_test.go:12: boom secret=secretsecret auth Bearer tokentoken")] + [(True, "")],
    "TestBroken": [(False, "broken_test.go:3: always")] * 5,
    "TestOK": [(True, "")] * 5,
}


class Parse(unittest.TestCase):
    def test_go_counts_passes_failures_and_the_first_failure_output(self):
        r = flaky.parse_go(go_night(FLAKY_GO), {})
        self.assertEqual((r["TestFlaky"].passes, r["TestFlaky"].fails), (4, 1))
        self.assertIn("boom", r["TestFlaky"].output)
        self.assertEqual((r["TestBroken"].passes, r["TestBroken"].fails), (0, 5))
        self.assertEqual((r["TestOK"].passes, r["TestOK"].fails), (5, 0))
        self.assertEqual(r["TestOK"].where, PKG)

    def test_go_output_belongs_to_the_failing_run_only(self):
        r = flaky.parse_go(go_night({"TestX": [(True, "noise of a passing run"), (False, "the failure")]}), {})
        self.assertEqual(r["TestX"].output.strip(), "the failure")

    def test_go_ignores_non_json_and_package_lines(self):
        r = flaky.parse_go(["# build output", "", "{not json"] + go_night({"TestX": [(True, "")]}), {})
        self.assertEqual(list(r), ["TestX"])

    def test_jest_counts_each_test_across_reports(self):
        res = {}
        for outcome in ("passed", "failed", "passed"):
            flaky.parse_jest(jest_report({"api › reads status": outcome, "api › stable": "passed"}), res)
        name = "__tests__/api.test.js › api › reads status"
        self.assertEqual((res[name].passes, res[name].fails), (2, 1))
        self.assertIn("expected 1, got 2", res[name].output)
        self.assertEqual(res["__tests__/api.test.js › api › stable"].passes, 3)
        # The suite is not flaky because one of its tests failed.
        self.assertFalse(res["__tests__/api.test.js"].flaky)

    def test_jest_suite_that_did_not_load_is_a_failure_of_the_suite(self):
        res = {}
        flaky.parse_jest(jest_report({"api › a": "passed"}), res)
        flaky.parse_jest({"testResults": [{"name": "/x/frontend/__tests__/api.test.js", "status": "failed",
                                           "message": "Cannot find module", "assertionResults": []}]}, res)
        self.assertTrue(res["__tests__/api.test.js"].flaky)
        self.assertIn("Cannot find module", res["__tests__/api.test.js"].output)


class Classify(unittest.TestCase):
    def test_flaky_and_always_failing(self):
        flaky_, broken = flaky.classify(flaky.parse_go(go_night(FLAKY_GO), {}))
        self.assertEqual([r.name for r in flaky_], ["TestFlaky"])
        self.assertEqual([r.name for r in broken], ["TestBroken"])

    def test_a_flaky_subtest_is_filed_not_its_parent(self):
        night = go_night({"TestP/sub": [(True, ""), (False, "x")], "TestP": [(True, ""), (False, "")]})
        self.assertEqual([r.name for r in flaky.classify(flaky.parse_go(night, {}))[0]], ["TestP/sub"])


class Find(unittest.TestCase):
    def test_scenario_an_open_issue_already_names_that_test(self):
        issues = [{"number": 5, "title": "[E6] Flaky test: TestFlaky", "state": "OPEN"}]
        self.assertEqual(flaky.find("TestFlaky", issues, NOW), 5)

    def test_another_epic_prefix_still_names_it(self):
        self.assertEqual(flaky.find("TestFlaky", [{"number": 6, "title": "[E3] Flaky test: TestFlaky", "state": "OPEN"}], NOW), 6)

    def test_closed_within_14_days_is_a_duplicate_older_is_not(self):
        recent = {"number": 7, "title": "[E6] Flaky test: TestFlaky", "state": "CLOSED", "closedAt": "2026-10-15T00:00:00Z"}
        old = dict(recent, number=8, closedAt="2026-10-01T00:00:00Z")
        self.assertEqual(flaky.find("TestFlaky", [old, recent], NOW), 7)
        self.assertIsNone(flaky.find("TestFlaky", [old], NOW))

    def test_a_longer_name_is_another_test(self):
        issues = [{"number": 5, "title": "[E6] Flaky test: TestFlakyToo", "state": "OPEN"},
                  {"number": 9, "title": "[E6] Flaky test: TestFlaky/sub", "state": "OPEN"}]
        self.assertIsNone(flaky.find("TestFlaky", issues, NOW))


class Plan(unittest.TestCase):
    def setUp(self):
        self.results = flaky.parse_go(go_night(FLAKY_GO), {})
        self.pairs = flaky.deploy_issue.secrets(ENV)

    def test_scenario_passed_4_failed_once_files_one_issue_with_counts_and_output(self):
        lines, filings = flaky.plan(self.results, [], SHA, self.pairs, NOW)
        self.assertEqual(len(filings), 1)
        title, body = filings[0]
        self.assertEqual(title, "[E6] Flaky test: TestFlaky")
        self.assertIn("passed 4 and failed 1 of 5 runs", body)
        self.assertIn("**Passed:** 4 · **Failed:** 1", body)
        self.assertIn(f"`{SHA}`", body)
        self.assertIn("flaky_test.go:12: boom", body)
        self.assertIn("<!-- flaky-test: TestFlaky -->", body)
        self.assertTrue(any("filing" in l and "TestFlaky" in l for l in lines))

    def test_scenario_failure_output_secrets_are_redacted(self):
        body = flaky.plan(self.results, [], SHA, self.pairs, NOW)[1][0][1]
        self.assertNotIn("secretsecret", body)
        self.assertNotIn("tokentoken", body)
        self.assertIn("[redacted DERIBIT_CLIENT_SECRET]", body)

    def test_scenario_a_test_that_failed_every_run_files_nothing(self):
        lines, filings = flaky.plan(self.results, [], SHA, self.pairs, NOW)
        self.assertNotIn("[E6] Flaky test: TestBroken", [t for t, _ in filings])
        self.assertTrue(any(l.startswith("always fails: TestBroken") for l in lines))

    def test_scenario_open_issue_names_it_no_new_issue(self):
        issues = [{"number": 5, "title": "[E6] Flaky test: TestFlaky", "state": "OPEN"}]
        lines, filings = flaky.plan(self.results, issues, SHA, self.pairs, NOW)
        self.assertEqual(filings, [])
        self.assertTrue(any("#5 already names it" in l for l in lines))

    def test_nothing_failed(self):
        lines, filings = flaky.plan(flaky.parse_go(go_night({"TestOK": [(True, "")]}), {}), [], SHA, [], NOW)
        self.assertEqual((lines, filings), (["no test failed"], []))


class Run(unittest.TestCase):
    def test_kills_the_command_at_the_deadline(self):
        with tempfile.TemporaryDirectory() as d:
            start = time.time()
            rc = flaky.run(start + 1, os.path.join(d, "o"), os.path.join(d, "e"), ["bash", "-c", "sleep 30 & wait"])
            self.assertEqual(rc, flaky.TIMED_OUT)
            self.assertLess(time.time() - start, 10)

    def test_returns_the_exit_code_and_captures_stdout(self):
        with tempfile.TemporaryDirectory() as d:
            rc = flaky.run(time.time() + 30, os.path.join(d, "o"), os.path.join(d, "e"), ["bash", "-c", "echo hi; exit 3"])
            self.assertEqual(rc, 3)
            with open(os.path.join(d, "o")) as f:
                self.assertEqual(f.read(), "hi\n")


GO = """#!/usr/bin/env bash
echo "$PWD $*" >> "$FLAKY_TEST_DIR/go.log"
[[ -f "$FLAKY_TEST_DIR/go_sleep" ]] && sleep "$(cat "$FLAKY_TEST_DIR/go_sleep")"
cat "$FLAKY_TEST_DIR/go.json"
exit 1
"""

NPM = """#!/usr/bin/env bash
echo "$PWD $*" >> "$FLAKY_TEST_DIR/npm.log"
[[ "$1" == ci ]] && exit 0
n="$(grep -c ' test ' "$FLAKY_TEST_DIR/npm.log")"
for a in "$@"; do [[ "$a" == --outputFile=* ]] && cp "$FLAKY_TEST_DIR/jest-$n.json" "${a#--outputFile=}"; done
exit 0
"""

GH = """#!/usr/bin/env bash
echo "$*" >> "$FLAKY_TEST_DIR/gh.log"
case "$1 $2" in
  "repo view") echo owner/repo ;;
  "issue list") cat "$FLAKY_TEST_DIR/issues.json" 2>/dev/null || echo '[]' ;;
  "issue create")
    n="$(ls "$FLAKY_TEST_DIR" | grep -c '^title-' || true)"
    while [[ $# -gt 0 ]]; do
      case "$1" in --body-file) cp "$2" "$FLAKY_TEST_DIR/body-$n.md"; shift ;; --title) echo "$2" > "$FLAKY_TEST_DIR/title-$n.txt"; shift ;; esac
      shift
    done
    echo "https://github.com/owner/repo/issues/$((900 + n))" ;;
  *) exit 1 ;;
esac
"""


def git(*args, cwd=None):
    return subprocess.run(["git", *args], cwd=cwd, check=True, capture_output=True, text=True).stdout.strip()


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(text)


class FlakyScript(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, True)
        t = lambda *p: os.path.join(self.tmp, *p)
        os.environ.update(GIT_AUTHOR_NAME="t", GIT_AUTHOR_EMAIL="t@t", GIT_COMMITTER_NAME="t", GIT_COMMITTER_EMAIL="t@t")
        # origin/main with a frontend dir; the agents' clone holds the scripts.
        git("init", "-q", "--bare", "-b", "main", t("origin.git"))
        git("clone", "-q", t("origin.git"), t("repo"))
        write(t("repo", "frontend", "package.json"), "{}\n")
        git("add", "frontend", cwd=t("repo"))
        git("commit", "-q", "-m", "A", cwd=t("repo"))
        git("push", "-q", "origin", "HEAD:main", cwd=t("repo"))
        self.sha = git("rev-parse", "HEAD", cwd=t("repo"))
        shutil.copytree(SCRIPTS, t("repo", "scripts"))
        for env, port in (("stable", 3000), ("mvp", 3100)):
            write(t("envs", env, ".env"), f"DERIBIT_CLIENT_ID={env}idid\n" + ENV)
            write(t("envs", env, "deploy.env"), f"OPTIONSBOT_ENV={env}\nMONITOR_PORT={port}\n")
        write(t("go.json"), "\n".join(go_night(FLAKY_GO)) + "\n")
        for i, outcome in enumerate(("passed", "passed", "passed"), 1):
            write(t(f"jest-{i}.json"), json.dumps(jest_report({"api › reads status": outcome})))
        for name, text in (("go", GO), ("npm", NPM), ("gh", GH)):
            write(t("bin", name), text)
            os.chmod(t("bin", name), 0o755)
        self.env = dict(os.environ, PATH=t("bin") + os.pathsep + os.environ["PATH"], OPTIONSBOT_ENVS=t("envs"),
                        AGENT_WORK_DIR=t("work"), FLAKY_TEST_DIR=self.tmp, AGENT_NOTIFY="0", FLAKY_HOUR_UTC="0")
        for k in ("MONITOR_URL", "MVP_MONITOR_URL", "MONITOR_TOKEN", "MVP_MONITOR_TOKEN", "AGENT_STATE_DIR",
                  "AGENT_RUNS_FILE", "FLAKY_LIMIT_MIN", "FLAKY_LIMIT_SEC", "FLAKY_JEST_RUNS"):
            self.env.pop(k, None)
        self.t = t
        self.night = dt.datetime.now(dt.timezone.utc).strftime("%Y-%m-%d")

    def run_script(self):
        p = subprocess.run(["bash", self.t("repo", "scripts", "agents", "flaky.sh")], env=self.env,
                           capture_output=True, text=True, timeout=120)
        self.output = p.stdout + p.stderr
        self.assertEqual(p.returncode, 0, self.output)

    def read(self, *p):
        try:
            with open(self.t(*p)) as f:
                return f.read()
        except FileNotFoundError:
            return ""

    def titles(self):
        return [self.read(n).strip() for n in sorted(os.listdir(self.tmp)) if n.startswith("title-")]

    def ledger(self):
        return [json.loads(l) for l in self.read("envs", "history", "agent_runs.jsonl").splitlines()]

    def test_scenario_flaky_test_filed_once_in_triage_with_counts_and_redacted_output(self):
        write(self.t("jest-2.json"), json.dumps(jest_report({"api › reads status": "failed"})))
        self.run_script()
        self.assertEqual(self.titles(), ["[E6] Flaky test: TestFlaky",
                                         "[E6] Flaky test: __tests__/api.test.js › api › reads status"], self.output)
        body = self.read("body-0.md")
        self.assertIn("passed 4 and failed 1 of 5 runs", body)
        self.assertIn(self.sha, body)
        self.assertNotIn("secretsecret", body)
        self.assertNotIn("tokentoken", body)
        gh = self.read("gh.log")
        self.assertIn("--label bug", gh)
        self.assertNotIn("TestBroken", gh)          # always failing: not filed
        self.assertIn("always fails: TestBroken", self.output)
        # The suites ran in a clean worktree of origin/main: go with -race -count=5, jest 3 times.
        go = self.read("go.log")
        self.assertIn(os.path.join(self.t("work"), "flaky-" + self.night), go)
        self.assertIn("test ./tests/... -race -count=5 -json", go)
        self.assertEqual(self.read("npm.log").count(" test -- --ci --json"), 3)
        self.assertTrue(os.path.exists(self.t("repo", "data", "agents", "flaky", self.night + ".done")))
        self.assertFalse(os.path.exists(self.t("work", "flaky-" + self.night)))  # worktree removed
        end = [r for r in self.ledger() if r["event"] == "end"]
        self.assertEqual((end[0]["agent"], end[0]["outcome"]), ("flaky", "filed"))

    def test_scenario_open_issue_names_the_test_nothing_filed(self):
        write(self.t("issues.json"), json.dumps([{"number": 5, "title": "[E6] Flaky test: TestFlaky", "state": "OPEN"}]))
        self.run_script()
        self.assertEqual(self.titles(), [])
        self.assertIn("#5 already names it", self.output)

    def test_scenario_time_limit_passed_nothing_filed_log_says_timed_out(self):
        write(self.t("go_sleep"), "30\n")
        self.env["FLAKY_LIMIT_SEC"] = "2"
        start = time.time()
        self.run_script()
        self.assertLess(time.time() - start, 25)
        self.assertEqual(self.titles(), [])
        self.assertNotIn("issue list", self.read("gh.log"))
        self.assertIn("timed out", self.output)
        end = [r for r in self.ledger() if r["event"] == "end"]
        self.assertEqual(end[0]["outcome"], "timed_out")

    def test_once_a_night_and_not_before_the_hour(self):
        write(self.t("repo", "data", "agents", "flaky", self.night + ".done"), "x\n")
        self.run_script()
        self.assertEqual(self.read("go.log"), "")
        os.remove(self.t("repo", "data", "agents", "flaky", self.night + ".done"))
        self.env["FLAKY_HOUR_UTC"] = "25"
        self.run_script()
        self.assertEqual(self.read("go.log"), "")
        self.assertEqual(self.read("gh.log"), "")


if __name__ == "__main__":
    unittest.main()
