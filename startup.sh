#!/usr/bin/env sh
set -e

DOCKER_SOCK="${DOCKER_HOST:-unix://$HOME/.docker/run/docker.sock}"
echo "Starting Code Share with Docker Compose using $DOCKER_SOCK..."
DOCKER_HOST="$DOCKER_SOCK" docker compose up --build
