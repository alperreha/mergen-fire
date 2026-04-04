package config

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr        string
	ConfigRoot      string
	DataRoot        string
	RunRoot         string
	GlobalHooksDir  string
	UnitPrefix      string
	SystemctlPath   string
	CommandTimeout  time.Duration
	ShutdownTimeout time.Duration
	PortStart       int
	PortEnd         int
	GuestCIDR       string
	BaseAssetsDir   string
	ImagesRoot      string
	S3Endpoint      string
	S3Region        string
	S3Bucket        string
	S3Prefix        string
	S3Username      string
	S3AccessKeyID   string
	S3SecretKey     string
	S3SessionToken  string
	S3UsePathStyle  bool
	ProgressEvery   time.Duration
	LogLevel        string
	LogFormat       string
}

func FromEnv() Config {
	dataRoot := getEnv("MGR_DATA_ROOT", "/var/lib/mergen")
	return Config{
		HTTPAddr:        getEnv("MGR_HTTP_ADDR", ":8080"),
		ConfigRoot:      getEnv("MGR_CONFIG_ROOT", "/var/lib/mergen/vm.d"),
		DataRoot:        dataRoot,
		RunRoot:         getEnv("MGR_RUN_ROOT", "/run/mergen"),
		GlobalHooksDir:  getEnv("MGR_GLOBAL_HOOKS_DIR", "/var/lib/mergen/hooks.d"),
		UnitPrefix:      getEnv("MGR_UNIT_PREFIX", "mergen"),
		SystemctlPath:   getEnv("MGR_SYSTEMCTL_PATH", "systemctl"),
		CommandTimeout:  time.Duration(getEnvInt("MGR_COMMAND_TIMEOUT_SECONDS", 10)) * time.Second,
		ShutdownTimeout: time.Duration(getEnvInt("MGR_SHUTDOWN_TIMEOUT_SECONDS", 15)) * time.Second,
		PortStart:       getEnvInt("MGR_PORT_START", 20000),
		PortEnd:         getEnvInt("MGR_PORT_END", 40000),
		GuestCIDR:       getEnv("MGR_GUEST_CIDR", "172.30.0.0/24"),
		BaseAssetsDir:   getEnv("MGR_BASE_ASSETS_DIR", filepath.Join(dataRoot, "base", "current")),
		ImagesRoot:      getEnv("MGR_IMAGES_ROOT", filepath.Join(dataRoot, "images")),
		S3Endpoint:      getEnv("MGR_S3_ENDPOINT", ""),
		S3Region:        getEnv("MGR_S3_REGION", "us-east-1"),
		S3Bucket:        getEnv("MGR_S3_BUCKET", ""),
		S3Prefix:        getEnv("MGR_S3_PREFIX", "users"),
		S3Username:      getEnv("MGR_S3_USERNAME", ""),
		S3AccessKeyID:   getEnv("MGR_S3_ACCESS_KEY_ID", ""),
		S3SecretKey:     getEnv("MGR_S3_SECRET_ACCESS_KEY", ""),
		S3SessionToken:  getEnv("MGR_S3_SESSION_TOKEN", ""),
		S3UsePathStyle:  getEnvBool("MGR_S3_USE_PATH_STYLE", false),
		ProgressEvery:   time.Duration(getEnvInt("MGR_PROGRESS_EVERY_MILLISECONDS", 250)) * time.Millisecond,
		LogLevel:        getEnv("MGR_LOG_LEVEL", "info"),
		LogFormat:       getEnv("MGR_LOG_FORMAT", "console"),
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}
