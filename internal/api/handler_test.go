package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/alperreha/mergen-fire/internal/hooks"
	"github.com/alperreha/mergen-fire/internal/manager"
	"github.com/alperreha/mergen-fire/internal/model"
	"github.com/alperreha/mergen-fire/internal/network"
	"github.com/alperreha/mergen-fire/internal/store"
	"github.com/alperreha/mergen-fire/internal/systemd"
)

type fakeSystemd struct{}

func (f *fakeSystemd) Start(_ context.Context, _ string) error   { return nil }
func (f *fakeSystemd) Stop(_ context.Context, _ string) error    { return nil }
func (f *fakeSystemd) Disable(_ context.Context, _ string) error { return nil }
func (f *fakeSystemd) IsActive(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (f *fakeSystemd) Status(_ context.Context, id string) (systemd.Status, error) {
	return systemd.Status{
		Available:   true,
		Unit:        "mergen@" + id + ".service",
		Active:      false,
		ActiveState: "inactive",
		SubState:    "dead",
		MainPID:     0,
	}, nil
}

func TestHandler_GetVMArtifacts(t *testing.T) {
	e, id, kernelPath := newTestServerWithVM(t)

	metaReq := httptest.NewRequest(http.MethodGet, "/v1/vms/"+id+"/meta.json", nil)
	metaRec := httptest.NewRecorder()
	e.ServeHTTP(metaRec, metaReq)
	if metaRec.Code != http.StatusOK {
		t.Fatalf("meta status code: got %d want %d body=%s", metaRec.Code, http.StatusOK, metaRec.Body.String())
	}
	var meta model.VMMetadata
	if err := json.Unmarshal(metaRec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode meta response: %v", err)
	}
	if meta.ID != id {
		t.Fatalf("meta id mismatch: got %q want %q", meta.ID, id)
	}

	vmReq := httptest.NewRequest(http.MethodGet, "/v1/vms/"+id+"/vm.json", nil)
	vmRec := httptest.NewRecorder()
	e.ServeHTTP(vmRec, vmReq)
	if vmRec.Code != http.StatusOK {
		t.Fatalf("vm config status code: got %d want %d body=%s", vmRec.Code, http.StatusOK, vmRec.Body.String())
	}
	var vmCfg model.VMConfig
	if err := json.Unmarshal(vmRec.Body.Bytes(), &vmCfg); err != nil {
		t.Fatalf("decode vm config response: %v", err)
	}
	if vmCfg.BootSource.KernelImagePath != kernelPath {
		t.Fatalf("vm config kernel mismatch: got %q want %q", vmCfg.BootSource.KernelImagePath, kernelPath)
	}

	hooksReq := httptest.NewRequest(http.MethodGet, "/v1/vms/"+id+"/hooks.json", nil)
	hooksRec := httptest.NewRecorder()
	e.ServeHTTP(hooksRec, hooksReq)
	if hooksRec.Code != http.StatusOK {
		t.Fatalf("hooks status code: got %d want %d body=%s", hooksRec.Code, http.StatusOK, hooksRec.Body.String())
	}
	var hooksCfg model.HooksConfig
	if err := json.Unmarshal(hooksRec.Body.Bytes(), &hooksCfg); err != nil {
		t.Fatalf("decode hooks response: %v", err)
	}
	if len(hooksCfg.OnStart) != 1 {
		t.Fatalf("hooks onStart length mismatch: got %d want 1", len(hooksCfg.OnStart))
	}
	if hooksCfg.OnStart[0].URL != "http://127.0.0.1:9000/on-start" {
		t.Fatalf("hooks onStart[0] url mismatch: got %q", hooksCfg.OnStart[0].URL)
	}
}

func TestHandler_GetVMArtifacts_NotFound(t *testing.T) {
	e, _, _ := newTestServerWithVM(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/vms/does-not-exist/meta.json", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status code mismatch: got %d want %d body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body["error"] != "not_found" {
		t.Fatalf("error code mismatch: got %#v want %q", body["error"], "not_found")
	}
}

func newTestServerWithVM(t *testing.T) (*echo.Echo, string, string) {
	t.Helper()

	base := t.TempDir()
	fsStore := store.NewFSStore(
		filepath.Join(base, "var", "lib", "mergen", "vm.d"),
		filepath.Join(base, "var", "lib", "mergen"),
		filepath.Join(base, "run", "mergen"),
		filepath.Join(base, "var", "lib", "mergen", "hooks.d"),
	)
	if err := fsStore.EnsureBaseDirs(); err != nil {
		t.Fatalf("ensure base dirs: %v", err)
	}

	kernelPath := filepath.Join(base, "vmlinux")
	rootfsPath := filepath.Join(base, "rootfs.ext4")
	agentPath := filepath.Join(base, "agent.ext4")
	payloadPath := filepath.Join(base, "payload.ext4")
	envPath := filepath.Join(base, "env.ext4")
	for _, p := range []string{kernelPath, rootfsPath, agentPath, payloadPath, envPath} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("write fixture file %s: %v", p, err)
		}
	}

	svc := manager.NewService(
		fsStore,
		&fakeSystemd{},
		hooks.NewRunner(nil),
		network.NewAllocator(20000, 20020, "172.30.0.0/24"),
		nil,
	)

	vmID, err := svc.CreateVM(context.Background(), model.CreateVMRequest{
		RootFS:      rootfsPath,
		Kernel:      kernelPath,
		AgentDisk:   agentPath,
		PayloadDisk: payloadPath,
		EnvDisk:     envPath,
		VCPU:        1,
		MemMiB:      512,
		Hooks: map[string][]model.HookEntry{
			model.HookOnStart: []model.HookEntry{
				{Type: "http", URL: "http://127.0.0.1:9000/on-start"},
			},
		},
	})
	if err != nil {
		t.Fatalf("create vm: %v", err)
	}

	e := echo.New()
	Register(e, svc, nil)
	return e, vmID, kernelPath
}
