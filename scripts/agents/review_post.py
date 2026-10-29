#!/usr/bin/env python3
"""review_post.py — turns the Reviewer agent's answer into a GitHub PR review.

    review_post.py parse <agent.md> <review.json>
        Reads the agent's REVIEW-JSON block, keeps the findings with a known
        severity and adds the verdict: "blocking" if any finding blocks, else
        "clean", and the policy declaration ("yes" | "no"; anything else is
        "yes", so a PR the Reviewer did not judge waits for a person, #468).
        Exits 1 when the block is missing or not valid JSON.

    review_post.py body <review.json> <out.json> <slug> <head> <round> <next> <marker>
        Writes two review payloads for POST /pulls/{n}/reviews: <out.json> with
        the findings as inline comments, and <out.json>.plain with the body
        alone (GitHub refuses the whole review when one inline comment falls
        outside the diff). The body lists every finding either way.

Standard library only: launchd runs it with the system python3.
"""
import json
import re
import sys

SEVERITIES = ("blocking", "suggestion")
ICON = {"blocking": "🛑", "suggestion": "💡"}
NEXT = {
    "ready": "**Next:** blocking findings → the card goes back to **Ready**; the Dev agent will address them on this PR.",
    "human": "**Next:** still blocking after {fixes} fix round(s) → a person decides (the card stays in **In review**, `agent:needs-human`).",
    "clean": "**Next:** no blocking findings → the PR gate merges it once CI is green and it is up to date with main.",
    "clean-policy": "**Next:** no blocking findings, but it changes a business rule or policy → a person merges it.",
}


def parse(agent_md, review_json):
    text = open(agent_md).read()
    m = re.search(r"<!-- REVIEW-JSON\s*(.*?)\s*-->", text, re.S)
    if not m:
        sys.exit("no REVIEW-JSON block in the agent's answer")
    review = json.loads(m.group(1))
    findings = [f for f in review.get("findings", []) if f.get("severity") in SEVERITIES]
    review["findings"] = findings
    review["verdict"] = "blocking" if any(f["severity"] == "blocking" for f in findings) else "clean"
    pol = review.get("policy") if isinstance(review.get("policy"), dict) else {}
    review["policy"] = {"changes": "no" if pol.get("changes") == "no" else "yes",
                        "why": str(pol.get("why") or "not stated").strip()}
    json.dump(review, open(review_json, "w"), indent=1)


def line_of(f):
    try:
        return int(f.get("line") or 0)
    except (TypeError, ValueError):
        return 0


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
    if review.get("checked"):
        out += ["<details><summary>What the reviewer checked</summary>", "", review["checked"].strip(), "", "</details>", ""]
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


if __name__ == "__main__":
    cmd, args = sys.argv[1], sys.argv[2:]
    {"parse": parse, "body": body}[cmd](*args)
