#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'HELP'
Build Go-based mergen guest/base binaries and place them into artifacts for mergen-converter.

Usage:
  build-sbin-init-from-go.sh [options]

Options:
  --output-dir PATH     Output directory (default: <repo>/artifacts/sbin-init)
  --output-name NAME    Output init binary name (default: sbin-init)
  --install-base        Also install binaries into base/current dir
  --base-dir PATH       Base assets directory (default: /var/lib/mergen/base/current)
  --go-bin PATH         Absolute go binary path (overrides PATH lookup)
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
RUNTIME_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
WORKSPACE_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

OUTPUT_DIR="${WORKSPACE_ROOT}/artifacts/sbin-init"
OUTPUT_NAME="sbin-init"
INSTALL_BASE="0"
BASE_DIR="/var/lib/mergen/base/current"
TARGET_GOOS="linux"
TARGET_GOARCH="amd64"
CGO_VALUE="0"
USER_LDFLAGS=""
GO_BIN_OVERRIDE="${GO_BIN:-}"

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
    --goos)
      TARGET_GOOS="${2:-}"
      shift 2
      ;;
    --go-bin)
      GO_BIN_OVERRIDE="${2:-}"
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

require_cmd chmod
require_cmd mkdir
require_cmd cp

resolve_go_bin() {
  local override="${GO_BIN_OVERRIDE:-}"
  if [[ -n "${override}" ]]; then
    if [[ -x "${override}" ]]; then
      echo "${override}"
      return 0
    fi
    if command -v "${override}" >/dev/null 2>&1; then
      command -v "${override}"
      return 0
    fi
    echo "specified --go-bin/GO_BIN not executable: ${override}" >&2
    return 1
  fi

  if command -v go >/dev/null 2>&1; then
    command -v go
    return 0
  fi

  local candidates=(
    "/usr/local/go/bin/go"
    "/usr/lib/go/bin/go"
    "/snap/bin/go"
  )

  if [[ -n "${SUDO_USER:-}" ]]; then
    local sudo_home=""
    if command -v getent >/dev/null 2>&1; then
      sudo_home="$(getent passwd "${SUDO_USER}" 2>/dev/null | cut -d: -f6 || true)"
    fi
    if [[ -z "${sudo_home}" ]]; then
      if [[ -d "/home/${SUDO_USER}" ]]; then
        sudo_home="/home/${SUDO_USER}"
      elif [[ -d "/Users/${SUDO_USER}" ]]; then
        sudo_home="/Users/${SUDO_USER}"
      fi
    fi
    if [[ -n "${sudo_home}" ]]; then
      candidates+=(
        "${sudo_home}/.local/go/bin/go"
        "${sudo_home}/go/bin/go"
      )
    fi
  fi

  local candidate
  for candidate in "${candidates[@]}"; do
    if [[ -x "${candidate}" ]]; then
      echo "${candidate}"
      return 0
    fi
  done

  return 1
}

if ! GO_BIN="$(resolve_go_bin)"; then
  echo "required command not found: go" >&2
  echo "hint: when running with sudo, PATH may not include your user Go installation." >&2
  echo "try one of these:" >&2
  echo "  1) sudo env \"PATH=\$PATH\" ./scripts/build-sbin-init-from-go.sh ..." >&2
  echo "  2) sudo GO_BIN=\"/absolute/path/to/go\" ./scripts/build-sbin-init-from-go.sh ..." >&2
  echo "  3) sudo ./scripts/build-sbin-init-from-go.sh --go-bin /absolute/path/to/go ..." >&2
  exit 1
fi

mkdir -p "${OUTPUT_DIR}"
OUTPUT_PATH="${OUTPUT_DIR}/${OUTPUT_NAME}"
EXTRA_BINARIES=()

BASE_LDFLAGS="-s -w"
if [[ -n "${USER_LDFLAGS}" ]]; then
  LDFLAGS_COMBINED="${BASE_LDFLAGS} ${USER_LDFLAGS}"
else
  LDFLAGS_COMBINED="${BASE_LDFLAGS}"
fi

echo "building guest/base binaries (init + agent)"
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

package_has_go_files() {
  local pkg="${1#./}"
  local dir="${RUNTIME_ROOT}/${pkg}"
  if [[ ! -d "${dir}" ]]; then
    return 1
  fi
  compgen -G "${dir}/*.go" >/dev/null
}

pushd "${RUNTIME_ROOT}" >/dev/null
build_cmd ./cmd/mergen-init "${OUTPUT_PATH}"
build_cmd ./cmd/mergen-agent "${OUTPUT_DIR}/mergen-agent"

if package_has_go_files ./cmd/mergen-supervisor; then
  build_cmd ./cmd/mergen-supervisor "${OUTPUT_DIR}/mergen-supervisor"
  EXTRA_BINARIES+=("mergen-supervisor")
else
  echo "  - skip ./cmd/mergen-supervisor (no Go files)"
fi

if package_has_go_files ./cmd/mergen-telemetry; then
  build_cmd ./cmd/mergen-telemetry "${OUTPUT_DIR}/mergen-telemetry"
  EXTRA_BINARIES+=("mergen-telemetry")
else
  echo "  - skip ./cmd/mergen-telemetry (no Go files)"
fi
popd >/dev/null

chmod +x "${OUTPUT_PATH}"
chmod +x "${OUTPUT_DIR}/mergen-agent"
for extra_bin in "${EXTRA_BINARIES[@]}"; do
  chmod +x "${OUTPUT_DIR}/${extra_bin}"
done

GIT_SHA="unknown"
if command -v git >/dev/null 2>&1; then
  GIT_SHA="$(git -C "${WORKSPACE_ROOT}" rev-parse --short HEAD 2>/dev/null || echo unknown)"
fi

EXTRA_BINARIES_CSV=""
if [[ ${#EXTRA_BINARIES[@]} -gt 0 ]]; then
  EXTRA_BINARIES_CSV="$(IFS=,; echo "${EXTRA_BINARIES[*]}")"
fi

cat > "${OUTPUT_DIR}/build-info.txt" <<INFO
source=runtime/cmd/mergen-init
binary=${OUTPUT_PATH}
agent_binary=${OUTPUT_DIR}/mergen-agent
extra_binaries=${EXTRA_BINARIES_CSV}
goos=${TARGET_GOOS}
goarch=${TARGET_GOARCH}
cgo_enabled=${CGO_VALUE}
ldflags=${LDFLAGS_COMBINED}
git_commit=${GIT_SHA}
built_at=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
INFO

if [[ "${INSTALL_BASE}" == "1" ]]; then
  BASE_VERSION_DIR="${BASE_DIR}"
  BASE_BIN_DIR="${BASE_VERSION_DIR}/bin"
  echo "installing to base dir: ${BASE_VERSION_DIR}"
  mkdir -p "${BASE_BIN_DIR}"
  cp "${OUTPUT_PATH}" "${BASE_BIN_DIR}/sbin-init"
  cp "${OUTPUT_DIR}/mergen-agent" "${BASE_BIN_DIR}/mergen-agent"
  for extra_bin in "${EXTRA_BINARIES[@]}"; do
    cp "${OUTPUT_DIR}/${extra_bin}" "${BASE_BIN_DIR}/${extra_bin}"
  done
  cp "${OUTPUT_DIR}/build-info.txt" "${BASE_VERSION_DIR}/build-info.txt"
  chmod +x "${BASE_BIN_DIR}/sbin-init" "${BASE_BIN_DIR}/mergen-agent"
  for extra_bin in "${EXTRA_BINARIES[@]}"; do
    chmod +x "${BASE_BIN_DIR}/${extra_bin}"
  done
fi

echo
echo "build completed"
echo "  binary: ${OUTPUT_PATH}"
echo "  agent: ${OUTPUT_DIR}/mergen-agent"
if [[ ${#EXTRA_BINARIES[@]} -gt 0 ]]; then
  for extra_bin in "${EXTRA_BINARIES[@]}"; do
    echo "  ${extra_bin}: ${OUTPUT_DIR}/${extra_bin}"
  done
fi
echo "  info:   ${OUTPUT_DIR}/build-info.txt"
if [[ "${INSTALL_BASE}" == "1" ]]; then
  echo "  base:   ${BASE_DIR}"
fi
