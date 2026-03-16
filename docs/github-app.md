# GitHub App setup

This page documents the full manual setup flow for creating the GitHub App at:

`https://github.com/settings/apps/new`

The goal is to make the app-creation process explicit instead of assuming GitHub defaults.

## Before you start

You need two things first:

1. a public HTTPS webhook URL for your `pr-size` deployment
2. a random secret you will use both in GitHub and in the app runtime as `WEBHOOK_SECRET`

For example:

- local/self-hosted: your reverse-proxied webhook endpoint
- Hugging Face public Space: the public Space URL
- Hugging Face private setup: your private/proxied endpoint, depending on how you deploy it

## Manual creation at `github.com/settings/apps/new`

Open `https://github.com/settings/apps/new` and fill in the fields like this.

### Basic identification

#### GitHub App name

Use any unique app name you want. A good default is `pr-size` or `your-org-pr-size`.

#### Homepage URL

Use the repository URL for this project, or your own fork URL if you run a fork.

Recommended value:

```text
https://github.com/<owner>/<repo>
```

#### Description

Describe it as a pull request size labeling app.

Example:

```text
Transparent OSS GitHub App that applies pull request size labels.
```

## Identifying and authorizing users

This project does not need a web login flow.

### Callback URL

Leave it empty.

#### Request user authorization (OAuth) during installation

Leave this disabled.

#### Expire user authorization tokens

Leave this disabled.

## Post installation

### Setup URL

Leave it empty.

#### Redirect on update

Leave it disabled.

## Webhook

### Active

Enable webhooks.

#### Webhook URL

Set this to the public HTTPS URL where `pr-size` receives GitHub webhooks.

Examples:

```text
https://your-domain.example/
https://your-space.hf.space/
```

#### Webhook secret

Generate a strong random string yourself and save it. GitHub does not generate this for you.

You must reuse the exact same value as the runtime `WEBHOOK_SECRET`.

## Permissions

Set only the permissions the current implementation needs.

### Repository permissions

- Pull requests: **Read and write**
- Contents: **Read-only**
- Metadata: **Read-only**

No other repository permissions are required for the current app behavior.

### Organization permissions

None.

### Account permissions

None.

## Events

Subscribe to:

- `Pull request`

That covers the app logic for opened, reopened, synchronize, and edited pull request events.

## Installation scope

### Where can this GitHub App be installed?

Choose based on your use case:

- **Any account** if you want the app installable on multiple user/org accounts
- **Only on this account** if this is a private/internal app for one owner

For a reusable OSS deployment, **Any account** is the practical default.

## After clicking Create GitHub App

Once the app exists, GitHub gives you the rest of the values you need.

### 1. Copy the App ID

GitHub shows the numeric App ID on the app settings page.

This becomes:

- `APP_ID`

### 2. Generate and download the private key

In the app settings page, generate a private key and download the `.pem` file.

The full file contents become:

- `PRIVATE_KEY`

### 3. Install the app

Install the GitHub App on the repositories where you want PR size labels to be managed.

Make sure the target repositories are the ones receiving pull request events.

## Required runtime values

These are the values the `pr-size` app itself needs.

### `APP_ID`

Comes from the GitHub App settings page after creation.

### `PRIVATE_KEY`

Comes from the downloaded `.pem` private key generated in the GitHub App settings page.

### `WEBHOOK_SECRET`

Comes from the secret you manually entered in the GitHub App webhook section.

### `LISTEN_ADDR`

This comes from your runtime platform, not GitHub.

- local development: `:8080`
- Hugging Face Spaces: `:7860`

### `GITHUB_API_BASE_URL`

Use:

```text
https://api.github.com/
```

Change it only if you run against GitHub Enterprise Server.

## Where to set the runtime values

- Local development: shell environment or your preferred `.env` loader
- Hugging Face public Space: Space secrets for `APP_ID`, `PRIVATE_KEY`, `WEBHOOK_SECRET`
- Hugging Face private Space: the same secrets, but on the private Space
- Self-hosted deployment: standard process manager, container secret store, or platform secret manager

## Optional manifest file

This repo includes `app.yml` as a transparent reference for the required event and permission set, but GitHub App settings are not automatically synced from that file.

## Notes

- The app expects signed GitHub webhook deliveries via `X-Hub-Signature-256`.
- Repository config is read from the pull request base branch.
- If the webhook URL changes later, update it in the GitHub App settings page.
