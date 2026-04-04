package images

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alperreha/mergen-fire/internal/config"
)

func TestSaveAndLoadRegistryConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	input := RegistryConfig{
		CurrentRegistry: "default",
		Registries: map[string]RegistryProfile{
			"default": {
				Name:            "default",
				Endpoint:        "http://127.0.0.1:9000",
				Region:          "us-east-1",
				Bucket:          "mergen-config",
				Prefix:          "artifacts",
				AccessKeyID:     "key",
				SecretAccessKey: "secret",
				UsePathStyle:    true,
				Username:        "admin",
				Password:        "demo",
			},
		},
	}

	if err := SaveRegistryConfig(configPath, input); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loaded, err := LoadRegistryConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if loaded.CurrentRegistry != input.CurrentRegistry {
		t.Fatalf("current registry mismatch: got %q want %q", loaded.CurrentRegistry, input.CurrentRegistry)
	}
	if loaded.Registries["default"].Bucket != input.Registries["default"].Bucket {
		t.Fatalf("bucket mismatch: got %q want %q", loaded.Registries["default"].Bucket, input.Registries["default"].Bucket)
	}
}

func TestApplyRegistryProfileHonorsEnvAndProfile(t *testing.T) {
	t.Setenv("MGR_CONFIG_S3_BUCKET", "env-bucket")

	cfg := ApplyRegistryProfile(config.FromEnv(), RegistryProfile{
		Endpoint:        "http://127.0.0.1:9000",
		Region:          "us-east-1",
		Bucket:          "profile-bucket",
		Prefix:          "artifacts",
		AccessKeyID:     "key",
		SecretAccessKey: "secret",
		Username:        "admin",
		Password:        "demo",
		UsePathStyle:    true,
	})

	if cfg.ConfigS3Bucket != "env-bucket" {
		t.Fatalf("expected env bucket to win, got %q", cfg.ConfigS3Bucket)
	}
	if cfg.ConfigS3Endpoint != "http://127.0.0.1:9000" {
		t.Fatalf("endpoint mismatch: got %q", cfg.ConfigS3Endpoint)
	}
	if cfg.ConfigS3Username != "admin" {
		t.Fatalf("username mismatch: got %q", cfg.ConfigS3Username)
	}
	if !cfg.ConfigS3PathStyle {
		t.Fatal("expected path-style flag from profile")
	}
}

func TestDefaultRegistryConfigPathHonorsEnv(t *testing.T) {
	want := filepath.Join(t.TempDir(), "mergen.json")
	t.Setenv("MERGEN_CONFIG_FILE", want)

	got, err := DefaultRegistryConfigPath()
	if err != nil {
		t.Fatalf("default config path: %v", err)
	}
	if got != want {
		t.Fatalf("path mismatch: got %q want %q", got, want)
	}
}

func TestSaveRegistryConfigCreatesSecureFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := SaveRegistryConfig(configPath, RegistryConfig{}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != defaultConfigFileMode {
		t.Fatalf("unexpected file mode: got %o want %o", info.Mode().Perm(), defaultConfigFileMode)
	}
}
