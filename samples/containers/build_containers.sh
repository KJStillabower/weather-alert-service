#!/bin/bash
#
# build_containers.sh - Build weather_service and weather_memcache containers
#
# Usage:  ./samples/containers/build_containers.sh [tag]
#         tag defaults to "latest"
#
# Builds from project root. Run from project root or samples/containers/.
# Requires: docker

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TAG="${1:-latest}"

cd "$PROJECT_ROOT"

echo "Building weather_memcache:$TAG..."
docker build -f samples/containers/Dockerfile.weather_memcache -t weather_memcache:"$TAG" .

echo "Building weather_service:$TAG..."
docker build -f samples/containers/Dockerfile.weather_service -t weather_service:"$TAG" .

echo "Done. Images:"
docker images weather_memcache weather_service --format "  {{.Repository}}:{{.Tag}}"
