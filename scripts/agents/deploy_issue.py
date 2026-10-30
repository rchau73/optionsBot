#!/usr/bin/env python3
"""deploy_issue.py — the bug issue deploy.sh files when a commit fails to
deploy to mvp (#204). Pure text here; deploy.sh reads GitHub and acts.

    deploy_issue.py title <sha> <subject>                        → the issue title
    deploy_issue.py find <sha> < issues.json                     → number of an issue already filed for <sha>, or nothing
    deploy_issue.py body <sha> <meta> <tail> <range> [env...]    → the issue body (markdown)

issues.json: gh issue list --json number,title. meta: the failed step on its
first line, the rollback outcome on its second. tail: the log of the failed
step (the last TAIL_LINES lines are kept). range: git log --oneline of the
commits deployed. env: dotenv files whose secret values are redacted from the
log (the variable names the pre-commit hook guards: *SECRET*, *TOKEN*,
*CLIENT_ID*, *KEY*, *PASSWORD*, *SALT*); the values themselves never leave this
process.

Standard library only.
"""
import json
import re
import sys

TAIL_LINES = 80
MAX_LINE = 400  # characters kept per log line: a GitHub body holds 65,536
SECRET_NAME = re.compile(r"^[A-Z0-9_]*(SECRET|TOKEN|CLIENT_ID|KEY|PASSWORD|SALT)[A-Z0-9_]*$")
MIN_SECRET_LEN = 6  # as the pre-commit hook: shorter values are not secrets (and would redact noise)
BEARER = re.compile(r"(Bearer\s+)[A-Za-z0-9._~+/=-]+")


def title(sha, subject):
    return f"[E6] mvp deploy failed: {sha[:7]} ({subject.strip()})"


def find(sha, issues):
    """Number of an issue (open or closed) whose title names this commit."""
    want = f"mvp deploy failed: {sha[:7]}"
    for i in issues:
        if want in i.get("title", ""):
            return i["number"]
    return None


def secrets(env_text):
    """(name, value) of every secret variable in a dotenv text, longest value first."""
    out = []
    for line in env_text.splitlines():
        m = re.match(r"^\s*(?:export\s+)?([A-Za-z0-9_]+)\s*=(.*)$", line)
        if not m or not SECRET_NAME.match(m.group(1)):
            continue
        value = re.sub(r"\s#.*$", "", m.group(2)).strip().strip("'\"").strip()
        if len(value) >= MIN_SECRET_LEN:
            out.append((m.group(1), value))
    return sorted(out, key=lambda nv: -len(nv[1]))


def redact(text, pairs):
    for name, value in pairs:
        text = text.replace(value, f"[redacted {name}]")
    return BEARER.sub(r"\1[redacted]", text)


def tail(text, n=TAIL_LINES):
    lines = text.rstrip("\n").splitlines()[-n:]
    return "\n".join(l if len(l) <= MAX_LINE else l[:MAX_LINE] + " …" for l in lines)


def fence(text):
    """A code fence longer than any backtick run in the text."""
    longest = max((len(r) for r in re.findall(r"`+", text)), default=0)
    return "`" * max(3, longest + 1)


def cause_hint(rollback, log_tail):
    """Where to look first. A rollback that failed too means the commit that
    last passed fails now as well, so the cause is likely outside the repo
    (#524: Deribit testnet refused both with "bad handshake")."""
    if not rollback.startswith("**FAILED**"):
        return ""
    hint = ("\n- **Likely outside the repo:** the commit that last passed failed too. "
            "Check the Deribit testnet endpoint and its status, the mvp `.env` credentials, DNS and the network first.")
    if "bad handshake" in log_tail:
        hint += " Deribit refused the WebSocket connection: the HTTP status in the log says why (503 maintenance, 403 blocked, 404 endpoint moved)."
    return hint


def body(sha, step, rollback, log_tail, commits, pairs):
    hint = cause_hint(rollback, log_tail)
    log_tail = redact(tail(log_tail), pairs) or "(empty)"
    commits = redact(commits.strip(), pairs) or "(none)"
    f, g = fence(log_tail), fence(commits)
    return f"""🚀 **Deploy** — commit `{sha[:7]}` failed to deploy to **mvp** (testnet).

- **Failed step:** {step}
- **Rollback:** {rollback}{hint}
- **Not retried:** `mvp.failed` holds `{sha[:7]}`; the next commit on main deploys as usual.

### Commits in this deploy
{g}text
{commits}
{g}

### Last {TAIL_LINES} log lines ({step}, secrets redacted)
{f}text
{log_tail}
{f}

Filed by `scripts/agents/deploy.sh` (#204). Fix it on main: the next merge redeploys mvp.
<!-- deploy-failed: {sha} -->
"""


def _read(path):
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            return f.read()
    except OSError:
        return ""


def main(argv):
    cmd = argv[1] if len(argv) > 1 else ""
    if cmd == "title" and len(argv) == 4:
        print(title(argv[2], argv[3]))
    elif cmd == "find" and len(argv) == 3:
        n = find(argv[2], json.load(sys.stdin))
        if n is not None:
            print(n)
    elif cmd == "body" and len(argv) >= 6:
        meta = _read(argv[3]).splitlines() + ["", ""]
        pairs = sorted((p for e in argv[6:] for p in secrets(_read(e))), key=lambda nv: -len(nv[1]))
        sys.stdout.write(body(argv[2], meta[0] or "unknown", meta[1] or "unknown",
                              _read(argv[4]), _read(argv[5]), pairs))
    else:
        print(__doc__, file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
