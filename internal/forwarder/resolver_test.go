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

func TestResolverResolveFirst(t *testing.T) {
	root := t.TempDir()

	olderID := "00000000-1111-2222-3333-444444444444"
	newerID := "ffffffff-1111-2222-3333-444444444444"
	olderDir := filepath.Join(root, olderID)
	newerDir := filepath.Join(root, newerID)

	if err := os.MkdirAll(olderDir, 0o755); err != nil {
		t.Fatalf("mkdir older dir: %v", err)
	}
	if err := os.MkdirAll(newerDir, 0o755); err != nil {
		t.Fatalf("mkdir newer dir: %v", err)
	}

	olderMeta := `{
  "id":"00000000-1111-2222-3333-444444444444",
  "createdAt":"2026-02-09T00:00:00Z",
  "guestIP":"172.30.0.2",
  "netns":"mergen-00000000"
}`
	newerMeta := `{
  "id":"ffffffff-1111-2222-3333-444444444444",
  "createdAt":"2026-02-10T00:00:00Z",
  "guestIP":"172.30.0.3",
  "netns":"mergen-ffffffff"
}`

	if err := os.WriteFile(filepath.Join(olderDir, "meta.json"), []byte(olderMeta), 0o644); err != nil {
		t.Fatalf("write older meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(newerDir, "meta.json"), []byte(newerMeta), 0o644); err != nil {
		t.Fatalf("write newer meta: %v", err)
	}

	resolver := NewResolver(root, "", "localhost", time.Second, nil)
	first, err := resolver.ResolveFirst()
	if err != nil {
		t.Fatalf("resolve first: %v", err)
	}
	if first.ID != olderID {
		t.Fatalf("expected older vm id %s, got %s", olderID, first.ID)
	}
}
