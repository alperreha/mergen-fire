package forwarder

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolverResolveByTagAndUUID(t *testing.T) {
	root := t.TempDir()
	vmID := "084604f6-0766-4b7d-9d23-0b7a011d6eaa"
	vmDir := filepath.Join(root, vmID)
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatalf("mkdir vm dir: %v", err)
	}

	meta := `{
  "id":"084604f6-0766-4b7d-9d23-0b7a011d6eaa",
  "guestIP":"172.30.0.5",
  "netns":"mergen-084604f6",
  "tapName":"tap-084604f6",
  "ports":[{"guest":8080,"host":20002,"protocol":"tcp"}],
  "tags":{"app":"app1","host":"app1"}
}`
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	resolver := NewResolver(root, "", "localhost", 1*time.Second, nil)

	byApp, err := resolver.Resolve("app1.localhost")
	if err != nil {
		t.Fatalf("resolve app alias: %v", err)
	}
	if byApp.ID != vmID {
		t.Fatalf("unexpected vm id by alias: %s", byApp.ID)
	}

	byShort, err := resolver.Resolve("084604f6.localhost")
	if err != nil {
		t.Fatalf("resolve uuid short: %v", err)
	}
	if byShort.ID != vmID {
		t.Fatalf("unexpected vm id by short uuid: %s", byShort.ID)
	}
}

func TestResolverResolveWithPrefixAndSuffix(t *testing.T) {
	root := t.TempDir()
	vmID := "11111111-2222-3333-4444-555555555555"
	vmDir := filepath.Join(root, vmID)
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatalf("mkdir vm dir: %v", err)
	}

	meta := `{
  "id":"11111111-2222-3333-4444-555555555555",
  "guestIP":"10.0.0.3",
  "netns":"mergen-11111111",
  "tags":{"app":"edgeapp"}
}`
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	resolver := NewResolver(root, "vm", "example.com", 1*time.Second, nil)
	byApp, err := resolver.Resolve("edgeapp.vm.example.com")
	if err != nil {
		t.Fatalf("resolve with prefix/suffix failed: %v", err)
	}
	if byApp.ID != vmID {
		t.Fatalf("unexpected vm id: %s", byApp.ID)
	}
}
