# Stage 1: Build the binary using official Go alpine image
FROM --platform=linux/amd64 golang:alpine AS builder

# Install CA certificates and tzdata for HTTPS scraping and accurate timezone support
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy module definitions and download dependencies for cache efficiency
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code and frontend assets
COPY cmd/ cmd/
COPY pkg/ pkg/
COPY web/ web/

# Build statically linked binary with stripped debug symbols for minimal size
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/server ./cmd/server

# Stage 2: Minimal runtime container
FROM --platform=linux/amd64 alpine:3.21

# Install CA certificates for outgoing TLS scraping and tzdata for Jakarta / UTC scheduling
RUN apk add --no-cache ca-certificates tzdata curl

# Create non-root system user and group
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

# Copy compiled binary from builder
COPY --from=builder /app/server /app/server

# Copy frontend static assets
COPY web/ /app/web/

# Create runtime directories and grant ownership to non-root user
RUN mkdir -p /app/output /app/data /app/gambar && \
    chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose standard application port
EXPOSE 8080

# Environment defaults
ENV PORT=8080
ENV TZ=Asia/Jakarta

# Healthcheck targeting system status endpoint
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8080/api/system/status || exit 1

# Entry point starts the server
ENTRYPOINT ["/app/server"]
CMD ["-port", "8080", "-host", "0.0.0.0", "-web-dir", "web", "-output-dir", "output"]
