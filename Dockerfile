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

# Debian's restic package is far behind (0.14). Size/Files in the UI need
# restic >= 0.17, which stores a snapshot summary. Install the official binary.
ARG RESTIC_VERSION=0.19.1
ARG TARGETARCH
RUN apt-get update -y \
  && apt-get install -y --no-install-recommends ca-certificates curl bzip2 \
  && ARCH="${TARGETARCH:-$(dpkg --print-architecture 2>/dev/null || uname -m)}" \
  && case "$ARCH" in x86_64) ARCH=amd64 ;; aarch64) ARCH=arm64 ;; esac \
  && case "$ARCH" in amd64|arm64|arm|386) ;; *) echo "unsupported TARGETARCH=$ARCH" >&2; exit 1 ;; esac \
  && curl -fsSL "https://github.com/restic/restic/releases/download/v${RESTIC_VERSION}/restic_${RESTIC_VERSION}_linux_${ARCH}.bz2" \
    | bunzip2 > /usr/local/bin/restic \
  && chmod +x /usr/local/bin/restic \
  && restic version \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /out/restic-web /app/restic-web
COPY --from=builder /out/restic-webctl /app/restic-webctl

RUN mkdir -p /app/data /app/data/cache \
  && groupadd --system --gid 1001 app \
  && useradd --system --uid 1001 --gid app --home-dir /app --shell /usr/sbin/nologin app \
  && chown -R app:app /app \
  && ln -sf /app/restic-webctl /usr/local/bin/restic-webctl

USER app

ENV PORT=8080
ENV ADDR=0.0.0.0:8080
ENV HOME=/app
ENV PATH="/app:/usr/local/bin:${PATH}"
ENV RESTIC_WEB_DATA=/app/data
# restic defaults to $HOME/.cache; pin under the data volume so it survives and is writable.
ENV RESTIC_CACHE_DIR=/app/data/cache

EXPOSE 8080

CMD ["/app/restic-web", "-data", "/app/data"]
