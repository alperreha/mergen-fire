package forwarder

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ConfigRoot       string
	NetNSRoot        string
	CertFile         string
	KeyFile          string
	HTTPSAddr        string
	Domain           string
	LogLevel         string
	LogFormat        string
	DialTimeout      time.Duration
	ResolverCacheTTL time.Duration
	ShutdownTimeout  time.Duration
}

func FromEnv() (Config, error) {
	httpsAddr, err := normalizeListenAddr(getEnv("FWD_HTTPS_ADDR", ":443"))
	if err != nil {
		return Config{}, err
	}

	domain := normalizeDomain(getEnv("FWD_DOMAIN", "localhost"))
	if domain == "" {
		return Config{}, fmt.Errorf("FWD_DOMAIN cannot be empty")
	}

	defaultCertBase := domain

	cfg := Config{
		ConfigRoot:       getEnv("FWD_CONFIG_ROOT", "/var/lib/mergen/vm.d"),
		NetNSRoot:        getEnv("FWD_NETNS_ROOT", "/run/netns"),
		CertFile:         getEnv("FWD_TLS_CERT_FILE", "/var/lib/mergen/certs/wildcard."+defaultCertBase+".crt"),
		KeyFile:          getEnv("FWD_TLS_KEY_FILE", "/var/lib/mergen/certs/wildcard."+defaultCertBase+".key"),
		HTTPSAddr:        httpsAddr,
		Domain:           domain,
		LogLevel:         getEnv("FWD_LOG_LEVEL", "debug"),
		LogFormat:        getEnv("FWD_LOG_FORMAT", "console"),
		DialTimeout:      time.Duration(getEnvInt("FWD_DIAL_TIMEOUT_SECONDS", 5)) * time.Second,
		ResolverCacheTTL: time.Duration(getEnvInt("FWD_RESOLVER_CACHE_TTL_SECONDS", 5)) * time.Second,
		ShutdownTimeout:  time.Duration(getEnvInt("FWD_SHUTDOWN_TIMEOUT_SECONDS", 15)) * time.Second,
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
		return "", fmt.Errorf("FWD_HTTPS_ADDR cannot be empty")
	}
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid FWD_HTTPS_ADDR %q: %w", raw, err)
	}
	parsedPort, err := strconv.Atoi(port)
	if err != nil || parsedPort <= 0 || parsedPort > 65535 {
		return "", fmt.Errorf("invalid https listen port in FWD_HTTPS_ADDR: %q", port)
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
