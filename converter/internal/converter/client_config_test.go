package converter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadClientConfig(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")
	input := ClientConfig{
		CurrentRegistry: "team-a",
		Registries: map[string]RegistryProfile{
			"team-a": {
				Name:            "team-a",
				Endpoint:        "http://127.0.0.1:9000",
				Region:          "us-east-1",
				Bucket:          "mergen-user",
				Prefix:          "users",
				AccessKeyID:     "key",
				SecretAccessKey: "secret",
				UsePathStyle:    true,
				Username:        "alice",
				Password:        "demo",
			},
		},
	}

	if err := SaveClientConfig(configPath, input); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loaded, err := LoadClientConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if loaded.CurrentRegistry != input.CurrentRegistry {
		t.Fatalf("current registry mismatch: got %q want %q", loaded.CurrentRegistry, input.CurrentRegistry)
	}
	if loaded.Registries["team-a"].Endpoint != input.Registries["team-a"].Endpoint {
		t.Fatalf("endpoint mismatch: got %q want %q", loaded.Registries["team-a"].Endpoint, input.Registries["team-a"].Endpoint)
	}
}

func TestResolveRegistryAppliesEnvOverrides(t *testing.T) {
	t.Setenv("MERGEN_CONVERTER_USER_S3_ENDPOINT", "http://minio.local:9000")
	t.Setenv("MERGEN_CONVERTER_USER_S3_BUCKET", "bucket-a")
	t.Setenv("MERGEN_CONVERTER_USERNAME", "alice")
	t.Setenv("MERGEN_CONVERTER_USER_S3_USE_PATH_STYLE", "true")

	registry, alias, err := ResolveRegistry(ClientConfig{}, "")
	if err != nil {
		t.Fatalf("resolve registry: %v", err)
	}

	if alias != defaultRegistryAlias {
		t.Fatalf("alias mismatch: got %q want %q", alias, defaultRegistryAlias)
	}
	if registry.Endpoint != "http://minio.local:9000" {
		t.Fatalf("endpoint mismatch: got %q", registry.Endpoint)
	}
	if registry.Bucket != "bucket-a" {
		t.Fatalf("bucket mismatch: got %q", registry.Bucket)
	}
	if registry.Username != "alice" {
		t.Fatalf("username mismatch: got %q", registry.Username)
	}
	if !registry.UsePathStyle {
		t.Fatalf("expected use path style")
	}
}

func TestDefaultClientConfigPathHonorsEnv(t *testing.T) {
	want := filepath.Join(t.TempDir(), "cfg.json")
	t.Setenv("MERGEN_CONVERTER_CONFIG_FILE", want)

	got, err := DefaultClientConfigPath()
	if err != nil {
		t.Fatalf("default config path: %v", err)
	}
	if got != want {
		t.Fatalf("config path mismatch: got %q want %q", got, want)
	}
}

func TestSaveClientConfigCreatesSecureFile(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := SaveClientConfig(configPath, ClientConfig{}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config file: %v", err)
	}
	if info.Mode().Perm() != defaultConfigFileMode {
		t.Fatalf("unexpected file mode: got %o want %o", info.Mode().Perm(), defaultConfigFileMode)
	}
}
