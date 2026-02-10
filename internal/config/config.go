package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr       string
	ConfigRoot     string
	DataRoot       string
	RunRoot        string
	GlobalHooksDir string
	UnitPrefix     string
	SystemctlPath  string
	CommandTimeout time.Duration
	PortStart      int
	PortEnd        int
	GuestCIDR      string
	LogLevel       string
	LogFormat      string
}

func FromEnv() Config {
	return Config{
		HTTPAddr:       getEnv("MGR_HTTP_ADDR", ":8080"),
		ConfigRoot:     getEnv("MGR_CONFIG_ROOT", "/etc/firecracker/vm.d"),
		DataRoot:       getEnv("MGR_DATA_ROOT", "/var/lib/firecracker"),
		RunRoot:        getEnv("MGR_RUN_ROOT", "/run/firecracker"),
		GlobalHooksDir: getEnv("MGR_GLOBAL_HOOKS_DIR", "/etc/firecracker/hooks.d"),
		UnitPrefix:     getEnv("MGR_UNIT_PREFIX", "fc"),
		SystemctlPath:  getEnv("MGR_SYSTEMCTL_PATH", "systemctl"),
		CommandTimeout: time.Duration(getEnvInt("MGR_COMMAND_TIMEOUT_SECONDS", 10)) * time.Second,
		PortStart:      getEnvInt("MGR_PORT_START", 20000),
		PortEnd:        getEnvInt("MGR_PORT_END", 40000),
		GuestCIDR:      getEnv("MGR_GUEST_CIDR", "172.30.0.0/24"),
		LogLevel:       getEnv("MGR_LOG_LEVEL", "info"),
		LogFormat:      getEnv("MGR_LOG_FORMAT", "console"),
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
