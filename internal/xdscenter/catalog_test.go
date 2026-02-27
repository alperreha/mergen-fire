package xdscenter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogListRoutesIncludesUUIDAndTagAlias(t *testing.T) {
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
  "httpPort":80,
  "tags":{"app":"app1"}
}`
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	catalog := NewCatalog(root, "vm.example.com", "/run/netns", nil)
	routes, err := catalog.ListRoutes()
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}

	if len(routes) != 3 {
		t.Fatalf("expected 3 aliases (id, short, app), got %d", len(routes))
	}

	hosts := map[string]RouteRecord{}
	for _, route := range routes {
		hosts[route.Host] = route
	}

	appHost := "app1.vm.example.com"
	appRoute, ok := hosts[appHost]
	if !ok {
		t.Fatalf("missing app alias host: %s", appHost)
	}
	if appRoute.TargetAddr != "172.30.0.5:80" {
		t.Fatalf("unexpected target addr for app host: %s", appRoute.TargetAddr)
	}

	shortHost := "084604f6.vm.example.com"
	if _, ok := hosts[shortHost]; !ok {
		t.Fatalf("missing short id host: %s", shortHost)
	}
}

func TestCatalogListRoutesSkipsInvalidHTTPPort(t *testing.T) {
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
  "httpPort":0,
  "tags":{"app":"edgeapp"}
}`
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	catalog := NewCatalog(root, "vm.example.com", "/run/netns", nil)
	routes, err := catalog.ListRoutes()
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	if len(routes) != 0 {
		t.Fatalf("expected no routes for invalid httpPort, got %d", len(routes))
	}
}

func TestLabelFromHost(t *testing.T) {
	label, err := labelFromHost("App1.vm.example.com", "vm.example.com")
	if err != nil {
		t.Fatalf("label from host failed: %v", err)
	}
	if label != "app1" {
		t.Fatalf("expected app1, got %s", label)
	}
}
