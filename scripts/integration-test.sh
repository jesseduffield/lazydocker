#!/usr/bin/env bash
set -e

echo "=== lazypodman Integration Tests ==="

# Check if Podman is available
if ! command -v podman &> /dev/null; then
    echo "Error: Podman is not installed"
    exit 1
fi

# Check if Podman is running (socket available)
if ! podman info &> /dev/null; then
    echo "Error: Podman is not running."
    echo ""
    echo "To start Podman:"
    echo "  Linux (rootless): systemctl --user start podman.socket"
    echo "  Linux (rootful):  sudo systemctl start podman.socket"
    echo "  macOS/Windows:    podman machine start"
    exit 1
fi

echo "Podman is available and running."
echo ""

# Display Podman version
echo "Podman version:"
podman --version
echo ""

# Display socket info
echo "Podman socket info:"
if [ -n "$CONTAINER_HOST" ]; then
    echo "  CONTAINER_HOST: $CONTAINER_HOST"
elif [ -n "$DOCKER_HOST" ]; then
    echo "  DOCKER_HOST: $DOCKER_HOST"
else
    echo "  Using default socket path"
fi
echo ""

# Run integration tests
echo "Running integration tests..."
echo ""

export GOFLAGS=-mod=vendor

# Run integration tests with verbose output
go test -tags=integration -v ./pkg/commands/...

echo ""
echo "=== Integration tests completed ==="
