# Build stage
FROM golang:1.21-alpine AS builder

# Install necessary build dependencies
RUN apk add --no-cache gcc musl-dev

WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/server

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Set timezone
ENV TZ=Europe/Moscow

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/main .

# Copy static files
COPY --from=builder /app/static ./static

# Expose port 8080
EXPOSE 8080

# Run the application
CMD ["./main"] 