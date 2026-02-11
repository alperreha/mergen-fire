#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Build latest stable Linux kernel (vmlinux) and run converted rootfs directly on Firecracker.

This script:
  1) Finds latest stable Linux version from kernel.org
  2) Downloads + builds vmlinux
  3) Stores kernel in:
     - ./artifacts/kernels/linux-<version>/vmlinux
     - /var/lib/mergen/base/vmlinux
  4) Finds rootfs.ext4 (artifacts or /var/lib/mergen/base)
  5) Runs scripts/test-firecracker-rootfs.sh (without mergend)

Usage:
  sudo scripts/smoke-test-converted-rootfs.sh [options] [-- <extra args for test-firecracker-rootfs.sh>]

Options:
  --rootfs PATH            Rootfs path. If omitted, auto-detects newest rootfs.ext4.
  --kernel PATH            Kernel path. If omitted, builds latest stable kernel unless --skip-kernel-build.
  --kernel-version VER     Override kernel version (default: latest stable from kernel.org).
  --skip-kernel-build      Skip kernel build and use --kernel or /var/lib/mergen/base/vmlinux.
  --jobs N                 Build parallelism for make (default: nproc or 4).
  --keep-workdir           Keep temporary kernel build directory.
  --boot-args STR          Boot args passed to Firecracker test script
                           (default includes init=/sbin/init).
  --no-shell               Pass through to test-firecracker-rootfs.sh.
  --keep-run-dir           Pass through to test-firecracker-rootfs.sh.
  --help                   Show this help.

Examples:
  sudo scripts/smoke-test-converted-rootfs.sh
  sudo scripts/smoke-test-converted-rootfs.sh --rootfs ./artifacts/converter/nginx/rootfs.ext4
  sudo scripts/smoke-test-converted-rootfs.sh --skip-kernel-build --kernel /var/lib/mergen/base/vmlinux
  sudo scripts/smoke-test-converted-rootfs.sh -- --guest-ip 172.30.0.2 --host-ip 172.30.0.1
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
TEST_SCRIPT="${REPO_ROOT}/scripts/test-firecracker-rootfs.sh"

ROOTFS=""
KERNEL=""
KERNEL_VERSION=""
SKIP_KERNEL_BUILD=0
JOBS=""
KEEP_WORKDIR=0
BOOT_ARGS="console=ttyS0 reboot=k panic=1 pci=off init=/sbin/init"
NO_SHELL=0
KEEP_RUN_DIR=0
EXTRA_ARGS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --rootfs) ROOTFS="${2:-}"; shift 2 ;;
    --kernel) KERNEL="${2:-}"; shift 2 ;;
    --kernel-version) KERNEL_VERSION="${2:-}"; shift 2 ;;
    --skip-kernel-build) SKIP_KERNEL_BUILD=1; shift ;;
    --jobs) JOBS="${2:-}"; shift 2 ;;
    --keep-workdir) KEEP_WORKDIR=1; shift ;;
    --boot-args) BOOT_ARGS="${2:-}"; shift 2 ;;
    --no-shell) NO_SHELL=1; shift ;;
    --keep-run-dir) KEEP_RUN_DIR=1; shift ;;
    --help|-h) usage; exit 0 ;;
    --)
      shift
      EXTRA_ARGS=("$@")
      break
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ "$(id -u)" -ne 0 ]]; then
  echo "run as root (required for firecracker smoke test)" >&2
  exit 1
fi

if [[ ! -x "${TEST_SCRIPT}" ]]; then
  echo "required script not found/executable: ${TEST_SCRIPT}" >&2
  exit 1
fi

if [[ -z "${JOBS}" ]]; then
  if command -v nproc >/dev/null 2>&1; then
    JOBS="$(nproc)"
  else
    JOBS="4"
  fi
fi
if ! [[ "${JOBS}" =~ ^[0-9]+$ ]] || [[ "${JOBS}" -le 0 ]]; then
  echo "invalid --jobs value: ${JOBS}" >&2
  exit 1
fi

if [[ "${SKIP_KERNEL_BUILD}" -eq 0 ]]; then
  require_cmd curl
  require_cmd jq
  require_cmd tar
  require_cmd xz
  require_cmd make
  require_cmd gcc
  require_cmd ld
  require_cmd bc
  require_cmd bison
  require_cmd flex
fi

find_latest_rootfs() {
  local best=""
  local best_mtime=0
  local candidate
  local mtime

  while IFS= read -r candidate; do
    [[ -f "${candidate}" ]] || continue
    mtime="$(stat -c %Y "${candidate}" 2>/dev/null || stat -f %m "${candidate}" 2>/dev/null || echo 0)"
    if [[ "${mtime}" -gt "${best_mtime}" ]]; then
      best_mtime="${mtime}"
      best="${candidate}"
    fi
  done < <(
    {
      find "${REPO_ROOT}/artifacts/converter" -type f -name 'rootfs.ext4' 2>/dev/null || true
      find "/var/lib/mergen/base" -type f -name 'rootfs.ext4' 2>/dev/null || true
      if [[ -f "/var/lib/mergen/base/rootfs.ext4" ]]; then
        echo "/var/lib/mergen/base/rootfs.ext4"
      fi
    } | awk '!seen[$0]++'
  )

  echo "${best}"
}

latest_stable_version() {
  local releases_json
  releases_json="$(curl -fsSL https://www.kernel.org/releases.json)"

  local version
  version="$(echo "${releases_json}" | jq -r '.latest_stable.version // empty')"
  if [[ -n "${version}" && "${version}" != "null" ]]; then
    echo "${version}"
    return 0
  fi

  version="$(echo "${releases_json}" | jq -r '.latest_stable // empty')"
  if [[ -n "${version}" && "${version}" != "null" ]]; then
    echo "${version}"
    return 0
  fi

  version="$(echo "${releases_json}" | jq -r '[.releases[] | select(.moniker=="stable")][0].version // empty')"
  if [[ -n "${version}" && "${version}" != "null" ]]; then
    echo "${version}"
    return 0
  fi

  echo "failed to resolve latest stable kernel version from kernel.org" >&2
  exit 1
}

build_kernel() {
  local version="$1"

  local major="${version%%.*}"
  local tar_name="linux-${version}.tar.xz"
  local kernel_url="https://cdn.kernel.org/pub/linux/kernel/v${major}.x/${tar_name}"
  local kernel_cache_dir="${REPO_ROOT}/artifacts/kernels/src"
  local kernel_out_dir="${REPO_ROOT}/artifacts/kernels/linux-${version}"
  local tar_path="${kernel_cache_dir}/${tar_name}"
  local work_dir
  local src_dir

  mkdir -p "${kernel_cache_dir}" "${kernel_out_dir}" "/var/lib/mergen/base"

  if [[ ! -f "${tar_path}" ]]; then
    echo "downloading linux kernel source: ${kernel_url}"
    curl -fL "${kernel_url}" -o "${tar_path}"
  else
    echo "using cached kernel source tarball: ${tar_path}"
  fi

  work_dir="$(mktemp -d /tmp/mergen-kernel-build.XXXXXX)"
  if [[ "${KEEP_WORKDIR}" -eq 1 ]]; then
    echo "kernel build workdir kept: ${work_dir}"
  fi

  tar -C "${work_dir}" -xf "${tar_path}"
  src_dir="${work_dir}/linux-${version}"

  echo "configuring kernel linux-${version}"
  pushd "${src_dir}" >/dev/null
  make mrproper
  make defconfig

  if [[ -x "./scripts/config" ]]; then
    ./scripts/config --enable CONFIG_VIRTIO
    ./scripts/config --enable CONFIG_VIRTIO_MMIO
    ./scripts/config --enable CONFIG_VIRTIO_MMIO_CMDLINE_DEVICES
    ./scripts/config --enable CONFIG_VIRTIO_BLK
    ./scripts/config --enable CONFIG_VIRTIO_NET
    ./scripts/config --enable CONFIG_SERIAL_8250
    ./scripts/config --enable CONFIG_SERIAL_8250_CONSOLE
    ./scripts/config --enable CONFIG_BLK_DEV
    ./scripts/config --enable CONFIG_EXT4_FS
    ./scripts/config --enable CONFIG_DEVTMPFS
    ./scripts/config --enable CONFIG_DEVTMPFS_MOUNT
    ./scripts/config --enable CONFIG_PROC_FS
    ./scripts/config --enable CONFIG_SYSFS
    ./scripts/config --enable CONFIG_TMPFS
  fi

  make olddefconfig
  echo "building vmlinux (jobs=${JOBS})"
  make -j"${JOBS}" vmlinux
  popd >/dev/null

  local artifact_kernel="${kernel_out_dir}/vmlinux"
  cp "${src_dir}/vmlinux" "${artifact_kernel}"
  install -D -m 0644 "${artifact_kernel}" "/var/lib/mergen/base/vmlinux"

  cat > "${kernel_out_dir}/build-info.txt" <<EOF
version=${version}
source_tar=${tar_path}
kernel_url=${kernel_url}
built_at=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
jobs=${JOBS}
artifact_kernel=${artifact_kernel}
varlib_kernel=/var/lib/mergen/base/vmlinux
EOF

  if [[ "${KEEP_WORKDIR}" -eq 0 ]]; then
    rm -rf "${work_dir}"
  fi

  echo "${artifact_kernel}"
}

if [[ -z "${ROOTFS}" ]]; then
  ROOTFS="$(find_latest_rootfs)"
fi
if [[ -z "${ROOTFS}" ]]; then
  echo "rootfs.ext4 could not be auto-detected. Use --rootfs /path/to/rootfs.ext4" >&2
  exit 1
fi
if [[ ! -f "${ROOTFS}" ]]; then
  echo "rootfs not found: ${ROOTFS}" >&2
  exit 1
fi

if [[ -z "${KERNEL}" ]]; then
  if [[ "${SKIP_KERNEL_BUILD}" -eq 1 ]]; then
    if [[ -f "/var/lib/mergen/base/vmlinux" ]]; then
      KERNEL="/var/lib/mergen/base/vmlinux"
    else
      echo "--skip-kernel-build set but no kernel found at /var/lib/mergen/base/vmlinux (use --kernel)" >&2
      exit 1
    fi
  else
    if [[ -z "${KERNEL_VERSION}" ]]; then
      KERNEL_VERSION="$(latest_stable_version)"
    fi
    echo "using kernel version: ${KERNEL_VERSION}"
    KERNEL="$(build_kernel "${KERNEL_VERSION}")"
  fi
fi

if [[ ! -f "${KERNEL}" ]]; then
  echo "kernel not found: ${KERNEL}" >&2
  exit 1
fi

echo
echo "firecracker smoke test inputs"
echo "  kernel: ${KERNEL}"
echo "  rootfs: ${ROOTFS}"
echo "  boot args: ${BOOT_ARGS}"
echo

CMD=("${TEST_SCRIPT}" "--kernel" "${KERNEL}" "--rootfs" "${ROOTFS}" "--boot-args" "${BOOT_ARGS}")
if [[ "${NO_SHELL}" -eq 1 ]]; then
  CMD+=("--no-shell")
fi
if [[ "${KEEP_RUN_DIR}" -eq 1 ]]; then
  CMD+=("--keep-run-dir")
fi
if [[ ${#EXTRA_ARGS[@]} -gt 0 ]]; then
  CMD+=("${EXTRA_ARGS[@]}")
fi

exec "${CMD[@]}"
