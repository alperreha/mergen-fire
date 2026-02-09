package manager

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alperreha/mergen-fire/internal/hooks"
	"github.com/alperreha/mergen-fire/internal/model"
	"github.com/alperreha/mergen-fire/internal/network"
	"github.com/alperreha/mergen-fire/internal/store"
	"github.com/alperreha/mergen-fire/internal/systemd"
)

type fakeSystemd struct {
	active    map[string]bool
	startCall int
	stopCall  int
}

func newFakeSystemd() *fakeSystemd {
	return &fakeSystemd{
		active: map[string]bool{},
	}
}

func (f *fakeSystemd) Start(_ context.Context, id string) error {
	f.startCall++
	f.active[id] = true
	return nil
}

func (f *fakeSystemd) Stop(_ context.Context, id string) error {
	f.stopCall++
	f.active[id] = false
	return nil
}

func (f *fakeSystemd) Disable(_ context.Context, _ string) error {
	return nil
}

func (f *fakeSystemd) IsActive(_ context.Context, id string) (bool, error) {
	return f.active[id], nil
}

func (f *fakeSystemd) Status(_ context.Context, id string) (systemd.Status, error) {
	return systemd.Status{
		Available:   true,
		Unit:        "fc@" + id + ".service",
		Active:      f.active[id],
		ActiveState: map[bool]string{true: "active", false: "inactive"}[f.active[id]],
		SubState:    "running",
		MainPID:     1234,
	}, nil
}

func TestServiceLifecycle_IdempotentStartStop(t *testing.T) {
	base := t.TempDir()

	fsStore := store.NewFSStore(
		filepath.Join(base, "etc", "firecracker", "vm.d"),
		filepath.Join(base, "var", "lib", "firecracker"),
		filepath.Join(base, "run", "firecracker"),
		filepath.Join(base, "etc", "firecracker", "hooks.d"),
	)
	if err := fsStore.EnsureBaseDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	kernelPath := filepath.Join(base, "vmlinux")
	rootfsPath := filepath.Join(base, "rootfs.ext4")
	if err := osWrite(kernelPath); err != nil {
		t.Fatalf("write kernel: %v", err)
	}
	if err := osWrite(rootfsPath); err != nil {
		t.Fatalf("write rootfs: %v", err)
	}

	fake := newFakeSystemd()
	service := NewService(
		fsStore,
		fake,
		hooks.NewRunner(nil),
		network.NewAllocator(20000, 20010, "172.30.0.0/24"),
		nil,
	)

	id, err := service.CreateVM(context.Background(), model.CreateVMRequest{
		RootFS: rootfsPath,
		Kernel: kernelPath,
		VCPU:   1,
		MemMiB: 512,
		Ports: []model.PortBindingRequest{
			{Guest: 8080, Host: 0},
		},
	})
	if err != nil {
		t.Fatalf("create vm: %v", err)
	}

	if err := service.StartVM(context.Background(), id); err != nil {
		t.Fatalf("start vm: %v", err)
	}
	if err := service.StartVM(context.Background(), id); err != nil {
		t.Fatalf("start vm second call: %v", err)
	}
	if fake.startCall != 1 {
		t.Fatalf("expected start call 1, got %d", fake.startCall)
	}

	if err := service.StopVM(context.Background(), id); err != nil {
		t.Fatalf("stop vm: %v", err)
	}
	if err := service.StopVM(context.Background(), id); err != nil {
		t.Fatalf("stop vm second call: %v", err)
	}
	if fake.stopCall != 1 {
		t.Fatalf("expected stop call 1, got %d", fake.stopCall)
	}

	if err := service.DeleteVM(context.Background(), id, false); err != nil {
		t.Fatalf("delete vm: %v", err)
	}
}

func osWrite(path string) error {
	return os.WriteFile(path, []byte("x"), 0o600)
}
