#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'HELP'
Build Go-based mergen init-snapshot binary and place it into artifacts for mergen-converter.

Usage:
  build-sbin-init-from-go.sh [options]

Options:
  --output-dir PATH     Output directory (default: <repo>/artifacts/sbin-init)
  --output-name NAME    Output binary name (default: sbin-init)
  --goos OS             Target GOOS (default: linux)
  --goarch ARCH         Target GOARCH (default: amd64)
  --cgo-enabled 0|1     CGO_ENABLED value (default: 0)
  --ldflags STR         Additional linker flags
  --help                Show this help

Examples:
  scripts/build-sbin-init-from-go.sh
  scripts/build-sbin-init-from-go.sh --goarch arm64
  scripts/build-sbin-init-from-go.sh --output-dir /tmp/sbin-init --output-name init
HELP
}

require_cmd() {
  local cmd="${1}"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "required command not found: ${cmd}" >&2
    exit 1
  fi
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

OUTPUT_DIR="${REPO_ROOT}/artifacts/sbin-init"
OUTPUT_NAME="sbin-init"
TARGET_GOOS="linux"
TARGET_GOARCH="amd64"
CGO_VALUE="0"
USER_LDFLAGS=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-dir)
      OUTPUT_DIR="${2:-}"
      shift 2
      ;;
    --output-name)
      OUTPUT_NAME="${2:-}"
      shift 2
      ;;
    --goos)
      TARGET_GOOS="${2:-}"
      shift 2
      ;;
    --goarch)
      TARGET_GOARCH="${2:-}"
      shift 2
      ;;
    --cgo-enabled)
      CGO_VALUE="${2:-}"
      shift 2
      ;;
    --ldflags)
      USER_LDFLAGS="${2:-}"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ -z "${OUTPUT_DIR}" ]]; then
  echo "--output-dir cannot be empty" >&2
  exit 1
fi
if [[ -z "${OUTPUT_NAME}" ]]; then
  echo "--output-name cannot be empty" >&2
  exit 1
fi
if [[ -z "${TARGET_GOOS}" || -z "${TARGET_GOARCH}" ]]; then
  echo "--goos and --goarch cannot be empty" >&2
  exit 1
fi
if [[ "${CGO_VALUE}" != "0" && "${CGO_VALUE}" != "1" ]]; then
  echo "--cgo-enabled must be 0 or 1" >&2
  exit 1
fi

require_cmd go
require_cmd chmod
require_cmd mkdir

mkdir -p "${OUTPUT_DIR}"
OUTPUT_PATH="${OUTPUT_DIR}/${OUTPUT_NAME}"

BASE_LDFLAGS="-s -w"
if [[ -n "${USER_LDFLAGS}" ]]; then
  LDFLAGS_COMBINED="${BASE_LDFLAGS} ${USER_LDFLAGS}"
else
  LDFLAGS_COMBINED="${BASE_LDFLAGS}"
fi

echo "building cmd/mergen-init-snapshot"
echo "  target: ${TARGET_GOOS}/${TARGET_GOARCH}"
echo "  cgo:    ${CGO_VALUE}"
echo "  output: ${OUTPUT_PATH}"

(
  cd "${REPO_ROOT}"
  CGO_ENABLED="${CGO_VALUE}" GOOS="${TARGET_GOOS}" GOARCH="${TARGET_GOARCH}" \
    go build -trimpath -ldflags "${LDFLAGS_COMBINED}" -o "${OUTPUT_PATH}" ./cmd/mergen-init-snapshot
)

chmod +x "${OUTPUT_PATH}"

GIT_SHA="unknown"
if command -v git >/dev/null 2>&1; then
  GIT_SHA="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo unknown)"
fi

cat > "${OUTPUT_DIR}/build-info.txt" <<INFO
source=cmd/mergen-init-snapshot
binary=${OUTPUT_PATH}
goos=${TARGET_GOOS}
goarch=${TARGET_GOARCH}
cgo_enabled=${CGO_VALUE}
ldflags=${LDFLAGS_COMBINED}
git_commit=${GIT_SHA}
built_at=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
INFO

echo
echo "build completed"
echo "  binary: ${OUTPUT_PATH}"
echo "  info:   ${OUTPUT_DIR}/build-info.txt"
