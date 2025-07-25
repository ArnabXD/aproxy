# Build stage
FROM golang:1.24-alpine AS builder

# Install git, ca-certificates, and build tools (for CGO)
RUN apk add --no-cache git ca-certificates gcc musl-dev

# Set working directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o aproxy ./cmd/aproxy

# Final stage
FROM alpine:latest

# Install ca-certificates and curl for HTTPS requests and healthcheck
RUN apk --no-cache add ca-certificates curl

# Create non-root user
RUN addgroup -S aproxy && adduser -S aproxy -G aproxy

# Set working directory
WORKDIR /app

# Create data directory for database and logs
RUN mkdir -p /app/data && chown aproxy:aproxy /app/data

# Copy binary from builder stage
COPY --from=builder /app/aproxy .

# Set ownership
RUN chown -R aproxy:aproxy /app

# Switch to non-root user
USER aproxy

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Default command
CMD ["./aproxy"]