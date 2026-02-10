#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Build a Firecracker-friendly rootfs from a Docker/OCI image.

Usage:
  build-rootfs-from-dockerhub.sh --image nginx:alpine [--output-dir ./artifacts/rootfs] [--name nginx-alpine] [--size-mib 768] [--skip-pull]

What it does:
  1) Pulls image (unless --skip-pull)
  2) Exports container filesystem
  3) Reads image Entrypoint/Cmd/Env/WorkingDir/User/ExposedPorts
  4) Writes /init wrapper inside rootfs to run image start command as PID 1
  5) Produces:
     - rootfs/      (expanded directory)
     - rootfs.tar   (tar archive)
     - rootfs.ext4  (ext4 image)
     - image-meta.json
     - suggested-bootargs.txt

Examples:
  scripts/build-rootfs-from-dockerhub.sh --image nginx:alpine
  scripts/build-rootfs-from-dockerhub.sh --image redis:7 --output-dir /var/lib/mergen/base --name redis-rootfs --size-mib 1024
EOF
}

require_cmd() {
  local cmd="${1}"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "required command not found: ${cmd}" >&2
    exit 1
  fi
}

sanitize_name() {
  local raw="${1}"
  local out
  out="$(echo "${raw}" | tr '/:@' '-' | tr -cd 'A-Za-z0-9._-')"
  if [[ -z "${out}" ]]; then
    out="image-rootfs"
  fi
  echo "${out}"
}

IMAGE=""
OUTPUT_DIR=""
NAME=""
SIZE_MIB=""
SKIP_PULL=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --image|-i)
      IMAGE="${2:-}"
      shift 2
      ;;
    --output-dir|-o)
      OUTPUT_DIR="${2:-}"
      shift 2
      ;;
    --name|-n)
      NAME="${2:-}"
      shift 2
      ;;
    --size-mib)
      SIZE_MIB="${2:-}"
      shift 2
      ;;
    --skip-pull)
      SKIP_PULL=1
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

if [[ -z "${IMAGE}" ]]; then
  echo "--image is required" >&2
  usage
  exit 1
fi

require_cmd docker
require_cmd jq
require_cmd tar
require_cmd mkfs.ext4
require_cmd truncate
require_cmd du

if [[ "$(id -u)" -ne 0 ]]; then
  echo "warning: running without root; file ownership inside rootfs may differ from image" >&2
fi

if [[ -z "${NAME}" ]]; then
  NAME="$(sanitize_name "${IMAGE}")"
fi
if [[ -z "${OUTPUT_DIR}" ]]; then
  OUTPUT_DIR="./artifacts/rootfs/${NAME}"
fi
mkdir -p "${OUTPUT_DIR}"

WORK_DIR="$(mktemp -d)"
CONTAINER_ID=""
cleanup() {
  if [[ -n "${CONTAINER_ID}" ]]; then
    docker rm -f "${CONTAINER_ID}" >/dev/null 2>&1 || true
  fi
  rm -rf "${WORK_DIR}"
}
trap cleanup EXIT

ROOTFS_STAGE="${WORK_DIR}/rootfs"
mkdir -p "${ROOTFS_STAGE}"

if [[ "${SKIP_PULL}" -ne 1 ]]; then
  echo "pulling image: ${IMAGE}"
  docker pull "${IMAGE}" >/dev/null
fi

echo "reading image config: ${IMAGE}"
CONFIG_JSON="$(docker image inspect "${IMAGE}" --format '{{json .Config}}')"
if [[ -z "${CONFIG_JSON}" || "${CONFIG_JSON}" == "null" ]]; then
  echo "failed to inspect image config: ${IMAGE}" >&2
  exit 1
fi

echo "exporting image filesystem"
CONTAINER_ID="$(docker create "${IMAGE}")"
docker export "${CONTAINER_ID}" | tar -C "${ROOTFS_STAGE}" -xf -
docker rm -f "${CONTAINER_ID}" >/dev/null
CONTAINER_ID=""

ENTRYPOINT_JSON="$(echo "${CONFIG_JSON}" | jq -c '.Entrypoint // []')"
CMD_JSON="$(echo "${CONFIG_JSON}" | jq -c '.Cmd // []')"
ENV_JSON="$(echo "${CONFIG_JSON}" | jq -c '.Env // []')"
WORKING_DIR="$(echo "${CONFIG_JSON}" | jq -r '.WorkingDir // ""')"
IMAGE_USER="$(echo "${CONFIG_JSON}" | jq -r '.User // ""')"
EXPOSED_PORTS="$(echo "${CONFIG_JSON}" | jq -r '(.ExposedPorts // {}) | keys | join(",")')"
START_CMD_JSON="$(echo "${CONFIG_JSON}" | jq -c '((.Entrypoint // []) + (.Cmd // [])) | if length == 0 then ["/bin/sh"] else . end')"
START_CMD_SHELL="$(echo "${START_CMD_JSON}" | jq -r 'map(@sh) | join(" ")')"
ENV_EXPORT_LINES="$(echo "${ENV_JSON}" | jq -r '.[] | select(test("^[A-Za-z_][A-Za-z0-9_]*=")) | capture("^(?<k>[A-Za-z_][A-Za-z0-9_]*)=(?<v>.*)$") | "export \(.k)=\(.v|@sh)"')"

mkdir -p "${ROOTFS_STAGE}/etc/mergen"
CREATED_AT="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
META_PATH="${OUTPUT_DIR}/image-meta.json"
jq -n \
  --arg image "${IMAGE}" \
  --arg createdAt "${CREATED_AT}" \
  --argjson entrypoint "${ENTRYPOINT_JSON}" \
  --argjson cmd "${CMD_JSON}" \
  --argjson startCmd "${START_CMD_JSON}" \
  --argjson env "${ENV_JSON}" \
  --arg workingDir "${WORKING_DIR}" \
  --arg user "${IMAGE_USER}" \
  --arg exposedPorts "${EXPOSED_PORTS}" \
  '{
    image: $image,
    createdAt: $createdAt,
    entrypoint: $entrypoint,
    cmd: $cmd,
    startCmd: $startCmd,
    env: $env,
    workingDir: $workingDir,
    user: $user,
    exposedPorts: $exposedPorts
  }' > "${META_PATH}"
cp "${META_PATH}" "${ROOTFS_STAGE}/etc/mergen/image-meta.json"

INIT_PATH="${ROOTFS_STAGE}/init"
cat > "${INIT_PATH}" <<EOF
#!/bin/sh
set -eu

mountpoint -q /proc || mount -t proc proc /proc || true
mountpoint -q /sys || mount -t sysfs sys /sys || true
mountpoint -q /dev || mount -t devtmpfs devtmpfs /dev || true
mkdir -p /dev/pts /run
mountpoint -q /dev/pts || mount -t devpts devpts /dev/pts || true

ip link set lo up >/dev/null 2>&1 || true
ip link set eth0 up >/dev/null 2>&1 || true

${ENV_EXPORT_LINES}

if [ -n "${WORKING_DIR}" ] && [ -d "${WORKING_DIR}" ]; then
  cd "${WORKING_DIR}"
fi

exec ${START_CMD_SHELL}
EOF
chmod +x "${INIT_PATH}"

ROOTFS_DIR="${OUTPUT_DIR}/rootfs"
rm -rf "${ROOTFS_DIR}"
mkdir -p "${ROOTFS_DIR}"
tar -C "${ROOTFS_STAGE}" -cf - . | tar -C "${ROOTFS_DIR}" -xf -

ROOTFS_TAR="${OUTPUT_DIR}/rootfs.tar"
ROOTFS_EXT4="${OUTPUT_DIR}/rootfs.ext4"
tar -C "${ROOTFS_DIR}" -cf "${ROOTFS_TAR}" .

if [[ -z "${SIZE_MIB}" ]]; then
  ROOTFS_BYTES="$(du -s -B1 "${ROOTFS_DIR}" | awk '{print $1}')"
  SIZE_MIB="$(( (ROOTFS_BYTES + 1024 * 1024 - 1) / (1024 * 1024) + 256 ))"
fi
if ! [[ "${SIZE_MIB}" =~ ^[0-9]+$ ]]; then
  echo "--size-mib must be numeric, got: ${SIZE_MIB}" >&2
  exit 1
fi

truncate -s "${SIZE_MIB}M" "${ROOTFS_EXT4}"
mkfs.ext4 -q -F -d "${ROOTFS_DIR}" "${ROOTFS_EXT4}"

BOOT_ARGS_PATH="${OUTPUT_DIR}/suggested-bootargs.txt"
cat > "${BOOT_ARGS_PATH}" <<'EOF'
console=ttyS0 reboot=k panic=1 pci=off init=/init
EOF

echo
echo "rootfs build completed"
echo "  image:                ${IMAGE}"
echo "  output dir:           ${OUTPUT_DIR}"
echo "  rootfs dir:           ${ROOTFS_DIR}"
echo "  rootfs tar:           ${ROOTFS_TAR}"
echo "  rootfs ext4:          ${ROOTFS_EXT4}"
echo "  image metadata:       ${META_PATH}"
echo "  suggested boot args:  ${BOOT_ARGS_PATH}"
echo "  detected entrypoint:  ${ENTRYPOINT_JSON}"
echo "  detected cmd:         ${CMD_JSON}"
echo "  detected start cmd:   ${START_CMD_JSON}"
echo "  detected exposed:     ${EXPOSED_PORTS}"
echo
echo "Next:"
echo "  1) Use rootfs.ext4 path in POST /v1/vms rootfs field"
echo "  2) Use a kernel that supports your userspace"
echo "  3) Set bootArgs to include init=/init (see suggested-bootargs.txt)"
