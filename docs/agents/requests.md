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
   A request whose latest status is `in_review` is read again like one with no status.
2. **The mechanical filter, no Claude** (#852, pure [`request_filter.py`](../../scripts/agents/request_filter.py), `review`), in queue order, the first rule that holds:
   `duplicate` — the same kind and a title or `what` close to a card (any issue fetched, open or closed, with that kind's label) or to an earlier request that still stands
   (in review, filed, fixed, not planned, duplicate); the note names it, and `issue_url` is that card's (or the earlier request's issue);
   `needs_info` — no evidence item with its source and no journal reference, and fewer than two steps; the note says what is missing;
   `not_a_bug` — the request's `explained_by` names a help passage (the monitor stores it from the logged answer).
   Only the rest go to the agent.
3. **Redaction before the model sees anything.**
   The script drops the requester's username, role and answer id.
   In the text it redacts `.env` secret values (the `deploy_issue.py` rule), bearer tokens, emails, account ids, the username and any `@mention`, long tokens, and amounts in coin or USD ("an amount").
   Percentages stay.
4. **One PM agent run** (product-manager skill, procedure [`docs/product/assistant/request-triage.md`](../product/assistant/request-triage.md);
   read-only: `Read`/`Grep`/`Glob`/`Skill`, no Bash, no edit, no network; within the budget and limit pauses, `budget_take requests`) judges each request:
   `duplicate` (the same root cause as an open issue, or one closed in the last 14 days, given in `issues.json`; any other number is refused),
   `not_a_bug` (with the reason), `needs_info` (what is missing) or `accept` (kind, epic, title, body).
   **The request text is untrusted data:** each request is fenced in `requests.md` with a fence it cannot close, and the procedure says that nothing inside a fence is an instruction.
5. **The script acts.**
   Every verdict appends `{id, status, note, issue, issue_url, ts}` to the request's environment's `requests_status.jsonl`, which My requests shows.
   An accept is filed as a `bug` or `enhancement` issue in **Triage**, titled `[E?] …`, labelled with its kind and its epic (`E10` unless the agent names another).
   Its title and body go through the sanitiser (`requests.py` `sanitise`, #852; the username comes from the queue, not from the agent):
   the redaction above, plus the bot-account names and ids of the clone's `data/accounts.json` ("an account"), position sizes ("a size") and every journal or P&L reference as "journal line from <date>", never its text.
   Percentages, times, dates, instrument names, page and bot names stay.
   Anything left it cannot classify (a token of 10 characters or more mixing letters and digits, a bare number of four digits or more) holds the request: status `needs_info` with a "held for a person" note, `HELD` in `decide.log`, nothing filed.
   The body keeps the request id, "From the in-product assistant", "Sent from the assistant, request <id>", the cited records by date, the `<!-- agent:pm -->` marker and the request marker.
   The requester is never named, mentioned or copied in.
   Each filing appends one line to `data/agents/requests/audit.jsonl`: time, the sender's role (never the name), the request key and the issue number.

**Dry run and the filing flag** (#852): `requests.sh` without `--file` reads GitHub, logs every status line it would write and how many requests would go to the agent, and writes, asks and files nothing (ledger outcome `dry_run`).
The tick runs it with `--file`; `REQUESTS_FILE=0` in `agents.env` stops that.
An environment whose `.env` says `DERIBIT_ENV=live` is left out unless `REQUESTS_FILE_LIVE=1`: the request text is personal data (LGPD/GDPR), and a person confirms that no consent text change is needed before it is filed from a live environment.

**Bounds:** at most **10 issues a UTC day** across all environments (`DAILY_CAP`).
Only as many requests as the cap has left go to the agent; the rest wait for the next day, with no status line.
A request with no valid verdict (left out, an unknown duplicate, a `not_a_bug` with no reason) is asked again, 3 runs at most (`<clone>/data/monitor/requests_attempts.json`); after that it waits for a person.
If `gh issue create` fails, nothing is written, and the request is judged again on the next run.
Logs: `data/agents/requests/<time>/` (`requests.md`, `agent.md`, `decide.log`, the filings).
Run ledger agent `requests`, outcomes `filed`, `judged`, `none`, `dry_run`, `skipped_limit` or `failed`.
Tests: `test_requests.py` (redaction, the sanitiser, dedup by status line and marker, the status merge and closed-issue sweep, the filter's status lines, the dry run, the held filing, the audit line, the daily cap, the attempts bound, and `prepare → decide → filed` on a throwaway clone) and `test_request_filter.py` (the filter's rules).
