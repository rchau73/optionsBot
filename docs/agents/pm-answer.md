# Talking to the PM

Part of [Agentic development](../../AGENTIC.md).
The PM's rules for the board: [pm.md](pm.md).

**Talking to the PM: answer on the ticket.**
The PM asks on `agent:pm` tickets.
To answer, or to give it new work, comment on that ticket (or on its PRD pull request), or file an issue labelled `agent:pm`.
Within 6 minutes [`pm_answer.sh`](../../scripts/agents/pm_answer.sh) picks up the newest person's message on it.
Claude runs with the same skill in a worktree, allowed to edit only `docs/product/`.
The script checks that nothing else changed and that the epic markers parse.
Then it either opens or revises the PRD pull request (`Closes #n`, card → In review) or replies on the ticket (questions add `agent:needs-human`).
The merge of that pull request approves the PRD.
**Who merges** (product owner on #151): the PM itself merges a PRD pull request that changes no strategy policy.
It gets the label `pm:self-merge`, and the script merges it once CI is green and GitHub reports it clean (`pm_board.py selfmerge`, re-checked on the exact head commit).
A **strategy policy** change gets `agent:needs-human` and waits for your merge: an approved epic gaining trading-rule areas (`pm_board.py policy`, checked by the script), or a change the agent declares on its `POLICY:` line (margin, exits, entry, repair, GEX, orders, kill switch, the autonomy line; a missing line counts as a policy change).
The declaration is also written into the PR body (`<!-- pm:policy=yes|no -->`) and `selfmerge` reads it again at merge time, so a PR without `pm:policy=no` is never self-merged, whatever its labels say.
Taking the label off before a reply, a revision or a failure is a precondition: if GitHub refuses, the run stops before your message is marked handled, and the waiting message keeps blocking the merge.
To stop a self-merge, remove the `pm:self-merge` label.
Commenting also stops it: the PM acts on your message first, and if it only replies, the label comes off.
A run that fails does not swallow your message (#441: on #185 a run stopped by a weekly limit hid the product owner's answer for 8 days).
Its comment carries `<!-- agent:pm-failed -->`, which `pm_board.py answered` does not count as the PM's answer.
A run stopped by a limit posts nothing and is retried after the pause.
A run that did not finish for another reason says so and is retried on a later tick, up to 3 runs of the same message (`MAX_ANSWER_ATTEMPTS`, counted in the PM state's `attempts`); the third failure adds `agent:needs-human` and marks the message handled.
Other failures (no comment, files outside `docs/product/`, a malformed epic marker) are not retried.
There is nothing to trigger from chat.
Every comment comes from one GitHub account, so the agents' comments carry a header (🧭 PM, 🤖 Dev/Reviewer, 🚀 deploy), and anything without one is read as yours.

**Design and the showcase.**
For UI epics the PM works with the [ux-designer](../../.claude/skills/ux-designer/SKILL.md) skill in interactive sessions: decisions inside the approved PRD are made without waiting for a person and recorded in the PRD's decision log.
The prototype (a private claude.ai Artifact) is presented on the epic issue (`prototype:review`), and the human approves it by adding `prototype:approved`.
That is when the product work turns into stories the squad builds.
