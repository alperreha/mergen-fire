package converter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyAgentBinariesFromBaseCopiesExecutables(t *testing.T) {
	baseBinDir := t.TempDir()
	destDir := t.TempDir()

	mustWriteAgentFile(t, filepath.Join(baseBinDir, "mergen-agent"), []byte("agent"), 0o755)
	mustWriteAgentFile(t, filepath.Join(baseBinDir, "mergen-vsock-guest"), []byte("vsock"), 0o755)
	mustWriteAgentFile(t, filepath.Join(baseBinDir, "README.txt"), []byte("not executable"), 0o644)

	files, err := copyAgentBinariesFromBase(baseBinDir, destDir)
	if err != nil {
		t.Fatalf("copyAgentBinariesFromBase failed: %v", err)
	}

	if !containsString(files, "mergen-agent") || !containsString(files, "mergen-vsock-guest") {
		t.Fatalf("unexpected copied files: %#v", files)
	}
	if _, err := os.Stat(filepath.Join(destDir, "README.txt")); !os.IsNotExist(err) {
		t.Fatalf("non executable file should not be copied")
	}
}

func TestCopyAgentBinariesFromBaseFailsWithoutMergenAgent(t *testing.T) {
	baseBinDir := t.TempDir()
	destDir := t.TempDir()

	mustWriteAgentFile(t, filepath.Join(baseBinDir, "mergen-vsock-guest"), []byte("vsock"), 0o755)

	_, err := copyAgentBinariesFromBase(baseBinDir, destDir)
	if err == nil {
		t.Fatalf("expected missing mergen-agent error")
	}
	if !strings.Contains(err.Error(), "mergen-agent") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureReadOnlyBestEffort(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "agent-rootfs.ext4")
	mustWriteAgentFile(t, filePath, []byte("x"), 0o644)

	changed := ensureReadOnlyBestEffort(filePath, nil)
	if !changed {
		t.Fatalf("expected mode change to read-only")
	}

	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("expected read-only perms, got %o", info.Mode().Perm())
	}
}

func mustWriteAgentFile(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}
