# pr-size-labeler

[![Build Status](https://img.shields.io/github/actions/workflow/status/kvokka/pr-size-labeler/ci.yml)](https://github.com/kvokka/pr-size-labeler/actions)
[![Open in HuggingFace](https://img.shields.io/badge/Open%20in-HuggingFace-9595ff?logo=huggingface&logoColor=white)](https://huggingface.co/spaces/kvokka/pr-size-labeler/tree/main)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![GitHub release (latest by date)](https://img.shields.io/github/v/release/kvokka/pr-size-labeler)](https://github.com/kvokka/pr-size-labeler/releases/latest)
[![GitHub stars](https://img.shields.io/github/stars/kvokka/pr-size-labeler)](https://github.com/kvokka/pr-size-labeler/stargazers)
[![Sponsor](https://img.shields.io/badge/Sponsor-%E2%9D%A4-pink?logo=github-sponsors)](https://github.com/sponsors/kvokka)

`pr-size-labeler` is a fully open-source GitHub App service that applies `size/*` labels to pull requests based on effective code change size.

👉 **Install via [GitHub App directly](https://github.com/apps/pr-size-labeler/)**
👉 **Install via [GitHub marketplace](https://github.com/marketplace/pr-size-labeler/)**

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
- **[Proactive relabeling](#proactive-relabeling)**: optionally relabel existing open pull requests on app install or after merged `.github/labels.yml` changes instead of waiting for each PR to receive a new event.
- **[Startup recovery for missed failed deliveries](#startup-recovery-for-missed-failed-deliveries)**: optionally recover recent failed webhook deliveries after downtime or restarts so missed labeling events are less likely to stay missed.
- **No GitHub Actions quota usage**: labeling runs in the app service, not in GitHub-hosted runners, so it does not consume Actions minutes or compete with CI jobs.

## Example

![screenshot](assets/labels.png)

Live [repo](https://github.com/kvokka/pr-size-labeler-test/pulls)

## What it does

For normal pull request labeling, `pr-size-labeler`:

1. loads `.github/labels.yml` from the repository default branch when present, otherwise uses the built-in default label set
2. reads pull request file additions/deletions and patch hunks
3. computes effective changed lines and effective changed symbols
4. subtracts files matched by `.gitattributes` entries marked `linguist-generated=true`
5. chooses the largest eligible size label when either that label's `lines` threshold or its `symbols` threshold is met
6. removes older configured size labels
7. creates the selected repository label if needed
8. applies exactly one configured size label
9. optionally adds one configured comment

Normal labeling runs for:

- `pull_request.opened`
- `pull_request.reopened`
- `pull_request.synchronize`
- `pull_request.edited`

Proactive relabeling runs only when `.github/labels.yml` explicitly enables it:

- on repository connect (`installation.created`, `installation_repositories.added`)
- on merged `pull_request.closed` events into the default branch when that PR changed `.github/labels.yml`

Config behavior:

- if `.github/labels.yml` is missing, normal PR labeling uses built-in defaults and backfill stays disabled
- if `.github/labels.yml` contains unknown keys, they are ignored and the triggering PR gets a warning comment
- if `.github/labels.yml` has invalid values, the triggering PR gets a warning comment and that run skips label changes
- if the selected repository label does not exist yet, the app creates it

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

`labels.yml` is now the single repository-owned source of truth for both label selection and optional proactive relabeling.

Top-level keys:

- `backfill` (optional): proactive relabel settings; if omitted, backfill defaults to disabled
- `backfill.enabled` (optional): controls whether install-time and merged-config relabeling run; default `false`
- `backfill.lookback` (optional): proactive relabel age window; default `720h` (30 days)
- `labels` (optional): override map for size keys (`XS`, `S`, `M`, `L`, `XL`, `XXL`); if omitted, the built-in label set is used unchanged

Each label supports:

- `name`
- `lines`
- `symbols` (optional)
- `comment` (optional)
- `color` (optional; used when the app creates a missing repository label)

If `symbols` is omitted, it defaults to `lines * 100`.

You only need to include the sections and keys you want to override.

Unknown keys are ignored. On `pull_request`-driven runs, the app leaves a warning comment on the triggering PR so the config problem is visible without checking server logs.

Example:

```yaml
backfill:
  enabled: true
  lookback: 168h
labels:
  XS:
    name: size/XS
    lines: 0
    symbols: 0
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
  L:
    name: size/L
    lines: 100
    symbols: 10000
  XL:
    name: size/XL
    lines: 500
  XXL:
    name: size/XXL
    lines: 1000
    comment: |
      This PR is very large. Consider splitting it up.
```

`labels.yml` is always read from the repository default branch.

That is different from `.gitattributes`, which is still read from the pull request base branch.

For selection, a label is eligible when either its `lines` threshold or its `symbols` threshold is met.

Effective changed lines are counted as `additions + deletions`, matching GitHub diff totals. That means one removed line counts as one changed line, and one modified line counts as two changed lines.

If you enable backfill, `lookback` is a pull request age window. The app relabels only open pull requests whose `created_at` is inside that window.

Duration format note:

- use standard Go duration syntax with `h`, `m`, and `s` units
- valid examples: `100h`, `72h30m`, `720h`
- invalid examples: `1y`, `30d`

## Quick start

### 1. Create a GitHub App

Use the checked-in `app.yml` manifest or configure the app manually with:

- events:
  - `pull_request`
  - `installation`
  - `installation_repositories`
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

See [`docs/github-app.md`](docs/github-app.md) for where each value comes from.

Optionally add `.github/labels.yml` on the default branch if you want custom thresholds, custom names/comments/colors, or proactive backfill. If the file is absent, normal pull request labeling still works with the built-in defaults.

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

### Proactive relabeling

When `backfill.enabled: true`, the app proactively relabels already-open pull requests:

- when repositories are connected to the app
- when a merged PR updates `.github/labels.yml` on the default branch

Behavior notes:

- only open pull requests are relabeled
- only pull requests created inside `backfill.lookback` are relabeled
- direct pushes do not trigger relabeling
- merged PR relabeling runs only when the merged PR touched exact path `.github/labels.yml`
- if `.github/labels.yml` already exists on the default branch before installation, the installation event uses that existing file immediately
- normal pull request labeling does not require `backfill.enabled`

How to backfill existing open pull requests:

1. Add or update `.github/labels.yml` on the default branch so backfill is enabled. Minimal example:

```yaml
backfill:
  enabled: true
  lookback: 720h
```

2. If the app is not installed yet, make sure that config already exists on the default branch and then install the app. The installation event will use that pre-existing file and relabel open pull requests inside the lookback window.
3. If the app is already installed, merge a pull request that changes `.github/labels.yml` on the default branch with `backfill.enabled: true`. That merge event will relabel open pull requests inside the lookback window.
4. The app will create any missing size labels as it applies them.
5. If you only wanted a one-time backfill, merge a follow-up config change that sets `backfill.enabled: false` again after the relabel run you wanted has already happened.

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

Pushing a canonical full semver tag in the form `vMAJOR.MINOR.PATCH` runs the separate `release.yml` workflow. That workflow first runs the shared local CI action for test/build validation, then creates the GitHub Release for the tagged commit, builds platform-specific Go binaries, and uploads them as Release assets. The GitHub App runtime binary should be taken from Releases rather than rebuilt manually if you want a published artifact.

Users should create releases with full tags such as:

- `v0.1.2`
- `v0.1.3`
- `v1.0.0`

After the release workflow starts from a successful CI run for the tagged commit, it creates or moves these shortcut tags to the same commit as that release:

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
