#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'HELP'
Build Go-based mergen init/agent binaries and place them into artifacts for mergen-converter.

Usage:
  build-sbin-init-from-go.sh [options]

Options:
  --output-dir PATH     Output directory (default: <repo>/artifacts/sbin-init)
  --output-name NAME    Output init binary name (default: sbin-init)
  --install-base        Also install binaries into versioned base dir
  --base-dir PATH       Base directory root (default: /var/lib/mergen/base)
  --base-version NAME   Base version directory name (default: auto from git sha + utc time)
  --no-current-link     Do not update <base-dir>/current symlink
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
INSTALL_BASE="0"
BASE_DIR="/var/lib/mergen/base"
BASE_VERSION=""
UPDATE_CURRENT_LINK="1"
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
    --install-base)
      INSTALL_BASE="1"
      shift
      ;;
    --base-dir)
      BASE_DIR="${2:-}"
      shift 2
      ;;
    --base-version)
      BASE_VERSION="${2:-}"
      shift 2
      ;;
    --no-current-link)
      UPDATE_CURRENT_LINK="0"
      shift
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
if [[ "${INSTALL_BASE}" != "0" && "${INSTALL_BASE}" != "1" ]]; then
  echo "--install-base validation failed" >&2
  exit 1
fi

require_cmd go
require_cmd chmod
require_cmd mkdir
require_cmd cp

GO_BIN="$(go env GOROOT)/bin/go"
if [[ ! -x "${GO_BIN}" ]]; then
  GO_BIN="$(command -v go)"
fi

mkdir -p "${OUTPUT_DIR}"
OUTPUT_PATH="${OUTPUT_DIR}/${OUTPUT_NAME}"

BASE_LDFLAGS="-s -w"
if [[ -n "${USER_LDFLAGS}" ]]; then
  LDFLAGS_COMBINED="${BASE_LDFLAGS} ${USER_LDFLAGS}"
else
  LDFLAGS_COMBINED="${BASE_LDFLAGS}"
fi

echo "building cmd/mergen-init (+ agent + vsock-guest)"
echo "  target: ${TARGET_GOOS}/${TARGET_GOARCH}"
echo "  cgo:    ${CGO_VALUE}"
echo "  go:     ${GO_BIN}"
echo "  output dir: ${OUTPUT_DIR}"

build_cmd() {
  local pkg="${1}"
  local out="${2}"
  echo "  - ${pkg} -> ${out}"
  CGO_ENABLED="${CGO_VALUE}" GOOS="${TARGET_GOOS}" GOARCH="${TARGET_GOARCH}" \
    "${GO_BIN}" build -trimpath -ldflags "${LDFLAGS_COMBINED}" -o "${out}" "${pkg}"
}

(
  cd "${REPO_ROOT}"
  build_cmd ./cmd/mergen-init "${OUTPUT_PATH}"
  build_cmd ./cmd/mergen-agent "${OUTPUT_DIR}/mergen-agent"
  build_cmd ./cmd/mergen-vsock-guest "${OUTPUT_DIR}/mergen-vsock-guest"
)

chmod +x "${OUTPUT_PATH}"
chmod +x "${OUTPUT_DIR}/mergen-agent"
chmod +x "${OUTPUT_DIR}/mergen-vsock-guest"

GIT_SHA="unknown"
if command -v git >/dev/null 2>&1; then
  GIT_SHA="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo unknown)"
fi

if [[ -z "${BASE_VERSION}" ]]; then
  BASE_VERSION="${GIT_SHA}-$(date -u +"%Y%m%d%H%M%S")"
fi

cat > "${OUTPUT_DIR}/build-info.txt" <<INFO
source=cmd/mergen-init
binary=${OUTPUT_PATH}
agent_binary=${OUTPUT_DIR}/mergen-agent
vsock_guest_binary=${OUTPUT_DIR}/mergen-vsock-guest
goos=${TARGET_GOOS}
goarch=${TARGET_GOARCH}
cgo_enabled=${CGO_VALUE}
ldflags=${LDFLAGS_COMBINED}
git_commit=${GIT_SHA}
built_at=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
INFO

if [[ "${INSTALL_BASE}" == "1" ]]; then
  BASE_VERSION_DIR="${BASE_DIR}/${BASE_VERSION}"
  BASE_BIN_DIR="${BASE_VERSION_DIR}/bin"
  echo "installing to base dir: ${BASE_VERSION_DIR}"
  mkdir -p "${BASE_BIN_DIR}"
  cp "${OUTPUT_PATH}" "${BASE_BIN_DIR}/sbin-init"
  cp "${OUTPUT_DIR}/mergen-agent" "${BASE_BIN_DIR}/mergen-agent"
  cp "${OUTPUT_DIR}/mergen-vsock-guest" "${BASE_BIN_DIR}/mergen-vsock-guest"
  cp "${OUTPUT_DIR}/build-info.txt" "${BASE_VERSION_DIR}/build-info.txt"
  chmod +x "${BASE_BIN_DIR}/sbin-init" "${BASE_BIN_DIR}/mergen-agent" "${BASE_BIN_DIR}/mergen-vsock-guest"

  if [[ "${UPDATE_CURRENT_LINK}" == "1" ]]; then
    ln -sfn "${BASE_VERSION}" "${BASE_DIR}/current"
  fi
fi

echo
echo "build completed"
echo "  binary: ${OUTPUT_PATH}"
echo "  agent: ${OUTPUT_DIR}/mergen-agent"
echo "  vsock guest: ${OUTPUT_DIR}/mergen-vsock-guest"
echo "  info:   ${OUTPUT_DIR}/build-info.txt"
if [[ "${INSTALL_BASE}" == "1" ]]; then
  echo "  base:   ${BASE_DIR}/${BASE_VERSION}"
  if [[ "${UPDATE_CURRENT_LINK}" == "1" ]]; then
    echo "  current symlink: ${BASE_DIR}/current -> ${BASE_VERSION}"
  fi
fi
