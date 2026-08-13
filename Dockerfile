# ── Build Stage ────────────────────────────────────────────────────────────────
FROM golang:1.23-alpine AS builder

# Install git (needed for go modules with private repos if any)
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Cache dependency downloads separately from source code
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build a statically linked binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /bin/api ./cmd/api

# ── Runtime Stage ──────────────────────────────────────────────────────────────
FROM alpine:latest

# Security: run as non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# Copy timezone data and CA certs from builder
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy compiled binary
COPY --from=builder /bin/api /usr/local/bin/api

USER appuser

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/api"]
