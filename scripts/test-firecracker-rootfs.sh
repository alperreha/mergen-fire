#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Standalone Firecracker rootfs smoke test (outside mergend).

This script:
  1) Creates a test netns + tap
  2) Starts Firecracker inside that netns
  3) Configures VM via Firecracker API socket
  4) Starts VM and opens an interactive shell inside netns
  5) Cleans up netns/tap/process on exit

Usage:
  test-firecracker-rootfs.sh --kernel /path/vmlinux --rootfs /path/rootfs.ext4 [options]
  test-firecracker-rootfs.sh --vm-json /path/vm.json [options]

Required:
  --kernel PATH                  Kernel image path (unless --vm-json provides it)
  --rootfs PATH                  Rootfs image path (unless --vm-json provides it)

Alternative:
  --vm-json PATH                 Read defaults from vm.json (boot-source, drives, machine-config, guest_mac)

Network options:
  --guest-ip IP                  Guest IP (default: 172.30.0.2)
  --host-ip IP                   Host(tap) IP in netns (default: 172.30.0.1)
  --host-prefix N                Prefix length for host ip (default: 24)
  --guest-netmask MASK           Netmask for kernel ip= boot arg (default: 255.255.255.0)
  --netns-name NAME              Explicit netns name (default auto)
  --tap-name NAME                Explicit tap name (default auto)

VM options:
  --vcpu N                       vCPU count (default: 1)
  --mem-mib N                    Memory MiB (default: 512)
  --boot-args STR                Boot args (default augmented with ip=... and init=/init)
  --guest-mac MAC                Guest MAC (default: 02:FC:00:00:00:02)

Runtime options:
  --no-shell                     Do not open interactive netns shell after start
  --keep-run-dir                 Keep temp run dir/logs after exit
  --help                         Show this help

Examples:
  sudo scripts/test-firecracker-rootfs.sh \
    --kernel /var/lib/mergen/base/vmlinux \
    --rootfs /var/lib/mergen/base/nginx/rootfs.ext4

  sudo scripts/test-firecracker-rootfs.sh \
    --vm-json /etc/mergen/vm.d/<vm-id>/vm.json \
    --guest-ip 172.30.0.2 --host-ip 172.30.0.1
EOF
}

require_cmd() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "required command not found: ${cmd}" >&2
    exit 1
  fi
}

random_hex() {
  tr -dc 'a-f0-9' </dev/urandom | head -c "${1}"
}

VM_JSON=""
KERNEL=""
ROOTFS=""
VCPU=""
MEM_MIB=""
BOOT_ARGS=""
GUEST_MAC=""
GUEST_IP="172.30.0.2"
HOST_IP="172.30.0.1"
HOST_PREFIX="24"
GUEST_NETMASK="255.255.255.0"
NETNS_NAME=""
TAP_NAME=""
NO_SHELL=0
KEEP_RUN_DIR=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --vm-json) VM_JSON="${2:-}"; shift 2 ;;
    --kernel) KERNEL="${2:-}"; shift 2 ;;
    --rootfs) ROOTFS="${2:-}"; shift 2 ;;
    --vcpu) VCPU="${2:-}"; shift 2 ;;
    --mem-mib) MEM_MIB="${2:-}"; shift 2 ;;
    --boot-args) BOOT_ARGS="${2:-}"; shift 2 ;;
    --guest-mac) GUEST_MAC="${2:-}"; shift 2 ;;
    --guest-ip) GUEST_IP="${2:-}"; shift 2 ;;
    --host-ip) HOST_IP="${2:-}"; shift 2 ;;
    --host-prefix) HOST_PREFIX="${2:-}"; shift 2 ;;
    --guest-netmask) GUEST_NETMASK="${2:-}"; shift 2 ;;
    --netns-name) NETNS_NAME="${2:-}"; shift 2 ;;
    --tap-name) TAP_NAME="${2:-}"; shift 2 ;;
    --no-shell) NO_SHELL=1; shift ;;
    --keep-run-dir) KEEP_RUN_DIR=1; shift ;;
    --help|-h) usage; exit 0 ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ "$(id -u)" -ne 0 ]]; then
  echo "run as root (required for netns/tap setup)" >&2
  exit 1
fi

require_cmd firecracker
require_cmd ip
require_cmd curl
require_cmd jq

if [[ -n "${VM_JSON}" ]]; then
  if [[ ! -f "${VM_JSON}" ]]; then
    echo "vm json not found: ${VM_JSON}" >&2
    exit 1
  fi
  if [[ -z "${KERNEL}" ]]; then
    KERNEL="$(jq -r '.["boot-source"].kernel_image_path // empty' "${VM_JSON}")"
  fi
  if [[ -z "${ROOTFS}" ]]; then
    ROOTFS="$(jq -r '([.drives[] | select(.is_root_device == true)][0].path_on_host // .drives[0].path_on_host // empty)' "${VM_JSON}")"
  fi
  if [[ -z "${VCPU}" ]]; then
    VCPU="$(jq -r '.["machine-config"].vcpu_count // empty' "${VM_JSON}")"
  fi
  if [[ -z "${MEM_MIB}" ]]; then
    MEM_MIB="$(jq -r '.["machine-config"].mem_size_mib // empty' "${VM_JSON}")"
  fi
  if [[ -z "${BOOT_ARGS}" ]]; then
    BOOT_ARGS="$(jq -r '.["boot-source"].boot_args // empty' "${VM_JSON}")"
  fi
  if [[ -z "${GUEST_MAC}" ]]; then
    GUEST_MAC="$(jq -r '.["network-interfaces"][0].guest_mac // empty' "${VM_JSON}")"
  fi
fi

if [[ -z "${KERNEL}" || -z "${ROOTFS}" ]]; then
  echo "--kernel and --rootfs are required (or provide --vm-json with those fields)" >&2
  exit 1
fi
if [[ ! -f "${KERNEL}" ]]; then
  echo "kernel file not found: ${KERNEL}" >&2
  exit 1
fi
if [[ ! -f "${ROOTFS}" ]]; then
  echo "rootfs file not found: ${ROOTFS}" >&2
  exit 1
fi

if [[ -z "${VCPU}" ]]; then
  VCPU="1"
fi
if [[ -z "${MEM_MIB}" ]]; then
  MEM_MIB="512"
fi
if [[ -z "${GUEST_MAC}" ]]; then
  GUEST_MAC="02:FC:00:00:00:02"
fi
if ! [[ "${VCPU}" =~ ^[0-9]+$ ]] || [[ "${VCPU}" -le 0 ]]; then
  echo "invalid --vcpu: ${VCPU}" >&2
  exit 1
fi
if ! [[ "${MEM_MIB}" =~ ^[0-9]+$ ]] || [[ "${MEM_MIB}" -lt 128 ]]; then
  echo "invalid --mem-mib: ${MEM_MIB}" >&2
  exit 1
fi
if ! [[ "${HOST_PREFIX}" =~ ^[0-9]+$ ]] || [[ "${HOST_PREFIX}" -lt 1 ]] || [[ "${HOST_PREFIX}" -gt 30 ]]; then
  echo "invalid --host-prefix: ${HOST_PREFIX}" >&2
  exit 1
fi

if [[ -z "${BOOT_ARGS}" ]]; then
  BOOT_ARGS="console=ttyS0 reboot=k panic=1 pci=off"
fi
if [[ "${BOOT_ARGS}" != *"ip="* ]]; then
  BOOT_ARGS="${BOOT_ARGS} ip=${GUEST_IP}::${HOST_IP}:${GUEST_NETMASK}::eth0:off"
fi
if [[ "${BOOT_ARGS}" != *"init="* ]]; then
  BOOT_ARGS="${BOOT_ARGS} init=/init"
fi
BOOT_ARGS="$(echo "${BOOT_ARGS}" | awk '{$1=$1; print}')"

if [[ -z "${NETNS_NAME}" ]]; then
  NETNS_NAME="mgnfc-$(random_hex 6)"
fi
if [[ -z "${TAP_NAME}" ]]; then
  TAP_NAME="mgt$(random_hex 6)"
fi
if [[ ${#TAP_NAME} -gt 15 ]]; then
  echo "tap name must be <= 15 chars: ${TAP_NAME}" >&2
  exit 1
fi

RUN_DIR="$(mktemp -d /tmp/mergen-fc-test.XXXXXX)"
API_SOCK="${RUN_DIR}/firecracker.socket"
FC_STDOUT="${RUN_DIR}/firecracker.stdout.log"
FC_PID=""

cleanup() {
  set +e
  if [[ -n "${FC_PID}" ]] && kill -0 "${FC_PID}" >/dev/null 2>&1; then
    kill "${FC_PID}" >/dev/null 2>&1 || true
    wait "${FC_PID}" >/dev/null 2>&1 || true
  fi
  if ip netns list | awk '{print $1}' | grep -Fxq "${NETNS_NAME}"; then
    ip netns delete "${NETNS_NAME}" >/dev/null 2>&1 || true
  fi
  if [[ "${KEEP_RUN_DIR}" -eq 1 ]]; then
    echo "run dir kept: ${RUN_DIR}"
  else
    rm -rf "${RUN_DIR}"
  fi
}
trap cleanup EXIT

api_call() {
  local method="$1"
  local path="$2"
  local payload="$3"
  local out_file="${RUN_DIR}/api-$(echo "${path}" | tr '/:' '__').out"
  local status

  status="$(curl -sS \
    --unix-socket "${API_SOCK}" \
    -o "${out_file}" \
    -w "%{http_code}" \
    -X "${method}" \
    "http://localhost${path}" \
    -H "Accept: application/json" \
    -H "Content-Type: application/json" \
    -d "${payload}")"

  if [[ "${status}" -lt 200 || "${status}" -ge 300 ]]; then
    echo "firecracker api failed: ${method} ${path} status=${status}" >&2
    echo "response:" >&2
    cat "${out_file}" >&2 || true
    echo "firecracker stdout (tail):" >&2
    tail -n 120 "${FC_STDOUT}" >&2 || true
    exit 1
  fi
}

echo "creating netns=${NETNS_NAME} tap=${TAP_NAME}"
ip netns add "${NETNS_NAME}"
ip -n "${NETNS_NAME}" link set lo up
ip tuntap add dev "${TAP_NAME}" mode tap
ip link set "${TAP_NAME}" netns "${NETNS_NAME}"
ip -n "${NETNS_NAME}" addr add "${HOST_IP}/${HOST_PREFIX}" dev "${TAP_NAME}"
ip -n "${NETNS_NAME}" link set "${TAP_NAME}" up

echo "starting firecracker"
ip netns exec "${NETNS_NAME}" firecracker --api-sock "${API_SOCK}" >"${FC_STDOUT}" 2>&1 &
FC_PID="$!"

for _ in $(seq 1 100); do
  if [[ -S "${API_SOCK}" ]]; then
    break
  fi
  sleep 0.1
done
if [[ ! -S "${API_SOCK}" ]]; then
  echo "firecracker socket not ready: ${API_SOCK}" >&2
  tail -n 120 "${FC_STDOUT}" >&2 || true
  exit 1
fi

echo "configuring vm via api"
MACHINE_PAYLOAD="$(jq -nc --argjson vcpu "${VCPU}" --argjson mem "${MEM_MIB}" '{vcpu_count:$vcpu, mem_size_mib:$mem, smt:false}')"
BOOT_PAYLOAD="$(jq -nc --arg kernel "${KERNEL}" --arg boot "${BOOT_ARGS}" '{kernel_image_path:$kernel, boot_args:$boot}')"
DRIVE_PAYLOAD="$(jq -nc --arg rootfs "${ROOTFS}" '{drive_id:"rootfs", path_on_host:$rootfs, is_root_device:true, is_read_only:false}')"
NET_PAYLOAD="$(jq -nc --arg tap "${TAP_NAME}" --arg mac "${GUEST_MAC}" '{iface_id:"eth0", host_dev_name:$tap, guest_mac:$mac}')"
START_PAYLOAD='{"action_type":"InstanceStart"}'

api_call PUT "/machine-config" "${MACHINE_PAYLOAD}"
api_call PUT "/boot-source" "${BOOT_PAYLOAD}"
api_call PUT "/drives/rootfs" "${DRIVE_PAYLOAD}"
api_call PUT "/network-interfaces/eth0" "${NET_PAYLOAD}"
api_call PUT "/actions" "${START_PAYLOAD}"

echo
echo "vm started"
echo "  netns:          ${NETNS_NAME}"
echo "  tap:            ${TAP_NAME}"
echo "  host ip:        ${HOST_IP}/${HOST_PREFIX}"
echo "  guest ip:       ${GUEST_IP}"
echo "  api socket:     ${API_SOCK}"
echo "  firecracker pid:${FC_PID}"
echo "  firecracker log:${FC_STDOUT}"
echo
echo "quick checks:"
echo "  ip netns exec ${NETNS_NAME} ip -br addr"
echo "  ip netns exec ${NETNS_NAME} ip route"
echo "  ip netns exec ${NETNS_NAME} nc -vz -w2 ${GUEST_IP} 22"
echo "  ip netns exec ${NETNS_NAME} ssh root@${GUEST_IP}"
echo

if [[ "${NO_SHELL}" -eq 1 ]]; then
  echo "--no-shell set, waiting (Ctrl+C to stop and cleanup)"
  wait "${FC_PID}"
else
  echo "opening interactive shell in netns ${NETNS_NAME} (exit to cleanup)"
  ip netns exec "${NETNS_NAME}" bash || ip netns exec "${NETNS_NAME}" sh
fi
