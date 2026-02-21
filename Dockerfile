# Afficho Client — Docker Image
# Multi-stage build: Go builder + minimal Debian runtime.
#
# Build:  docker build -t afficho-client:dev --build-arg VERSION=dev .
# Run:    docker run -p 8080:8080 -v afficho-data:/var/lib/afficho afficho-client:dev

# ── Stage 1: Build ───────────────────────────────────────────────────────────
FROM golang:1.24-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/afficho ./cmd/afficho

# ── Stage 2: Runtime ─────────────────────────────────────────────────────────
FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

RUN useradd --system --no-create-home --shell /usr/sbin/nologin afficho && \
    mkdir -p /var/lib/afficho /var/log/afficho /etc/afficho && \
    chown afficho:afficho /var/lib/afficho /var/log/afficho

COPY --from=builder /out/afficho /usr/local/bin/afficho
COPY config.example.toml /etc/afficho/config.toml

USER afficho
EXPOSE 8080
VOLUME ["/var/lib/afficho"]

ENTRYPOINT ["/usr/local/bin/afficho"]
CMD ["-config", "/etc/afficho/config.toml"]
