# syntax=docker/dockerfile:1
#
# Production image for Coolify (Dockerfile build pack).
# Runtime: restic-web on :8080 with restic on PATH.
# Required env: DATABASE_URL
# Optional: PORT (default 8080), ADDR
# Persistent storage: mount a volume at /app/data for download workspaces.

FROM golang:1.25-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/restic-web .

FROM debian:bookworm-slim

RUN apt-get update -y \
  && apt-get install -y --no-install-recommends ca-certificates curl restic \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /out/restic-web /app/restic-web

RUN mkdir -p /app/data \
  && groupadd --system --gid 1001 app \
  && useradd --system --uid 1001 --gid app app \
  && chown -R app:app /app

USER app

ENV PORT=8080
ENV ADDR=0.0.0.0:8080

EXPOSE 8080

CMD ["/app/restic-web", "-data", "/app/data"]
