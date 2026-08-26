# syntax=docker/dockerfile:1
#
# Production image for Coolify (Dockerfile build pack).
# Runtime: restic-web on :8080 with restic on PATH; restic-webctl for CLI/agents.
# Required env: DATABASE_URL
# Optional: PORT (default 8080), ADDR
# Persistent storage: mount a volume at /app/data for download workspaces.
# restic-webctl talks to Postgres directly (no HTTP/auth): needs DATABASE_URL + data dir.

FROM golang:1.25-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/restic-web ./cmd/restic-web \
  && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/restic-webctl ./cmd/restic-webctl

FROM debian:bookworm-slim

RUN apt-get update -y \
  && apt-get install -y --no-install-recommends ca-certificates curl restic \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /out/restic-web /app/restic-web
COPY --from=builder /out/restic-webctl /app/restic-webctl

RUN mkdir -p /app/data \
  && groupadd --system --gid 1001 app \
  && useradd --system --uid 1001 --gid app app \
  && chown -R app:app /app \
  && ln -sf /app/restic-webctl /usr/local/bin/restic-webctl

USER app

ENV PORT=8080
ENV ADDR=0.0.0.0:8080
ENV PATH="/app:/usr/local/bin:${PATH}"
ENV RESTIC_WEB_DATA=/app/data

EXPOSE 8080

CMD ["/app/restic-web", "-data", "/app/data"]
