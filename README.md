# pr-size

`pr-size` is a fully open-source GitHub App service that applies `size/*` labels to pull requests based on effective lines changed.

It is inspired by [`noqcks/pull-request-size`](https://github.com/noqcks/pull-request-size), but built around a transparent workflow, free use, and easy setup for both public repositories and private self-hosted setups.

This project is intentionally transparent:

- no billing
- no marketplace plan logic
- no hidden hosted-app behavior
- reproducible Go 1.26 binary builds attached to GitHub Releases

## What it does

For each supported pull request event, `pr-size`:

1. reads the pull request additions and deletions
2. subtracts files matched by `.gitattributes` entries marked `linguist-generated=true`
3. chooses one size label
4. removes older configured size labels
5. creates the chosen label if needed
6. optionally adds one configured comment

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

`pr-size` uses the same basic idea as the reference project: patterns marked `linguist-generated=true` are excluded from size totals.

Example:

```gitattributes
generated/* linguist-generated=true
vendor/** linguist-generated=true
```

In this implementation, `.gitattributes` is read from the pull request base branch.

### `.github/labels.yml`

You can override label names, thresholds, colors, and optional comments.

Example:

```yaml
XS:
  name: size/XS
  lines: 0
  color: 2FBF6B
S:
  name: size/S
  lines: 10
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

See [`docs/github-app.md`](docs/github-app.md) for where each value comes from.

### 3. Run locally

```bash
go run ./cmd/pr-size
```

### 4. Test

```bash
go test ./...
```

### 5. Build a release binary locally

```bash
mkdir -p dist && go build -o dist/pr-size ./cmd/pr-size
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
