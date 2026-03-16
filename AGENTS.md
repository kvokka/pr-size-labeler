# AGENTS.md

## Project intent

This repository is an OSS, self-hostable replacement for `pull-request-size`. The core contract is simple: receive GitHub pull request webhooks, compute effective PR size, and apply exactly one configured size label.

## Important constraints

- no billing or marketplace features
- no hidden hosted-only behavior
- Go 1.26 is the runtime baseline
- release binaries are published through GitHub Releases
- `.gitattributes` exclusions and `.github/labels.yml` overrides are first-class behavior

## Key paths

- `cmd/pr-size-labeler/main.go` — process entrypoint
- `internal/webhook/` — HTTP webhook handling
- `internal/githubapi/` — GitHub REST calls
- `internal/generated/` — `.gitattributes` parsing and matching
- `internal/labels/` — default palette and threshold selection
- `internal/config/` — env and label config loading
- `.sisyphus/plans/` — implementation plans for larger work

## Standard commands

```bash
go test ./...
mkdir -p dist && go build -o dist/pr-size-labeler ./cmd/pr-size-labeler
go run ./cmd/pr-size-labeler
```

## Development preference

Prefer test-first changes. Keep diffs small. Preserve explicit behavior in docs when changing configuration, deployment, or release automation.

When a feature is added or updated, reflect that change in the docs and README in the same work.

Respect KISS, DRY, and YAGNI. Prefer the simplest solution, avoid duplication, and do not add behavior that is not needed for the current request.
