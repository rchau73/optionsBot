#!/usr/bin/env python3
"""review_post.py — turns the Reviewer agent's answer into a GitHub PR review.

    review_post.py parse <agent.md> <review.json> [<files.txt>]
        Reads the agent's REVIEW-JSON block, keeps the findings with a known
        severity and adds the verdict: "blocking" if any finding blocks, else
        "incomplete" when `checked` leaves a changed file of <files.txt>
        unread or out (#476: the Read tool stops at 2,000 lines, and a partial
        read must never come out clean), else "clean"; and the policy
        declaration ("yes" | "no"; anything else is "yes", so a PR the
        Reviewer did not judge waits for a person, #468).
        Exits 1 when the block is missing or not valid JSON.

    review_post.py body <review.json> <out.json> <slug> <head> <round> <next> <marker>
        Writes two review payloads for POST /pulls/{n}/reviews: <out.json> with
        the findings as inline comments, and <out.json>.plain with the body
        alone (GitHub refuses the whole review when one inline comment falls
        outside the diff). The body lists every finding either way.

    review_post.py due <head> <markers.txt>
        Prints "yes" when the head still needs a review: no marker of it yet,
        or only "incomplete" ones, fewer than MAX_INCOMPLETE (the first
        incomplete review is retried on the next tick, then a person reviews).
        Prints "no" otherwise.

Standard library only: launchd runs it with the system python3.
"""
import json
import re
import sys

SEVERITIES = ("blocking", "suggestion")
MAX_INCOMPLETE = 2  # incomplete reviews of one head before it is left for a person
DIFF_DIR = ".agent-review/diff/"
ICON = {"blocking": "🛑", "suggestion": "💡"}
NEXT = {
    "ready": "**Next:** blocking findings → the card goes back to **Ready**; the Dev agent will address them on this PR.",
    "human": "**Next:** still blocking after {fixes} fix round(s) → a person decides (the card stays in **In review**, `agent:needs-human`).",
    "clean": "**Next:** no blocking findings → the PR gate merges it once CI is green and it is up to date with main.",
    "clean-policy": "**Next:** no blocking findings, but it changes a business rule or policy → a person merges it.",
    "incomplete": "**Next:** not every changed file was read → not reviewed; the Reviewer tries this commit once more on the next tick.",
    "incomplete-human": "**Next:** still not every changed file read after a retry → a person reviews this PR (`agent:needs-human`).",
}


def norm_path(path):
    """A changed file's repo path from the one the Reviewer wrote: its diff
    file under .agent-review/diff/, or an a/ b/ ./ prefix, is taken back."""
    p = str(path or "").strip().strip("`")
    if p.startswith(DIFF_DIR):
        p = p[len(DIFF_DIR):]
        if p.endswith(".diff"):
            p = p[:-len(".diff")]
    for prefix in ("./", "a/", "b/"):
        if p.startswith(prefix):
            p = p[len(prefix):]
    return p


def unread(checked, files):
    """The changed files that `checked` does not list with read: yes, in the
    order of files. `checked` is [{"file", "read": "yes|no"}]; anything else
    (the old free-text form, nothing) reads no file."""
    read = set()
    if isinstance(checked, list):
        for c in checked:
            if isinstance(c, dict) and str(c.get("read", "")).strip().lower() == "yes":
                read.add(norm_path(c.get("file")))
    return [f for f in files if f not in read]


def due(head, markers):
    """True when the head still needs a review (see the module doc)."""
    verdicts = []
    for m in markers:
        parts = dict(p.split("=", 1) for p in m.split() if "=" in p)
        if parts.get("sha") == head:
            verdicts.append(parts.get("verdict"))
    return all(v == "incomplete" for v in verdicts) and len(verdicts) < MAX_INCOMPLETE


def lines(path):
    return [l.strip() for l in open(path) if l.strip()]


def parse(agent_md, review_json, files_txt=None):
    text = open(agent_md).read()
    m = re.search(r"<!-- REVIEW-JSON\s*(.*?)\s*-->", text, re.S)
    if not m:
        sys.exit("no REVIEW-JSON block in the agent's answer")
    review = json.loads(m.group(1))
    findings = [f for f in review.get("findings", []) if f.get("severity") in SEVERITIES]
    review["findings"] = findings
    review["unread"] = unread(review.get("checked"), lines(files_txt) if files_txt else [])
    if any(f["severity"] == "blocking" for f in findings):
        review["verdict"] = "blocking"
    elif review["unread"]:
        review["verdict"] = "incomplete"
    else:
        review["verdict"] = "clean"
    pol = review.get("policy") if isinstance(review.get("policy"), dict) else {}
    review["policy"] = {"changes": "no" if pol.get("changes") == "no" else "yes",
                        "why": str(pol.get("why") or "not stated").strip()}
    json.dump(review, open(review_json, "w"), indent=1)


def line_of(f):
    try:
        return int(f.get("line") or 0)
    except (TypeError, ValueError):
        return 0


def checked_section(review):
    """The body's account of what the Reviewer read and ran."""
    out = []
    if review.get("unread"):
        out += [f"**Not fully reviewed:** {len(review['unread'])} changed file(s) not read: "
                + ", ".join(f"`{f}`" for f in review["unread"]), ""]
    checked = review.get("checked")
    if isinstance(checked, list) and checked:
        rows = [f"- `{norm_path(c.get('file'))}` — read: {c.get('read', 'no')}" for c in checked if isinstance(c, dict)]
        out += ["<details><summary>Files the reviewer read</summary>", "", *rows, "", "</details>", ""]
    ran = review.get("ran") if isinstance(review.get("ran"), str) else (checked if isinstance(checked, str) else "")
    if ran.strip():
        out += ["<details><summary>What the reviewer ran</summary>", "", ran.strip(), "", "</details>", ""]
    return out


def body(review_json, out_json, slug, head, rnd, nxt, marker):
    review = json.load(open(review_json))
    findings = review["findings"]
    out = [f"🤖 **Reviewer agent** — round {rnd}, commit `{head[:7]}`", "", review.get("summary", "").strip(), ""]
    if findings:
        out += ["| | Finding | Where |", "|---|---|---|"]
        for f in findings:
            path, line = f.get("file", ""), line_of(f)
            where = f"[`{path}:{line}`](https://github.com/{slug}/blob/{head}/{path}#L{max(line, 1)})" if path else "—"
            out.append(f"| {ICON[f['severity']]} {f['severity']} | {f.get('title', '')} | {where} |")
        out.append("")
        for i, f in enumerate(findings, 1):
            out += [f"**{i}. {f.get('title', '')}** ({f['severity']})", "",
                    f"- **Failure:** {f.get('scenario', '')}", f"- **Fix:** {f.get('fix', '')}", ""]
    else:
        out += ["No blocking findings and no suggestions.", ""]
    out += checked_section(review)
    pol = review.get("policy") or {"changes": "yes", "why": "not stated"}
    out += [f"**Policy change:** {pol['changes']} — {pol['why']}", ""]
    if nxt == "clean" and pol["changes"] != "no":
        nxt = "clean-policy"
    out += [NEXT[nxt].format(fixes=int(rnd) - 1), "",
            f"<!-- {marker} sha={head} verdict={review['verdict']} policy={pol['changes']} -->"]
    text = "\n".join(out)

    comments = [{"path": f["file"], "line": line_of(f), "side": "RIGHT",
                 "body": f"{ICON[f['severity']]} **{f.get('title', '')}** ({f['severity']})\n\n"
                         f"{f.get('scenario', '')}\n\n**Fix:** {f.get('fix', '')}"}
                for f in findings if f.get("file") and line_of(f) > 0]
    json.dump({"commit_id": head, "event": "COMMENT", "body": text, "comments": comments}, open(out_json, "w"))
    json.dump({"commit_id": head, "event": "COMMENT", "body": text}, open(out_json + ".plain", "w"))


def due_cmd(head, markers_txt):
    print("yes" if due(head, lines(markers_txt)) else "no")


if __name__ == "__main__":
    cmd, args = sys.argv[1], sys.argv[2:]
    {"parse": parse, "body": body, "due": due_cmd}[cmd](*args)
