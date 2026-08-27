#!/usr/bin/env bash
# Build linux/amd64 and/or linux/arm64 images.
#
# Local load (one platform, current machine):
#   ./scripts/docker-build.sh
#   ./scripts/docker-build.sh amd64
#   ./scripts/docker-build.sh arm64
#
# Multi-arch push to a registry:
#   IMAGE=ghcr.io/you/ipv6-proxy:latest PUSH=1 ./scripts/docker-build.sh both
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

IMAGE="${IMAGE:-ipv6-proxy:local}"
BUILDER="${BUILDER:-ipv6-proxy-builder}"
ENGINE_ARCH="$(docker version --format '{{.Server.Arch}}' 2>/dev/null || echo amd64)"
ARCH="${1:-local}"

if ! docker buildx inspect "$BUILDER" >/dev/null 2>&1; then
  docker buildx create --name "$BUILDER" --driver docker-container --use
  docker buildx inspect --bootstrap
else
  docker buildx use "$BUILDER"
fi

case "$ARCH" in
  local)
    if [ "$ENGINE_ARCH" = "arm64" ]; then
      docker buildx bake --set "image.tags=${IMAGE}" local-arm64
    else
      docker buildx bake --set "image.tags=${IMAGE}" local-amd64
    fi
    ;;
  amd64|x86|x86_64)
    docker buildx bake --set "image.tags=${IMAGE}" local-amd64
    ;;
  arm64|arm|aarch64)
    docker buildx bake --set "image.tags=${IMAGE}" local-arm64
    ;;
  both|multi)
    if [ "${PUSH:-0}" != "1" ]; then
      echo "multi-arch images cannot --load into docker; set PUSH=1 and IMAGE=registry/name:tag" >&2
      exit 1
    fi
    docker buildx bake --set "image.tags=${IMAGE}" --push image
    ;;
  *)
    echo "usage: $0 [local|amd64|arm64|both]" >&2
    exit 1
    ;;
esac

echo "built ${IMAGE}"
