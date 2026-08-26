# syntax=docker/dockerfile:1
#
# Production image for Coolify (Dockerfile build pack).
# Runtime: restic-web on :8080 with restic on PATH; restic-webctl for CLI/agents.
# Required env: DATABASE_URL
# Optional: PORT (default 8080), ADDR, RESTIC_WEB_PASSWORD (for restic-webctl)
# Persistent storage: mount a volume at /app/data for download workspaces + CLI session.

FROM golang:1.25-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/restic-web . \
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
ENV RESTIC_WEB_URL=http://127.0.0.1:8080
ENV RESTIC_WEB_SESSION_FILE=/app/data/.restic-webctl-session

EXPOSE 8080

CMD ["/app/restic-web", "-data", "/app/data"]
