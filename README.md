# pr-size-labeler

`pr-size-labeler` is a fully open-source GitHub App service that applies `size/*` labels to pull requests based on effective code change size.

It is inspired by [`noqcks/pull-request-size`](https://github.com/noqcks/pull-request-size), but built around a transparent workflow, free use, and easy setup for both public repositories and private self-hosted setups.

This project is intentionally transparent:

- no billing
- no marketplace plan logic
- no hidden hosted-app behavior
- reproducible Go 1.26 binary builds attached to GitHub Releases

## Example

![screenshot](assets/labels.png)

Live [repo](https://github.com/kvokka/pr-size-labler-test/pulls)

## What it does

For each supported pull request event, `pr-size-labeler`:

1. reads pull request file additions/deletions and patch hunks
2. computes effective changed lines and effective changed symbols (added/deleted diff lines only; diff metadata/context ignored)
3. subtracts files matched by `.gitattributes` entries marked `linguist-generated=true` from both totals
4. loads per-label `lines` thresholds and optional per-label `symbols` thresholds from `.github/labels.yml`
5. chooses the largest eligible size label when either that label's `lines` threshold or its `symbols` threshold is met
6. removes older configured size labels
7. creates the chosen label if needed
8. optionally adds one configured comment

Supported actions:

- `pull_request.opened`
- `pull_request.reopened`
- `pull_request.synchronize`
- `pull_request.edited`

## Default labels

The thresholds intentionally follow the same sizing model as the original `pull-request-size` project, but the default colors are slightly different.

| Key | Label | Lines | Color |
| --- | --- | ---: | --- |
| XS | `size/XS` | 0 | `2FBF6B` |
| S | `size/S` | 10 | `55A84B` |
| M | `size/M` | 30 | `7A9135` |
| L | `size/L` | 100 | `9F6A27` |
| XL | `size/XL` | 500 | `C44319` |
| XXL | `size/XXL` | 1000 | `E91C0B` |

## Repository configuration

### `.gitattributes`

`pr-size-labeler` uses the same basic idea as the reference project: patterns marked `linguist-generated=true` are excluded from size totals.

Example:

```gitattributes
generated/* linguist-generated=true
vendor/** linguist-generated=true
```

In this implementation, `.gitattributes` is read from the pull request base branch.

### `.github/labels.yml`

You can override label names, thresholds, colors, and optional comments.

Each label supports:

- `lines`: the changed-line threshold for that label
- `symbols` (optional): the changed-symbol threshold for that label

If `symbols` is omitted, it defaults to `lines * 100`. That keeps existing `lines`-only configs working unchanged while letting you tune symbol sensitivity per label.

Example:

```yaml
XS:
  name: size/XS
  lines: 0
  symbols: 0
  color: 2FBF6B
S:
  name: size/S
  lines: 10
  symbols: 800
  color: 55A84B
  comment: |
    This PR is still in the small range.
M:
  name: size/M
  lines: 30
  color: 7A9135
L:
  name: size/L
  lines: 100
  symbols: 10000
  color: 9F6A27
XL:
  name: size/XL
  lines: 500
  color: C44319
XXL:
  name: size/XXL
  lines: 1000
  color: E91C0B
  comment: |
    This PR is very large. Consider splitting it up.
```

Like `.gitattributes`, `labels.yml` is read from the pull request base branch.

For selection, a label is eligible when either its `lines` threshold or its `symbols` threshold is met.

## Quick start

### 1. Create a GitHub App

Use the checked-in `app.yml` manifest or configure the app manually with:

- event: `pull_request`
- permissions:
  - `pull_requests: write`
  - `contents: read`
  - `metadata: read`

More detail: [`docs/github-app.md`](docs/github-app.md)

### 2. Configure environment

Copy `.env.example` and set:

- `APP_ID`
- `PRIVATE_KEY`
- `WEBHOOK_SECRET`
- optional `LISTEN_ADDR`
- optional `GITHUB_API_BASE_URL`
- optional `LOG_PRIVATE_DETAILS`
- optional `STARTUP_FAILED_DELIVERY_RECOVERY_ENABLED`
- optional `STARTUP_FAILED_DELIVERY_RECOVERY_LOOKBACK`

See [`docs/github-app.md`](docs/github-app.md) for where each value comes from.

### Startup recovery for missed failed deliveries

If you deploy on Hugging Face Spaces, rebuilds and restarts can make the webhook endpoint temporarily unavailable. `pr-size-labeler` can optionally try to recover from that on process startup by listing recent **failed** GitHub App webhook deliveries and asking GitHub to redeliver them.

For this repository's default GitHub Actions → Hugging Face deployment, startup recovery is enabled automatically. The code-level default is still `false`, but the deploy workflow sets `STARTUP_FAILED_DELIVERY_RECOVERY_ENABLED=true` for the Space.

Environment variables:

- `LOG_PRIVATE_DETAILS` — set to `true` to include request IP/header details, installation IDs, and detailed startup/startup-recovery diagnostics in logs; default `false`
- `STARTUP_FAILED_DELIVERY_RECOVERY_ENABLED` — set to `true` to enable startup recovery
- `STARTUP_FAILED_DELIVERY_RECOVERY_LOOKBACK` — Go duration string for how far back to inspect deliveries, default `2h`

Example:

```bash
STARTUP_FAILED_DELIVERY_RECOVERY_ENABLED=true
STARTUP_FAILED_DELIVERY_RECOVERY_LOOKBACK=2h
```

Behavior notes:

- this runs **once on startup**, not on a schedule
- the HTTP server starts first, then recovery runs in the background so redeliveries can reach a live process
- it only looks at deliveries inside the configured lookback window
- it only redelivers deliveries whose GitHub delivery `status` is not `OK`
- repeated restarts inside the same lookback window can cause the same failed original delivery to be redelivered again
- GitHub only allows webhook redelivery for recent deliveries, so this is a best-effort recovery tool, not a durable queue

### 3. Run locally

```bash
go run ./cmd/pr-size-labeler
```

### 4. Test

```bash
go test ./...
```

### 5. Build a release binary locally

```bash
mkdir -p dist && go build -o dist/pr-size-labeler ./cmd/pr-size-labeler
```

## Deployment

Deployment guidance lives in [`docs/deployment.md`](docs/deployment.md).

That includes:

- the default public Hugging Face deployment workflow for this repo
- the private Hugging Face setup path
- the private-Space plus public-proxy pattern for keeping execution logs private while still exposing a public endpoint when needed
- the GitHub-side and Hugging Face-side secrets and variables required for the app to run

## Releases

The release workflow builds platform-specific Go binaries and uploads them as GitHub Release assets. The GitHub App runtime binary should be taken from Releases rather than rebuilt manually if you want a published artifact.

## Contributing

Contribution setup and workflow are in [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Agent-oriented development

This repository includes [`AGENTS.md`](AGENTS.md) and `.sisyphus/plans/` so future automated development has explicit project context instead of hidden conventions.

## License

MIT.
