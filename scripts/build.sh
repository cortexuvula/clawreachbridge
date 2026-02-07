#!/usr/bin/env bash
set -euo pipefail

# Cross-compilation build script for ClawReach Bridge
# Usage: ./scripts/build.sh [version]

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
BUILD_TIME="$(date -u '+%Y-%m-%d_%H:%M:%S')"
GIT_COMMIT="$(git rev-parse HEAD 2>/dev/null || echo "unknown")"
LDFLAGS="-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT}"

DIST_DIR="dist"
mkdir -p "${DIST_DIR}"

PLATFORMS=(
    "linux/amd64"
    "linux/arm64"
    "linux/arm/7"
    "darwin/amd64"
    "darwin/arm64"
)

echo "Building ClawReach Bridge ${VERSION}"
echo "  Build time: ${BUILD_TIME}"
echo "  Git commit: ${GIT_COMMIT}"
echo ""

for platform in "${PLATFORMS[@]}"; do
    IFS='/' read -r GOOS GOARCH GOARM_VAL <<< "${platform}"
    output="clawreachbridge-${GOOS}-${GOARCH}"
    if [ -n "${GOARM_VAL:-}" ]; then
        output="clawreachbridge-${GOOS}-armv${GOARM_VAL}"
    fi

    echo "Building ${output}..."
    env GOOS="${GOOS}" GOARCH="${GOARCH}" GOARM="${GOARM_VAL:-}" \
        go build -ldflags="${LDFLAGS}" -o "${DIST_DIR}/${output}" ./cmd/clawreachbridge
done

echo ""
echo "Generating checksums..."
(cd "${DIST_DIR}" && sha256sum * > checksums.txt)

echo ""
echo "Build complete. Artifacts in ${DIST_DIR}/:"
ls -lh "${DIST_DIR}/"
