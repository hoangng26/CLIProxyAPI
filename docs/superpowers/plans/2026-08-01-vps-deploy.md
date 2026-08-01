# VPS Private Registry Deploy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a GitHub Actions workflow that builds an amd64 image, pushes it to `registry.hoangng26.work/cliproxyapi/cli-proxy-api`, and deploys it to a VPS over SSH with `docker compose`.

**Architecture:** Separate workflow (Approach A) from public Docker Hub release. Tag `v*` runs build+push+deploy. Manual `workflow_dispatch` runs deploy-only for an existing tag. VPS keeps host-owned `config.yaml` / `auths` volumes; CI only updates the image via `CLI_PROXY_IMAGE`.

**Tech Stack:** GitHub Actions, Docker Buildx, private OCI registry, `appleboy/ssh-action`, Docker Compose v2.

**Spec:** `docs/superpowers/specs/2026-08-01-vps-deploy-design.md`

## Global Constraints

- Never commit registry/SSH passwords or any real credentials
- Do not modify `.github/workflows/docker-image.yml`, root `Dockerfile`, or root `docker-compose.yml`
- Image path fixed: `registry.hoangng26.work/cliproxyapi/cli-proxy-api`
- Platform: `linux/amd64` only
- Service name on VPS must remain `cli-proxy-api`
- Workflow permissions: `contents: read` only
- Password shared in chat is compromised — document rotate-before-use; do not embed it
- Follow existing workflow action versions where possible (`actions/checkout@v4`, `docker/setup-buildx-action@v3`, `docker/login-action@v3`, `docker/build-push-action@v6`)
- `docs/*` is gitignored — force-add any files under `docs/superpowers/`

---

## File map

| Path | Responsibility |
|------|----------------|
| `deploy/docker-compose.vps.yml` | Sample compose for VPS operators; private registry default image |
| `.github/workflows/deploy-vps.yml` | Tag build/push + SSH deploy; manual deploy-only |

---

### Task 1: VPS compose sample

**Files:**
- Create: `deploy/docker-compose.vps.yml`

**Interfaces:**
- Consumes: none
- Produces: compose service `cli-proxy-api` reading `CLI_PROXY_IMAGE` (default private registry `:latest`)

- [ ] **Step 1: Create deploy directory and compose sample**

Create `deploy/docker-compose.vps.yml` with exactly:

```yaml
# Sample compose for VPS deploy via private registry.
# Copy to the VPS compose directory (see VPS_COMPOSE_PATH secret).
# Host owns config.yaml, auths/, logs/, plugins/ — CI only changes CLI_PROXY_IMAGE.
services:
  cli-proxy-api:
    image: ${CLI_PROXY_IMAGE:-registry.hoangng26.work/cliproxyapi/cli-proxy-api:latest}
    pull_policy: always
    container_name: cli-proxy-api
    environment:
      DEPLOY: ${DEPLOY:-}
    ports:
      - "8317:8317"
      - "8085:8085"
      - "1455:1455"
      - "54545:54545"
      - "51121:51121"
      - "11451:11451"
    volumes:
      - ${CLI_PROXY_CONFIG_PATH:-./config.yaml}:/CLIProxyAPI/config.yaml
      - ${CLI_PROXY_AUTH_PATH:-./auths}:/root/.cli-proxy-api
      - ${CLI_PROXY_LOG_PATH:-./logs}:/CLIProxyAPI/logs
      - ${CLI_PROXY_PLUGIN_PATH:-./plugins}:/CLIProxyAPI/plugins
    restart: unless-stopped
```

- [ ] **Step 2: Validate YAML structure**

Run:

```bash
python3 -c "import yaml,sys; yaml.safe_load(open('deploy/docker-compose.vps.yml')); print('ok')"
```

Expected: `ok`

If PyYAML missing:

```bash
grep -E '^(services:|  cli-proxy-api:|    image:)' deploy/docker-compose.vps.yml
```

Expected lines include `services:`, `cli-proxy-api:`, and `image: ${CLI_PROXY_IMAGE:-registry.hoangng26.work/cliproxyapi/cli-proxy-api:latest}`

- [ ] **Step 3: Commit**

```bash
git add deploy/docker-compose.vps.yml
git commit -m "$(cat <<'EOF'
chore(deploy): add VPS private-registry compose sample

EOF
)"
```

---

### Task 2: GitHub Actions deploy workflow

**Files:**
- Create: `.github/workflows/deploy-vps.yml`

**Interfaces:**
- Consumes: `deploy/docker-compose.vps.yml` contract (service name `cli-proxy-api`, env `CLI_PROXY_IMAGE`)
- Produces: workflow jobs `build-and-push`, `deploy-vps`
- Secrets required (operator-configured, not in repo):
  - `REGISTRY_HOST`, `REGISTRY_USERNAME`, `REGISTRY_PASSWORD`
  - `VPS_HOST`, `VPS_USER`, `VPS_PASSWORD`
  - `VPS_COMPOSE_PATH`
- Optional secrets/vars: `VPS_PORT` (default 22), `VPS_COMPOSE_FILE` (default `docker-compose.yml`)

- [ ] **Step 1: Create workflow file**

Create `.github/workflows/deploy-vps.yml` with exactly:

```yaml
# Deploy CLIProxyAPI to VPS via private Docker registry.
#
# Required GitHub Secrets:
#   REGISTRY_HOST, REGISTRY_USERNAME, REGISTRY_PASSWORD
#   VPS_HOST, VPS_USER, VPS_PASSWORD, VPS_COMPOSE_PATH
# Optional:
#   VPS_PORT (default 22), VPS_COMPOSE_FILE (default docker-compose.yml)
#
# Tag push v*: build linux/amd64 → push registry → SSH compose pull/up
# workflow_dispatch: deploy-only existing image tag (default latest)
#
# Security: never commit real credentials. Rotate any password shared outside Secrets.

name: deploy-vps

on:
  push:
    tags:
      - 'v*'
  workflow_dispatch:
    inputs:
      tag:
        description: 'Image tag to deploy (deploy-only; image must already exist in registry)'
        required: false
        default: 'latest'

permissions:
  contents: read

env:
  IMAGE_NAME: cliproxyapi/cli-proxy-api

jobs:
  build-and-push:
    if: github.event_name == 'push'
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Refresh models catalog
        run: bash .github/scripts/refresh-model-catalogs.sh

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Login to private registry
        uses: docker/login-action@v3
        with:
          registry: ${{ secrets.REGISTRY_HOST }}
          username: ${{ secrets.REGISTRY_USERNAME }}
          password: ${{ secrets.REGISTRY_PASSWORD }}

      - name: Generate Build Metadata
        run: |
          echo "VERSION=${GITHUB_REF_NAME}" >> "$GITHUB_ENV"
          echo "COMMIT=$(git rev-parse --short HEAD)" >> "$GITHUB_ENV"
          echo "BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$GITHUB_ENV"
          echo "REGISTRY_IMAGE=${{ secrets.REGISTRY_HOST }}/${{ env.IMAGE_NAME }}" >> "$GITHUB_ENV"

      - name: Build and push (amd64)
        uses: docker/build-push-action@v6
        with:
          context: .
          platforms: linux/amd64
          push: true
          build-args: |
            VERSION=${{ env.VERSION }}
            COMMIT=${{ env.COMMIT }}
            BUILD_DATE=${{ env.BUILD_DATE }}
          tags: |
            ${{ env.REGISTRY_IMAGE }}:${{ env.VERSION }}
            ${{ env.REGISTRY_IMAGE }}:latest

  deploy-vps:
    runs-on: ubuntu-latest
    needs: [build-and-push]
    if: always() && (github.event_name == 'workflow_dispatch' || needs.build-and-push.result == 'success')
    steps:
      - name: Resolve image tag
        id: meta
        run: |
          if [ "${{ github.event_name }}" = "workflow_dispatch" ]; then
            echo "image_tag=${{ inputs.tag }}" >> "$GITHUB_OUTPUT"
          else
            echo "image_tag=${GITHUB_REF_NAME}" >> "$GITHUB_OUTPUT"
          fi

      - name: Deploy over SSH
        uses: appleboy/ssh-action@v1.2.0
        env:
          REGISTRY_HOST: ${{ secrets.REGISTRY_HOST }}
          REGISTRY_USERNAME: ${{ secrets.REGISTRY_USERNAME }}
          REGISTRY_PASSWORD: ${{ secrets.REGISTRY_PASSWORD }}
          VPS_COMPOSE_PATH: ${{ secrets.VPS_COMPOSE_PATH }}
          VPS_COMPOSE_FILE: ${{ secrets.VPS_COMPOSE_FILE }}
          IMAGE_NAME: ${{ env.IMAGE_NAME }}
          IMAGE_TAG: ${{ steps.meta.outputs.image_tag }}
        with:
          host: ${{ secrets.VPS_HOST }}
          username: ${{ secrets.VPS_USER }}
          password: ${{ secrets.VPS_PASSWORD }}
          port: ${{ secrets.VPS_PORT || 22 }}
          envs: REGISTRY_HOST,REGISTRY_USERNAME,REGISTRY_PASSWORD,VPS_COMPOSE_PATH,VPS_COMPOSE_FILE,IMAGE_NAME,IMAGE_TAG
          script_stop: true
          script: |
            set -euo pipefail
            COMPOSE_FILE="${VPS_COMPOSE_FILE:-docker-compose.yml}"
            echo "${REGISTRY_PASSWORD}" | docker login "${REGISTRY_HOST}" -u "${REGISTRY_USERNAME}" --password-stdin
            cd "${VPS_COMPOSE_PATH}"
            export CLI_PROXY_IMAGE="${REGISTRY_HOST}/${IMAGE_NAME}:${IMAGE_TAG}"
            echo "Deploying ${CLI_PROXY_IMAGE}"
            docker compose -f "${COMPOSE_FILE}" pull cli-proxy-api
            docker compose -f "${COMPOSE_FILE}" up -d --remove-orphans
            docker compose -f "${COMPOSE_FILE}" ps
            docker compose -f "${COMPOSE_FILE}" ps --status running | grep -q cli-proxy-api
```

- [ ] **Step 2: Static checks — no secrets in tree**

Run:

```bash
# Must find ZERO matches for the chat-shared password or obvious secret material
! grep -RInE 'Gi@ng07082002|REGISTRY_PASSWORD:\s*['\''\"][^$]' deploy .github/workflows/deploy-vps.yml
# Workflow must reference secrets, not literals for host/user
grep -n 'secrets.REGISTRY_HOST\|secrets.VPS_HOST\|secrets.VPS_PASSWORD' .github/workflows/deploy-vps.yml
# Public docker workflow untouched
git diff --name-only HEAD -- .github/workflows/docker-image.yml Dockerfile docker-compose.yml
```

Expected:
- first command exits 0 (no matches)
- second shows secret references
- third prints nothing (no local diff on those files vs HEAD for this work — or only if previously dirty unrelated)

- [ ] **Step 3: YAML parse check**

Run:

```bash
python3 - <<'PY'
import sys
try:
    import yaml
except ImportError:
    print("skip: PyYAML not installed")
    sys.exit(0)
with open(".github/workflows/deploy-vps.yml") as f:
    docs = list(yaml.safe_load_all(f))
assert docs and docs[0]["name"] == "deploy-vps"
assert "build-and-push" in docs[0]["jobs"]
assert "deploy-vps" in docs[0]["jobs"]
print("workflow yaml ok")
PY
```

Expected: `workflow yaml ok` or `skip: PyYAML not installed`

If `actionlint` is installed:

```bash
actionlint .github/workflows/deploy-vps.yml
```

Expected: exit 0

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/deploy-vps.yml
git commit -m "$(cat <<'EOF'
ci: add VPS private-registry build and deploy workflow

EOF
)"
```

---

### Task 3: Operator verification checklist (no live secrets in repo)

**Files:**
- None required (verification only). If operator docs are desired later, keep them out of v1 unless user asks.

**Interfaces:**
- Consumes: Tasks 1–2 artifacts + design spec secrets table
- Produces: confirmation that local tree is complete and safe

- [ ] **Step 1: Confirm deliverables exist**

Run:

```bash
test -f deploy/docker-compose.vps.yml
test -f .github/workflows/deploy-vps.yml
test -f docs/superpowers/specs/2026-08-01-vps-deploy-design.md
grep -q 'cliproxyapi/cli-proxy-api' .github/workflows/deploy-vps.yml
grep -q 'appleboy/ssh-action' .github/workflows/deploy-vps.yml
grep -q 'linux/amd64' .github/workflows/deploy-vps.yml
echo "deliverables ok"
```

Expected: `deliverables ok`

- [ ] **Step 2: Confirm public release path untouched**

Run:

```bash
git log -1 --oneline -- .github/workflows/docker-image.yml
# Working tree must not modify public release files in this feature branch tip commits
git diff origin/main...HEAD --name-only | grep -E 'docker-image.yml|^Dockerfile$|^docker-compose.yml$' || echo "public paths clean"
```

Expected: `public paths clean` (or only historical commits unrelated — for this feature, those three paths must not appear in the feature commits)

- [ ] **Step 3: Print operator secret checklist (do not set secrets from agent)**

Print for the human operator (do not write passwords into files):

```
GitHub repo → Settings → Secrets and variables → Actions
  REGISTRY_HOST=registry.hoangng26.work
  REGISTRY_USERNAME=<ci user>
  REGISTRY_PASSWORD=<ROTATED password — do not reuse chat password>
  VPS_HOST=<vps ip/host>
  VPS_USER=<ssh user>
  VPS_PASSWORD=<ssh password>
  VPS_COMPOSE_PATH=<absolute path on VPS>
  optional: VPS_PORT=22, VPS_COMPOSE_FILE=docker-compose.yml

VPS one-time:
  1. Install Docker + compose plugin
  2. Copy deploy/docker-compose.vps.yml to VPS_COMPOSE_PATH (as docker-compose.yml or set VPS_COMPOSE_FILE)
  3. Place config.yaml + auths/ + logs/ + plugins/ beside compose
  4. Ensure VPS can pull from registry.hoangng26.work
  5. First deploy: push tag vX.Y.Z OR workflow_dispatch after an image exists
```

- [ ] **Step 4: Final commit only if any checklist doc was added**

If no extra files: skip commit.

If something was fixed in prior tasks after review, ensure clean tree:

```bash
git status
```

Expected: clean working tree (or only unrelated WIP)

---

## Spec coverage checklist

| Spec requirement | Task |
|------------------|------|
| Separate workflow Approach A | Task 2 |
| Tag `v*` build+push+deploy | Task 2 |
| `workflow_dispatch` deploy-only | Task 2 |
| Image `registry.../cliproxyapi/cli-proxy-api` | Task 2 |
| amd64 only | Task 2 |
| SSH password via appleboy | Task 2 |
| compose pull + up -d | Task 2 |
| `CLI_PROXY_IMAGE` env contract | Task 1 + 2 |
| Sample compose with volumes | Task 1 |
| Secrets not in repo | Task 2 step 2 + Task 3 |
| Public docker-image.yml untouched | Global + Task 3 |
| Build-args VERSION/COMMIT/BUILD_DATE | Task 2 |
| Catalog refresh parity | Task 2 |
| Running-container verify | Task 2 remote script |
| Password rotation warning | Workflow header + Task 3 |

## Self-review notes

- No TBD/placeholder steps
- `VPS_COMPOSE_FILE` read from secrets with default in remote script (`${VPS_COMPOSE_FILE:-docker-compose.yml}`) — empty secret is fine
- `needs.build-and-push` + `if: always() && (...)` matches design for skipped build on dispatch
- `port: ${{ secrets.VPS_PORT || 22 }}` — if empty secret unsupported on some GH versions, operator can set `VPS_PORT=22` explicitly
- No live deploy test in CI without operator secrets — verification is static + operator checklist
