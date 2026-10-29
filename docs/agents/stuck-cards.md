# Stuck cards

Part of [Agentic development](../../AGENTIC.md).

A card can sit In progress or In review with nothing working on it: a run that died, a loop that stopped.
[`stuck.sh`](../../scripts/agents/stuck.sh) runs from the tick, at most once every 6 h (#329).
It makes no Claude call, moves nothing and changes no label.
The rules are in [`stuck.py`](../../scripts/agents/stuck.py), tested offline in `test_stuck.py`.

- **Stuck** = an open card **In progress for more than 6 h** or **In review for more than 24 h** since its last activity (the card's last update, or its newest run in the ledger), with no open run on it, and without `pm:waiting`, `agent:needs-human` or `ready-to-merge`.
- **Explained, not a fault:** if the agents are paused or out of budget in `agents.env` (`budget_reason`), or a usage limit was hit in the last 6 h (`data/agents/limit_hit`), the card is listed with that reason (`paused`, `budget: …`, `usage limit … ago`) and gets no comment.
- **A fault** gets one 🤖 comment naming the card, the idle hours and the last run with its log.
  The comment is deduplicated by a marker per card, column and last run (`data/agents/stuck/marks/`), so a card is reported again only after a new run stalls.
- Every card found is appended to `data/agents/stuck/reports.jsonl`.
  The weekly metrics read it.
