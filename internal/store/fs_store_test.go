package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alperreha/mergen-fire/internal/model"
)

func TestSaveReadDeleteVM(t *testing.T) {
	base := t.TempDir()
	s := NewFSStore(
		filepath.Join(base, "etc", "mergen", "vm.d"),
		filepath.Join(base, "var", "lib", "mergen"),
		filepath.Join(base, "run", "mergen"),
		filepath.Join(base, "etc", "mergen", "hooks.d"),
	)
	if err := s.EnsureBaseDirs(); err != nil {
		t.Fatalf("ensure base dirs: %v", err)
	}

	id := "test-vm-1"
	meta := model.VMMetadata{
		ID:        id,
		CreatedAt: time.Now().UTC(),
		RootFS:    "/tmp/rootfs.ext4",
		Kernel:    "/tmp/vmlinux",
		GuestIP:   "172.30.0.2",
		TapName:   "tap-testvm1",
		NetNS:     "mergen-testvm1",
		Ports: []model.PortBinding{
			{Guest: 8080, Host: 20000, Protocol: "tcp"},
		},
	}
	cfg := model.VMConfig{
		BootSource: model.BootSource{
			KernelImagePath: "/tmp/vmlinux",
			BootArgs:        "console=ttyS0",
		},
	}
	hooks := model.HooksConfig{
		OnCreate: []model.HookEntry{
			{Type: "http", URL: "http://127.0.0.1:9000/hook"},
		},
	}

	paths, err := s.SaveVM(id, cfg, meta, hooks, map[string]string{"A": "B"})
	if err != nil {
		t.Fatalf("save vm: %v", err)
	}

	if _, err := os.Stat(paths.MetaPath); err != nil {
		t.Fatalf("meta file missing: %v", err)
	}
	if _, err := os.Stat(paths.VMConfigPath); err != nil {
		t.Fatalf("vm config missing: %v", err)
	}

	readMeta, err := s.ReadMeta(id)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if readMeta.ID != id {
		t.Fatalf("meta id mismatch")
	}
	if readMeta.GuestIP != "172.30.0.2" {
		t.Fatalf("guest ip mismatch")
	}

	ids, err := s.ListVMIDs()
	if err != nil {
		t.Fatalf("list vm ids: %v", err)
	}
	if len(ids) != 1 || ids[0] != id {
		t.Fatalf("unexpected vm ids: %#v", ids)
	}

	if err := s.DeleteVM(id, false); err != nil {
		t.Fatalf("delete vm: %v", err)
	}
	exists, err := s.Exists(id)
	if err != nil {
		t.Fatalf("exists check: %v", err)
	}
	if exists {
		t.Fatalf("vm should be deleted")
	}
}
