package converter

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCollectEnvLines_FromFileAndInline(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "app.env")
	content := "# comment\nA=1\n\nB=2\n"
	if err := os.WriteFile(envFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	lines, err := collectEnvLines(envFile, []string{"C=3", "  ", "#skip", "D=4"})
	if err != nil {
		t.Fatalf("collectEnvLines failed: %v", err)
	}

	want := []string{"A=1", "B=2", "C=3", "D=4"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines mismatch: got %#v want %#v", lines, want)
	}
}

func TestWriteEnvDiskLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mergen.env")
	lines := []string{"A=1", "B=2"}

	if err := writeEnvDiskLines(path, lines); err != nil {
		t.Fatalf("writeEnvDiskLines failed: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if string(body) != "A=1\nB=2\n" {
		t.Fatalf("unexpected env file body: %q", string(body))
	}
}
