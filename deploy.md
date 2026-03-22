# Deployment Guide

## Prerequisites

- Docker Engine 24+ with Compose v2 plugin
- Host with cgroup v2 enabled (required for isolate sandbox)
- Dedicated host recommended (worker runs privileged containers)

## Quick Start

```bash
git clone <repo-url> && cd Exec0
cp .env.sample .env
# Edit .env with production values (see Configuration below)
docker compose up -d --build
```

## Verify

```bash
# All containers should be running
docker compose ps

# Health check — returns postgres and redis status
curl http://localhost:8080/health

# Submit a test
curl -X POST http://localhost:8080/submissions \
  -H "Content-Type: application/json" \
  -d '{"language_id": 3, "source_code": "print(\"hello\")", "stdin": ""}'
```

## Configuration

All configuration is via environment variables in `.env`. See `.env.sample` for all options.

### Required Variables

| Variable | Description | Example |
|---|---|---|
| `PRIMARY_ENV` | Environment name (`local` or `production`) | `production` |
| `SERVER_PORT` | API listen port | `8080` |
| `SERVER_READ_TIMEOUT` | HTTP read timeout (seconds) | `15` |
| `SERVER_WRITE_TIMEOUT` | HTTP write timeout (seconds) | `15` |
| `SERVER_IDLE_TIMEOUT` | HTTP idle timeout (seconds) | `60` |
| `DATABASE_HOST` | PostgreSQL host | `postgres` (in Docker) |
| `DATABASE_PORT` | PostgreSQL port | `5432` |
| `DATABASE_USER` | PostgreSQL user | `exec0` |
| `DATABASE_PASSWORD` | PostgreSQL password | (use a strong password) |
| `DATABASE_NAME` | PostgreSQL database name | `exec0` |
| `DATABASE_SSL_MODE` | PostgreSQL SSL mode | `disable` (internal network) |
| `DATABASE_MAX_OPEN_CONNS` | Max open DB connections | `25` |
| `DATABASE_MAX_IDLE_CONNS` | Max idle DB connections | `25` |
| `DATABASE_CONN_MAX_LIFETIME` | Connection max lifetime (seconds) | `300` |
| `DATABASE_CONN_MAX_IDLE_TIME` | Connection max idle time (seconds) | `300` |
| `REDIS_ADDRESS` | Redis address | `redis:6379` (in Docker) |
| `WORKER_CONCURRENCY` | Max concurrent executions | `4` (see sizing below) |
| `WORKER_METRICS_PORT` | Worker Prometheus metrics port | `9091` |

### Optional Variables

| Variable | Description | Default |
|---|---|---|
| `SERVER_ALLOWED_IPS` | Comma-separated IPs/CIDRs to allow | empty (allow all) |
| `OTEL_ENDPOINT` | OpenTelemetry collector gRPC endpoint | `otel-collector:4317` |
| `OTEL_SERVICE_NAME` | OTel service name | `exec0-api` / `exec0-worker` |

### IP Allowlist

Restrict API access to specific IPs or subnets:

```env
# Single IP
SERVER_ALLOWED_IPS=192.168.1.100

# Multiple IPs
SERVER_ALLOWED_IPS=192.168.1.100,192.168.1.101

# CIDR range (entire campus network)
SERVER_ALLOWED_IPS=10.0.0.0/8

# Mixed
SERVER_ALLOWED_IPS=10.0.0.0/8,192.168.1.100
```

When empty or unset, all IPs are allowed.

## Architecture

```
                    ┌───────────┐
contest backend ──> │    api    │ ──> Redis queue
                    └───────────┘
                                      │
                    ┌────────────┐    │
                    │   worker   │ <──┘
                    │(privileged)│
                    │ ┌────────┐ │
                    │ │isolate │ │
                    │ │sandbox │ │
                    │ └────────┘ │
                    └────────────┘
                         │
                    ┌────────────┐
                    │ PostgreSQL │
                    └────────────┘
```

### Networks

- **core**: postgres, redis, api, worker — application traffic
- **observability**: otel-collector, tempo, loki, promtail, prometheus, grafana — monitoring traffic
- api and worker bridge both networks

### Services

| Service | Port | Exposed | Purpose |
|---|---|---|---|
| api | 8080 | Yes (configurable) | HTTP API |
| worker | 9091 | No (internal metrics) | Processes execution queue |
| postgres | 5432 | No | Submission storage |
| redis | 6379 | No | Task queue (asynq) |
| prometheus | 9090 | localhost only | Metrics collection (5s scrape) |
| grafana | 3000 | localhost only | Dashboards |
| otel-collector | 4317 | No | Trace collection |
| tempo | - | No | Trace storage |
| loki | - | No | Log aggregation |
| promtail | - | No | Log shipping |

## Worker Sizing

The worker runs code in isolate sandboxes. Each concurrent execution uses:
- One isolate box (cgroup namespace)
- Memory up to the submission's `memory_limit` (default 256MB, max 512MB)
- CPU time up to the submission's `cpu_time_limit`

**Formula**: `WORKER_CONCURRENCY` x max memory per submission = peak memory.

Example: 4 concurrent x 512MB max = 2GB peak for sandboxes alone, plus ~500MB for the JVM/Go runtime overhead.

For a contest with ~50 participants:
- `WORKER_CONCURRENCY=8` handles typical load
- 8GB RAM minimum, 16GB recommended
- 4+ CPU cores

## Health Check

```bash
curl http://localhost:8080/health
```

Returns service dependency status:

```json
{
  "status": "ok",
  "services": {
    "postgres": "up",
    "redis": "up"
  }
}
```

Returns HTTP 503 with `"status": "degraded"` if any dependency is down.

## Monitoring

### Dashboards

Access Grafana at `http://localhost:3000` (SSH tunnel if remote):

```bash
ssh -L 3000:localhost:3000 user@exec0-host
```

Default credentials: `admin` / `admin`.

### Key Metrics to Watch

- `exec0_worker_active_jobs` vs `exec0_worker_concurrency` — worker saturation
- `exec0_job_queue_wait_seconds` — if growing, workers can't keep up
- `exec0_jobs_processed_total` by status — acceptance rate
- `exec0_sandbox_failures_total` — should always be 0

## Database

### Migrations

Migrations run automatically on first startup via `deploy/init-db.sh`. Migration files are in `database/migrations/`.

For subsequent migrations, run manually:

```bash
docker compose exec postgres psql -U $DATABASE_USER -d $DATABASE_NAME -f /migrations/<migration>.up.sql
```

### Backup

Set up a periodic pg_dump:

```bash
# Add to crontab on the host
0 */6 * * * docker compose exec -T postgres pg_dump -U $DATABASE_USER $DATABASE_NAME | gzip > /backups/exec0_$(date +\%Y\%m\%d_\%H\%M).sql.gz
```

## Scaling Workers

To run multiple worker replicas:

```bash
docker compose up -d --scale worker=3
```

Each worker allocates unique box IDs via an atomic counter, so multiple instances can run safely. Adjust `WORKER_CONCURRENCY` per replica based on available CPU.

## Running Without Observability

To run only the core services:

```bash
docker compose up -d postgres redis api worker
```

The API and worker will log warnings about the missing OTel collector but function normally.

## Security Notes

- **Worker runs privileged** — required for isolate cgroup management. Dedicate the host to exec0; don't run untrusted workloads alongside it.
- **Internal network only** — designed for campus/internal network use behind a firewall. Use `SERVER_ALLOWED_IPS` to restrict access to the contest backend.
- **Database credentials** — stored in `.env`. For production, consider Docker secrets.
- **Grafana** — has anonymous admin access enabled for convenience. Acceptable for campus use; restrict if needed.

## Troubleshooting

### Worker can't start isolate boxes

Ensure cgroup v2 is enabled and the worker container has `privileged: true` and `cgroup: host`.

```bash
# Verify cgroup v2 on host
mount | grep cgroup2
```

### Submissions stuck in "pending"

Check if the worker is running and connected to Redis:

```bash
docker compose logs worker --tail 50
curl http://localhost:8080/monitoring/queues
```

### Java compilation timeouts

Java compilation uses fixed limits (5s CPU, 30s wall, 512MB memory). Under high concurrency, `javac` wall time can spike due to CPU contention. If persistent, reduce `WORKER_CONCURRENCY` or add more CPU.

## Warning

Do not use `docker compose down -v` on a live instance — this wipes all PostgreSQL data, metrics, and logs.

## Debug

```bash
docker compose logs -f api worker postgres redis
```
