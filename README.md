# pr-size-labeler

[![Build Status](https://img.shields.io/github/actions/workflow/status/kvokka/pr-size-labler/ci.yml)](https://github.com/kvokka/pr-size-labler/actions)
[![Open in HuggingFace](https://img.shields.io/badge/Open%20in-HuggingFace-9595ff?logo=huggingface&logoColor=white)](https://huggingface.co/spaces/kvokka/pr-size-labeler/tree/main)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![GitHub release (latest by date)](https://img.shields.io/github/v/release/kvokka/pr-size-labler)](https://github.com/kvokka/pr-size-labler/releases/latest)
[![GitHub stars](https://img.shields.io/github/stars/kvokka/pr-size-labler)](https://github.com/kvokka/pr-size-labler/stargazers)
[![Sponsor](https://img.shields.io/badge/Sponsor-%E2%9D%A4-pink?logo=github-sponsors)](https://github.com/sponsors/kvokka)

`pr-size-labeler` is a fully open-source GitHub App service that applies `size/*` labels to pull requests based on effective code change size.

It is inspired by [`noqcks/pull-request-size`](https://github.com/noqcks/pull-request-size), but built around a transparent workflow, free use, and easy setup for both public repositories and private self-hosted setups.

This project is intentionally transparent:

- no billing
- no marketplace plan logic
- no hidden hosted-app behavior
- reproducible Go 1.26 binary builds attached to GitHub Releases

## Why use this instead of a GitHub Action?

- **Standardized behavior across repositories**: one app deployment can apply the same size-label logic everywhere instead of copying and maintaining separate workflow files per repo.
- **No-code repository onboarding**: maintainers install the GitHub App and optionally tune `.github/labels.yml` instead of authoring and debugging Actions YAML.
- **Instant updates**: fixes and improvements ship once in the app deployment, so you do not need to wait for every repository to update its workflow.
- **No GitHub Actions quota usage**: labeling runs in the app service, not in GitHub-hosted runners, so it does not consume Actions minutes or compete with CI jobs.

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

On repository connect, the app can also backfill labels for already-open pull requests inside a configured lookback window.

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
  - `metadata: read`
  - `single_file: read`
- single-file paths:
  - `.gitattributes`
  - `.github/labels.yml`

`app.yml` is only a transparent reference. GitHub App settings are still applied and updated manually in the GitHub UI.

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
- optional `CONNECT_OPEN_PRS_BACKFILL_ENABLED`
- optional `CONNECT_OPEN_PRS_BACKFILL_LOOKBACK`

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

### Connect-time backfill for already-open pull requests

When a repository is connected to the app, `pr-size-labeler` can proactively label pull requests that are already open instead of waiting for their next `pull_request` webhook.

This feature is optional. If you leave it disabled, normal `pull_request` labeling still works unchanged.

For this repository's default GitHub Actions → Hugging Face deployment, connect-time backfill is enabled automatically with a `1y` lookback.

Environment variables:

- `CONNECT_OPEN_PRS_BACKFILL_ENABLED` — set to `true` to enable connect-time backfill
- `CONNECT_OPEN_PRS_BACKFILL_LOOKBACK` — lookback window for already-open pull requests; accepts normal Go durations and a `y` shorthand such as `1y`; default `1y`

Example:

```bash
CONNECT_OPEN_PRS_BACKFILL_ENABLED=true
CONNECT_OPEN_PRS_BACKFILL_LOOKBACK=1y
```

Behavior notes:

- this runs only when repositories are connected to the app
- it handles both initial installs and repositories added to an existing installation
- it does not require extra repository, organization, or account permissions beyond the normal permissions already listed for this app
- it only inspects pull requests that are still open
- it only processes pull requests created inside the configured lookback window
- it reuses the normal PR-labeling flow, including `.gitattributes`, `.github/labels.yml`, label cleanup, and optional comments

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

The release workflow creates a GitHub Release when you push a canonical full semver tag in the form `vMAJOR.MINOR.PATCH`, then builds platform-specific Go binaries and uploads them as Release assets. The GitHub App runtime binary should be taken from Releases rather than rebuilt manually if you want a published artifact.

Users should create releases with full tags such as:

- `v0.1.2`
- `v0.1.3`
- `v1.0.0`

CI then creates or moves these shortcut tags to the same commit as that release:

- `vMAJOR.MINOR`
- `vMAJOR`
- `MAJOR.MINOR.PATCH`
- `MAJOR.MINOR`
- `MAJOR`

For example, pushing `v0.1.2` creates or updates `v0.1`, `v0`, `0.1.2`, `0.1`, and `0` to point at that same commit. Pushing `v0.1.3` later creates `0.1.3` and moves the shared aliases `v0.1`, `v0`, `0.1`, and `0` forward to the new release commit while the GitHub Release itself remains `v0.1.3`.

## Contributing

Contribution setup and workflow are in [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Agent-oriented development

This repository includes [`AGENTS.md`](AGENTS.md) and `.sisyphus/plans/` so future automated development has explicit project context instead of hidden conventions.

## License

MIT.
