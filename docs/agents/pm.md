# The PM agent: the product order

Part of [Agentic development](../../AGENTIC.md).
Answers to the PM and the design work: [pm-answer.md](pm-answer.md).

The SME agent says *what is wrong*; the PM agent says *what comes next* across everything on the board, against the roadmap a person approved.

| Rule | Why |
|---|---|
| A ticket goes to Ready only if its epic is `approved` in the PRD on `main` | the person approved the business requirement in advance, when they merged the PRD |
| A ticket that changes trading rules goes only if its epic lists that rule area (`rule_areas`: entry, exits, margin, …) | live order behaviour stays a person's call unless the PRD delegated it |
| A UI story goes only once the epic carries `prototype:approved` | the human sees and approves the prototype before the squad builds it |
| It takes out of Ready only cards it put there, and never touches In progress, In review, Blocked or Done | the squad's work in flight is not re-ordered under it |
| Every decision is a comment on the issue (epic, priority, why, or what a person must decide) | audit trail |
| `agent:pm` tickets (PRD changes, decision requests) never go to Ready, and the Dev agent never builds one | they are the PM's work, not code |

Like the SME agent it is read-only: Claude Code with the [product-manager](../../.claude/skills/product-manager/SKILL.md) skill prints its decisions, and [`pm_agent.sh`](../../scripts/agents/pm_agent.sh) applies only what [`pm_board.py`](../../scripts/agents/pm_board.py) allows (tested in `scripts/agents/test_pm_board.py`).
Without an approved PRD on `main` it does nothing.
