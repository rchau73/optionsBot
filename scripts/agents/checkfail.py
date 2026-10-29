#!/usr/bin/env python3
"""checkfail.py — which suites a change needs, whether a failing test is the
change's doing, and how many check failures a card has had (#472). Pure rules;
CI's `changes` job, dev_agent.sh and pr_gate.sh call it.

    checkfail.py suites [<files.txt>]
        → "<suite>=true|false" lines (GITHUB_OUTPUT form) for the changed
          paths (stdin without a file): go, go_lint, frontend, docs,
          scripts, docker, vuln.
    checkfail.py go-tests <repo> <files.txt> <report regex>
        → "all" when every Go test is needed (go.mod/go.sum changed), else
          two lines "core <regex>" and "reports <regex>" (empty: none) for the
          tests the change touches, split as scripts/go_tests.sh splits them.
    checkfail.py jest-files <files.txt>
        → "all", or the frontend/-relative files for jest --findRelatedTests.
    checkfail.py repo-wide <files.txt>
        → the checks that read every file, for the Dev agent to run before it
          hands over (#467): "jest <frontend test>" lines for a frontend
          change, "helpindex" when a help-index source changed.
    checkfail.py retry <repo> <files.txt> <log>
        → the failing tests the change does not touch, one "go|jest <name>"
          a line; exit 1 when none may be retried (a touched test failed, or
          the failure names no test: gofmt, vet, eslint, a build error).
    checkfail.py tails <log> [lines]
        → each failing test's own output (its last `lines`, 40), fenced.
    checkfail.py ci-plan <repo> <files.txt> <reruns> <failed checks.txt> <log>
        → "rerun <names>" (only untouched tests failed, under MAX_RETRIES
          re-runs of this head) or "send_back <why>".
    checkfail.py failed-checks <pr.json>
        → "<run id>\t<check name>" for each failed CI check of the PR's head.
    checkfail.py count <history.json>
        → check failures on the card since a person was last asked.
    checkfail.py next <count>        → needs-fix (the first) | needs-human
    checkfail.py marker <n> [<source> [<sha>]]   → the comment marker
    checkfail.py pending <history.json>
        → the last check-failure comment, when no pass came after it (the
          next Dev run reads it first, as it reads review feedback).

A suite runs when a changed file is code it builds or tests, or a doc one of
its tests opens: test_checkfail.py scans tests/*.go and the frontend tests for
the paths they open and fails when one would not trigger its suite. Any .md
runs the two doc-wide line checks (the `docs` suite: DOC_TESTS); a CLAUDE.md
runs only those, and an image or diagram source nothing. A push to main and the nightly run skip this: CI runs
every job there.

"Touched" (go-tests, retry): the test's file changed, or a path it opens, or a
package it imports (transitively, in this module), or a helper of tests/ it
names. A touched test that fails is the change's doing and never retried; an
untouched one is re-run up to MAX_RETRIES times, and passing makes it flaky.

Standard library only.
"""
import json
import os
import re
import sys

MAX_RETRIES = 3
TAIL_LINES = 40
GO_MODULE = "optionsbot"
CLAUDE = "CLAUDE.md"
DOC_EXT = (".md", ".mmd", ".png", ".svg", ".jpg", ".jpeg", ".gif", ".webp")
AGGREGATOR = "Lint, test & coverage"  # ci.yml's required check: fails when a job it needs fails

# Paths an entry names: one ending in "/" or "_" is a prefix, anything else an exact path.
# Docs and frontend files the Go tests open (tests/*.go); other non-doc paths
# outside frontend/ always run the Go tests.
GO_READS = (
    ".claude/skills/", "AGENTIC.md", "OptionStrategy.md", "README.md",
    "docs/account-isolation.md", "docs/agents/", "docs/ai-analysis.md", "docs/architecture.md",
    "docs/direction-trial.md", "docs/exchange-adapter.md", "docs/exchanges/", "docs/legal/",
    "docs/operations.md", "docs/parameters.md", "docs/product/", "docs/usability/",
    "frontend/Dockerfile", "frontend/README.md", "frontend/app/api/", "frontend/content/", "frontend/docs/",
    "frontend/lib/analytics/", "frontend/lib/assistant/", "frontend/lib/menu.js", "frontend/package.json",
    "scripts/product/README.md", "tests/",
)
# Docs of frontend/ and files outside it the frontend tests open; other
# non-doc paths under frontend/ always run them.
FRONTEND_READS = (
    "OptionStrategy.md", "config_", "docs/parameters.md", "docs/product/",
    "frontend/README.md", "frontend/content/", "frontend/docs/",
    "internal/assistant/guard/", "tests/testdata/",
)
# What the scripts job tests (shellcheck, the Python unit tests) and what those tests read.
SCRIPTS_READS = (".githooks/", ".github/", "cmd/", "docs/legal/", "docs/product/", "go.mod", "go.sum",
                 "internal/", "scripts/")
DOCKER_READS = ("Dockerfile", ".dockerignore", "cmd/", "frontend/", "go.mod", "go.sum", "internal/")
VULN_READS = ("frontend/package-lock.json", "go.mod", "go.sum")
SUITES = ("go", "go_lint", "frontend", "docs", "scripts", "docker", "vuln")
# Go tests that read every tracked .md (or every CLAUDE.md): fast line checks,
# run for any doc change. file → the changed paths it reads.
DOC_TESTS = {
    "tests/claude_docs_test.go": lambda f: os.path.basename(f) == CLAUDE,
    "tests/docs_line_length_test.go": lambda f: f.endswith(".md"),
}
DOC_TESTS_RUN = "^(TestClaudeDocs|TestDocs_)"  # ci.yml runs these when the Go tests are skipped


def matches(path, entries):
    return any(path == e or (e[-1] in "/_" and path.startswith(e)) for e in entries)


def is_doc(path):
    return path.lower().endswith(DOC_EXT)


def suites_for(path):
    """The suites one changed path needs."""
    if os.path.basename(path) == CLAUDE:
        return {"docs"}
    doc, front = is_doc(path), path.startswith("frontend/")
    out = {"docs"} if path.endswith(".md") else set()
    if matches(path, GO_READS) or not (doc or front):
        out.add("go")
    if path.endswith(".go") or path in ("go.mod", "go.sum"):
        out.add("go_lint")
    if matches(path, FRONTEND_READS) or (front and not doc):
        out.add("frontend")
    if matches(path, SCRIPTS_READS):
        out.add("scripts")
    if matches(path, DOCKER_READS) and not doc:
        out.add("docker")
    if matches(path, VULN_READS):
        out.add("vuln")
    return out


def suites(files):
    out = set()
    for f in files:
        out |= suites_for(f)
    return out


# ── What a test opens and imports ────────────────────────────────────────────
GO_STRING = re.compile(r'"((?:[^"\\\n]|\\.)*)"')
JS_STRING = re.compile(r'"((?:[^"\\\n]|\\.)*)"|\'((?:[^\'\\\n]|\\.)*)\'|`([^`$\n]*)`')
CALL_ARGS = re.compile(r"\b(\w+(?:\.\w+)?)\(((?:[^()]|\([^()]*\))*)\)")
# A Go helper whose path arguments are relative to a folder of the repo.
GO_HELPER_BASES = {"readFrontend": "frontend"}
GO_IMPORT = re.compile(r'"' + GO_MODULE + r'/([^"]+)"')
GO_TEST_FUNC = re.compile(r"^func (Test\w+)\(\w+ \*testing\.T\)", re.M)
GO_TOP_NAME = re.compile(r"^(?:func (?:\([^)]*\) )?|type |var |const )(\w+)", re.M)
JS_IMPORT = re.compile(r"""(?:\bfrom\s+|\bimport\s+|\brequire\(\s*|\bimport\(\s*|jest\.mock\(\s*)["']([^"']+)["']""")
JS_EXT = ("", ".js", ".jsx", ".mjs", ".ts", ".tsx", ".json", "/index.js", "/index.jsx")


def _read(repo, rel):
    try:
        with open(os.path.join(repo, rel), encoding="utf-8") as f:
            return f.read()
    except (OSError, UnicodeDecodeError):
        return ""


def _resolve(repo, bases, literal):
    """The repo path a string in a test names, tried against each base folder
    in turn, or None. Anchors and globs are cut; a bare word is not a path."""
    p = literal.split("#", 1)[0].split("*", 1)[0].rstrip("/")
    if not p or p.startswith("/") or "://" in p or ("/" not in p and "." not in p) or " " in p:
        return None
    for base in bases:
        cand = os.path.normpath(os.path.join(base, p))
        # A top-level folder alone is a helper's base (readFrontend), not a read.
        if cand in (".", "") or cand.startswith("..") or ("/" not in cand and os.path.isdir(os.path.join(repo, cand))):
            continue
        if os.path.exists(os.path.join(repo, cand)):
            return cand
    return None


def go_reads(repo, test_file, src=None):
    """Repo paths (files or folders) a Go test file opens."""
    src = _read(repo, test_file) if src is None else src
    base = os.path.dirname(test_file)
    out = set()
    for s in GO_STRING.findall(src):
        r = _resolve(repo, (base, ""), s)
        if r:
            out.add(r)
    for fn, args in CALL_ARGS.findall(src):
        parts = GO_STRING.findall(args)
        if len(parts) < 2 and fn not in GO_HELPER_BASES:
            continue
        name = fn.split(".")[-1]
        bases = (GO_HELPER_BASES[name],) if name in GO_HELPER_BASES else (base, "")
        r = _resolve(repo, bases, "/".join(parts))
        if r:
            out.add(r)
    return out


def js_reads(repo, test_file, src=None):
    """Repo paths a frontend test file opens: string paths and path.join/resolve
    pieces, tried from the file's folder, frontend/ (the tests' cwd) and the repo."""
    src = _read(repo, test_file) if src is None else src
    bases = (os.path.dirname(test_file), "frontend", "")
    out = set()
    strings = lambda text: ["".join(m) for m in JS_STRING.findall(text)]
    for s in strings(src):
        r = _resolve(repo, bases, s)
        if r:
            out.add(r)
    for fn, args in CALL_ARGS.findall(src):
        if fn.split(".")[-1] not in ("join", "resolve"):
            continue
        r = _resolve(repo, bases, "/".join(strings(args)))
        if r:
            out.add(r)
    return out


def _under(path, reads):
    return any(path == r or path.startswith(r + "/") for r in reads)


def go_packages(repo, seeds):
    """The module's package folders seeds import, transitively (seeds included)."""
    seen, todo = set(), list(seeds)
    while todo:
        d = todo.pop()
        if d in seen or not os.path.isdir(os.path.join(repo, d)):
            continue
        seen.add(d)
        for name in sorted(os.listdir(os.path.join(repo, d))):
            if name.endswith(".go") and not name.endswith("_test.go"):
                todo.extend(GO_IMPORT.findall(_read(repo, os.path.join(d, name))))
    return seen


def _helper_names(repo, changed):
    """Top-level names the changed tests/*.go files define."""
    names = set()
    for f in changed:
        if f.startswith("tests/") and f.endswith(".go") and "/" not in f[len("tests/"):]:
            names.update(GO_TOP_NAME.findall(_read(repo, f)))
    return names


def go_file_touched(repo, test_file, changed, helpers=None):
    """True when the change touches what this Go test file runs."""
    if test_file in changed or any(f in ("go.mod", "go.sum") for f in changed):
        return True
    if test_file in DOC_TESTS and any(DOC_TESTS[test_file](f) for f in changed):
        return True
    # A folder's CLAUDE.md is no part of its package: only DOC_TESTS read it.
    changed = [f for f in changed if os.path.basename(f) != CLAUDE]
    if not changed:
        return False
    src = _read(repo, test_file)
    reads = go_reads(repo, test_file, src)
    if any(_under(f, reads) for f in changed):
        return True
    pkgs = go_packages(repo, set(GO_IMPORT.findall(src)) | {r for r in reads if os.path.isdir(os.path.join(repo, r))})
    if any(os.path.dirname(f) in pkgs for f in changed):
        return True
    helpers = _helper_names(repo, changed) if helpers is None else helpers
    own = set(GO_TOP_NAME.findall(src))
    return any(re.search(r"\b" + re.escape(n) + r"\b", src) for n in helpers - own)


def js_imports(repo, test_file):
    """The frontend files a test file imports, transitively (relative and @/ paths)."""
    seen, todo = set(), [test_file]
    while todo:
        f = todo.pop()
        if f in seen:
            continue
        seen.add(f)
        for spec in JS_IMPORT.findall(_read(repo, f)):
            if spec.startswith("."):
                base = os.path.normpath(os.path.join(os.path.dirname(f), spec))
            elif spec.startswith("@/"):
                base = os.path.join("frontend", spec[2:])
            else:
                continue
            for ext in JS_EXT:
                if os.path.isfile(os.path.join(repo, base + ext)):
                    todo.append(base + ext)
                    break
    return seen


def jest_touched(repo, test_file, changed):
    if test_file in changed or "frontend/package-lock.json" in changed or "frontend/package.json" in changed:
        return True
    files = js_imports(repo, test_file)
    reads = set().union(*(js_reads(repo, f) for f in files))
    return any(f in files or _under(f, reads) for f in changed)


def go_test_file(repo, name):
    """tests/<file>.go that defines func <name>, or None."""
    d = os.path.join(repo, "tests")
    pat = re.compile(r"^func " + re.escape(name) + r"\(", re.M)
    for f in sorted(os.listdir(d)) if os.path.isdir(d) else ():
        if f.endswith(".go") and pat.search(_read(repo, os.path.join("tests", f))):
            return os.path.join("tests", f)
    return None


def touched(repo, kind, name, changed):
    """Does the change touch this failing test? Unknown → yes (never retried)."""
    if kind == "go":
        f = go_test_file(repo, name)
        return f is None or go_file_touched(repo, f, changed)
    f = name if name.startswith("frontend/") else os.path.join("frontend", name)
    return not os.path.isfile(os.path.join(repo, f)) or jest_touched(repo, f, changed)


def go_tests(repo, changed, report_regex):
    """"all", or (core names, report names) of the Go tests the change touches."""
    if any(f in ("go.mod", "go.sum") for f in changed):
        return "all"
    helpers = _helper_names(repo, changed)
    names = []
    d = os.path.join(repo, "tests")
    for f in sorted(os.listdir(d)) if os.path.isdir(d) else ():
        rel = os.path.join("tests", f)
        if f.endswith("_test.go") and go_file_touched(repo, rel, changed, helpers):
            names += GO_TEST_FUNC.findall(_read(repo, rel))
    rep = re.compile(report_regex) if report_regex else None
    core = sorted(n for n in names if not (rep and rep.match(n)))
    reports = sorted(n for n in names if rep and rep.match(n))
    return core, reports


def jest_files(changed):
    """"all", or the frontend/-relative code paths for `jest --findRelatedTests`:
    jest follows imports only, so a doc, a file outside frontend/ or the
    package files the frontend tests depend on run every test."""
    out = []
    for f in changed:
        if "frontend" not in suites_for(f):
            continue
        if not f.startswith("frontend/") or is_doc(f) or matches(f, FRONTEND_READS) or \
                os.path.dirname(f) == "frontend":
            return "all"
        out.append(f[len("frontend/"):])
    return out


# The frontend tests that read every page, label or panel (#467): #445's agent
# ran only the tests its change imported and missed the glossary and the
# responsive audit. A frontend change runs them all before it is handed over.
REPO_WIDE_JEST = (
    "__tests__/empty-states.test.js",
    "__tests__/glossary.test.js",
    "__tests__/menu.test.jsx",
    "__tests__/responsive-audit.test.js",
)
# The help index's sources (internal/helpkb LoadSources): a change to one needs
# `go run ./cmd/helpindex`.
HELP_SOURCES = (
    "OptionStrategy.md",
    "docs/parameters.md",
    "frontend/content/glossary.json",
    "frontend/content/monitor-guide.md",
    "internal/api/catalogue.go",
    "internal/api/explain.go",
)


def repo_wide(changed):
    """The repo-wide checks a change needs: ["jest <test>", …, "helpindex"]."""
    out = []
    if any("frontend" in suites_for(f) for f in changed):
        out += [f"jest {t}" for t in REPO_WIDE_JEST]
    if any(f in HELP_SOURCES for f in changed):
        out.append("helpindex")
    return out


def run_regex(names):
    return "^(" + "|".join(names) + ")$" if names else ""


# ── Failures in a log ───────────────────────────────────────────────────────
# `gh run view --log-failed` prefixes "<job>\t<step>\t<timestamp>Z ".
CI_PREFIX = re.compile(r"^([^\t]*)\t[^\t]*\t\S*Z ?")
GO_FAIL = re.compile(r"^--- FAIL: (Test[^\s/(]+)")
JEST_FAIL = re.compile(r"^FAIL (\S+\.(?:test|spec)\.[cm]?[jt]sx?)\b")
JEST_NEXT = re.compile(r"^(PASS|FAIL) |^Test Suites:")


def _strip(line):
    return CI_PREFIX.sub("", line.rstrip("\n"))


def job_of(line):
    m = CI_PREFIX.match(line)
    return m.group(1) if m else ""


def failures(text):
    """[(kind, name)] of the failing top-level Go tests and jest files, in order, once each."""
    out = []
    for raw in text.splitlines():
        line = _strip(raw)
        m = GO_FAIL.match(line)
        hit = ("go", m.group(1)) if m else None
        m = JEST_FAIL.match(line)
        if m:
            hit = ("jest", m.group(1))
        if hit and hit not in out:
            out.append(hit)
    return out


def tail_of(text, kind, name, n=TAIL_LINES):
    """A failing test's own output: Go — its FAIL line and the indented lines
    after it; jest — from its FAIL line to the next file's result. Last n lines."""
    lines = [_strip(l) for l in text.splitlines()]
    block = []
    for i, line in enumerate(lines):
        if kind == "go" and GO_FAIL.match(line) and GO_FAIL.match(line).group(1) == name:
            block = [line]
            for nxt in lines[i + 1:]:
                if not nxt[:1].isspace():
                    break
                block.append(nxt)
        elif kind == "jest" and JEST_FAIL.match(line) and JEST_FAIL.match(line).group(1) == name:
            block = [line]
            for nxt in lines[i + 1:]:
                if JEST_NEXT.match(nxt):
                    break
                block.append(nxt)
    return block[-n:]


def tails(text, n=TAIL_LINES):
    """Markdown: each failing test's output, or the log's last n lines when it names none."""
    out = []
    for kind, name in failures(text):
        out += [f"`{name}`:", "```", *tail_of(text, kind, name, n), "```"]
    if not out:
        out = ["```", *[_strip(l) for l in text.splitlines()[-n:]], "```"]
    return "\n".join(out)


def retry_list(repo, changed, text):
    """The failing tests that may be re-run (none touched by the change), or None."""
    fails = failures(text)
    if not fails or any(touched(repo, k, n, changed) for k, n in fails):
        return None
    return fails


def ci_plan(repo, changed, reruns, failed_checks, text):
    """("rerun", names) or ("send_back", why) for a red CI on the PR's head."""
    jobs = [c for c in failed_checks if c != AGGREGATOR]
    if not jobs:
        return "send_back", f"{AGGREGATOR} failed on its own (the coverage floor or a job it needs)"
    by_job = {}
    for raw in text.splitlines():
        by_job.setdefault(job_of(raw), []).append(raw)
    names = []
    for job in jobs:
        fails = failures("\n".join(by_job.get(job, [])))
        if not fails:
            return "send_back", f"{job} failed outside a test"
        names += [f for f in fails if f not in names]
    hit = [n for k, n in names if touched(repo, k, n, changed)]
    if hit:
        return "send_back", "a test the change touches failed: " + ", ".join(hit)
    if reruns >= MAX_RETRIES:
        return "send_back", f"still failing after {reruns} re-runs: " + ", ".join(n for _, n in names)
    return "rerun", ",".join(n for _, n in names)


RUN_URL = re.compile(r"/actions/runs/(\d+)")
GREEN = {"SUCCESS", "NEUTRAL", "SKIPPED"}


def failed_checks(rollup):
    """[(run id or "", name)] of the CI checks that completed red."""
    out = []
    for c in rollup or []:
        if c.get("__typename") == "StatusContext":
            bad = c.get("state") in ("FAILURE", "ERROR")
            name = c.get("context", "?")
        else:
            bad = c.get("status") == "COMPLETED" and c.get("conclusion") not in GREEN
            name = c.get("name", "?")
        if bad:
            m = RUN_URL.search(c.get("detailsUrl") or c.get("targetUrl") or "")
            out.append((m.group(1) if m else "", name))
    return out


# ── The count on the issue ──────────────────────────────────────────────────
MARKER = re.compile(r"<!-- dev:check-fail n=(\d+)[^>]*-->")
PASS_MARKER = "<!-- dev:check-pass -->"
BLOCKED = ("<!-- dev:blocked -->", "Dev agent stopped — a person is needed")


def _bodies(history):
    return [c.get("body") or "" for c in history.get("comments", [])]


def count(history):
    """Check failures (the Dev script's and the gate's CI ones) since the last
    time the card was handed to a person: one goes back to the agent, the
    second to a person."""
    n = 0
    for body in _bodies(history):
        if any(b in body for b in BLOCKED):
            n = 0
        elif MARKER.search(body):
            n += 1
    return n


def next_step(n):
    return "needs-fix" if n == 0 else "needs-human"


def marker(n, source="dev", sha=""):
    return f"<!-- dev:check-fail n={n} source={source}" + (f" sha={sha}" if sha else "") + " -->"


def pending(history):
    """The last check-failure comment when no pass or person came after it."""
    last = ""
    for body in _bodies(history):
        if MARKER.search(body):
            last = body
        elif PASS_MARKER in body or any(b in body for b in BLOCKED):
            last = ""
    return last


def _lines(path):
    with open(path, encoding="utf-8") as f:
        return [l.strip() for l in f if l.strip()]


def _text(path):
    with open(path, encoding="utf-8", errors="replace") as f:
        return f.read()


def main(argv):
    cmd = argv[1] if len(argv) > 1 else ""
    if cmd == "suites":
        files = _lines(argv[2]) if len(argv) > 2 else [l.strip() for l in sys.stdin if l.strip()]
        got = suites(files)
        for s in SUITES:
            print(f"{s}={'true' if s in got else 'false'}")
    elif cmd == "go-tests":
        got = go_tests(argv[2], _lines(argv[3]), argv[4] if len(argv) > 4 else "")
        if got == "all":
            print("all")
        else:
            print("core", run_regex(got[0]))
            print("reports", run_regex(got[1]))
    elif cmd == "jest-files":
        got = jest_files(_lines(argv[2]))
        print("all" if got == "all" else "\n".join(got))
    elif cmd == "repo-wide":
        for line in repo_wide(_lines(argv[2])):
            print(line)
    elif cmd == "retry":
        got = retry_list(argv[2], _lines(argv[3]), _text(argv[4]))
        if got is None:
            return 1
        for kind, name in got:
            print(kind, name)
    elif cmd == "tails":
        print(tails(_text(argv[2]), int(argv[3]) if len(argv) > 3 else TAIL_LINES))
    elif cmd == "ci-plan":
        action, why = ci_plan(argv[2], _lines(argv[3]), int(argv[4] or 0), _lines(argv[5]), _text(argv[6]))
        print(action, why)
    elif cmd == "failed-checks":
        with open(argv[2], encoding="utf-8") as f:
            for run, name in failed_checks(json.load(f).get("statusCheckRollup")):
                print(f"{run}\t{name}")
    elif cmd in ("count", "pending"):
        with open(argv[2], encoding="utf-8") as f:
            history = json.load(f)
        print(count(history) if cmd == "count" else pending(history))
    elif cmd == "next":
        print(next_step(int(argv[2])))
    elif cmd == "marker":
        print(marker(int(argv[2]), *argv[3:5]))
    else:
        raise SystemExit(f"usage: see {__file__}'s docstring (unknown command {cmd!r})")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
