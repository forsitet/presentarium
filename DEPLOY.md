# Deploying Presentarium to a VPS

This runbook covers a fresh production deploy to a single Linux host using
`docker-compose.prod.yml` + GitHub Container Registry + the `.github/workflows/deploy.yml`
pipeline. It documents what's already automated and what you do once by hand.

## Architecture recap

```
                 Internet :443
                     │
                     ▼
               ┌──────────┐
               │ (edge TLS│  Caddy / Traefik / Cloudflare / etc.
               │  proxy)  │  — NOT in this repo; see "TLS" below.
               └──────────┘
                     │ http://vps:80
                     ▼
┌────────────────────────────────────────────────────────────────┐
│  docker-compose.prod.yml (single docker network)                │
│                                                                 │
│   nginx :80  ──►  /api/       ─► backend :8080                  │
│                   /ws/        ─► backend :8080  (WebSocket)     │
│                   /uploads/   ─► volume alias                   │
│                   /media/     ─► minio :9000   (public bucket)  │
│                   /           ─► frontend :80  (static SPA)     │
│                                                                 │
│   backend   ─►  postgres :5432                                  │
│   backend   ─►  minio    :9000  (both public + private buckets) │
│   minio-init (one-shot)                                         │
└────────────────────────────────────────────────────────────────┘
```

Key design choices:

- **MinIO is never exposed publicly.** The public bucket is anonymous-read,
  and `nginx /media/` reverse-proxies it with aggressive caching. This is the
  "CDN" path participants' browsers use for slide and question images.
- **Backend gets two S3 URLs.** `S3_ENDPOINT=http://minio:9000` for signing /
  PUTs on the docker network, and `S3_PUBLIC_BASE_URL=/media/<bucket>` for the
  URLs it hands out to browsers. Both are injected by the compose file — do not
  set them in `.env`.
- **TLS is terminated one layer above.** `docker-compose.prod.yml` only listens
  on port 80. Put Caddy, Traefik, a Cloudflare tunnel, or any managed reverse
  proxy in front if you want HTTPS (recommended for anything public). See
  [TLS termination](#tls-termination).

## One-time VPS setup

Requires Ubuntu 22.04+ (or equivalent) with root-level sudo.

### 1. Install Docker Engine + compose plugin

```bash
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $USER   # log out + back in for this to take effect
docker compose version          # must print v2.x.y
```

### 2. Clone the repo into `/opt/presentarium`

The GitHub Action `git pull`s from this path on every deploy, so it must exist.

```bash
sudo mkdir -p /opt/presentarium
sudo chown $USER:$USER /opt/presentarium
git clone https://github.com/<your-user>/presentarium.git /opt/presentarium
cd /opt/presentarium
```

### 3. Create `.env` from the template

```bash
cp .env.prod.example .env
vi .env
```

Fill in:

- `GHCR_USER` — the GitHub user/org that owns the built container images
- Strong random values for `DB_PASSWORD`, `JWT_SECRET`, `MINIO_ROOT_PASSWORD`, `S3_SECRET_ACCESS_KEY`.
  Generate with `openssl rand -base64 48` (JWT) or `openssl rand -base64 24` (others).
- `CORS_ALLOWED_ORIGIN` and `APP_BASE_URL` — both should be the public HTTPS URL
  your users type (e.g. `https://presentarium.example.com`).
- `SMTP_*` if you want password-reset emails to work. Leave `SMTP_HOST` blank to
  skip email entirely.

**Never commit this file.** `.env` is gitignored.

### 4. Log in to GHCR so private images pull

```bash
echo "<your-GHCR-PAT>" | docker login ghcr.io -u "<your-GHCR-USER>" --password-stdin
```

The PAT needs `read:packages` scope. The GitHub Action does its own login on each
deploy; this manual step is only for bootstrapping before the first CI run.

### 5. First start

```bash
docker compose -f docker-compose.prod.yml up -d
docker compose -f docker-compose.prod.yml ps
```

Wait ~30 s for everything to go `healthy`. Verify:

```bash
curl -fsS http://localhost/api/health        # → {"status":"ok"} (or equivalent)
curl -I http://localhost/                    # → 200, serves the SPA
```

### 6. Configure the GitHub Action deploy step

In the repo's **Settings → Secrets and variables → Actions**, add:

| Secret        | Value                                                    |
| ------------- | -------------------------------------------------------- |
| `VPS_HOST`    | Public IP or DNS name of the VPS                         |
| `VPS_USER`    | SSH login that owns `/opt/presentarium` (typically same user as step 2) |
| `VPS_SSH_KEY` | Private key whose public half is in `~/.ssh/authorized_keys` on the VPS |
| `GHCR_TOKEN`  | GitHub PAT with `read:packages` so the VPS can `docker pull`            |

`GHCR_USER` is derived from `github.repository_owner` automatically.

## Routine deploys

Push to `master` → CI runs the `Build & Deploy` workflow (`.github/workflows/deploy.yml`):

1. `go build / vet / test` against the backend.
2. `npm ci && npm run build` against the frontend.
3. Builds + pushes two images: `ghcr.io/<owner>/presentarium-{backend,frontend}:latest`.
4. SSHes into the VPS, runs `git pull`, `docker compose pull`, `docker compose up -d`.

Zero-downtime-ish: `docker compose up -d` only restarts the services whose image
digests actually changed. Postgres + MinIO are untouched on app-only deploys.

**Watch the deploy log** in the Actions tab. The SSH step prints `docker compose ps`
at the end — check every service is `(healthy)`.

## TLS termination

`nginx` inside the compose stack only listens on `:80` — no certs, no ACME. Put
one of these in front on the host:

### Option A — Caddy on the host (easiest)

```bash
sudo apt install caddy
sudo tee /etc/caddy/Caddyfile > /dev/null <<'EOF'
your-domain.example {
    reverse_proxy localhost:80
}
EOF
sudo systemctl reload caddy
```

Caddy handles Let's Encrypt automatically. Change the compose port from `80:80`
to `127.0.0.1:8080:80` so only Caddy can reach it.

### Option B — Cloudflare tunnel

```bash
cloudflared tunnel --url http://localhost:80 ...
```

No open ports needed at all. Good fit for small deployments.

### Option C — Terminate at an external load balancer

Any HTTPS load balancer (AWS ALB, Hetzner LB, etc.) that forwards port 443 → port
80 on the VPS works. Make sure it sets `X-Forwarded-Proto: https` if you later
add redirect logic.

## Operational tasks

### Logs

```bash
docker compose -f docker-compose.prod.yml logs -f backend
docker compose -f docker-compose.prod.yml logs -f nginx
docker compose -f docker-compose.prod.yml logs --tail=200        # all services
```

### Backups

**Postgres:** run `pg_dump` from the host into a timestamped file.

```bash
docker compose -f docker-compose.prod.yml exec -T postgres \
  pg_dump -U "$DB_USER" -d "$DB_NAME" | gzip > backup-$(date +%F).sql.gz
```

Schedule via cron. Copy off-host.

**MinIO / uploads:** the `minio_data` and `uploads` named volumes live under
`/var/lib/docker/volumes/`. Snapshot them with `restic`, `borg`, or a block-level
VPS snapshot. The private bucket holds source `.pptx` files, which are worth
preserving. The public bucket holds slide/question images, which are regenerable
from the sources.

### Rollback

If a deploy goes bad:

```bash
cd /opt/presentarium
git log --oneline -5           # find the last-known-good commit
git checkout <sha>
docker compose -f docker-compose.prod.yml pull   # pull the image tagged `latest`
# ...but `latest` now points to the bad build. Two options:
#   1. Tag images by SHA in CI and pin here.
#   2. Temporarily roll back in GHCR (re-tag a prior digest as `latest`).
docker compose -f docker-compose.prod.yml up -d
```

A follow-up improvement is to tag images by `github.sha` so rollback is just
"point compose at the previous tag." Not done yet — tracked as tech debt.

### Migrations

Migrations run automatically on backend startup (`pkg/migrate` applies anything
new in `migrations/`). If a migration fails the backend exits and the compose
healthcheck keeps it out of rotation; `docker compose logs backend` will show
the SQL error.

To roll back a specific migration, shell into the backend container and use
`golang-migrate` directly — there is no compose-level helper for this yet.

### Clearing nginx's media cache

The `/media/` proxy caches responses under the `nginx_cache` volume with a 30-day
TTL. Images are content-addressed by UUID, so invalidation is rarely needed. If
you must blow it away:

```bash
docker compose -f docker-compose.prod.yml exec nginx \
  rm -rf /var/cache/nginx/media/*
```

## Smoke test after any deploy

1. `https://your-domain/` → SPA loads.
2. Log in → dashboard renders.
3. Create a session → join from a second browser as a participant.
4. Upload a small `.pptx` → wait for conversion (`status: ready`).
5. Open it from the host session → participant browser shows slides in real-time.
6. Nav with arrow keys → participant follows.

Items 4–6 specifically exercise MinIO → `/media/` → nginx → browser, which is the
prod-specific path most likely to break.

## Troubleshooting

| Symptom                                                | Check                                                                                      |
| ------------------------------------------------------ | ------------------------------------------------------------------------------------------ |
| Upload succeeds, participant sees a broken image icon  | `curl -I http://localhost/media/presentarium-public/<any-uuid>.webp` from the VPS — 404 means the backend didn't write it; 502 means nginx can't reach MinIO. |
| Backend stuck `unhealthy`                              | `docker compose logs backend` — usually a DB or S3 connection issue. Check `.env`.          |
| Conversion fails for a valid `.pptx`                   | Backend needs LibreOffice + Poppler + webp in the image; if they're missing the container rebuild is broken. |
| Participant sees stale slide after reconnect          | WebSocket snapshot on-connect is what rehydrates state. Confirm `/ws/` returns 101, not 502. |
| `docker compose up` complains about `GHCR_USER`       | `.env` wasn't read — run compose from `/opt/presentarium` (same dir as `.env`).             |
