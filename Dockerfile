# Stage 1: Build the binary and prepare assets using official Go alpine image
FROM golang:1.22-alpine AS builder

# Install CA certificates and tzdata for HTTPS scraping and accurate timezone support
RUN apk add --no-cache ca-certificates tzdata

# Create non-root system user and runtime directories in builder stage
RUN addgroup -S appgroup && adduser -S appuser -G appgroup -D && \
    mkdir -p /app/output /app/data /app/gambar && \
    chown -R appuser:appgroup /app

WORKDIR /app

# Copy module definitions and download dependencies for cache efficiency
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code and frontend assets
COPY cmd/ cmd/
COPY pkg/ pkg/
COPY web/ web/

# Build statically linked binary with stripped debug symbols for minimal size
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /app/server ./cmd/server

# Stage 2: Minimal universal runtime container (compatible with all CPU architectures: AMD64, ARM64, etc.)
FROM alpine:3.21

# Copy system certificates, timezone data, and user from builder (zero build-time execution required)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group

WORKDIR /app

# Copy compiled binary, frontend assets, and initialized runtime directories
COPY --from=builder --chown=appuser:appgroup /app /app

# Switch to non-root user
USER appuser

# Expose standard application port
EXPOSE 8080

# Environment defaults
ENV PORT=8080
ENV TZ=Asia/Jakarta

# Healthcheck targeting system status endpoint using Alpine's built-in wget
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -q --spider http://127.0.0.1:8080/api/system/status || exit 1

# Entry point starts the server
ENTRYPOINT ["/app/server"]
CMD ["-port", "8080", "-host", "0.0.0.0", "-web-dir", "web", "-output-dir", "output"]

