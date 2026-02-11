package manager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
		Unit:        "mergen@" + id + ".service",
		Active:      f.active[id],
		ActiveState: map[bool]string{true: "active", false: "inactive"}[f.active[id]],
		SubState:    "running",
		MainPID:     1234,
	}, nil
}

func TestServiceLifecycle_IdempotentStartStop(t *testing.T) {
	base := t.TempDir()

	fsStore := store.NewFSStore(
		filepath.Join(base, "etc", "mergen", "vm.d"),
		filepath.Join(base, "var", "lib", "mergen"),
		filepath.Join(base, "run", "mergen"),
		filepath.Join(base, "etc", "mergen", "hooks.d"),
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

func TestServiceCreateVM_HTTPPortPersisted(t *testing.T) {
	base := t.TempDir()

	fsStore := store.NewFSStore(
		filepath.Join(base, "etc", "mergen", "vm.d"),
		filepath.Join(base, "var", "lib", "mergen"),
		filepath.Join(base, "run", "mergen"),
		filepath.Join(base, "etc", "mergen", "hooks.d"),
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

	service := NewService(
		fsStore,
		newFakeSystemd(),
		hooks.NewRunner(nil),
		network.NewAllocator(20000, 20010, "172.30.0.0/24"),
		nil,
	)

	id, err := service.CreateVM(context.Background(), model.CreateVMRequest{
		RootFS:   rootfsPath,
		Kernel:   kernelPath,
		VCPU:     1,
		MemMiB:   512,
		HTTPPort: 80,
		Ports: []model.PortBindingRequest{
			{Guest: 80, Host: 0},
		},
	})
	if err != nil {
		t.Fatalf("create vm: %v", err)
	}

	meta, err := fsStore.ReadMeta(id)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if meta.HTTPPort != 80 {
		t.Fatalf("expected httpPort=80 in meta, got %d", meta.HTTPPort)
	}

	envBytes, err := os.ReadFile(meta.Paths.EnvPath)
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	if !strings.Contains(string(envBytes), "MGN_HTTP_PORT=80") {
		t.Fatalf("expected MGN_HTTP_PORT in env file, got: %s", string(envBytes))
	}
}

func TestServiceCreateVM_HTTPPortRangeValidation(t *testing.T) {
	base := t.TempDir()

	fsStore := store.NewFSStore(
		filepath.Join(base, "etc", "mergen", "vm.d"),
		filepath.Join(base, "var", "lib", "mergen"),
		filepath.Join(base, "run", "mergen"),
		filepath.Join(base, "etc", "mergen", "hooks.d"),
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

	service := NewService(
		fsStore,
		newFakeSystemd(),
		hooks.NewRunner(nil),
		network.NewAllocator(20000, 20010, "172.30.0.0/24"),
		nil,
	)

	_, err := service.CreateVM(context.Background(), model.CreateVMRequest{
		RootFS:   rootfsPath,
		Kernel:   kernelPath,
		VCPU:     1,
		MemMiB:   512,
		HTTPPort: 70000,
		Ports: []model.PortBindingRequest{
			{Guest: 8080, Host: 0},
		},
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid request error, got %v", err)
	}
}

func osWrite(path string) error {
	return os.WriteFile(path, []byte("x"), 0o600)
}
