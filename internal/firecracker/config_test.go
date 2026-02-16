package firecracker

import (
	"testing"

	"github.com/alperreha/mergen-fire/internal/model"
)

func TestRenderVMConfig_Defaults(t *testing.T) {
	req := model.CreateVMRequest{
		RootFS: "/var/lib/mergen/vm1/rootfs.ext4",
		Kernel: "/var/lib/mergen/vm1/vmlinux",
		VCPU:   1,
		MemMiB: 512,
	}
	meta := model.VMMetadata{
		ID:      "6f008233-68f7-47b8-b2d1-6a9f0632b30b",
		TapName: "tap-6f008233",
		GuestIP: "172.30.0.2",
	}

	cfg := RenderVMConfig(req, meta)
	expectedBootArgs := "console=ttyS0 reboot=k panic=1 pci=off random.trust_cpu=on random.trust_bootloader=on ip=172.30.0.2::172.30.0.1:255.255.255.0::eth0:off"
	if cfg.BootSource.BootArgs != expectedBootArgs {
		t.Fatalf("unexpected boot args: %q", cfg.BootSource.BootArgs)
	}
	if cfg.BootSource.KernelImagePath != req.Kernel {
		t.Fatalf("kernel mismatch")
	}
	if len(cfg.Drives) != 1 {
		t.Fatalf("expected one drive, got %d", len(cfg.Drives))
	}
	if !cfg.Drives[0].IsRootDevice {
		t.Fatalf("root drive should be root device")
	}
	if len(cfg.NetworkInterfaces) != 1 {
		t.Fatalf("expected one network interface")
	}
	if cfg.NetworkInterfaces[0].HostDevName != meta.TapName {
		t.Fatalf("tap mismatch")
	}
}

func TestRenderVMConfig_DoesNotDuplicateExistingBootArgs(t *testing.T) {
	req := model.CreateVMRequest{
		RootFS:   "/var/lib/mergen/vm1/rootfs.ext4",
		Kernel:   "/var/lib/mergen/vm1/vmlinux",
		VCPU:     1,
		MemMiB:   512,
		BootArgs: "console=ttyS0 init=/init ip=10.0.0.2::10.0.0.1:255.255.255.0::eth0:off",
	}
	meta := model.VMMetadata{
		ID:      "6f008233-68f7-47b8-b2d1-6a9f0632b30b",
		TapName: "tap-6f008233",
		GuestIP: "172.30.0.2",
	}

	cfg := RenderVMConfig(req, meta)
	expectedBootArgs := "console=ttyS0 init=/init ip=10.0.0.2::10.0.0.1:255.255.255.0::eth0:off"
	if cfg.BootSource.BootArgs != expectedBootArgs {
		t.Fatalf("unexpected boot args: %q", cfg.BootSource.BootArgs)
	}
}

func TestRenderVMConfig_NoGuestIPKeepsDefaultBootArgs(t *testing.T) {
	req := model.CreateVMRequest{
		RootFS: "/var/lib/mergen/vm1/rootfs.ext4",
		Kernel: "/var/lib/mergen/vm1/vmlinux",
		VCPU:   1,
		MemMiB: 512,
	}
	meta := model.VMMetadata{
		ID:      "6f008233-68f7-47b8-b2d1-6a9f0632b30b",
		TapName: "tap-6f008233",
	}

	cfg := RenderVMConfig(req, meta)
	if cfg.BootSource.BootArgs != defaultBootArgs {
		t.Fatalf("unexpected boot args: %q", cfg.BootSource.BootArgs)
	}
}

func TestRenderVMConfig_WithPayloadAndEnvDisks(t *testing.T) {
	req := model.CreateVMRequest{
		RootFS:      "/var/lib/mergen/golden-rootfs.ext4",
		Kernel:      "/var/lib/mergen/vmlinux",
		PayloadDisk: "/var/lib/mergen/payload.ext4",
		EnvDisk:     "/var/lib/mergen/env.ext4",
		VCPU:        1,
		MemMiB:      512,
	}
	meta := model.VMMetadata{
		ID:      "6f008233-68f7-47b8-b2d1-6a9f0632b30b",
		TapName: "tap-6f008233",
		GuestIP: "172.30.0.2",
	}

	cfg := RenderVMConfig(req, meta)
	if len(cfg.Drives) != 3 {
		t.Fatalf("expected 3 drives, got %d", len(cfg.Drives))
	}
	if cfg.Drives[0].DriveID != "rootfs" || !cfg.Drives[0].IsRootDevice {
		t.Fatalf("unexpected rootfs drive: %#v", cfg.Drives[0])
	}
	if cfg.Drives[1].DriveID != "payload" || cfg.Drives[1].IsRootDevice || cfg.Drives[1].IsReadOnly {
		t.Fatalf("unexpected payload drive: %#v", cfg.Drives[1])
	}
	if cfg.Drives[2].DriveID != "env" || cfg.Drives[2].IsRootDevice || !cfg.Drives[2].IsReadOnly {
		t.Fatalf("unexpected env drive: %#v", cfg.Drives[2])
	}
}
