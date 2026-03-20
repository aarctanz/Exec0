FROM golang:1.26-bookworm AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 go build -o /exec0-api cmd/Exec0/main.go
RUN CGO_ENABLED=0 go build -o /exec0-worker cmd/worker/main.go

# --- API image ---
FROM debian:bookworm-slim AS api

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /exec0-api /usr/local/bin/exec0-api
EXPOSE 8080
CMD ["exec0-api"]

# --- Worker image ---
FROM debian:bookworm-slim AS worker

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    build-essential \
    pkg-config \
    g++ \
    python3 \
    default-jdk-headless \
    libcap-dev \
    libsystemd-dev \
    git \
    && rm -rf /var/lib/apt/lists/*

# Build isolate from source
RUN git clone --branch v2.2.1 --depth 1 https://github.com/ioi/isolate.git /tmp/isolate \
    && cd /tmp/isolate \
    && make install \
    && rm -rf /tmp/isolate

COPY --from=builder /exec0-worker /usr/local/bin/exec0-worker

# Isolate needs cgroup dir and /run/isolate at runtime
RUN mkdir -p /var/local/lib/isolate

COPY deploy/worker-entrypoint.sh /usr/local/bin/worker-entrypoint.sh
RUN chmod +x /usr/local/bin/worker-entrypoint.sh

EXPOSE 9091
ENTRYPOINT ["worker-entrypoint.sh"]
CMD ["exec0-worker"]
