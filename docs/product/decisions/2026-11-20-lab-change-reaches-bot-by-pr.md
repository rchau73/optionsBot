# A confirmed lab change reaches the bot through a reviewed config pull request

- **Date:** 2026-11-20
- **Decision:** a confirmed lab change reaches the bot only as a draft pull request `cmd/labpr` opens from a ready candidate (only the recorded values, in the shipped config and its twin); a person's merge is the approval, the usual deploy restarts the bot, and `cmd/labpr` runs by hand only
- **Details:** the branch is `lab/<run id>`; mvp deploys by `deploy.sh`, stable by `scripts/promote.sh`; `cmd/labapply` checks the record into the candidate first
- **Options:** a reviewed config pull request and the usual restart by the deploy (chosen); hot-reload of a running bot's settings (a later story)
- **Reason:** the merge is a person's approval, git audits every change with its evidence (the backtested numbers and the risks in the pull request), and it reuses `deploy.sh` and its health check and rollback; hot-reload would change a running account with no review and needs its own safety work
- **Decided by:** proposed by the PM agent on #862; a person confirms it by merging #862's pull request
- **Epic:** E9
