# Exec0

A code execution service API built in Go. Submit code, run it in an [isolate](https://github.com/ioi/isolate) sandbox with configurable resource limits, and retrieve results asynchronously.

## Features

- **Sandboxed execution** via isolate with cgroups — CPU, memory, wall time, processes, file size, network, and stack limits
- **Async processing** — submissions are queued via Redis (asynq) and processed by a separate worker
- **Multi-language** — ships with C++ (GCC), Java (OpenJDK), and Python; extensible via the `languages` table
- **Retry support** — infrastructure failures retry up to 3 times; user code errors (TLE, RE, CE) do not
- **Concurrent execution** — worker runs multiple submissions in parallel with collision-free sandbox allocation

## Requirements

- Go 1.26+
- PostgreSQL
- Redis
- [isolate](https://github.com/ioi/isolate) (with cgroup support enabled)

## Quick Start (Docker)

The fastest way to get everything running — API, worker, database, queue, and full observability stack:

```bash
# 1. Copy and configure environment
cp .env.sample .env
# Edit .env if needed (defaults work out of the box for Docker)

# 2. Start everything
docker compose up -d

# 3. Verify
curl http://localhost:8080/health
```

This starts 10 services:

| Service | Port | Purpose |
|---------|------|---------|
| exec0-api | 8080 | API server |
| exec0-worker | 9091 | Execution worker (isolate sandbox) |
| postgres | 5432 | Database (auto-migrated + seeded) |
| redis | 6379 | asynq message queue |
| otel-collector | 4317 | OpenTelemetry Collector |
| tempo | 3200 | Distributed tracing backend |
| loki | 3100 | Log aggregation |
| promtail | — | Ships container logs to Loki |
| prometheus | 9090 | Metrics scraping |
| grafana | 3000 | Dashboards (admin/admin) |

## Local Development (without Docker)

```bash
# 1. Copy and configure environment
cp .env.sample .env
# Set DATABASE_HOST=localhost, REDIS_ADDRESS=localhost:6379, PRIMARY_ENV=local

# 2. Start PostgreSQL and Redis locally

# 3. Run database migrations
migrate -source file://database/migrations \
  -database "postgres://user:pass@localhost:5432/exec0?sslmode=disable" up

# 4. Start the API server
go run cmd/Exec0/main.go

# 5. Start the worker (in a separate terminal)
go run cmd/worker/main.go
```

Requires Go 1.26+, PostgreSQL, Redis, and [isolate](https://github.com/ioi/isolate) with cgroup support.

## API

### Languages

| Method | Endpoint            | Description         |
|--------|---------------------|---------------------|
| GET    | `/languages`        | List all languages  |
| GET    | `/languages/{id}`   | Get language by ID  |

### Submissions

| Method | Endpoint              | Description              |
|--------|-----------------------|--------------------------|
| POST   | `/submissions`        | Create a new submission  |
| GET    | `/submissions`        | List submissions (paginated) |
| GET    | `/submissions/{id}`   | Get submission by ID     |

### Health

| Method | Endpoint  | Description  |
|--------|-----------|--------------|
| GET    | `/health` | Health check |

### Create Submission

```json
POST /submissions
{
  "language_id": 1,
  "source_code": "#include<iostream>\nint main(){std::cout<<\"hello\";return 0;}",
  "stdin": "optional input",
  "cpu_time_limit": 5.0,
  "wall_time_limit": 10.0,
  "memory_limit": 256000
}
```

All resource limit fields are optional — server defaults apply when omitted.

### Submission Lifecycle

`pending` → `compiling` → `running` → `accepted` | `compilation_error` | `runtime_error` | `time_limit_exceeded` | `internal_error`

## Architecture

```
cmd/
  Exec0/main.go           # API server entry point
  worker/main.go           # Queue worker entry point
internal/
  config/                  # Environment config + execution defaults
  database/                # PostgreSQL connection pool
  database/queries/        # sqlc-generated type-safe DB code
  handlers/                # HTTP handlers (languages, submissions, health, monitoring)
  logger/                  # zerolog setup + context helpers
  metrics/                 # Prometheus metrics definitions
  middleware/              # CORS, logging, metrics, panic recovery
  models/                  # DTOs
  queue/                   # asynq client (enqueue) and server (dequeue)
  server/                  # HTTP server + route registration
  services/                # Business logic (execution, submissions, languages)
  telemetry/               # OpenTelemetry tracing init
  util/                    # Response helpers
database/
  queries/                 # SQL files for sqlc
  migrations/              # Timestamped migration files
deploy/                    # Docker infrastructure configs
  grafana/                 # Grafana datasource provisioning
  loki/                    # Loki config
  otel-collector/          # OTel Collector config
  prometheus/              # Prometheus scrape config
  promtail/                # Promtail Docker log collection config
  tempo/                   # Tempo trace storage config
```

## Supported Languages

| Language | Version     | Compile Command | Run Command |
|----------|-------------|-----------------|-------------|
| C++      | GCC 14.2.0  | `g++ -B/usr/bin -o main main.cpp` | `./main` |
| Java     | OpenJDK 23  | `javac Main.java` | `java Main` |
| Python   | 3.13        | — | `python3 script.py` |

Add more by inserting into the `languages` table.

## Observability

- **Metrics** — Prometheus scrapes `/metrics` on both API (8080) and worker (9091). 14 custom `exec0_*` metrics covering HTTP requests, job processing, sandbox failures, DB operations.
- **Tracing** — OpenTelemetry distributed traces flow from API through Redis queue to worker. Spans cover HTTP requests, DB calls, and sandbox phases (init, compile, run). Traces export via OTLP gRPC to the collector, then to Tempo.
- **Logging** — Structured JSON logs (zerolog) with `trace_id` and `request_id` fields. Promtail ships container logs to Loki with `trace_id` as an indexed label, enabling log-to-trace correlation in Grafana.

## Configuration

All configuration is via environment variables (`.env` file). See `.env.sample` for the full list.

| Variable | Description | Docker default |
|----------|-------------|----------------|
| `PRIMARY_ENV` | `production` (JSON logs) or `local` (console) | `production` |
| `SERVER_PORT` | API server port | `8080` |
| `DATABASE_HOST` | PostgreSQL host | `postgres` |
| `REDIS_ADDRESS` | Redis address | `redis:6379` |
| `WORKER_CONCURRENCY` | Worker parallelism (0 = auto) | `4` |
| `OTEL_ENDPOINT` | OTel Collector gRPC endpoint | `otel-collector:4317` |

See `internal/config/` for the full list of execution defaults (CPU time, memory, processes, etc.).
