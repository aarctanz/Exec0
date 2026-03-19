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

## Quick Start

```bash
# 1. Copy and configure environment
cp .env.sample .env
# Edit .env with your PostgreSQL and Redis connection details

# 2. Run database migrations
migrate -source file://database/migrations \
  -database "postgres://user:pass@localhost:5432/exec0?sslmode=disable" up

# 3. Start the API server
go run cmd/Exec0/main.go

# 4. Start the worker (in a separate terminal)
go run cmd/worker/main.go
```

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
  Exec0/main.go          # API server entry point
  worker/main.go          # Queue worker entry point
internal/
  config/                 # Environment config + execution defaults
  database/               # PostgreSQL connection pool
  database/queries/       # sqlc-generated type-safe DB code
  handlers/               # HTTP handlers (languages, submissions, health)
  middleware/              # CORS, logging, panic recovery
  models/                 # DTOs
  queue/                  # asynq client (enqueue) and server (dequeue)
  server/                 # HTTP server + route registration
  services/               # Business logic (execution, submissions, languages)
  util/                   # Response helpers
database/
  queries/                # SQL files for sqlc
  migrations/             # Timestamped migration files
```

## Supported Languages

| Language | Version     | Compile Command | Run Command |
|----------|-------------|-----------------|-------------|
| C++      | GCC 14.2.0  | `g++ -B/usr/bin -o main main.cpp` | `./main` |
| Java     | OpenJDK 23  | `javac Main.java` | `java Main` |
| Python   | 3.13        | — | `python3 script.py` |

Add more by inserting into the `languages` table.

## Configuration

All configuration is via environment variables (`.env` file). Key settings:

| Variable | Description | Default |
|----------|-------------|---------|
| `SERVER_PORT` | API server port | `8080` |
| `DATABASE_HOST` | PostgreSQL host | `localhost` |
| `REDIS_ADDRESS` | Redis address for asynq | — |

See `internal/config/` for the full list of execution defaults (CPU time, memory, processes, etc.).
