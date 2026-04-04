package images

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alperreha/mergen-fire/internal/config"
)

const (
	defaultConfigRegistryAlias = "default"
	defaultConfigFileMode      = 0o600
	defaultConfigS3Region      = "us-east-1"
	defaultConfigS3Prefix      = "artifacts"
)

type RegistryConfig struct {
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

func DefaultRegistryConfigPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("MERGEN_CONFIG_FILE")); value != "" {
		return filepath.Clean(value), nil
	}
	if xdgHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdgHome != "" {
		return filepath.Join(xdgHome, "mergen", "config.json"), nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(homeDir, ".config", "mergen", "config.json"), nil
}

func LoadRegistryConfig(path string) (RegistryConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return RegistryConfig{}, errors.New("config path is empty")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RegistryConfig{Registries: make(map[string]RegistryProfile)}, nil
		}
		return RegistryConfig{}, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg RegistryConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return RegistryConfig{}, fmt.Errorf("decode config %s: %w", path, err)
	}
	if cfg.Registries == nil {
		cfg.Registries = make(map[string]RegistryProfile)
	}
	return cfg, nil
}

func SaveRegistryConfig(path string, cfg RegistryConfig) error {
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

	tempFile, err := os.CreateTemp(filepath.Dir(path), "mergen-config-*.json")
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

func UpsertRegistry(cfg RegistryConfig, alias string, profile RegistryProfile, setCurrent bool) RegistryConfig {
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

func ResolveRegistry(cfg RegistryConfig, alias string) (RegistryProfile, string, error) {
	if cfg.Registries == nil {
		cfg.Registries = make(map[string]RegistryProfile)
	}

	resolvedAlias := normalizeRegistryAlias(alias)
	if resolvedAlias == "" {
		resolvedAlias = normalizeRegistryAlias(cfg.CurrentRegistry)
	}
	if resolvedAlias == "" {
		resolvedAlias = defaultConfigRegistryAlias
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

func ApplyRegistryProfile(cfg config.Config, profile RegistryProfile) config.Config {
	if strings.TrimSpace(cfg.ConfigS3Endpoint) == "" && !envSet("MGR_CONFIG_S3_ENDPOINT", "MGR_S3_ENDPOINT") {
		cfg.ConfigS3Endpoint = profile.Endpoint
	}
	if strings.TrimSpace(cfg.ConfigS3Region) == "" && !envSet("MGR_CONFIG_S3_REGION", "MGR_S3_REGION") {
		cfg.ConfigS3Region = profile.Region
	}
	if strings.TrimSpace(cfg.ConfigS3Bucket) == "" && !envSet("MGR_CONFIG_S3_BUCKET", "MGR_S3_BUCKET") {
		cfg.ConfigS3Bucket = profile.Bucket
	}
	if strings.TrimSpace(cfg.ConfigS3Prefix) == "" && !envSet("MGR_CONFIG_S3_PREFIX", "MGR_S3_PREFIX") {
		cfg.ConfigS3Prefix = profile.Prefix
	}
	if strings.TrimSpace(cfg.ConfigS3AccessKey) == "" && !envSet("MGR_CONFIG_S3_ACCESS_KEY_ID", "MGR_S3_ACCESS_KEY_ID") {
		cfg.ConfigS3AccessKey = profile.AccessKeyID
	}
	if strings.TrimSpace(cfg.ConfigS3SecretKey) == "" && !envSet("MGR_CONFIG_S3_SECRET_ACCESS_KEY", "MGR_S3_SECRET_ACCESS_KEY") {
		cfg.ConfigS3SecretKey = profile.SecretAccessKey
	}
	if strings.TrimSpace(cfg.ConfigS3SessionToken) == "" && !envSet("MGR_CONFIG_S3_SESSION_TOKEN", "MGR_S3_SESSION_TOKEN") {
		cfg.ConfigS3SessionToken = profile.SessionToken
	}
	if strings.TrimSpace(cfg.ConfigS3Username) == "" && !envSet("MGR_CONFIG_S3_USERNAME", "MGR_S3_USERNAME") {
		cfg.ConfigS3Username = profile.Username
	}
	if strings.TrimSpace(cfg.ConfigS3Password) == "" && !envSet("MGR_CONFIG_S3_PASSWORD") {
		cfg.ConfigS3Password = profile.Password
	}
	if !envSet("MGR_CONFIG_S3_USE_PATH_STYLE", "MGR_S3_USE_PATH_STYLE") {
		cfg.ConfigS3PathStyle = profile.UsePathStyle
	}
	return cfg
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
		return defaultConfigRegistryAlias
	}
	return out
}

func normalizeRegistryProfile(profile RegistryProfile) RegistryProfile {
	profile.Name = normalizeRegistryAlias(profile.Name)
	if profile.Name == "" {
		profile.Name = defaultConfigRegistryAlias
	}
	profile.Endpoint = strings.TrimSpace(profile.Endpoint)
	profile.Region = strings.TrimSpace(profile.Region)
	if profile.Region == "" {
		profile.Region = defaultConfigS3Region
	}
	profile.Bucket = strings.TrimSpace(profile.Bucket)
	profile.Prefix = strings.Trim(strings.TrimSpace(profile.Prefix), "/")
	if profile.Prefix == "" {
		profile.Prefix = defaultConfigS3Prefix
	}
	profile.AccessKeyID = strings.TrimSpace(profile.AccessKeyID)
	profile.SecretAccessKey = strings.TrimSpace(profile.SecretAccessKey)
	profile.SessionToken = strings.TrimSpace(profile.SessionToken)
	profile.Username = strings.TrimSpace(profile.Username)
	profile.Password = strings.TrimSpace(profile.Password)
	return profile
}

func applyRegistryEnv(profile RegistryProfile) RegistryProfile {
	if value := strings.TrimSpace(os.Getenv("MGR_CONFIG_S3_ENDPOINT")); value != "" {
		profile.Endpoint = value
	} else if value := strings.TrimSpace(os.Getenv("MGR_S3_ENDPOINT")); value != "" {
		profile.Endpoint = value
	}
	if value := strings.TrimSpace(os.Getenv("MGR_CONFIG_S3_REGION")); value != "" {
		profile.Region = value
	} else if value := strings.TrimSpace(os.Getenv("MGR_S3_REGION")); value != "" {
		profile.Region = value
	}
	if value := strings.TrimSpace(os.Getenv("MGR_CONFIG_S3_BUCKET")); value != "" {
		profile.Bucket = value
	} else if value := strings.TrimSpace(os.Getenv("MGR_S3_BUCKET")); value != "" {
		profile.Bucket = value
	}
	if value := strings.TrimSpace(os.Getenv("MGR_CONFIG_S3_PREFIX")); value != "" {
		profile.Prefix = value
	} else if value := strings.TrimSpace(os.Getenv("MGR_S3_PREFIX")); value != "" {
		profile.Prefix = value
	}
	if value := strings.TrimSpace(os.Getenv("MGR_CONFIG_S3_ACCESS_KEY_ID")); value != "" {
		profile.AccessKeyID = value
	} else if value := strings.TrimSpace(os.Getenv("MGR_S3_ACCESS_KEY_ID")); value != "" {
		profile.AccessKeyID = value
	}
	if value := strings.TrimSpace(os.Getenv("MGR_CONFIG_S3_SECRET_ACCESS_KEY")); value != "" {
		profile.SecretAccessKey = value
	} else if value := strings.TrimSpace(os.Getenv("MGR_S3_SECRET_ACCESS_KEY")); value != "" {
		profile.SecretAccessKey = value
	}
	if value := strings.TrimSpace(os.Getenv("MGR_CONFIG_S3_SESSION_TOKEN")); value != "" {
		profile.SessionToken = value
	} else if value := strings.TrimSpace(os.Getenv("MGR_S3_SESSION_TOKEN")); value != "" {
		profile.SessionToken = value
	}
	if value := strings.TrimSpace(os.Getenv("MGR_CONFIG_S3_USERNAME")); value != "" {
		profile.Username = value
	} else if value := strings.TrimSpace(os.Getenv("MGR_S3_USERNAME")); value != "" {
		profile.Username = value
	}
	if value := strings.TrimSpace(os.Getenv("MGR_CONFIG_S3_PASSWORD")); value != "" {
		profile.Password = value
	}
	if value := strings.TrimSpace(os.Getenv("MGR_CONFIG_S3_USE_PATH_STYLE")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			profile.UsePathStyle = parsed
		}
	} else if value := strings.TrimSpace(os.Getenv("MGR_S3_USE_PATH_STYLE")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			profile.UsePathStyle = parsed
		}
	}
	return profile
}

func envSet(keys ...string) bool {
	for _, key := range keys {
		if _, ok := os.LookupEnv(key); ok {
			return true
		}
	}
	return false
}
