#!/bin/bash
set -e

# Forgejo Act Runner startup script
# This script registers the runner with Forgejo and then executes a single job

# Required environment variables:
# - TOKEN: Registration token (from registration token secret)
# - FORGEJO_SERVER: Forgejo server URL (e.g., https://git.cloud.danmanners.com)
# - FORGEJO_ORG: Organization name
# - FORGEJO_LABELS: Comma-separated list of runner labels

# Optional environment variables:
# - FORGEJO_RUNNER_NAME: Custom runner name (defaults to auto-generated)

# Docker socket path (must match the mount path in the pod spec)
DOCKER_SOCKET="/var/docker/docker.sock"

# Function to check if Docker daemon is accessible
check_docker_socket() {
    local attempt=$1
    local max_attempts=10
    local wait_interval=3
    
    echo "Checking Docker socket availability (attempt $attempt/$max_attempts)..."
    
    # Check if socket file exists
    if [ ! -S "$DOCKER_SOCKET" ]; then
        echo "  Docker socket file does not exist: $DOCKER_SOCKET" >&2
        return 1
    fi
    
    # Try to ping the Docker daemon using docker version (lightweight check)
    if docker version --format '{{.Server.Version}}' >/dev/null 2>&1; then
        echo "  Docker daemon is responsive"
        return 0
    else
        echo "  Docker daemon is not responding" >&2
        return 1
    fi
}

# Wait for Docker socket to be available
wait_for_docker() {
    local max_attempts=10
    local wait_interval=3
    local attempt=1
    
    echo "Waiting for Docker socket to be available..."
    echo "  Socket path: $DOCKER_SOCKET"
    echo "  Max attempts: $max_attempts"
    echo "  Interval: ${wait_interval}s"
    
    while [ $attempt -le $max_attempts ]; do
        if check_docker_socket $attempt; then
            echo "Docker socket is ready!"
            return 0
        fi
        
        if [ $attempt -lt $max_attempts ]; then
            echo "  Waiting ${wait_interval} seconds before next attempt..."
            sleep $wait_interval
        fi
        
        attempt=$((attempt + 1))
    done
    
    echo "ERROR: Docker socket is not available after $max_attempts attempts" >&2
    echo "  Socket path: $DOCKER_SOCKET" >&2
    echo "  Please ensure the dind sidecar container is running" >&2
    exit 1
}

# Validate required environment variables
if [ -z "$TOKEN" ]; then
    echo "ERROR: TOKEN environment variable is required" >&2
    exit 1
fi

if [ -z "$FORGEJO_SERVER" ]; then
    echo "ERROR: FORGEJO_SERVER environment variable is required" >&2
    exit 1
fi

if [ -z "$FORGEJO_ORG" ]; then
    echo "ERROR: FORGEJO_ORG environment variable is required" >&2
    exit 1
fi

# Wait for Docker socket before proceeding
wait_for_docker

# Generate runner name if not provided
RUNNER_NAME="${FORGEJO_RUNNER_NAME:-runner-$(hostname)-$(date +%s)}"

echo "Registering runner with Forgejo..."
echo "  Server: $FORGEJO_SERVER"
echo "  Organization: $FORGEJO_ORG"
echo "  Name: $RUNNER_NAME"
if [ -n "$FORGEJO_LABELS" ]; then
    echo "  Labels: $FORGEJO_LABELS"
fi

# Register the runner
FORGEJO_RUNNER="/usr/local/bin/forgejo-runner"

if [ -n "$FORGEJO_LABELS" ]; then
    "$FORGEJO_RUNNER" register \
        --no-interactive \
        --instance "$FORGEJO_SERVER" \
        --token "$TOKEN" \
        --name "$RUNNER_NAME" \
        --labels "$FORGEJO_LABELS"
else
    "$FORGEJO_RUNNER" register \
        --no-interactive \
        --instance "$FORGEJO_SERVER" \
        --token "$TOKEN" \
        --name "$RUNNER_NAME"
fi

REGISTER_EXIT_CODE=$?

if [ $REGISTER_EXIT_CODE -ne 0 ]; then
    echo "ERROR: Failed to register runner (exit code: $REGISTER_EXIT_CODE)" >&2
    exit $REGISTER_EXIT_CODE
fi

echo "✔ Runner registered successfully"
echo "✔ Runner is ready to execute jobs"
echo "---------------------------------"

# Execute a single job
exec "$FORGEJO_RUNNER" one-job

