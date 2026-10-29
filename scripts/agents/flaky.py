#!/usr/bin/env python3
"""flaky.py — the nightly flaky-test hunt's pure part (#248). flaky.sh runs the
suites; this reads their results and writes the issues to file.

    flaky.py run <deadline epoch> <out> <err> -- <cmd...>
        runs <cmd> in its own process group, stdout → <out>, stderr → <err>;
        killed at the deadline (exit 124, TIMED_OUT), else the command's exit.
    flaky.py plan <results dir> <issues.json> <out dir> <commit> [env file...]
        reads <results dir>/go*.json (go test -json) and jest-*.json (jest
        --json), prints one log line per test that failed, and writes
        <out dir>/<n>.title and <n>.body for every flaky test no issue names
        yet. issues.json: gh issue list --state all --json
        number,title,state,closedAt. env: dotenv files whose secret values are
        redacted from the failure output (deploy_issue.py's rule).
    flaky.py title <name>                 → the issue title for a flaky test
    flaky.py find <name> <issues.json>    → the issue that already names it, if any
        (lib.sh note_flaky: a test a PR's checks retried and saw pass, #472)

A test is flaky when it both passed and failed tonight; one that failed every
run is not (CI catches it): it is logged, nothing is filed. An issue names a
test when its title ends with "Flaky test: <name>"; an open one, or one closed
in the last DEDUP_DAYS days, is a duplicate (as deploy_issue.py, nothing is
filed twice).

Standard library only.
"""
import datetime as dt
import json
import os
import signal
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import deploy_issue  # noqa: E402  (secrets, redact, tail, fence)

DEDUP_DAYS = 14
TIMED_OUT = 124
GO_SUITE = "Go: `go test ./tests/... -race -count=5`"
JEST_SUITE = "frontend: `npm test -- --ci`, 3 runs"


class Result:
    """Passes and failures of one test over the night, and its first failure."""

    def __init__(self, name, suite, where):
        self.name, self.suite, self.where = name, suite, where
        self.passes = self.fails = 0
        self.output = None  # the first failure's output

    def failed(self, output):
        self.fails += 1
        if self.output is None:
            self.output = output

    @property
    def flaky(self):
        return self.passes > 0 and self.fails > 0


def parse_go(lines, results):
    """Adds the events of `go test -json` (one JSON object a line) to results
    (name → Result). Output is collected per test from its last `run`."""
    buf = {}
    for line in lines:
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            ev = json.loads(line)
        except ValueError:
            continue
        test = ev.get("Test")
        if not test:
            continue  # package-level lines: build output, the package's own pass/fail
        action = ev.get("Action")
        if action == "run":
            buf[test] = []
        elif action == "output":
            buf.setdefault(test, []).append(ev.get("Output", ""))
        elif action in ("pass", "fail"):
            r = results.setdefault(test, Result(test, GO_SUITE, ev.get("Package", "")))
            if action == "pass":
                r.passes += 1
            else:
                r.failed("".join(buf.get(test, [])))
    return results


def _jest_file(path):
    """A suite's path relative to frontend/, stable from one night to the next."""
    marker = "/frontend/"
    return path.split(marker, 1)[1] if marker in path else os.path.basename(path)


def parse_jest(report, results):
    """Adds one `jest --json` report to results. A suite that failed with no
    failing test (it did not load) counts as a failure of the suite itself."""
    for suite in report.get("testResults", []):
        where = _jest_file(suite.get("name", ""))
        any_failed = False
        for a in suite.get("assertionResults", []):
            status = a.get("status")
            if status not in ("passed", "failed"):
                continue
            name = f"{where} › {a.get('fullName') or a.get('title', '')}"
            r = results.setdefault(name, Result(name, JEST_SUITE, where))
            if status == "passed":
                r.passes += 1
            else:
                any_failed = True
                r.failed("\n".join(a.get("failureMessages") or []))
        r = results.setdefault(where, Result(where, JEST_SUITE, where))
        if suite.get("status") == "passed":
            r.passes += 1
        elif not any_failed:
            r.failed(suite.get("message") or "")
    return results


def _children_flaky(name, flaky_names):
    return any(n.startswith(name + "/") for n in flaky_names)


def classify(results):
    """(flaky, always failing), each sorted by name. A parent whose subtest is
    in the same list is dropped: Go fails the parent with its subtest, and the
    subtest is the one to fix."""
    flaky = [r for r in results.values() if r.flaky]
    broken = [r for r in results.values() if r.fails and not r.passes]
    fn, bn = {r.name for r in flaky}, {r.name for r in broken}
    flaky = [r for r in flaky if not _children_flaky(r.name, fn)]
    broken = [r for r in broken if not _children_flaky(r.name, bn)]
    return sorted(flaky, key=lambda r: r.name), sorted(broken, key=lambda r: r.name)


def title(name):
    return f"[E6] Flaky test: {name}"


def _parse_time(s):
    try:
        return dt.datetime.fromisoformat((s or "").replace("Z", "+00:00"))
    except ValueError:
        return None


def find(name, issues, now):
    """Number of an issue that already names this test: open, or closed in
    the last DEDUP_DAYS days; None otherwise."""
    want = f"Flaky test: {name}"
    since = now - dt.timedelta(days=DEDUP_DAYS)
    for i in issues:
        if not i.get("title", "").rstrip().endswith(want):
            continue
        if i.get("state", "OPEN").upper() == "OPEN":
            return i["number"]
        closed = _parse_time(i.get("closedAt"))
        if closed is None or closed >= since:
            return i["number"]
    return None


def body(r, commit, pairs):
    out = deploy_issue.redact(deploy_issue.tail(r.output or ""), pairs) or "(no output)"
    f = deploy_issue.fence(out)
    runs = r.passes + r.fails
    return f"""🧪 **Flaky test** — `{r.name}` passed {r.passes} and failed {r.fails} of {runs} runs tonight on `origin/main` at `{commit[:7]}`.

- **Suite:** {r.suite}
- **Where:** `{r.where or "-"}`
- **Commit:** `{commit}`
- **Passed:** {r.passes} · **Failed:** {r.fails}

A flaky test turns a pull request's CI red by chance. Make it pass every run (or fix the race it found); never retry it in CI.

### First failure's output (last {deploy_issue.TAIL_LINES} lines, secrets redacted)
{f}text
{out}
{f}

Filed by `scripts/agents/flaky.sh`, the nightly flaky-test hunt (#248).
<!-- flaky-test: {r.name} -->
"""


def plan(results, issues, commit, pairs, now):
    """(log lines, [(title, body)] to file)."""
    flaky, broken = classify(results)
    lines, filings = [], []
    for r in broken:
        lines.append(f"always fails: {r.name} (failed {r.fails} of {r.fails}) — not flaky, nothing filed")
    for r in flaky:
        n = find(r.name, issues, now)
        if n is not None:
            lines.append(f"flaky: {r.name} (passed {r.passes}, failed {r.fails}) — #{n} already names it, nothing filed")
            continue
        lines.append(f"flaky: {r.name} (passed {r.passes}, failed {r.fails}) — filing")
        filings.append((title(r.name), body(r, commit, pairs)))
    if not flaky and not broken:
        lines.append("no test failed")
    return lines, filings


def load_results(directory):
    results = {}
    for fn in sorted(os.listdir(directory)):
        path = os.path.join(directory, fn)
        if fn.startswith("go") and fn.endswith(".json"):
            with open(path, encoding="utf-8", errors="replace") as f:
                parse_go(f, results)
        elif fn.startswith("jest-") and fn.endswith(".json"):
            try:
                with open(path, encoding="utf-8", errors="replace") as f:
                    parse_jest(json.load(f), results)
            except ValueError:
                print(f"unreadable jest report: {fn}")
    return results


def run(deadline, out, err, cmd):
    """Runs cmd until the deadline (epoch seconds); its whole process group is
    killed when the deadline passes."""
    left = deadline - dt.datetime.now().timestamp()
    if left <= 0:
        return TIMED_OUT
    with open(out, "wb") as o, open(err, "wb") as e:
        p = subprocess.Popen(cmd, stdout=o, stderr=e, stdin=subprocess.DEVNULL, start_new_session=True)
        try:
            return p.wait(timeout=left)
        except subprocess.TimeoutExpired:
            try:
                os.killpg(p.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            p.wait()
            return TIMED_OUT


def main(argv):
    cmd = argv[1] if len(argv) > 1 else ""
    if cmd == "run" and len(argv) > 6 and argv[5] == "--":
        return run(float(argv[2]), argv[3], argv[4], argv[6:])
    if cmd == "plan" and len(argv) >= 6:
        results_dir, issues_path, out_dir, commit = argv[2:6]
        with open(issues_path, encoding="utf-8") as f:
            issues = json.load(f)
        pairs = sorted((p for e in argv[6:] for p in deploy_issue.secrets(deploy_issue._read(e))),
                       key=lambda nv: -len(nv[1]))
        lines, filings = plan(load_results(results_dir), issues, commit, pairs, dt.datetime.now(dt.timezone.utc))
        os.makedirs(out_dir, exist_ok=True)
        for i, (t, b) in enumerate(filings, 1):
            with open(os.path.join(out_dir, f"{i:03d}.title"), "w", encoding="utf-8") as f:
                f.write(t)
            with open(os.path.join(out_dir, f"{i:03d}.body"), "w", encoding="utf-8") as f:
                f.write(b)
        for line in lines:
            print(line)
        return 0
    if cmd == "title" and len(argv) == 3:
        print(title(argv[2]))
        return 0
    if cmd == "find" and len(argv) == 4:
        with open(argv[3], encoding="utf-8") as f:
            n = find(argv[2], json.load(f), dt.datetime.now(dt.timezone.utc))
        if n is not None:
            print(n)
        return 0
    print(__doc__, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
