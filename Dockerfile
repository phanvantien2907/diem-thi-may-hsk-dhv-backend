# ── Build Stage ────────────────────────────────────────────────────────────────
FROM golang:1.23-alpine AS builder

# Install build tools: git, certs, tzdata, and UPX executable compressor
RUN apk add --no-cache git ca-certificates tzdata upx

WORKDIR /app

# Cache dependency downloads separately from source code
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build a statically linked, stripped binary with UPX compression
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /bin/api ./cmd/api && \
    upx --best --lzma /bin/api

# ── Runtime Stage ──────────────────────────────────────────────────────────────
FROM alpine:3.21

# Security: run as non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# Copy timezone data and CA certs from builder
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy compiled and compressed binary
COPY --from=builder /bin/api /usr/local/bin/api

USER appuser

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/api"]

