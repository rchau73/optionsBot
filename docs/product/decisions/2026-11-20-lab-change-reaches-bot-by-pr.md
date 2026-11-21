# A confirmed lab change reaches the bot through a reviewed config pull request

- **Date:** 2026-11-20
- **Decision:** a lab change a manager confirmed (recorded, then checked by `cmd/labapply` into a ready candidate) reaches the bot only as a draft pull request that `cmd/labpr` opens on branch `lab/<run id>`, changing only the recorded values in the shipped config and its twin; a person's merge is the approval and `deploy.sh` (mvp) and `scripts/promote.sh` (stable) the deploy; `cmd/labpr` is run by hand only, with no schedule
- **Options:** a reviewed config pull request and the usual restart by the deploy (chosen); hot-reload of a running bot's settings (a later story)
- **Reason:** the merge is a person's approval, git audits every change with its evidence (the backtested numbers and the risks in the pull request), and it reuses `deploy.sh` and its health check and rollback; hot-reload would change a running account with no review and needs its own safety work
- **Decided by:** proposed by the PM agent on #862; a person confirms it by merging #862's pull request
- **Epic:** E9
