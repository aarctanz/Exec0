# Deploying Exec0

## Prerequisites

- Docker and Docker Compose v2
- The host must support cgroups v2 (required by isolate)
- Root or a user with Docker access

## Step-by-step

### 1. Clone and configure

```bash
git clone <repo-url> && cd Exec0
cp .env.sample .env
```

Edit `.env` to set at minimum:
- `DATABASE_PASSWORD` — change from default
- `SERVER_CORS_ALLOWED_ORIGINS` — set to your frontend origin(s)
- `WORKER_CONCURRENCY` — tune based on CPU count (0 = auto: NumCPU * 2)

### 2. Build and start

```bash
docker compose up -d --build
```

### 3. Verify

```bash
# All 10 containers should be running
docker compose ps

# Health check
curl http://localhost:8080/health

# Submit a test
curl -X POST http://localhost:8080/submissions \
  -H "Content-Type: application/json" \
  -d '{"language_id": 3, "source_code": "print(\"hello\")"}'
```

### 4. Access services

| Service | URL |
|---------|-----|
| API | http://localhost:8080 |
| Grafana | http://localhost:3000 (admin/admin) |
| Prometheus | http://localhost:9090 |
| Tempo | http://localhost:3200 |

## Worker requirements

The worker container runs with `privileged: true` and mounts the host's cgroup filesystem. This is required because isolate uses cgroups to enforce resource limits (memory, CPU, PIDs) on sandboxed processes.

If your host uses cgroups v1 instead of v2, you may need to adjust `deploy/worker-entrypoint.sh`.

## Scaling workers

To run multiple worker replicas:

```bash
docker compose up -d --scale exec0-worker=3
```

Each worker allocates unique box IDs via an atomic counter, so multiple instances can run safely. Adjust `WORKER_CONCURRENCY` per replica based on available CPU.

## Updating

```bash
git pull
docker compose up -d --build
```

Database migrations run automatically on first `postgres` container start. For subsequent migrations, run them manually:

```bash
docker compose exec postgres psql -U postgres -d exec0 -f /migrations/<migration>.up.sql
```

## Observability stack

The compose file includes a full Grafana LGTM stack:

- **Prometheus** scrapes API and worker `/metrics` endpoints every 15s
- **Promtail** collects Docker container logs and ships to Loki with `trace_id` labels
- **OTel Collector** receives traces via OTLP gRPC and forwards to Tempo
- **Grafana** is pre-provisioned with Prometheus, Tempo, and Loki datasources, including trace-to-log correlation

To run without the observability stack, start only the core services:

```bash
docker compose up -d postgres redis exec0-api exec0-worker
```

The API and worker will log warnings about the missing OTel collector but function normally.

## Production hardening

Before running in production:

- Change `DATABASE_PASSWORD` and `GF_SECURITY_ADMIN_PASSWORD` (in docker-compose.yaml)
- Restrict `SERVER_CORS_ALLOWED_ORIGINS` to your domain
- Put a reverse proxy (nginx, Caddy) in front of the API for TLS
- Restrict exposed ports — only the API port needs to be public
- Consider persistent volumes for Prometheus, Tempo, and Loki data
- Set `PRIMARY_ENV=production` for JSON log output (default in `.env.sample`)
