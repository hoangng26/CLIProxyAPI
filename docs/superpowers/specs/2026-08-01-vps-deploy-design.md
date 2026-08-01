# VPS Deploy via Private Docker Registry — Design

**Date:** 2026-08-01  
**Status:** Approved approach A (pending implementation plan)  
**Scope:** GitHub Actions pipeline: build → private registry → SSH deploy to VPS

## Problem

Deploy CLIProxyAPI to a personal VPS using a private Docker registry, automated from GitHub on version tags, without coupling to the existing public Docker Hub release workflow.

## Goals

- On tag `v*`, build `linux/amd64` image and push to private registry
- SSH into VPS and roll the running `docker compose` stack to the new image
- Keep public `docker-image.yml` (Docker Hub multi-arch) unchanged
- Store all credentials in GitHub Secrets (never in repo)

## Non-goals (v1)

- Multi-arch / arm64 private images
- Blue-green or zero-downtime orchestration
- Managing `config.yaml`, OAuth `auths/`, or app secrets via CI
- Migrating SSH password auth to deploy keys (recommended follow-up)
- Changing root `Dockerfile` behavior

## Decisions (locked)

| Decision | Choice |
|----------|--------|
| End-to-end scope | Build + push + deploy VPS |
| Trigger | Tag `v*` (+ optional `workflow_dispatch`) |
| VPS access | SSH password |
| Runtime on VPS | `docker compose` |
| Registry image | `registry.hoangng26.work/cliproxyapi/cli-proxy-api` |
| Architecture | `linux/amd64` only |
| Workflow layout | **Approach A** — separate workflow file |

### Approaches considered

1. **A — Separate workflow (chosen)**  
   New `.github/workflows/deploy-vps.yml`. Isolated secrets, independent of public Hub release.
2. **B — Extend `docker-image.yml`**  
   One file for Hub + private + deploy. Couples private deploy to public multi-arch release.
3. **C — Self-hosted runner on VPS**  
   No inbound SSH. More VPS setup; rejected given password-SSH preference.

## Architecture

```
push tag vX.Y.Z
       │
       ▼
┌──────────────────────────┐
│ job: build-and-push      │  ubuntu-latest
│  - checkout              │
│  - setup buildx          │
│  - login private registry│
│  - build linux/amd64     │
│  - push :vX.Y.Z + :latest│
└────────────┬─────────────┘
             │ needs: success
             ▼
┌──────────────────────────┐
│ job: deploy-vps          │
│  - SSH (password)        │
│  - docker login registry │
│  - cd $VPS_COMPOSE_PATH  │
│  - set CLI_PROXY_IMAGE   │
│  - compose pull && up -d │
│  - verify container up   │
└──────────────────────────┘
```

### Image tags

- `registry.hoangng26.work/cliproxyapi/cli-proxy-api:${GITHUB_REF_NAME}` (e.g. `v6.1.0`)
- `registry.hoangng26.work/cliproxyapi/cli-proxy-api:latest`

### Build metadata

Reuse existing Dockerfile build-args (same as `docker-image.yml`):

- `VERSION` = tag name
- `COMMIT` = short SHA
- `BUILD_DATE` = UTC ISO-8601

Optional: run `.github/scripts/refresh-model-catalogs.sh` before build for parity with public image workflow.

## Files

| Path | Action | Purpose |
|------|--------|---------|
| `.github/workflows/deploy-vps.yml` | **Add** | Build, push, SSH deploy |
| `deploy/docker-compose.vps.yml` | **Add** (sample) | VPS compose template using private registry image env |
| `docs/superpowers/specs/2026-08-01-vps-deploy-design.md` | **Add** | This design |

No edits to root `Dockerfile`, `docker-compose.yml`, or `.github/workflows/docker-image.yml`.

### Sample VPS compose contract

Service name must remain `cli-proxy-api`. Image must come from env (matches existing compose pattern):

```yaml
services:
  cli-proxy-api:
    image: ${CLI_PROXY_IMAGE:-registry.hoangng26.work/cliproxyapi/cli-proxy-api:latest}
    pull_policy: always
    container_name: cli-proxy-api
    ports:
      - "8317:8317"
      # other ports as needed on that VPS
    volumes:
      - ${CLI_PROXY_CONFIG_PATH:-./config.yaml}:/CLIProxyAPI/config.yaml
      - ${CLI_PROXY_AUTH_PATH:-./auths}:/root/.cli-proxy-api
      - ${CLI_PROXY_LOG_PATH:-./logs}:/CLIProxyAPI/logs
      - ${CLI_PROXY_PLUGIN_PATH:-./plugins}:/CLIProxyAPI/plugins
    restart: unless-stopped
```

VPS host owns real `config.yaml`, `auths/`, `logs/`, `plugins/`. CI never uploads those.

## Workflow specification

### Triggers

```yaml
on:
  push:
    tags:
      - 'v*'
  workflow_dispatch:
    inputs:
      tag:
        description: 'Image tag to deploy (default: latest)'
        required: false
        default: 'latest'
```

- Tag push: build that tag, push, deploy that tag.
- Manual dispatch: skip rebuild **or** rebuild+push only if implemented simply as “deploy existing `latest` / given tag”.  
  **v1 decision:** `workflow_dispatch` runs **deploy-only** using input tag (default `latest`), assuming image already in registry. Tag push runs full build+push+deploy. This avoids accidental rebuilds from the UI.

### Job: `build-and-push`

- Runner: `ubuntu-latest`
- Condition: only on `push` tags (`if: github.event_name == 'push'`)
- Steps:
  1. `actions/checkout@v4`
  2. Optional catalog refresh script
  3. `docker/setup-buildx-action@v3`
  4. `docker/login-action@v3` → `registry: ${{ secrets.REGISTRY_HOST }}`, user/password secrets
  5. Generate `VERSION`, `COMMIT`, `BUILD_DATE` env
  6. `docker/build-push-action@v6`  
     - `platforms: linux/amd64`  
     - `push: true`  
     - tags: version + latest  
     - build-args: VERSION, COMMIT, BUILD_DATE

### Job: `deploy-vps`

- Runner: `ubuntu-latest`
- Always `needs: [build-and-push]`
- `build-and-push` uses `if: github.event_name == 'push'` and is marked `if: success() || github.event_name == 'workflow_dispatch'` on deploy via:
  - `if: always() && (github.event_name == 'workflow_dispatch' || needs.build-and-push.result == 'success')`
  - So: tag push waits for successful build; manual dispatch runs deploy-only without requiring build
- SSH via `appleboy/ssh-action` (password auth)
- Scripted remote commands:

```bash
set -euo pipefail
echo "$REGISTRY_PASSWORD" | docker login "$REGISTRY_HOST" -u "$REGISTRY_USERNAME" --password-stdin
cd "$VPS_COMPOSE_PATH"
export CLI_PROXY_IMAGE="${REGISTRY_HOST}/cliproxyapi/cli-proxy-api:${IMAGE_TAG}"
docker compose -f "${VPS_COMPOSE_FILE:-docker-compose.yml}" pull cli-proxy-api
docker compose -f "${VPS_COMPOSE_FILE:-docker-compose.yml}" up -d --remove-orphans
docker compose -f "${VPS_COMPOSE_FILE:-docker-compose.yml}" ps
# Fail if service not running
docker compose -f "${VPS_COMPOSE_FILE:-docker-compose.yml}" ps --status running | grep -q cli-proxy-api
```

- `IMAGE_TAG` = `github.ref_name` on tag push, else `inputs.tag`
- Do not print secrets
- Permissions: `contents: read` only

## GitHub Secrets / Variables

| Name | Type | Example / notes |
|------|------|-----------------|
| `REGISTRY_HOST` | secret or var | `registry.hoangng26.work` |
| `REGISTRY_USERNAME` | secret | CI registry user |
| `REGISTRY_PASSWORD` | secret | **Must be rotated if ever pasted in chat/logs** |
| `VPS_HOST` | secret | IP or hostname |
| `VPS_USER` | secret | SSH user |
| `VPS_PASSWORD` | secret | SSH password |
| `VPS_PORT` | var (optional) | default `22` |
| `VPS_COMPOSE_PATH` | secret or var | Absolute path on VPS to compose directory |
| `VPS_COMPOSE_FILE` | var (optional) | default `docker-compose.yml` |

### Operator setup (one-time on VPS)

1. Install Docker Engine + Compose plugin
2. Place compose file (from sample) under `VPS_COMPOSE_PATH`
3. Place `config.yaml`, `auths/`, volume dirs beside compose
4. Ensure VPS can reach `registry.hoangng26.work` (DNS/TLS)
5. Open required host ports (at least `8317` if public API)
6. Configure GitHub repo secrets listed above
7. **Rotate registry password** if it was shared outside secrets storage

## Security

- Never commit registry or SSH passwords
- Chat-shared credentials are considered compromised until rotated
- Workflow must not `echo` secret values
- Prefer follow-up: SSH deploy key + disable password auth
- Registry user should be push/pull-scoped CI account, not personal admin if possible
- Deploy does not overwrite host config/auth volumes

## Error handling & success criteria

| Stage | Success |
|-------|---------|
| Build/push | Action green; image tags visible on registry |
| SSH | `appleboy/ssh-action` exit 0 |
| Compose | `pull` + `up -d` exit 0 |
| Runtime | `cli-proxy-api` listed as running |
| Optional health | Out of scope v1 unless a stable local health URL is confirmed |

On deploy failure: job fails red. Compose recreate behavior leaves prior container until new one starts; a failed pull leaves previous image running.

## Testing plan (implementation)

1. Dry-run workflow YAML validation (`actionlint` if available)
2. Manual `workflow_dispatch` deploy of known-good `latest` after first successful tag build
3. Push a test tag on a branch/fork only if secrets available — otherwise document manual verification steps for the operator

## Implementation outline (for writing-plans)

1. Add `deploy/docker-compose.vps.yml` sample
2. Add `.github/workflows/deploy-vps.yml`
3. Document required secrets in a short comment header in the workflow (no real secrets)
4. Self-check: no credentials in tree; tag trigger only `v*`; public workflows untouched

## Open follow-ups (not blocking v1)

- Switch SSH to key-based auth
- Optional HTTP health check after deploy
- Multi-arch private manifests if arm64 VPS appears later
