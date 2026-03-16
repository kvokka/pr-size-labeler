---
title: pr-size
emoji: 📏
colorFrom: blue
colorTo: green
sdk: docker
pinned: false
---

## pr-size

This Space runs the public deployment of `pr-size`.

The application binary is built from the GitHub repository and synced here by GitHub Actions.

Required Space secrets:

- `APP_ID`
- `PRIVATE_KEY`
- `WEBHOOK_SECRET`

Optional Space variables:

- `LISTEN_ADDR` (defaults to `:7860` in the container)
- `GITHUB_API_BASE_URL` (only needed for GitHub Enterprise)

See the GitHub repository README and deployment docs for the full setup flow.
