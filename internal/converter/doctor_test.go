package converter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunDoctorPassesWithRequiredAssets(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWriteFile(t, filepath.Join(base, "vmlinux"), []byte("kernel"), 0o444)
	mustWriteExt4Magic(t, filepath.Join(base, "golden-rootfs.ext4"), 0o444)
	mustWriteExt4Magic(t, filepath.Join(base, "agent-rootfs.ext4"), 0o444)
	mustWriteFile(t, filepath.Join(base, "bin", "sbin-init"), []byte("x"), 0o555)
	mustWriteFile(t, filepath.Join(base, "bin", "mergen-agent"), []byte("x"), 0o555)

	report := RunDoctor(DoctorOptions{BaseDir: base})
	if !report.Passed {
		t.Fatalf("expected passed report, got failedRequired=%d warnings=%d", report.FailedRequired, report.Warnings)
	}
}

func TestRunDoctorFailsWhenKernelMissing(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWriteExt4Magic(t, filepath.Join(base, "golden-rootfs.ext4"), 0o444)
	mustWriteExt4Magic(t, filepath.Join(base, "agent-rootfs.ext4"), 0o444)
	mustWriteFile(t, filepath.Join(base, "bin", "sbin-init"), []byte("x"), 0o555)
	mustWriteFile(t, filepath.Join(base, "bin", "mergen-agent"), []byte("x"), 0o555)

	report := RunDoctor(DoctorOptions{BaseDir: base})
	if report.Passed {
		t.Fatalf("expected failed report")
	}
	if report.FailedRequired == 0 {
		t.Fatalf("expected failed required checks")
	}
}

func TestRunDoctorWarnsForOptionalEnvDisk(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWriteFile(t, filepath.Join(base, "vmlinux"), []byte("kernel"), 0o444)
	mustWriteExt4Magic(t, filepath.Join(base, "golden-rootfs.ext4"), 0o444)
	mustWriteExt4Magic(t, filepath.Join(base, "agent-rootfs.ext4"), 0o444)
	mustWriteFile(t, filepath.Join(base, "bin", "sbin-init"), []byte("x"), 0o555)
	mustWriteFile(t, filepath.Join(base, "bin", "mergen-agent"), []byte("x"), 0o555)

	report := RunDoctor(DoctorOptions{BaseDir: base, RequireEnvDisk: false})
	if !report.Passed {
		t.Fatalf("expected pass with optional env disk")
	}
	if report.Warnings == 0 {
		t.Fatalf("expected warnings for optional missing checks")
	}
}

func TestRunDoctorCurrentSymlinkPass(t *testing.T) {
	baseRoot := t.TempDir()
	versionDir := filepath.Join(baseRoot, "v20260306")
	if err := os.MkdirAll(filepath.Join(versionDir, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWriteFile(t, filepath.Join(versionDir, "vmlinux"), []byte("kernel"), 0o444)
	mustWriteExt4Magic(t, filepath.Join(versionDir, "golden-rootfs.ext4"), 0o444)
	mustWriteExt4Magic(t, filepath.Join(versionDir, "agent-rootfs.ext4"), 0o444)
	mustWriteFile(t, filepath.Join(versionDir, "bin", "sbin-init"), []byte("x"), 0o555)
	mustWriteFile(t, filepath.Join(versionDir, "bin", "mergen-agent"), []byte("x"), 0o555)

	currentPath := filepath.Join(baseRoot, "current")
	if err := os.Symlink(versionDir, currentPath); err != nil {
		t.Fatalf("symlink current: %v", err)
	}

	report := RunDoctor(DoctorOptions{BaseDir: currentPath})
	if !report.Passed {
		t.Fatalf("expected passed report, got failedRequired=%d warnings=%d", report.FailedRequired, report.Warnings)
	}
	resolvedVersionDir, err := filepath.EvalSymlinks(versionDir)
	if err != nil {
		t.Fatalf("resolve version dir: %v", err)
	}
	if report.ResolvedBaseDir != resolvedVersionDir {
		t.Fatalf("expected resolved base dir %q, got %q", resolvedVersionDir, report.ResolvedBaseDir)
	}
	check, ok := findCheck(report.Checks, "base-current-symlink")
	if !ok {
		t.Fatalf("expected base-current-symlink check")
	}
	if check.Status != "pass" {
		t.Fatalf("expected current symlink check pass, got %q", check.Status)
	}
}

func TestRunDoctorCurrentSymlinkFailWhenNotSymlink(t *testing.T) {
	baseRoot := t.TempDir()
	currentPath := filepath.Join(baseRoot, "current")
	if err := os.MkdirAll(filepath.Join(currentPath, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWriteFile(t, filepath.Join(currentPath, "vmlinux"), []byte("kernel"), 0o444)
	mustWriteExt4Magic(t, filepath.Join(currentPath, "golden-rootfs.ext4"), 0o444)
	mustWriteExt4Magic(t, filepath.Join(currentPath, "agent-rootfs.ext4"), 0o444)
	mustWriteFile(t, filepath.Join(currentPath, "bin", "sbin-init"), []byte("x"), 0o555)
	mustWriteFile(t, filepath.Join(currentPath, "bin", "mergen-agent"), []byte("x"), 0o555)

	report := RunDoctor(DoctorOptions{BaseDir: currentPath})
	if report.Passed {
		t.Fatalf("expected failed report when current is not symlink")
	}
	check, ok := findCheck(report.Checks, "base-current-symlink")
	if !ok {
		t.Fatalf("expected base-current-symlink check")
	}
	if check.Status != "fail" {
		t.Fatalf("expected current symlink check fail, got %q", check.Status)
	}
}

func mustWriteFile(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustWriteExt4Magic(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	buf := make([]byte, 4096)
	buf[1080] = 0x53
	buf[1081] = 0xEF
	if err := os.WriteFile(path, buf, mode); err != nil {
		t.Fatalf("write ext4 image %s: %v", path, err)
	}
}

func findCheck(checks []DoctorCheck, name string) (DoctorCheck, bool) {
	for _, check := range checks {
		if check.Name == name {
			return check, true
		}
	}
	return DoctorCheck{}, false
}
