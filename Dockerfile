# syntax=docker/dockerfile:1
# Plex Smash Deck — single Go binary + static web UI. Run as non-root; persist /app/data.

FROM golang:1.22-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/plex-dashboard ./cmd/plex-dashboard

# Small runtime with TLS roots, timezone data, and wget for HEALTHCHECK (no shell in distroless).
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -g 65532 -S nonroot \
    && adduser -u 65532 -S -G nonroot -h /app nonroot

WORKDIR /app
COPY --from=build /out/plex-dashboard /usr/local/bin/plex-dashboard
# UI is embedded in the binary (web/embed_ui.go); no separate web/ copy needed.

RUN mkdir -p data && chown -R nonroot:nonroot /app

USER nonroot:nonroot
EXPOSE 8081
ENV PORT=8081

# Respects PORT if you override it at runtime.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD sh -c 'wget -q -O /dev/null "http://127.0.0.1:$${PORT:-8081}/api/health" || exit 1'

ENTRYPOINT ["/usr/local/bin/plex-dashboard"]
