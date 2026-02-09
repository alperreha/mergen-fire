package firecracker

import (
	"testing"

	"github.com/alperreha/mergen-fire/internal/model"
)

func TestRenderVMConfig_Defaults(t *testing.T) {
	req := model.CreateVMRequest{
		RootFS: "/var/lib/firecracker/vm1/rootfs.ext4",
		Kernel: "/var/lib/firecracker/vm1/vmlinux",
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
