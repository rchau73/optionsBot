.PHONY: build test cover fmt vet check diagrams hooks

COVER_OUT := coverage.out

build:
	go build -o bot ./cmd/bot

# The suite in two groups (scripts/go_tests.sh, #429): the core under -race, the
# single-goroutine report and research tests without it.
test:
	bash scripts/go_tests.sh all

# Tests live in ./tests, so coverage is measured across ./internal/... explicitly.
cover:
	bash scripts/go_tests.sh all $(COVER_OUT)
	go tool cover -func=$(COVER_OUT) | tail -1

fmt:
	gofmt -w cmd internal tests

vet:
	go vet ./...

# check is what CI runs: formatting, vet, the shell scripts (bash -n, and
# shellcheck when installed), the test suite in its two groups (the core under
# -race, #429) and the coverage floor (scripts/cover_baseline.txt, #330) on
# their merged profile. CI runs the two groups as parallel jobs.
check:
	@test -z "$$(gofmt -l cmd internal tests)" || (echo "gofmt needed:"; gofmt -l cmd internal tests; exit 1)
	go vet ./...
	bash scripts/check_shell.sh
	python3 -m unittest discover -s scripts/docs -p 'test_*.py'
	bash scripts/go_tests.sh all $(COVER_OUT)
	bash scripts/check_cover.sh --profile $(COVER_OUT)

# hooks enables .githooks/pre-commit (refuses .env files and any value from
# the local .env in a commit — the repository is public). Once per clone.
hooks:
	git config core.hooksPath .githooks

diagrams:
	cd docs && for f in *.mmd; do mmdc -i "$$f" -o "$${f%.mmd}.png" -c mmdc_config.json -s 4 -w 1600 -b white; done
