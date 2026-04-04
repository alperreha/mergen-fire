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
