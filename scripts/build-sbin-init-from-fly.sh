#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Build Fly.io init-snapshot binary and place it into artifacts for mergen-converter.

Usage:
  build-sbin-init-from-fly.sh [options]

Options:
  --repo URL            Git repo URL (default: https://github.com/superfly/init-snapshot)
  --ref REF             Git ref (branch/tag/commit). Default: repository default branch
  --target TARGET       Rust target triple (default: x86_64-unknown-linux-musl)
  --output-dir PATH     Output directory (default: <repo>/artifacts/sbin-init)
  --output-name NAME    Output binary name (default: sbin-init)
  --skip-rustup-target  Do not run `rustup target add <target>`
  --keep-workdir        Keep temporary working directory for debugging
  --help                Show this help

Examples:
  scripts/build-sbin-init-from-fly.sh
  scripts/build-sbin-init-from-fly.sh --ref main
  scripts/build-sbin-init-from-fly.sh --target x86_64-unknown-linux-gnu
EOF
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

REPO_URL="https://github.com/superfly/init-snapshot"
REF=""
TARGET="x86_64-unknown-linux-musl"
OUTPUT_DIR="${REPO_ROOT}/artifacts/sbin-init"
OUTPUT_NAME="sbin-init"
SKIP_RUSTUP_TARGET=0
KEEP_WORKDIR=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      REPO_URL="${2:-}"
      shift 2
      ;;
    --ref)
      REF="${2:-}"
      shift 2
      ;;
    --target)
      TARGET="${2:-}"
      shift 2
      ;;
    --output-dir)
      OUTPUT_DIR="${2:-}"
      shift 2
      ;;
    --output-name)
      OUTPUT_NAME="${2:-}"
      shift 2
      ;;
    --skip-rustup-target)
      SKIP_RUSTUP_TARGET=1
      shift
      ;;
    --keep-workdir)
      KEEP_WORKDIR=1
      shift
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

if [[ -z "${REPO_URL}" ]]; then
  echo "--repo cannot be empty" >&2
  exit 1
fi
if [[ -z "${OUTPUT_DIR}" ]]; then
  echo "--output-dir cannot be empty" >&2
  exit 1
fi
if [[ -z "${OUTPUT_NAME}" ]]; then
  echo "--output-name cannot be empty" >&2
  exit 1
fi

require_cmd git
require_cmd cargo
require_cmd cp
require_cmd chmod
require_cmd mktemp

if [[ -n "${TARGET}" && "${SKIP_RUSTUP_TARGET}" -ne 1 ]]; then
  if command -v rustup >/dev/null 2>&1; then
    echo "ensuring rust target: ${TARGET}"
    rustup target add "${TARGET}"
  else
    echo "warning: rustup not found, skipping rust target install step" >&2
  fi
fi

WORK_DIR="$(mktemp -d)"
cleanup() {
  if [[ "${KEEP_WORKDIR}" -eq 1 ]]; then
    echo "kept workdir: ${WORK_DIR}"
    return
  fi
  rm -rf "${WORK_DIR}"
}
trap cleanup EXIT

SOURCE_DIR="${WORK_DIR}/init-snapshot"

echo "cloning: ${REPO_URL}"
git clone "${REPO_URL}" "${SOURCE_DIR}" >/dev/null

pushd "${SOURCE_DIR}" >/dev/null
if [[ -n "${REF}" ]]; then
  echo "checking out ref: ${REF}"
  git checkout "${REF}" >/dev/null
fi

if [[ -n "${TARGET}" ]]; then
  echo "building release binary for target: ${TARGET}"
  cargo build --release --target "${TARGET}"
  CANDIDATE="target/${TARGET}/release/init"
else
  echo "building release binary for default target"
  cargo build --release
  CANDIDATE="target/release/init"
fi

if [[ ! -f "${CANDIDATE}" ]]; then
  echo "expected init binary not found at ${CANDIDATE}" >&2
  echo "available release binaries:" >&2
  find target -maxdepth 4 -type f -perm -u+x 2>/dev/null | sed 's/^/  - /' >&2 || true
  exit 1
fi

COMMIT_SHA="$(git rev-parse HEAD)"
popd >/dev/null

mkdir -p "${OUTPUT_DIR}"
OUTPUT_PATH="${OUTPUT_DIR}/${OUTPUT_NAME}"
cp "${SOURCE_DIR}/${CANDIDATE}" "${OUTPUT_PATH}"
chmod +x "${OUTPUT_PATH}"

cat > "${OUTPUT_DIR}/build-info.txt" <<EOF
repo=${REPO_URL}
ref=${REF:-default}
commit=${COMMIT_SHA}
target=${TARGET:-default}
built_at=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
binary=${OUTPUT_PATH}
EOF

echo
echo "build completed"
echo "  binary: ${OUTPUT_PATH}"
echo "  commit: ${COMMIT_SHA}"
echo "  info:   ${OUTPUT_DIR}/build-info.txt"
