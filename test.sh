





export IMAGE="nginx:alpine"

go run ./cmd/mergen-converter \
  -image $IMAGE \
  -golden-rootfs-dir ./artifacts/golden-rootfs/golden-rootfs


curl -s -X POST http://127.0.0.1:8080/v1/vms \
  -H 'content-type: application/json' \
  -d '{
    "rootfs": "/var/lib/mergen/images/nginx:alpine/golden-rootfs.ext4",
    "agentDisk": "/var/lib/mergen/images/nginx:alpine/agent-rootfs.ext4",
    "payloadDisk": "/var/lib/mergen/images/nginx:alpine/payload-rootfs.ext4",
    "envDisk": "/var/lib/mergen/images/nginx:alpine/env-rootfs.ext4",
    "kernel": "/var/lib/mergen/base/vmlinux",
    "vcpu": 1,
    "memMiB": 512,
    "ports": [{"guest": 80, "host": 0}],
    "httpPort": 80,
    "tags": {"app": "nginx"},
    "autoStart": false  
  }'


curl -k --resolve "ogar3.vm.example.com:443:127.0.0.1" https://ogar3.vm.example.com/

export FWD_DOMAIN="vm.mergen.com"


# start vm service
curl -s -X POST "http://127.0.0.1:8080/v1/vms/${APPID}/start"

# check logs
journalctl -xeu mergen@${APPID}.service
