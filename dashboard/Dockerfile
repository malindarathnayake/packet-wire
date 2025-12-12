# Multi-stage build for Packet Wire Dashboard
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY server.go ./

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o dashboard-server server.go

# Final stage
FROM alpine:3.19

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/dashboard-server .

# Copy static web files
COPY index.html dashboard.js dashboard-config.json ./

# Create non-root user
RUN addgroup -S dashboard && adduser -S dashboard -G dashboard
USER dashboard

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the server
CMD ["./dashboard-server"]
