# EveryoneAgent

A private, self-hosted chatbot for a small household. The initial foundation uses a single Go application container with no public-facing port. Data, account and chat features are not implemented yet.

## Run locally

Requirements: Docker Desktop with Compose.

Compose pulls the published GHCR image by default. To run the latest published image:

```powershell
docker compose up -d
```

To pin a specific commit-count release, set `APP_VERSION` before starting Compose; for example, `$env:APP_VERSION = "3"`. The GitHub Actions workflow tests the code and publishes `ghcr.io/everyonesoftware/agent:<commit-count>` on pushes to `main` and version tags. The numeric count is also embedded in the app's `/version` endpoint and OCI version label. It additionally publishes `latest`, branch/tag, and short-SHA tags.

Open http://127.0.0.1:8080. The container is published only on the host loopback interface. To reach it from other devices, configure Tailscale Serve on the host to proxy to `http://127.0.0.1:8080`; do not change the Compose binding to `0.0.0.0` or forward the port from your router.

Stop with `docker compose down`.

## Current status

This is only the bootable application foundation: Go HTTP server, health endpoint, simple placeholder page, and a minimal non-root runtime image. It does not yet provide authentication, chat, database persistence, encryption, OpenAI access, or web search. Those are the next features; do not use this placeholder with sensitive information.
