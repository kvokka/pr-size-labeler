# Deployment

The default deployment path for this repository is a public Hugging Face Space deployed by GitHub Actions.

## Default: public Hugging Face Space

This repo includes `.github/workflows/deploy-huggingface.yml`. On pushes to `main`, it:

1. builds a Linux `pr-size` binary
2. prepares a Docker-based Hugging Face Space bundle
3. syncs that bundle to a public Hugging Face Space with `kvokka/huggingface`

### GitHub-side deployment settings

Set these in the GitHub repository:

- secret `HF_TOKEN`: a Hugging Face token with permission to update the target Space
- variable `HUGGINGFACE_SPACE`: the target Space repo in `owner/space-name` form

### Hugging Face-side runtime settings

Set these in the target Hugging Face Space:

- Space secret `APP_ID`: from the GitHub App settings page
- Space secret `PRIVATE_KEY`: full PEM contents downloaded from the GitHub App settings page
- Space secret `WEBHOOK_SECRET`: the same secret configured in the GitHub App webhook settings
- optional Space variable `LISTEN_ADDR`: `:7860` is correct for Hugging Face Spaces and is already the image default
- optional Space variable `GITHUB_API_BASE_URL`: only needed for GitHub Enterprise Server

### How it is built

The workflow builds `./cmd/pr-size` as a Linux binary, copies it into `deploy/huggingface-space`, adds the Docker Space files from `deploy/huggingface-space.template`, and syncs that folder to Hugging Face.

## Private Hugging Face setup

If you want private execution logs instead of a public Space, keep the same build flow but deploy the Space as private.

### Private-only Space

```yaml
- uses: kvokka/huggingface@v0
  with:
    hf_token: ${{ secrets.HF_TOKEN }}
    huggingface_repo: ${{ vars.HUGGINGFACE_SPACE }}
    repo_type: space
    private: true
    source_dir: deploy/huggingface-space
```

That keeps the Space itself private, so Hugging Face execution logs stay visible only to the Space owner or members with access.

### Private Space with a public proxy

If you want private execution logs but still need a public endpoint, use the `kvokka/huggingface` proxy pattern:

```yaml
- uses: kvokka/huggingface@v0
  with:
    hf_token: ${{ secrets.HF_TOKEN }}
    huggingface_repo: ${{ vars.HUGGINGFACE_SPACE }}
    repo_type: space
    private: true
    source_dir: deploy/huggingface-space
    create_proxy: true
    proxy_hf_token: ${{ secrets.PROXY_HF_TOKEN }}
    proxy_allow_origins: "https://your-domain.example"
```

Recommended private-mode rules:

- use a dedicated `PROXY_HF_TOKEN` when proxying
- keep `APP_ID`, `PRIVATE_KEY`, and `WEBHOOK_SECRET` in Space secrets, never public variables
- restrict `proxy_allow_origins` if the endpoint is only meant for one site or service

## Reverse proxy

If you run `pr-size` outside Hugging Face, putting it behind a reverse proxy is still the simplest production setup. The app logs source information from forwarded headers and remote address, which is useful when that proxy is public.

## Generic self-hosting

`pr-size` is still a stateless Go HTTP service, so any platform that can run the binary and expose HTTPS can host it.

Example build:

```bash
mkdir -p dist && go build -o dist/pr-size ./cmd/pr-size
```

For where to obtain the GitHub App values, see `docs/github-app.md`.
