package converter

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "nginx:alpine", want: "nginx-alpine"},
		{in: "ghcr.io/org/app:1.2.3", want: "ghcr.io-org-app-1.2.3"},
		{in: "@@@", want: "image-rootfs"},
	}

	for _, tc := range cases {
		got := sanitizeName(tc.in)
		if got != tc.want {
			t.Fatalf("sanitizeName(%q) => %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDefaultOutputDirForImage(t *testing.T) {
	cases := []struct {
		image string
		want  string
	}{
		{
			image: "nginx:alpine",
			want:  filepath.Join(defaultOutputBase, "nginx:alpine"),
		},
		{
			image: "ghcr.io/org/app:1.2.3",
			want:  filepath.Join(defaultOutputBase, "ghcr.io", "org", "app:1.2.3"),
		},
		{
			image: "docker://localhost:5000/team/service@sha256:abc123",
			want:  filepath.Join(defaultOutputBase, "localhost:5000", "team", "service@sha256:abc123"),
		},
		{
			image: "///",
			want:  filepath.Join(defaultOutputBase, "image-rootfs"),
		},
	}

	for _, tc := range cases {
		got := defaultOutputDirForImage(tc.image)
		if got != tc.want {
			t.Fatalf("defaultOutputDirForImage(%q) => %q, want %q", tc.image, got, tc.want)
		}
	}
}

func TestNormalizeOptions_DefaultOutputDirFromImage(t *testing.T) {
	got, err := normalizeOptions(Options{
		Image: "ghcr.io/org/app:1.2.3",
	})
	if err != nil {
		t.Fatalf("normalizeOptions failed: %v", err)
	}

	wantOutput := filepath.Join(defaultOutputBase, "ghcr.io", "org", "app:1.2.3")
	if got.OutputDir != wantOutput {
		t.Fatalf("unexpected output dir: got %q want %q", got.OutputDir, wantOutput)
	}
}

func TestNormalizeOptions_DefaultOutputDirFromName(t *testing.T) {
	got, err := normalizeOptions(Options{
		Image: "nginx:alpine",
		Name:  "nginx.latest",
	})
	if err != nil {
		t.Fatalf("normalizeOptions failed: %v", err)
	}

	wantOutput := filepath.Join(defaultOutputBase, "nginx.latest")
	if got.OutputDir != wantOutput {
		t.Fatalf("unexpected output dir: got %q want %q", got.OutputDir, wantOutput)
	}
}

func TestComposeStartCommand(t *testing.T) {
	got := composeStartCommand([]string{"python"}, []string{"app.py"})
	if len(got) != 2 || got[0] != "python" || got[1] != "app.py" {
		t.Fatalf("unexpected start command: %#v", got)
	}

	fallback := composeStartCommand(nil, nil)
	if len(fallback) != 1 || fallback[0] != "/bin/sh" {
		t.Fatalf("unexpected fallback command: %#v", fallback)
	}
}

func TestInferHTTPPort(t *testing.T) {
	cases := []struct {
		name  string
		ports map[string]struct{}
		want  int
	}{
		{
			name: "prefers common http port",
			ports: map[string]struct{}{
				"5432/tcp": {},
				"3000/tcp": {},
			},
			want: 3000,
		},
		{
			name: "falls back to first tcp port",
			ports: map[string]struct{}{
				"7001/tcp": {},
				"9000/tcp": {},
			},
			want: 7001,
		},
		{
			name:  "returns zero when no ports",
			ports: map[string]struct{}{},
			want:  0,
		},
	}

	for _, tc := range cases {
		got := inferHTTPPort(tc.ports)
		if got != tc.want {
			t.Fatalf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestInjectSbinInitReplacesSymlinkWithoutTouchingTarget(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	rootfsDir := filepath.Join(tmpDir, "rootfs")
	if err := os.MkdirAll(filepath.Join(rootfsDir, "sbin"), 0o755); err != nil {
		t.Fatalf("prepare /sbin: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rootfsDir, "bin"), 0o755); err != nil {
		t.Fatalf("prepare /bin: %v", err)
	}

	busyboxPath := filepath.Join(rootfsDir, "bin", "busybox")
	const busyboxOriginal = "busybox-original"
	if err := os.WriteFile(busyboxPath, []byte(busyboxOriginal), 0o755); err != nil {
		t.Fatalf("write busybox: %v", err)
	}
	if err := os.Symlink("../bin/busybox", filepath.Join(rootfsDir, "sbin", "init")); err != nil {
		t.Fatalf("symlink /sbin/init -> /bin/busybox: %v", err)
	}

	hostInitPath := filepath.Join(tmpDir, "sbin-init")
	const initBinary = "init-binary-content"
	if err := os.WriteFile(hostInitPath, []byte(initBinary), 0o755); err != nil {
		t.Fatalf("write host sbin-init: %v", err)
	}

	if err := injectSbinInit(hostInitPath, rootfsDir); err != nil {
		t.Fatalf("injectSbinInit failed: %v", err)
	}

	busyboxAfter, err := os.ReadFile(busyboxPath)
	if err != nil {
		t.Fatalf("read busybox after inject: %v", err)
	}
	if string(busyboxAfter) != busyboxOriginal {
		t.Fatalf("busybox content changed: got %q want %q", string(busyboxAfter), busyboxOriginal)
	}

	initInfo, err := os.Lstat(filepath.Join(rootfsDir, "sbin", "init"))
	if err != nil {
		t.Fatalf("lstat /sbin/init: %v", err)
	}
	if initInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("/sbin/init should be regular file, got symlink")
	}

	initAfter, err := os.ReadFile(filepath.Join(rootfsDir, "sbin", "init"))
	if err != nil {
		t.Fatalf("read /sbin/init: %v", err)
	}
	if string(initAfter) != initBinary {
		t.Fatalf("/sbin/init content mismatch: got %q want %q", string(initAfter), initBinary)
	}

	copyAfter, err := os.ReadFile(filepath.Join(rootfsDir, "sbin", "mergen-init"))
	if err != nil {
		t.Fatalf("read /sbin/mergen-init: %v", err)
	}
	if string(copyAfter) != initBinary {
		t.Fatalf("/sbin/mergen-init content mismatch: got %q want %q", string(copyAfter), initBinary)
	}
}

func TestRunnerDelete_RemovesOutputDir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "nginx:alpine")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "rootfs.ext4"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write rootfs.ext4: %v", err)
	}

	runner := NewRunner(slog.New(slog.NewTextHandler(io.Discard, nil)))
	result, err := runner.Delete(context.Background(), DeleteOptions{
		Image:     "nginx:alpine",
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	if result.OutputDir != outputDir {
		t.Fatalf("unexpected output dir: got %q want %q", result.OutputDir, outputDir)
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("output dir should be removed, stat err=%v", err)
	}
}

func TestRunnerDelete_NotFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "missing")
	runner := NewRunner(slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err := runner.Delete(context.Background(), DeleteOptions{
		Image:     "nginx:alpine",
		OutputDir: outputDir,
	})
	if err == nil {
		t.Fatalf("expected error for missing output dir")
	}
}
