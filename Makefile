.PHONY: build test cover fmt vet check diagrams hooks

COVER_OUT := coverage.out

build:
	go build -o bot ./cmd/bot

test:
	go test ./tests/... -race -count=1

# Tests live in ./tests, so coverage is measured across ./internal/... explicitly.
cover:
	go test ./tests/... -race -count=1 -coverpkg=./internal/... -coverprofile=$(COVER_OUT)
	go tool cover -func=$(COVER_OUT) | tail -1

fmt:
	gofmt -w cmd internal tests

vet:
	go vet ./...

# check is what CI runs: formatting, vet and the race-enabled test suite.
check:
	@test -z "$$(gofmt -l cmd internal tests)" || (echo "gofmt needed:"; gofmt -l cmd internal tests; exit 1)
	go vet ./...
	go test ./tests/... -race -count=1

# hooks enables .githooks/pre-commit (refuses .env files and any value from
# the local .env in a commit — the repository is public). Once per clone.
hooks:
	git config core.hooksPath .githooks

diagrams:
	cd docs && for f in *.mmd; do mmdc -i "$$f" -o "$${f%.mmd}.png" -c mmdc_config.json -s 4 -w 1600 -b white; done
