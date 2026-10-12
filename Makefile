VERSION ?= 0.0.0-dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/MicroClub-USTHB/mctf-cli/cmd.version=$(VERSION) \
           -X github.com/MicroClub-USTHB/mctf-cli/cmd.commit=$(COMMIT) \
           -X github.com/MicroClub-USTHB/mctf-cli/cmd.date=$(DATE)

.PHONY: build test test-integration lint tidy run clean schemas man

build: schemas
	go build -ldflags "$(LDFLAGS)" -o mctf .

# Generate man pages (one per command) into manpages/. Release archives and
# the .deb/.rpm packages bundle these; goreleaser runs the same generator.
man:
	@mkdir -p manpages
	@go run . gen-man manpages
	@echo "man pages written to manpages/"

# Sync the canonical /schema/ JSON files into the embed package so the
# binary stays self-contained. Errors out if drift is detected after sync,
# which means someone edited internal/schemas/*.json directly.
schemas:
	@cp schema/*.json internal/schemas/
	@if ! diff -q schema/challenge.schema.json internal/schemas/challenge.schema.json >/dev/null; then \
	    echo "schema drift detected"; exit 1; fi

test:
	go test ./...

# Integration tests (gated by the `integration` build tag). Boots a real
# sshd container + exercises the SSH runner end-to-end. Requires docker
# daemon + ssh/scp/ssh-keygen on PATH; skips gracefully otherwise.
test-integration:
	go test -tags integration -run Integration ./...

lint:
	go vet ./...
	@command -v golangci-lint >/dev/null || { echo "golangci-lint not installed; see https://golangci-lint.run/welcome/install/"; exit 1; }
	golangci-lint run ./...

tidy:
	go mod tidy

run:
	go run -ldflags "$(LDFLAGS)" . $(ARGS)

clean:
	rm -f mctf
