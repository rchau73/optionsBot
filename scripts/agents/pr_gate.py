#!/usr/bin/env python3
"""pr_gate.py — decides, for each open Dev agent pull request, whether a person
may pick it up (product owner, 2026-10-19: "I'd rather pick only ready tickets
for review"). Pure rules here; pr_gate.sh reads GitHub and acts.

    pr_gate.py decide <pr.json> <markers.txt> <open_deps.txt> [<equiv.txt> <files.txt> <behind> <auto> [<card status>]]
        → one line: ACTION reason
    pr_gate.py equiv <compare.json>   → the base sha when every commit after it
        is a clean "update with main" merge GitHub made; nothing otherwise
    pr_gate.py green <pr.json>        → exit 0 when the PR is ready (not a
        draft) and CI is green on its head: the Reviewer starts only then (#472)
    pr_gate.py ci-green <pr.json>     → exit 0 when CI is green, draft or not
        (pr_gate.sh: a re-run of untouched tests passed → they are flaky)

pr.json: gh pr view --json number,state,isDraft,labels,headRefOid,mergeable,
mergeStateStatus,statusCheckRollup. markers.txt: the Reviewer's markers on the
PR, one "sha=<sha> verdict=<v> [policy=<yes|no>]" per line (verdict
incomplete = a changed file left unread, #476: waits as not reviewed). open_deps.txt:
issue numbers the PR's ticket still waits for, one per line. equiv.txt: shas
whose review counts for the head (`equiv`). files.txt: the PR's changed paths.
behind: commits of main the head lacks. auto: 1 = merge policy-free PRs.
card status: the ticket's board column ("In progress" = a Dev agent holds it).

Actions:
  conflict  — conflicts with main: the card goes back to Ready (needs-rebase)
              and the Dev agent merges main and resolves.
  ci_red    — CI failed on the head of a PR whose card is In review (#472):
              pr_gate.sh re-runs it when only tests the PR does not touch
              failed (checkfail.py ci-plan), else sends the card back to Ready
              with the failed job's log (needs-fix; the second time a person).
  undraft   — a draft whose card is In review and whose CI is green: the Dev
              agent opens every PR as a draft and hands the full suite to CI
              (#472); the gate marks it ready, and the Reviewer starts.
  update    — behind main without conflict: the script updates the branch.
  ready     — reviewed clean on the current head, CI green, GitHub CLEAN, no
              open dependency: label ready-to-merge. A person merges it when it
              changes a policy (the reason names which), or when auto is off.
  merge     — ready, up to date with main, and policy-free on both the path
              list and the Reviewer's declaration: the script merges it
              (product owner, 2026-10-28, #468: "I no longer need to approve
              PRs, unless they change business rules/policies").
  wait      — anything else (reason given): not ready, no action but the
              label comes off. A card In progress always waits, before any
              other rule: a Dev agent is working on it (#478).

Standard library only.
"""
import json
import re
import sys

sys.path.insert(0, __import__("os").path.dirname(__file__))
from pm_board import checks_refusal  # noqa: E402
from checkfail import failed_checks  # noqa: E402


# Paths whose change is a business rule or policy: a person merges (#468).
# A prefix ends in "/"; anything else is an exact path or a filename prefix.
POLICY_PATHS = [
    ("internal/strategy/", "trading rules"),
    ("internal/risk/", "margin policy"),
    ("internal/config/", "settings and fixed rule values"),
    ("config_", "tuned settings"),
    ("tests/testdata/effective_config_", "the effective settings"),
    ("OptionStrategy.md", "the strategy's rules"),
    ("internal/orders/", "the order path"),
    ("internal/gateway/", "the order path"),
    ("internal/exchange/", "the exchange boundary"),
    ("cmd/bot/", "the bot's wiring"),
    ("internal/assistant/guard/", "what the assistant may say (advice, forecasts)"),
    ("frontend/lib/auth/", "who may act (roles)"),
    ("docs/product/", "the PRD and product policies"),
    ("docs/legal/", "legal texts"),
    (".github/", "CI and repository rules"),
    (".githooks/", "the secrets hook"),
    (".claude/skills/", "the agents' instructions"),
    ("scripts/promote.sh", "releases"),
    ("scripts/cover_baseline", "the coverage floor"),
    ("scripts/agents/pr_gate.", "the merge rules"),
    ("scripts/agents/deploy.sh", "deploys"),
    ("scripts/agents/limit_lib.sh", "the agents' budget"),
]

# The merge `gh pr update-branch` makes: no conflict is possible in it, so the
# PR's own change is still the one the Reviewer saw.
UPDATE_SUBJECT = re.compile(r"^Merge branch 'main' into ")


def policy_paths(files):
    """The policy reasons the changed paths touch, each named once, in list order."""
    out = []
    for prefix, why in POLICY_PATHS:
        if any(f.startswith(prefix) for f in files) and why not in out:
            out.append(why)
    return out


def equiv(compare):
    """The base sha of a GitHub compare (base...head) when every commit in it is
    a clean update-with-main merge GitHub made, else None."""
    commits = compare.get("commits") or []
    if not commits:
        return None
    for c in commits:
        info = c.get("commit", {})
        subject = (info.get("message") or "").split("\n", 1)[0]
        if len(c.get("parents", [])) != 2 or info.get("committer", {}).get("name") != "GitHub" or not UPDATE_SUBJECT.match(subject):
            return None
    return compare.get("base_commit", {}).get("sha") or compare.get("merge_base_commit", {}).get("sha")


def decide(pr, markers, open_deps, equiv_shas=(), files=(), behind=0, auto=False, card=""):
    labels = {l["name"] for l in pr.get("labels", [])}
    if pr.get("state") != "OPEN":
        return "wait", "not an open pull request"
    if card == "In progress":
        # A Dev agent holds the card and may push to the PR any minute: never
        # hand it to a person, merge it or send it back (#478: PR #474 merged
        # while the agent fixed its review findings, and the fixes were stranded).
        return "wait", "a Dev agent is working on it"
    if pr.get("isDraft") and card != "In review":
        # A draft elsewhere (Blocked, Ready) waits for a person or the agent.
        return "wait", "not an open, ready pull request"
    if pr.get("mergeable") == "CONFLICTING" or pr.get("mergeStateStatus") == "DIRTY":
        return "conflict", "conflicts with main"
    failed = failed_checks(pr.get("statusCheckRollup"))
    if failed and card == "In review" and "agent:needs-human" not in labels:
        return "ci_red", "CI check " + ", ".join(name for _, name in failed) + " failed"
    if pr.get("isDraft"):
        why = checks_refusal(pr.get("statusCheckRollup"))
        return ("wait", f"a draft until CI is green: {why}") if why else ("undraft", "CI green on the draft")
    if open_deps:
        return "wait", "its ticket waits for " + ", ".join(f"#{n}" for n in open_deps)
    if pr.get("mergeStateStatus") == "BEHIND":
        return "update", "behind main"
    head = pr.get("headRefOid", "")
    counts = {head, *equiv_shas}
    verdict = policy = None
    for m in markers:
        parts = dict(p.split("=", 1) for p in m.split() if "=" in p)
        if parts.get("sha") in counts:
            verdict, policy = parts.get("verdict"), parts.get("policy")
    if verdict == "incomplete":
        # The Reviewer left a changed file unread (#476): not reviewed at all.
        return "wait", "not fully reviewed: a changed file was not read"
    if verdict != "clean":
        return "wait", "not yet reviewed clean on its current commit" if verdict is None else f"review verdict {verdict}"
    if "agent:needs-human" in labels:
        return "wait", "labelled agent:needs-human"
    why = checks_refusal(pr.get("statusCheckRollup"))
    if why:
        return "wait", why
    if pr.get("mergeStateStatus") not in ("CLEAN", "HAS_HOOKS"):
        return "wait", f"GitHub reports {pr.get('mergeStateStatus')}"
    reasons = [f"it touches {why}" for why in policy_paths(files)]
    if policy == "yes":
        reasons.append("the Reviewer marked it a policy change")
    elif policy != "no":
        reasons.append("the Reviewer did not say whether it changes a policy")
    if reasons:
        return "ready", "reviewed clean, CI green, no conflict; a person merges it: " + "; ".join(reasons)
    if not auto:
        return "ready", "reviewed clean, CI green, no conflict; auto-merge is off (PR_AUTO_MERGE=0)"
    if behind > 0:
        return "update", f"behind main by {behind}: updated before it merges"
    return "merge", "reviewed clean, CI green, up to date with main, no policy change"


def green(pr):
    """The Reviewer may start: a ready PR whose CI is green on its head (#472:
    it reviewed commits that then failed CI)."""
    return pr.get("state") == "OPEN" and not pr.get("isDraft") and not checks_refusal(pr.get("statusCheckRollup"))


def lines(path):
    return [l.strip() for l in open(path) if l.strip()]


def main(argv):
    if argv[1] in ("green", "ci-green"):
        pr = json.load(open(argv[2]))
        ok = green(pr) if argv[1] == "green" else not checks_refusal(pr.get("statusCheckRollup"))
        sys.exit(0 if ok else 1)
    if argv[1] == "equiv":
        sha = equiv(json.load(open(argv[2])))
        if sha:
            print(sha)
        return
    if argv[1] != "decide":
        raise SystemExit(f"unknown command {argv[1]}")
    pr = json.load(open(argv[2]))
    deps = [int(l) for l in lines(argv[4])]
    extra = argv[5:10]
    equiv_shas, files = (lines(extra[0]), lines(extra[1])) if extra else ((), ())
    behind, auto = (int(extra[2] or 0), extra[3] == "1") if extra else (0, False)
    card = extra[4] if len(extra) > 4 else ""
    action, why = decide(pr, lines(argv[3]), deps, equiv_shas, files, behind, auto, card)
    print(action, why)


if __name__ == "__main__":
    main(sys.argv)
