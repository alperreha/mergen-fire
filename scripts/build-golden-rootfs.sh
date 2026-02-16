#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'HELP'
Build BusyBox-based golden rootfs (disk0) using Buildroot, then inject mergen binaries.

Usage:
  scripts/build-golden-rootfs.sh [options]

Options:
  --output-dir PATH           Output directory (default: <repo>/artifacts/golden-rootfs)
  --work-dir PATH             Working directory for Buildroot build (default: /tmp/mergen-buildroot)
  --buildroot-dir PATH        Use existing Buildroot source directory (skip download)
  --buildroot-version VER     Buildroot version when downloading (default: 2024.02.2)
  --arch ARCH                 Target arch: amd64|arm64 (default: amd64)
  --jobs N                    Parallel build jobs (default: auto-detect)
  --size-mib N                golden-rootfs.ext4 size in MiB (default: 128)
  --sbin-init PATH            Path to mergen init binary (default: ./artifacts/sbin-init/sbin-init)
  --sbin-telemetry PATH       Path to mergen telemetry binary (default: ./artifacts/sbin-init/mergen-telemetry)
  --sbin-supervisor PATH      Path to mergen supervisor binary (default: ./artifacts/sbin-init/mergen-supervisor)
  --runtime-json PATH         Optional runtime metadata JSON copied to /etc/mergen/mergen.runtime.json
  --keep-work-dir             Keep work directory after build
  --help                      Show this help

Examples:
  scripts/build-golden-rootfs.sh
  scripts/build-golden-rootfs.sh --arch arm64 --size-mib 192
  scripts/build-golden-rootfs.sh --buildroot-dir /opt/buildroot --jobs 8
HELP
}

require_cmd() {
  local cmd="${1}"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "required command not found: ${cmd}" >&2
    exit 1
  fi
}

num_jobs() {
  if command -v nproc >/dev/null 2>&1; then
    nproc
    return
  fi
  if command -v sysctl >/dev/null 2>&1; then
    sysctl -n hw.ncpu 2>/dev/null || echo 4
    return
  fi
  echo 4
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

OUTPUT_DIR="${REPO_ROOT}/artifacts/golden-rootfs"
WORK_DIR="/tmp/mergen-buildroot"
BUILDROOT_DIR=""
BUILDROOT_VERSION="2024.02.2"
TARGET_ARCH="amd64"
JOBS="$(num_jobs)"
SIZE_MIB="128"
SBIN_INIT="${REPO_ROOT}/artifacts/sbin-init/sbin-init"
SBIN_TELEMETRY="${REPO_ROOT}/artifacts/sbin-init/mergen-telemetry"
SBIN_SUPERVISOR="${REPO_ROOT}/artifacts/sbin-init/mergen-supervisor"
RUNTIME_JSON=""
KEEP_WORK_DIR="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-dir)
      OUTPUT_DIR="${2:-}"
      shift 2
      ;;
    --work-dir)
      WORK_DIR="${2:-}"
      shift 2
      ;;
    --buildroot-dir)
      BUILDROOT_DIR="${2:-}"
      shift 2
      ;;
    --buildroot-version)
      BUILDROOT_VERSION="${2:-}"
      shift 2
      ;;
    --arch)
      TARGET_ARCH="${2:-}"
      shift 2
      ;;
    --jobs)
      JOBS="${2:-}"
      shift 2
      ;;
    --size-mib)
      SIZE_MIB="${2:-}"
      shift 2
      ;;
    --sbin-init)
      SBIN_INIT="${2:-}"
      shift 2
      ;;
    --sbin-telemetry)
      SBIN_TELEMETRY="${2:-}"
      shift 2
      ;;
    --sbin-supervisor)
      SBIN_SUPERVISOR="${2:-}"
      shift 2
      ;;
    --runtime-json)
      RUNTIME_JSON="${2:-}"
      shift 2
      ;;
    --keep-work-dir)
      KEEP_WORK_DIR="true"
      shift 1
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

if [[ -z "${OUTPUT_DIR}" || -z "${WORK_DIR}" ]]; then
  echo "--output-dir and --work-dir cannot be empty" >&2
  exit 1
fi

if ! [[ "${SIZE_MIB}" =~ ^[0-9]+$ ]] || [[ "${SIZE_MIB}" -le 0 ]]; then
  echo "--size-mib must be a positive integer" >&2
  exit 1
fi

if ! [[ "${JOBS}" =~ ^[0-9]+$ ]] || [[ "${JOBS}" -le 0 ]]; then
  echo "--jobs must be a positive integer" >&2
  exit 1
fi

ARCH_SYMBOL=""
case "${TARGET_ARCH}" in
  amd64|x86_64)
    TARGET_ARCH="amd64"
    ARCH_SYMBOL="BR2_x86_64=y"
    ;;
  arm64|aarch64)
    TARGET_ARCH="arm64"
    ARCH_SYMBOL="BR2_aarch64=y"
    ;;
  *)
    echo "--arch must be one of: amd64, arm64" >&2
    exit 1
    ;;
esac

for f in "${SBIN_INIT}" "${SBIN_TELEMETRY}" "${SBIN_SUPERVISOR}"; do
  if [[ ! -f "${f}" ]]; then
    echo "required binary not found: ${f}" >&2
    echo "hint: run scripts/build-sbin-init-from-go.sh first" >&2
    exit 1
  fi
done

if [[ -n "${RUNTIME_JSON}" && ! -f "${RUNTIME_JSON}" ]]; then
  echo "--runtime-json file not found: ${RUNTIME_JSON}" >&2
  exit 1
fi

require_cmd bash
require_cmd make
require_cmd tar
require_cmd truncate
require_cmd mkfs.ext4
require_cmd sha256sum
require_cmd chmod
require_cmd mkdir
require_cmd cp
require_cmd rm
require_cmd sed

DOWNLOAD_CMD=""
if [[ -z "${BUILDROOT_DIR}" ]]; then
  if command -v curl >/dev/null 2>&1; then
    DOWNLOAD_CMD="curl -fsSL"
  elif command -v wget >/dev/null 2>&1; then
    DOWNLOAD_CMD="wget -qO-"
  else
    echo "curl or wget is required when --buildroot-dir is not provided" >&2
    exit 1
  fi
fi

mkdir -p "${OUTPUT_DIR}"
mkdir -p "${WORK_DIR}"

if [[ -z "${BUILDROOT_DIR}" ]]; then
  BUILDROOT_ARCHIVE="${WORK_DIR}/buildroot-${BUILDROOT_VERSION}.tar.xz"
  BUILDROOT_SRC="${WORK_DIR}/buildroot-${BUILDROOT_VERSION}"
  BUILDROOT_URL="https://buildroot.org/downloads/buildroot-${BUILDROOT_VERSION}.tar.xz"

  if [[ ! -d "${BUILDROOT_SRC}" ]]; then
    echo "downloading Buildroot ${BUILDROOT_VERSION}..."
    if [[ "${DOWNLOAD_CMD}" == "curl -fsSL" ]]; then
      curl -fsSL "${BUILDROOT_URL}" -o "${BUILDROOT_ARCHIVE}"
    else
      wget -q "${BUILDROOT_URL}" -O "${BUILDROOT_ARCHIVE}"
    fi
    tar -xJf "${BUILDROOT_ARCHIVE}" -C "${WORK_DIR}"
  fi
else
  BUILDROOT_SRC="${BUILDROOT_DIR}"
fi

if [[ ! -f "${BUILDROOT_SRC}/Makefile" ]]; then
  echo "invalid Buildroot source directory: ${BUILDROOT_SRC}" >&2
  exit 1
fi

BUILD_DIR="${WORK_DIR}/output-${TARGET_ARCH}"
OVERLAY_DIR="${WORK_DIR}/overlay-${TARGET_ARCH}"
DEFCONFIG_PATH="${WORK_DIR}/mergen-golden-${TARGET_ARCH}.defconfig"
ROOTFS_DIR="${OUTPUT_DIR}/golden-rootfs"
EXT4_PATH="${OUTPUT_DIR}/golden-rootfs.ext4"
MANIFEST_PATH="${OUTPUT_DIR}/manifest.txt"

if [[ "${KEEP_WORK_DIR}" != "true" ]]; then
  rm -rf "${BUILD_DIR}" "${OVERLAY_DIR}" "${DEFCONFIG_PATH}"
fi

mkdir -p "${OVERLAY_DIR}/sbin"
mkdir -p "${OVERLAY_DIR}/etc/mergen"
mkdir -p "${OVERLAY_DIR}/dev"
mkdir -p "${OVERLAY_DIR}/proc"
mkdir -p "${OVERLAY_DIR}/sys"
mkdir -p "${OVERLAY_DIR}/run"
mkdir -p "${OVERLAY_DIR}/run/lock"
mkdir -p "${OVERLAY_DIR}/tmp"
mkdir -p "${OVERLAY_DIR}/var"
mkdir -p "${OVERLAY_DIR}/mnt/payload"
mkdir -p "${OVERLAY_DIR}/mnt/env"

chmod 1777 "${OVERLAY_DIR}/tmp"

cp "${SBIN_INIT}" "${OVERLAY_DIR}/sbin/init"
cp "${SBIN_INIT}" "${OVERLAY_DIR}/sbin/mergen-init"
cp "${SBIN_TELEMETRY}" "${OVERLAY_DIR}/sbin/mergen-telemetry"
cp "${SBIN_SUPERVISOR}" "${OVERLAY_DIR}/sbin/mergen-supervisor"
chmod 0755 \
  "${OVERLAY_DIR}/sbin/init" \
  "${OVERLAY_DIR}/sbin/mergen-init" \
  "${OVERLAY_DIR}/sbin/mergen-telemetry" \
  "${OVERLAY_DIR}/sbin/mergen-supervisor"

if [[ -n "${RUNTIME_JSON}" ]]; then
  cp "${RUNTIME_JSON}" "${OVERLAY_DIR}/etc/mergen/mergen.runtime.json"
else
  cat > "${OVERLAY_DIR}/etc/mergen/mergen.runtime.json" <<'JSON'
{
  "image": "placeholder",
  "bootArgs": "console=ttyS0 reboot=k panic=1 pci=off random.trust_cpu=on random.trust_bootloader=on init=/sbin/init",
  "startCmd": ["/bin/sh"],
  "payloadDevice": "/dev/vdb",
  "payloadFSType": "ext4",
  "payloadMountPoint": "/mnt/payload",
  "payloadReadOnly": false,
  "envDevice": "/dev/vdc",
  "envFSType": "ext4",
  "envMountPoint": "/mnt/env",
  "envReadOnly": true,
  "envFile": "/mnt/env/mergen.env"
}
JSON
fi

cat > "${DEFCONFIG_PATH}" <<EOF
${ARCH_SYMBOL}
BR2_TOOLCHAIN_BUILDROOT_MUSL=y
BR2_TARGET_GENERIC_HOSTNAME="mergen-golden"
BR2_TARGET_GENERIC_ISSUE="Mergen Golden RootFS"
BR2_TARGET_ENABLE_ROOT_LOGIN=y
BR2_INIT_NONE=y
BR2_TARGET_GENERIC_GETTY=n
BR2_SYSTEM_BIN_SH_BUSYBOX=y
BR2_TARGET_ROOTFS_TAR=y
BR2_ROOTFS_OVERLAY="${OVERLAY_DIR}"
EOF

echo "configuring Buildroot..."
make -C "${BUILDROOT_SRC}" O="${BUILD_DIR}" BR2_DEFCONFIG="${DEFCONFIG_PATH}" defconfig
make -C "${BUILDROOT_SRC}" O="${BUILD_DIR}" olddefconfig

echo "building BusyBox-based rootfs with Buildroot..."
make -C "${BUILDROOT_SRC}" O="${BUILD_DIR}" -j"${JOBS}"

ROOTFS_TAR="${BUILD_DIR}/images/rootfs.tar"
if [[ ! -f "${ROOTFS_TAR}" ]]; then
  echo "Buildroot output missing rootfs tar: ${ROOTFS_TAR}" >&2
  exit 1
fi

rm -rf "${ROOTFS_DIR}"
mkdir -p "${ROOTFS_DIR}"
tar -xf "${ROOTFS_TAR}" -C "${ROOTFS_DIR}"

mkdir -p "${ROOTFS_DIR}/var"
if [[ ! -e "${ROOTFS_DIR}/var/run" ]]; then
  ln -s /run "${ROOTFS_DIR}/var/run"
fi
if [[ ! -e "${ROOTFS_DIR}/var/lock" ]]; then
  ln -s /run/lock "${ROOTFS_DIR}/var/lock"
fi

for required in \
  "${ROOTFS_DIR}/sbin/init" \
  "${ROOTFS_DIR}/sbin/mergen-init" \
  "${ROOTFS_DIR}/sbin/mergen-telemetry" \
  "${ROOTFS_DIR}/sbin/mergen-supervisor" \
  "${ROOTFS_DIR}/etc/mergen/mergen.runtime.json"; do
  if [[ ! -f "${required}" ]]; then
    echo "missing required file in golden rootfs: ${required}" >&2
    exit 1
  fi
done

rm -f "${EXT4_PATH}"
truncate -s "${SIZE_MIB}M" "${EXT4_PATH}"
mkfs.ext4 -q -F -d "${ROOTFS_DIR}" "${EXT4_PATH}"

EXT4_SHA="$(sha256sum "${EXT4_PATH}" | awk '{print $1}')"
cat > "${MANIFEST_PATH}" <<EOF
build_type=golden-rootfs
buildroot_source=${BUILDROOT_SRC}
buildroot_version=${BUILDROOT_VERSION}
target_arch=${TARGET_ARCH}
jobs=${JOBS}
size_mib=${SIZE_MIB}
generated_at=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
sbin_init=${SBIN_INIT}
sbin_telemetry=${SBIN_TELEMETRY}
sbin_supervisor=${SBIN_SUPERVISOR}
runtime_json=${RUNTIME_JSON:-embedded-placeholder}
rootfs_dir=${ROOTFS_DIR}
rootfs_ext4=${EXT4_PATH}
rootfs_ext4_sha256=${EXT4_SHA}
EOF

echo
echo "golden rootfs build completed"
echo "  rootfs dir:   ${ROOTFS_DIR}"
echo "  rootfs ext4:  ${EXT4_PATH}"
echo "  manifest:     ${MANIFEST_PATH}"
echo "  sha256:       ${EXT4_SHA}"
