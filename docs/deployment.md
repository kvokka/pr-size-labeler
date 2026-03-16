# Deployment

The default deployment path for this repository is a public Hugging Face Space deployed by GitHub Actions.

## Bootstrap order

The first-time setup order matters.

1. Choose the Hugging Face Space name you will deploy to.
2. Use that Space name to determine the public webhook URL.
3. Create the GitHub App with that webhook URL and the required permissions.
4. Subscribe to the required `Pull request` event.
5. After GitHub creates the App, collect `APP_ID` and `PRIVATE_KEY`, and keep the webhook secret you chose.
6. Add the required GitHub Actions secrets and variables.
7. Run the deploy workflow.
8. Install the GitHub App on the repositories you want it to label.

For the exact GitHub App form fields to fill in, see [`docs/github-app.md`](docs/github-app.md).

## Default: public Hugging Face Space

This repo includes `.github/workflows/deploy-huggingface.yml`. On pushes to `main`, it:

1. builds a Linux `pr-size-labeler` binary
2. prepares a Docker-based Space bundle in the runner temp directory
3. syncs the Space bundle and runtime secrets/variables to Hugging Face with `kvokka/huggingface`

### GitHub-side deployment settings

Set these in the GitHub repository.

#### Required GitHub repository secrets

- `HF_TOKEN`
- `APP_ID`
- `PRIVATE_KEY`
- `WEBHOOK_SECRET`

#### Optional GitHub repository variables

- `HUGGINGFACE_SPACE`
- `GITHUB_API_BASE_URL`
- `LOG_PRIVATE_DETAILS`
- `CONNECT_OPEN_PRS_BACKFILL_ENABLED`
- `CONNECT_OPEN_PRS_BACKFILL_LOOKBACK`

#### Optional GitHub repository secrets for startup recovery behavior

No extra credentials are required for startup recovery beyond the existing GitHub App secrets. If you enable startup failed-delivery recovery, configure it with these environment variables in the runtime:

- `STARTUP_FAILED_DELIVERY_RECOVERY_ENABLED=true`
- `STARTUP_FAILED_DELIVERY_RECOVERY_LOOKBACK=2h`

The app uses its existing GitHub App credentials to list recent GitHub App webhook deliveries on startup and redeliver only failed deliveries within the configured lookback period.

#### Optional GitHub repository variables for connect-time open PR backfill

No extra credentials are required for connect-time backfill beyond the existing GitHub App secrets. If you enable backfill, configure it with these runtime environment variables:

- `CONNECT_OPEN_PRS_BACKFILL_ENABLED=true`
- `CONNECT_OPEN_PRS_BACKFILL_LOOKBACK=1y`

When enabled, the app uses the installation webhook events to scan newly connected repositories for open pull requests created inside the configured lookback window and applies the normal size-label flow immediately.

No extra repository, organization, or account permissions are required for this backfill behavior beyond the normal permissions already documented for the app.

For this repository's default GitHub Actions → Hugging Face deployment, the workflow currently hard-forces:

- `CONNECT_OPEN_PRS_BACKFILL_ENABLED=true`
- `CONNECT_OPEN_PRS_BACKFILL_LOOKBACK=1y`

So the repository variables above document the runtime knobs, but this repo's default workflow does not pass them through as overrides.

#### `HUGGINGFACE_SPACE`

Choose this first.

- for this repository's default deployment target: `kvokka/pr-size-labeler`
- for a fork: your own Space, for example `yourname/pr-size-labeler`

The public webhook URL used during GitHub App creation comes from this choice. For a public Space, that will be the public Space URL for the chosen Space name.

Example:

`https://kvokka-pr-size-labeler.hf.space/`

#### `HF_TOKEN`

Create it here:

`https://huggingface.co/settings/tokens`

Use a token with write access to the target Space repository. If you use a fine-grained token, it needs write access to the Space repo you are deploying to.

For this repository, the workflow default target is:

`kvokka/pr-size-labeler`

For a fork, set `HUGGINGFACE_SPACE` to your own `owner/space-name` value.

#### `APP_ID`, `PRIVATE_KEY`, `WEBHOOK_SECRET`

These are GitHub repository secrets because the workflow passes them directly to `kvokka/huggingface` via the action's built-in `space_secrets` support.

Get them like this:

1. Open `https://github.com/settings/apps/new` and create the GitHub App for this project.
2. In the app form, enable webhooks and set a webhook secret. Save that exact value as the GitHub repository secret `WEBHOOK_SECRET`. You can use `openssl rand -hex 32` for quick secure secret generation.
3. After the app is created, open its settings page and copy the numeric App ID. Save it as the GitHub repository secret `APP_ID`.
4. On the same settings page, generate a private key and download the `.pem` file. Copy the full file contents into the GitHub repository secret `PRIVATE_KEY`.

Practical note: copying the GitHub-generated key file with a command like `cat my-key.private-key.pem | pbcopy` and pasting it into the GitHub Actions secret is a valid way to store it. The app accepts the normal multi-line PEM form, and it also tolerates platforms that flatten that PEM into a single line during secret transport.

In short:

- `APP_ID`: copied from the GitHub App settings page after creation
- `PRIVATE_KEY`: full contents of the downloaded GitHub App private key `.pem`
- `WEBHOOK_SECRET`: the secret string you entered in the GitHub App webhook configuration

Add all three in GitHub at:

`Settings -> Secrets and variables -> Actions`

At GitHub App creation time, you must also set the App permissions and subscribed event manually in the GitHub UI. Those values are documented in [`docs/github-app.md`](docs/github-app.md).

#### `GITHUB_API_BASE_URL`

Optional repository variable. Leave it unset for GitHub.com and the workflow will default it to:

`https://api.github.com/`

### Hugging Face-side runtime settings

The workflow configures these on the target Hugging Face Space automatically through the action itself:

- Space secret `APP_ID`: from the GitHub App settings page
- Space secret `PRIVATE_KEY`: full PEM contents downloaded from the GitHub App settings page
- Space secret `WEBHOOK_SECRET`: the same secret configured in the GitHub App webhook settings
- Space variable `LISTEN_ADDR`: forced to `:7860` for Hugging Face Spaces
- Space variable `GITHUB_API_BASE_URL`: taken from the GitHub repo variable if set, otherwise defaults to `https://api.github.com/`
- Space variable `LOG_PRIVATE_DETAILS`: optional; leave unset or `false` for public deployments unless you explicitly want private diagnostics in logs
- Space variable `STARTUP_FAILED_DELIVERY_RECOVERY_ENABLED`: forced to `true` in this repo's default deployment so startup recovery is active on Hugging Face without extra manual setup
- Space variable `CONNECT_OPEN_PRS_BACKFILL_ENABLED`: forced to `true` in this repo's default deployment so newly connected repositories get proactive open-PR labeling
- Space variable `CONNECT_OPEN_PRS_BACKFILL_LOOKBACK`: forced to `1y` in this repo's default deployment

That means this repo's default deployment does not require the separate custom Python setup that older versions needed, and it also does not require you to click into Hugging Face Space settings manually as long as the GitHub repository secrets and variables are configured correctly.

### How it is built

The workflow builds `./cmd/pr-size-labeler` as a Linux binary, writes the deploy bundle into the GitHub runner temp directory, adds the Docker Space files from `deploy/huggingface-space.template`, and then lets `kvokka/huggingface` upload both the bundle and the configured `space_secrets` / `space_variables`.

The temp-directory approach is important here because `kvokka/huggingface` performs its own checkout/clean step; generating the bundle inside the tracked repo path can be wiped before `source_dir` is uploaded.

## Private Hugging Face setup

If you want private execution logs instead of a public Space, keep the same build flow but deploy the Space as private.

### Private-only Space

```yaml
- uses: kvokka/huggingface@v0
  with:
    hf_token: ${{ secrets.HF_TOKEN }}
    huggingface_repo: ${{ vars.HUGGINGFACE_SPACE || 'kvokka/pr-size-labeler' }}
    repo_type: space
    space_sdk: docker
    private: true
    source_dir: ${{ env.HF_SOURCE_DIR }}
```

That keeps the Space itself private, so Hugging Face execution logs stay visible only to the Space owner or members with access.

### Private Space with a public proxy

If you want private execution logs but still need a public endpoint, use the `kvokka/huggingface` proxy pattern:

```yaml
- uses: kvokka/huggingface@v0
  with:
    hf_token: ${{ secrets.HF_TOKEN }}
    huggingface_repo: ${{ vars.HUGGINGFACE_SPACE || 'kvokka/pr-size-labeler' }}
    repo_type: space
    space_sdk: docker
    private: true
    source_dir: ${{ env.HF_SOURCE_DIR }}
    create_proxy: true
    proxy_hf_token: ${{ secrets.PROXY_HF_TOKEN }}
    proxy_allow_origins: "https://your-domain.example"
```

Recommended private-mode rules:

- use a dedicated `PROXY_HF_TOKEN` when proxying
- keep `APP_ID`, `PRIVATE_KEY`, and `WEBHOOK_SECRET` in Space secrets, never public variables
- keep `LOG_PRIVATE_DETAILS=false` unless the deployment is private and you explicitly want request/header/startup diagnostics in logs
- restrict `proxy_allow_origins` if the endpoint is only meant for one site or service
- if you use a fork, do not leave `HUGGINGFACE_SPACE` at the default `kvokka/pr-size-labeler`; point it to your own Space instead

## Reverse proxy

If you run `pr-size-labeler` outside Hugging Face, putting it behind a reverse proxy is still the simplest production setup. If you enable `LOG_PRIVATE_DETAILS=true`, the app will also log source information from forwarded headers and remote address, which can be useful for debugging a public proxy.

## Generic self-hosting

`pr-size-labeler` is still a stateless Go HTTP service, so any platform that can run the binary and expose HTTPS can host it.

Example build:

```bash
mkdir -p dist && go build -o dist/pr-size-labeler ./cmd/pr-size-labeler
```

For where to obtain the GitHub App values, see `docs/github-app.md`.
