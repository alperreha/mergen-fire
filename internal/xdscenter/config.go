package xdscenter

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr         string
	ConfigRoot       string
	NetNSRoot        string
	Domain           string
	ResolverCacheTTL time.Duration
	RequestTimeout   time.Duration
	ShutdownTimeout  time.Duration
	ConsulHTTPAddr   string
	ConsulHTTPToken  string
	ConsulKVPrefix   string
	LogLevel         string
	LogFormat        string
}

func FromEnv() (Config, error) {
	httpAddr, err := normalizeListenAddr(getEnv("XDS_HTTP_ADDR", ":18080"))
	if err != nil {
		return Config{}, err
	}

	domain := normalizeDomain(getEnv("XDS_DOMAIN", "localhost"))
	if domain == "" {
		return Config{}, fmt.Errorf("XDS_DOMAIN cannot be empty")
	}

	cfg := Config{
		HTTPAddr:         httpAddr,
		ConfigRoot:       getEnv("XDS_CONFIG_ROOT", "/var/lib/mergen/vm.d"),
		NetNSRoot:        getEnv("XDS_NETNS_ROOT", "/run/netns"),
		Domain:           domain,
		ResolverCacheTTL: time.Duration(getEnvInt("XDS_RESOLVER_CACHE_TTL_SECONDS", 5)) * time.Second,
		RequestTimeout:   time.Duration(getEnvInt("XDS_REQUEST_TIMEOUT_SECONDS", 10)) * time.Second,
		ShutdownTimeout:  time.Duration(getEnvInt("XDS_SHUTDOWN_TIMEOUT_SECONDS", 15)) * time.Second,
		ConsulHTTPAddr:   strings.TrimSpace(getEnv("XDS_CONSUL_HTTP_ADDR", "")),
		ConsulHTTPToken:  strings.TrimSpace(getEnv("XDS_CONSUL_HTTP_TOKEN", "")),
		ConsulKVPrefix:   strings.Trim(strings.TrimSpace(getEnv("XDS_CONSUL_KV_PREFIX", "mergen/xds/routes")), "/"),
		LogLevel:         getEnv("XDS_LOG_LEVEL", "info"),
		LogFormat:        getEnv("XDS_LOG_FORMAT", "console"),
	}

	if cfg.ResolverCacheTTL <= 0 {
		cfg.ResolverCacheTTL = 5 * time.Second
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 10 * time.Second
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = 15 * time.Second
	}

	return cfg, nil
}

func normalizeDomain(raw string) string {
	part := strings.ToLower(strings.TrimSpace(raw))
	part = strings.Trim(part, ".")
	return part
}

func normalizeListenAddr(raw string) (string, error) {
	addr := strings.TrimSpace(raw)
	if addr == "" {
		return "", fmt.Errorf("XDS_HTTP_ADDR cannot be empty")
	}
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid XDS_HTTP_ADDR %q: %w", raw, err)
	}
	parsedPort, err := strconv.Atoi(port)
	if err != nil || parsedPort <= 0 || parsedPort > 65535 {
		return "", fmt.Errorf("invalid port in XDS_HTTP_ADDR: %q", port)
	}
	return addr, nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
