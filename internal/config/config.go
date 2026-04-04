package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr             string
	ConfigRoot           string
	DataRoot             string
	RunRoot              string
	GlobalHooksDir       string
	UnitPrefix           string
	SystemctlPath        string
	CommandTimeout       time.Duration
	ShutdownTimeout      time.Duration
	PortStart            int
	PortEnd              int
	GuestCIDR            string
	BaseAssetsDir        string
	ImagesRoot           string
	ConfigS3Endpoint     string
	ConfigS3Region       string
	ConfigS3Bucket       string
	ConfigS3Prefix       string
	ConfigS3Username     string
	ConfigS3Password     string
	ConfigS3AccessKey    string
	ConfigS3SecretKey    string
	ConfigS3SessionToken string
	ConfigS3PathStyle    bool
	BasePlatform         string
	BaseFlavor           string
	ProgressEvery        time.Duration
	LogLevel             string
	LogFormat            string
}

func FromEnv() Config {
	dataRoot := getEnv("MGR_DATA_ROOT", "/var/lib/mergen")
	return Config{
		HTTPAddr:         getEnv("MGR_HTTP_ADDR", ":8080"),
		ConfigRoot:       getEnv("MGR_CONFIG_ROOT", "/var/lib/mergen/vm.d"),
		DataRoot:         dataRoot,
		RunRoot:          getEnv("MGR_RUN_ROOT", "/run/mergen"),
		GlobalHooksDir:   getEnv("MGR_GLOBAL_HOOKS_DIR", "/var/lib/mergen/hooks.d"),
		UnitPrefix:       getEnv("MGR_UNIT_PREFIX", "mergen"),
		SystemctlPath:    getEnv("MGR_SYSTEMCTL_PATH", "systemctl"),
		CommandTimeout:   time.Duration(getEnvInt("MGR_COMMAND_TIMEOUT_SECONDS", 10)) * time.Second,
		ShutdownTimeout:  time.Duration(getEnvInt("MGR_SHUTDOWN_TIMEOUT_SECONDS", 15)) * time.Second,
		PortStart:        getEnvInt("MGR_PORT_START", 20000),
		PortEnd:          getEnvInt("MGR_PORT_END", 40000),
		GuestCIDR:        getEnv("MGR_GUEST_CIDR", "172.30.0.0/24"),
		BaseAssetsDir:    getEnv("MGR_BASE_ASSETS_DIR", filepath.Join(dataRoot, "base", "current")),
		ImagesRoot:       getEnv("MGR_IMAGES_ROOT", filepath.Join(dataRoot, "images")),
		ConfigS3Endpoint: getEnvAny([]string{"MGR_CONFIG_S3_ENDPOINT", "MGR_S3_ENDPOINT"}, ""),
		ConfigS3Region:   getEnvAny([]string{"MGR_CONFIG_S3_REGION", "MGR_S3_REGION"}, "us-east-1"),
		ConfigS3Bucket:   getEnvAny([]string{"MGR_CONFIG_S3_BUCKET", "MGR_S3_BUCKET"}, ""),
		ConfigS3Prefix:   getEnvAny([]string{"MGR_CONFIG_S3_PREFIX", "MGR_S3_PREFIX"}, ""),
		ConfigS3Username: getEnvAny([]string{"MGR_CONFIG_S3_USERNAME", "MGR_S3_USERNAME"}, ""),
		ConfigS3Password: getEnvAny([]string{"MGR_CONFIG_S3_PASSWORD"}, ""),
		ConfigS3AccessKey: getEnvAny([]string{
			"MGR_CONFIG_S3_ACCESS_KEY_ID",
			"MGR_S3_ACCESS_KEY_ID",
		}, ""),
		ConfigS3SecretKey: getEnvAny([]string{
			"MGR_CONFIG_S3_SECRET_ACCESS_KEY",
			"MGR_S3_SECRET_ACCESS_KEY",
		}, ""),
		ConfigS3SessionToken: getEnvAny([]string{
			"MGR_CONFIG_S3_SESSION_TOKEN",
			"MGR_S3_SESSION_TOKEN",
		}, ""),
		ConfigS3PathStyle: getEnvBoolAny([]string{
			"MGR_CONFIG_S3_USE_PATH_STYLE",
			"MGR_S3_USE_PATH_STYLE",
		}, false),
		BasePlatform:  getEnv("MGR_BASE_PLATFORM", "linux-"+runtime.GOARCH),
		BaseFlavor:    getEnv("MGR_BASE_FLAVOR", "buildroot"),
		ProgressEvery: time.Duration(getEnvInt("MGR_PROGRESS_EVERY_MILLISECONDS", 250)) * time.Millisecond,
		LogLevel:      getEnv("MGR_LOG_LEVEL", "info"),
		LogFormat:     getEnv("MGR_LOG_FORMAT", "console"),
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func getEnvAny(keys []string, fallback string) string {
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok && value != "" {
			return value
		}
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

func getEnvBoolAny(keys []string, fallback bool) bool {
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok && value != "" {
			parsed, err := strconv.ParseBool(value)
			if err == nil {
				return parsed
			}
		}
	}
	return fallback
}
