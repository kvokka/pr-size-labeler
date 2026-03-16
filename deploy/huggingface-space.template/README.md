---
title: pr-size-labeler
emoji: 📏
colorFrom: blue
colorTo: green
sdk: docker
pinned: false
---

## pr-size-labeler

This Space runs the public deployment of `pr-size-labeler`.

The application binary is built from the GitHub repository and synced here by GitHub Actions.

The workflow also syncs the runtime settings from GitHub Actions into Hugging Face Space secrets and variables through the `kvokka/huggingface` action itself.

Required Space secrets:

- `APP_ID`
- `PRIVATE_KEY`
- `WEBHOOK_SECRET`

Optional Space variables:

- `LISTEN_ADDR` (set to `:7860` by the workflow)
- `GITHUB_API_BASE_URL` (set by the workflow, defaults to GitHub.com)
- `LOG_PRIVATE_DETAILS` (optional, defaults to `false`; enable only if you want private diagnostics in logs)

See the GitHub repository README and deployment docs for the full setup flow.
