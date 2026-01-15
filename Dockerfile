FROM golang:1.25 as builder

# Create and change to the app directory.
WORKDIR /app

# Retrieve application dependencies.
# This allows the container build to reuse cached dependencies.
# Expecting to copy go.mod and if present go.sum.
COPY ./src/go.* ./
RUN go mod download

# Copy local code to the container image.
COPY ./src ./

# Build the binary.
RUN go build -v -o server

# Use the official Debian slim image for a lean production container.
# https://hub.docker.com/_/debian
# https://docs.docker.com/develop/develop-images/multistage-build/#use-multi-stage-builds
FROM docker.io/parrotsec/core:latest
RUN set -x && apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y \
    ca-certificates geoipupdate && \
    rm -rf /var/lib/apt/lists/*

# Copy the binary to the production image from the builder stage.
COPY --from=builder /app/server /app/server
COPY config/ /app/config/
COPY GeoIP.conf /etc/
RUN mkdir -p /app/data
RUN chmod +x /app/server

# Run the web service on container startup.
CMD ["/app/server/mirrordirector"]
