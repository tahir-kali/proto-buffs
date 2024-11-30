# Step 1: Build the Go binary inside a Go container
FROM golang:1.23 AS builder

# Set the working directory in the builder stage
WORKDIR /app

# Copy Go module files and download dependencies (optimizing cache)
COPY go.mod go.sum ./
RUN go mod tidy  # It's better to use go mod tidy instead of go mod download to clean up unused dependencies

# Copy the entire application source code
COPY . .

# Build the Go binary (assumes main entry point is server.go)
RUN go build -o server ./server.go

# Step 2: Use a lighter image to run the Go application
FROM debian:bullseye-slim

# Install necessary libraries including CA certificates for TLS handshake
RUN apt-get update && apt-get install -y \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*  # Clean up apt cache to keep the image small

# Copy the compiled binary and configuration file from the builder
COPY --from=builder /app/server /server
COPY ./default.json /default.json

# Ensure CA certificates are updated and trusted
RUN update-ca-certificates

# Set the environment variable for Google Cloud credentials
ENV GOOGLE_APPLICATION_CREDENTIALS=/default.json

# Set the working directory for the app
WORKDIR /app

# Expose the port the app will run on
EXPOSE 1000

# Run the Go server binary when the container starts
CMD ["/server"]
