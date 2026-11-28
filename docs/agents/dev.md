# Check failures go back to the Dev agent

Part of [Agentic development](../../AGENTIC.md).

Product owner, 2026-10-28/29 (#472): "Evaluate always possible CI/CD improvement to speed up SDLC", and failed checks must go back to the Dev agent, not wait for a person.
#484, #496, #500 and #509 sat in Blocked on 28–29 Oct.
Rules in [`checkfail.py`](../../scripts/agents/checkfail.py), tested in `test_checkfail.py`.

- **CI runs every suite, the Mac only what the change touches.**
  `dev_agent.sh` runs gofmt/vet, the scripts' checks, the Go tests and jest files the change touches and eslint; `next build` and the rest run in CI.
  It also runs the checks that read every file (#467, `checkfail.py repo-wide`: #445's agent missed the glossary and responsive-audit tests):
  for a frontend change the glossary, responsive-audit, empty-states and menu tests, and `go run ./cmd/helpindex` (the index must not change) when a help source changed.
  The agent runs them before it hands over, and merges `origin/main` as its last step and reruns its tests;
  the script merges `origin/main` again before its checks (a conflict only in generated files is regenerated; any other is left to the PR gate's `needs-rebase`).
  Every PR opens as a **draft**; the PR gate marks it ready once CI is green, and only then does the Reviewer start (`pr_gate.py green`: never a commit that then fails CI).
- **Suites by what they read** (`checkfail.py suites`, also CI's path filter): a suite runs when a changed file is code it builds, or a doc one of its tests opens.
  Any `.md` runs the two doc-wide line checks (`TestClaudeDocs`, `TestDocs_`); a `CLAUDE.md` only those; an image or diagram source nothing.
  Docker runs only on `Dockerfile`, `go.*`, `cmd/`, `internal/` or frontend code; the vulnerability scan only on `go.mod`, `go.sum` or `package-lock.json`.
  Go lint (gofmt, vet) runs only on Go files, and the scripts' checks have their own job.
  A push to `main`, the nightly run (02:30 UTC) and a manual run run every job.
  `test_checkfail.py` scans `tests/*.go` and the frontend tests for the paths they open and fails when one would not trigger its suite, so a new test that reads a doc cannot be skipped silently.
- **A frontend card's worktree has its dependencies before the agent starts** (#900: #878's and #887's agents could run no jest in a fresh worktree and stopped).
  `git worktree add` leaves out `frontend/node_modules`.
  When `checkfail.py frontend-needed` says the card needs the frontend (its Files line names a frontend file or one the frontend tests read, a `frontend`/`ui`/`ux`/`a11y` label, the branch already changes one, or no Files line at all),
  `dev_agent.sh` runs `frontend_deps.sh`: `npm ci --no-audit --no-fund` in `frontend/`, logged to the run's `npm-ci.log`.
  A failed install blocks the card with the log's last lines and the agent is never started.
  The script's own checks reuse that install while `package-lock.json` is unchanged.
  The agent runs the frontend from the worktree root with `npm --prefix frontend test -- --ci __tests__/<file>` (and `run lint`, `run build`, `run test:phone`, `ci`), which npm runs in `frontend/`.
  `npx jest --config frontend/jest.config.mjs` from the root does not work: `next/jest` reads `package.json` from the current folder, and `npx` picks up a stray parent install.
  #887's refused `git add`/`git commit` left no session to read (`--no-session-persistence`); both are allowed, so the prompt now asks for each as its own command, with no `git -C`, `$(...)` or heredoc, the forms a prefix rule does not match.
- **A check failure goes back once.**
  The first failure on a card (the script's, or CI's on an In review PR's head) sends it to **Ready** with `needs-fix` and a comment holding the failing checks and each failing test's own output (its last 40 lines).
  The next Dev run reads that comment first and fixes it on the same PR.
  The second failure goes to a person (Blocked, `agent:needs-human`).
  The count lives on the issue (`<!-- dev:check-fail n=… -->`) and restarts after a person was asked (`<!-- dev:blocked -->`).
- **A blocked run keeps its commits** (#517, [`branch_work.sh`](../../scripts/agents/branch_work.sh), tested in `test_branch_work.py`).
  A run that stops for a person with commits ahead of `main` pushes its branch (no PR), so the next run on the card starts from it, not from `main`; with none it pushes nothing.
  With an open PR it does not push: the branch is the PR's, and a half-done push would reach its review.
  Without an open PR, the commits to deliver are every one ahead of `main`, so a run that resumes a pushed branch and adds nothing opens the PR instead of being blocked.
- **A failure in a test the change does not touch is retried, not blamed.**
  Touched = the test's file, a path it opens, a package it imports (transitively) or a helper of `tests/` it names changed.
  An untouched failing test is re-run: the script re-runs that test up to 3 times, the gate the failed CI jobs up to 3 times a head.
  Passing makes it **flaky**: named on the PR and on its `[E6] Flaky test: <name>` issue (`lib.sh note_flaky`, filed in Triage or commented), and the PR goes on.
  A jest file that failed only by timeouts firing over a minute late is **stalled** (`checkfail.py stalled`, as the nightly hunt's #587 rule): the Mac slept, so passing on a re-run is logged and no flaky issue is filed (#869).
  The script keeps the Mac awake while it runs (`caffeinate -is`), as the hunt does.
  A touched test is never retried.
- **Go test cache:** `actions/setup-go` already caches the build cache keyed on `go.sum`.
  Test results stay uncached (`-count=1`): many tests run bash, python or `go run`, whose file reads Go's test cache does not see, so a cached pass could hide a broken script.
