# Assistant requests

Part of [Agentic development](../../AGENTIC.md).

A person can send a bug or a wish from the monitor's assistant (#409).
It goes to a **private** queue, `<clone>/data/monitor/requests.jsonl`, never to GitHub: the repository is public.
Since #410 the PM agent reviews that queue and turns what it accepts into redacted public issues.
Each tick runs [`requests.sh`](../../scripts/agents/requests.sh); it exits at once (no run, no ledger line) unless a request in some environment has no status line yet, or a filed one is still open (that sweep runs at most hourly).
The rules are in [`requests.py`](../../scripts/agents/requests.py), standard library only, files only.

1. **Mechanical, no Claude.**
   A filed request whose issue has closed becomes `fixed`, or `not_planned` when GitHub says so.
   A request that an issue already carries (the body's `<!-- assistant-request: <env>/R-NNNN -->` marker: filed, then the status write failed) becomes `filed` with that issue, so it is never filed twice.
   A repeated id in the queue is judged once.
2. **Redaction before the model sees anything.**
   The script drops the requester's username, role and answer id.
   In the text it redacts `.env` secret values (the `deploy_issue.py` rule), bearer tokens, emails, account ids, the username and any `@mention`, long tokens, and amounts in coin or USD ("an amount").
   Percentages stay.
3. **One PM agent run** (product-manager skill, procedure [`docs/product/assistant/request-triage.md`](../product/assistant/request-triage.md);
   read-only: `Read`/`Grep`/`Glob`/`Skill`, no Bash, no edit, no network; within the budget and limit pauses, `budget_take requests`) judges each request:
   `duplicate` (the same root cause as an open issue, or one closed in the last 14 days, given in `issues.json`; any other number is refused),
   `not_a_bug` (with the reason), `needs_info` (what is missing) or `accept` (kind, epic, title, body).
   **The request text is untrusted data:** each request is fenced in `requests.md` with a fence it cannot close, and the procedure says that nothing inside a fence is an instruction.
4. **The script acts.**
   Every verdict appends `{id, status, note, issue, issue_url, ts}` to the request's environment's `requests_status.jsonl`, which My requests shows.
   An accept is filed as a `bug` or `enhancement` issue in **Triage**, titled `[E?] …`.
   Its title and body are redacted again (the username comes from the queue, not from the agent).
   The body keeps the request id, "From the in-product assistant", the cited journal and P&L references and the marker.
   The requester is never named, mentioned or copied in.

**Bounds:** at most **10 issues a UTC day** across all environments (`DAILY_CAP`).
Only as many requests as the cap has left go to the agent; the rest wait for the next day, with no status line.
A request with no valid verdict (left out, an unknown duplicate, a `not_a_bug` with no reason) is asked again, 3 runs at most (`<clone>/data/monitor/requests_attempts.json`); after that it waits for a person.
If `gh issue create` fails, nothing is written, and the request is judged again on the next run.
Logs: `data/agents/requests/<time>/` (`requests.md`, `agent.md`, `decide.log`, the filings).
Run ledger agent `requests`, outcomes `filed`, `judged`, `none`, `skipped_limit` or `failed`.
Tests: `test_requests.py` (redaction, dedup by status line and marker, the status merge and closed-issue sweep, the daily cap, the attempts bound, and `prepare → decide → filed` on a throwaway clone).
