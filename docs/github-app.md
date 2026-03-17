# GitHub App setup

This page documents the full manual setup flow for creating the GitHub App at:

`https://github.com/settings/apps/new`

The goal is to make the app-creation process explicit instead of assuming GitHub defaults.

## Before you start

You need to decide two things first:

1. the final public deployment URL for `pr-size-labeler`
2. a random secret you will use both in GitHub and in the app runtime as `WEBHOOK_SECRET`

For the default Hugging Face flow, pick the Space name first. That Space name determines the public URL you put into the GitHub App webhook field.

Example target:

- Space repo: `kvokka/pr-size-labeler`
- public URL: `https://kvokka-pr-size-labeler.hf.space/`

For example:

- local/self-hosted: your reverse-proxied webhook endpoint
- Hugging Face public Space: the public Space URL
- Hugging Face private setup: your private/proxied endpoint, depending on how you deploy it

## Manual creation at `github.com/settings/apps/new`

Open `https://github.com/settings/apps/new` and fill in the fields like this.

Important: the checked-in `app.yml` file is only a reference for the required permissions, file paths, and event names. GitHub does not automatically apply or sync that file when you use the manual App creation page, so you must still set and update those values manually in the GitHub UI.

### Basic identification

#### GitHub App name

Use any unique app name you want. A good default is `pr-size-labeler` or `your-org-pr-size-labeler`.

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

Set this to the public HTTPS URL where `pr-size-labeler` receives GitHub webhooks.

For the default public deployment of this repository, use:

```text
https://kvokka-pr-size-labeler.hf.space/
```

Examples:

```text
https://your-domain.example/
https://your-space.hf.space/
```

#### Webhook secret

Generate a strong random string yourself and save it. GitHub does not generate this for you.

You must reuse the exact same value as the runtime `WEBHOOK_SECRET`.

## Permissions

Set only the permissions the current implementation needs. These must be selected manually in the GitHub UI.

### Repository permissions

- Pull requests: **Read and write**
- Metadata: **Read-only**
- Single file: **Read-only**

### Single-file paths

Allow exactly these repository files:

- `.gitattributes`
- `.github/labels.yml`

No other repository permissions are required for the current app behavior.

### Organization permissions

None.

### Account permissions

None.

## Events

- `Pull request`
- `Installation`
- `Installation repositories`

That covers:

- normal PR labeling for opened, reopened, synchronize, and edited pull request events
- install-time proactive relabeling when `.github/labels.yml` enables backfill
- merged-config proactive relabeling when a merged default-branch PR changes `.github/labels.yml`

Do not subscribe to `meta`. This app does not use it.

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

Practical note: the normal way to store this is to copy the full `.pem` file contents and paste them into your GitHub Actions secret. For example, `cat your-app.private-key.pem | pbcopy` on macOS is fine. The app accepts the original multi-line PEM, and it also tolerates a flattened version if a platform collapses the line breaks during secret transport.

### 3. Install the app

Install the GitHub App on the repositories where you want PR size labels to be managed.

Make sure the target repositories are the ones receiving pull request events.

Each target repository also needs:

- an optional `.github/labels.yml` on its default branch if you want overrides or proactive backfill

If `.github/labels.yml` is absent, normal pull request labeling still works with the built-in defaults.

## What you set now vs what you get later

### You set during App creation

- `Webhook URL`
- `Webhook secret`
- repository permissions
- required subscribed events `Pull request`, `Installation`, and `Installation repositories`

### GitHub gives you after creation

- `APP_ID`
- `PRIVATE_KEY` after you generate and download it
- the install flow for choosing repositories

## Required runtime values

These are the values the `pr-size-labeler` app itself needs.

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

### `LOG_PRIVATE_DETAILS`

Optional. Default:

```text
false
```

When left at the default, the app keeps logs redacted for public deployments by omitting request IP/header details, installation IDs, and detailed startup/startup-recovery diagnostics. Set it to `true` only when you explicitly want those extra details in logs, such as on a private self-hosted deployment.

## Where to set the runtime values

- Local development: shell environment or your preferred `.env` loader
- Default Hugging Face deployment for this repo: GitHub repository secrets `APP_ID`, `PRIVATE_KEY`, `WEBHOOK_SECRET`, plus optional repository variables like `HUGGINGFACE_SPACE` and `GITHUB_API_BASE_URL`; the deploy workflow passes those through the updated `kvokka/huggingface` action into the target Hugging Face Space automatically
- Manual Hugging Face deployment outside the default workflow: set the same values directly in the target Hugging Face Space secrets and variables
- Self-hosted deployment: standard process manager, container secret store, or platform secret manager

## Optional manifest file

This repo includes `app.yml` as a transparent reference for the required event, permission, and single-file path set, but GitHub App settings are not automatically synced from that file. If you change `app.yml`, your existing GitHub App will not update by itself.

## Notes

- The app expects signed GitHub webhook deliveries via `X-Hub-Signature-256`.
- `.github/labels.yml` is read from the repository default branch.
- `.gitattributes` is read from the pull request base branch.
- Proactive relabeling is controlled only by `.github/labels.yml`, not runtime env vars.
- If the webhook URL changes later, update it in the GitHub App settings page.
