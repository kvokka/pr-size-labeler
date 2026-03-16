# Contributing

## Requirements

- Go 1.26
- a GitHub App for end-to-end webhook testing

## Local workflow

```bash
go test ./...
mkdir -p dist && go build -o dist/pr-size ./cmd/pr-size
go run ./cmd/pr-size
```

## Development rules

- keep changes transparent and OSS-first
- do not add billing, marketplace, or payment logic
- keep `.gitattributes` and `.github/labels.yml` behavior explicit and documented
- prefer small, test-first changes

## Suggested contribution flow

1. add or update tests first
2. implement the smallest change that makes them pass
3. run `go test ./...`
4. build with `mkdir -p dist && go build -o dist/pr-size ./cmd/pr-size`
5. update docs when behavior or operations change

## Manual verification

At minimum before opening a PR:

```bash
go test ./...
mkdir -p dist && go build -o dist/pr-size ./cmd/pr-size
```

If you touch webhook handling, also run the server locally and deliver a signed test webhook or equivalent integration test.
