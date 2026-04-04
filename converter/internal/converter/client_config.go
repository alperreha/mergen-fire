package converter

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultRegistryAlias  = "default"
	defaultUserS3Region   = "us-east-1"
	defaultUserS3Prefix   = "users"
	defaultConfigFileMode = 0o600
)

type ClientConfig struct {
	CurrentRegistry string                     `json:"currentRegistry,omitempty"`
	Registries      map[string]RegistryProfile `json:"registries,omitempty"`
}

type RegistryProfile struct {
	Name            string `json:"name,omitempty"`
	Endpoint        string `json:"endpoint,omitempty"`
	Region          string `json:"region,omitempty"`
	Bucket          string `json:"bucket,omitempty"`
	Prefix          string `json:"prefix,omitempty"`
	AccessKeyID     string `json:"accessKeyId,omitempty"`
	SecretAccessKey string `json:"secretAccessKey,omitempty"`
	SessionToken    string `json:"sessionToken,omitempty"`
	UsePathStyle    bool   `json:"usePathStyle,omitempty"`
	Username        string `json:"username,omitempty"`
	Password        string `json:"password,omitempty"`
}

func DefaultClientConfigPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("MERGEN_CONVERTER_CONFIG_FILE")); value != "" {
		return filepath.Clean(value), nil
	}
	if xdgHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdgHome != "" {
		return filepath.Join(xdgHome, "mergen-converter", "config.json"), nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(homeDir, ".config", "mergen-converter", "config.json"), nil
}

func LoadClientConfig(path string) (ClientConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return ClientConfig{}, errors.New("config path is empty")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ClientConfig{Registries: make(map[string]RegistryProfile)}, nil
		}
		return ClientConfig{}, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg ClientConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return ClientConfig{}, fmt.Errorf("decode config %s: %w", path, err)
	}
	if cfg.Registries == nil {
		cfg.Registries = make(map[string]RegistryProfile)
	}
	return cfg, nil
}

func SaveClientConfig(path string, cfg ClientConfig) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("config path is empty")
	}
	if cfg.Registries == nil {
		cfg.Registries = make(map[string]RegistryProfile)
	}

	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	body = append(body, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir config dir: %w", err)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(path), "mergen-converter-config-*.json")
	if err != nil {
		return fmt.Errorf("create temp config file: %w", err)
	}
	tempPath := tempFile.Name()
	if _, err := tempFile.Write(body); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("write temp config file: %w", err)
	}
	if err := tempFile.Chmod(defaultConfigFileMode); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("chmod temp config file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close temp config file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("rename config file: %w", err)
	}
	return nil
}

func UpsertRegistry(cfg ClientConfig, alias string, profile RegistryProfile, setCurrent bool) ClientConfig {
	if cfg.Registries == nil {
		cfg.Registries = make(map[string]RegistryProfile)
	}
	alias = normalizeRegistryAlias(alias)
	profile.Name = alias
	cfg.Registries[alias] = normalizeRegistryProfile(profile)
	if setCurrent {
		cfg.CurrentRegistry = alias
	}
	return cfg
}

func ResolveRegistry(cfg ClientConfig, alias string) (RegistryProfile, string, error) {
	if cfg.Registries == nil {
		cfg.Registries = make(map[string]RegistryProfile)
	}
	resolvedAlias := normalizeRegistryAlias(alias)
	if resolvedAlias == "" {
		resolvedAlias = normalizeRegistryAlias(cfg.CurrentRegistry)
	}
	if resolvedAlias == "" {
		resolvedAlias = defaultRegistryAlias
	}

	profile, ok := cfg.Registries[resolvedAlias]
	if !ok {
		profile = RegistryProfile{Name: resolvedAlias}
	}
	profile.Name = resolvedAlias
	profile = applyRegistryEnv(profile)
	profile = normalizeRegistryProfile(profile)
	return profile, resolvedAlias, nil
}

func normalizeRegistryAlias(alias string) string {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range alias {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-_.")
	if out == "" {
		return defaultRegistryAlias
	}
	return out
}

func normalizeRegistryProfile(profile RegistryProfile) RegistryProfile {
	profile.Name = normalizeRegistryAlias(profile.Name)
	if profile.Name == "" {
		profile.Name = defaultRegistryAlias
	}
	profile.Endpoint = strings.TrimSpace(profile.Endpoint)
	profile.Region = strings.TrimSpace(profile.Region)
	if profile.Region == "" {
		profile.Region = defaultUserS3Region
	}
	profile.Bucket = strings.TrimSpace(profile.Bucket)
	profile.Prefix = strings.Trim(strings.TrimSpace(profile.Prefix), "/")
	if profile.Prefix == "" {
		profile.Prefix = defaultUserS3Prefix
	}
	profile.AccessKeyID = strings.TrimSpace(profile.AccessKeyID)
	profile.SecretAccessKey = strings.TrimSpace(profile.SecretAccessKey)
	profile.SessionToken = strings.TrimSpace(profile.SessionToken)
	profile.Username = strings.TrimSpace(profile.Username)
	profile.Password = strings.TrimSpace(profile.Password)
	return profile
}

func applyRegistryEnv(profile RegistryProfile) RegistryProfile {
	if value := strings.TrimSpace(os.Getenv("MERGEN_CONVERTER_USER_S3_ENDPOINT")); value != "" {
		profile.Endpoint = value
	}
	if value := strings.TrimSpace(os.Getenv("MERGEN_CONVERTER_USER_S3_REGION")); value != "" {
		profile.Region = value
	}
	if value := strings.TrimSpace(os.Getenv("MERGEN_CONVERTER_USER_S3_BUCKET")); value != "" {
		profile.Bucket = value
	}
	if value := strings.TrimSpace(os.Getenv("MERGEN_CONVERTER_USER_S3_PREFIX")); value != "" {
		profile.Prefix = value
	}
	if value := strings.TrimSpace(os.Getenv("MERGEN_CONVERTER_USER_S3_ACCESS_KEY_ID")); value != "" {
		profile.AccessKeyID = value
	}
	if value := strings.TrimSpace(os.Getenv("MERGEN_CONVERTER_USER_S3_SECRET_ACCESS_KEY")); value != "" {
		profile.SecretAccessKey = value
	}
	if value := strings.TrimSpace(os.Getenv("MERGEN_CONVERTER_USER_S3_SESSION_TOKEN")); value != "" {
		profile.SessionToken = value
	}
	if value := strings.TrimSpace(os.Getenv("MERGEN_CONVERTER_USERNAME")); value != "" {
		profile.Username = value
	}
	if value := strings.TrimSpace(os.Getenv("MERGEN_CONVERTER_PASSWORD")); value != "" {
		profile.Password = value
	}
	if value := strings.TrimSpace(os.Getenv("MERGEN_CONVERTER_USER_S3_USE_PATH_STYLE")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			profile.UsePathStyle = parsed
		}
	}
	return profile
}

func ValidateRegistryForTransfer(profile RegistryProfile) error {
	if strings.TrimSpace(profile.Bucket) == "" {
		return errors.New("user S3 bucket is empty; run `mergen-converter init` or set MERGEN_CONVERTER_USER_S3_BUCKET")
	}
	if strings.TrimSpace(profile.AccessKeyID) == "" {
		return errors.New("user S3 access key is empty; run `mergen-converter init` or set MERGEN_CONVERTER_USER_S3_ACCESS_KEY_ID")
	}
	if strings.TrimSpace(profile.SecretAccessKey) == "" {
		return errors.New("user S3 secret key is empty; run `mergen-converter init` or set MERGEN_CONVERTER_USER_S3_SECRET_ACCESS_KEY")
	}
	if strings.TrimSpace(profile.Username) == "" {
		return errors.New("username is empty; run `mergen-converter login` or set MERGEN_CONVERTER_USERNAME")
	}
	return nil
}
