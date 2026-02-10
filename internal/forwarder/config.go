package forwarder

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Listener struct {
	Addr      string
	GuestPort int
}

type Config struct {
	ConfigRoot        string
	CertFile          string
	KeyFile           string
	DomainPrefix      string
	DomainSuffix      string
	LogLevel          string
	LogFormat         string
	Listeners         []Listener
	DialTimeout       time.Duration
	ResolverCacheTTL  time.Duration
	AllowedGuestPorts map[int]struct{}
}

func FromEnv() (Config, error) {
	listeners, err := parseListeners(getEnv("FWD_LISTENERS", ":8443=8080,:9443=443,:10022=22"))
	if err != nil {
		return Config{}, err
	}

	allowedPorts, err := parseAllowedPorts(getEnv("FWD_ALLOWED_GUEST_PORTS", "22,8080,443"))
	if err != nil {
		return Config{}, err
	}

	domainPrefix := normalizeDomainPart(getEnv("FWD_DOMAIN_PREFIX", ""))
	domainSuffix := normalizeDomainPart(getEnv("FWD_DOMAIN_SUFFIX", "localhost"))
	if domainSuffix == "" {
		return Config{}, fmt.Errorf("FWD_DOMAIN_SUFFIX cannot be empty")
	}

	defaultCertBase := domainBase(domainPrefix, domainSuffix)

	cfg := Config{
		ConfigRoot:        getEnv("FWD_CONFIG_ROOT", "/etc/firecracker/vm.d"),
		CertFile:          getEnv("FWD_TLS_CERT_FILE", "/etc/mergen/certs/wildcard."+defaultCertBase+".crt"),
		KeyFile:           getEnv("FWD_TLS_KEY_FILE", "/etc/mergen/certs/wildcard."+defaultCertBase+".key"),
		DomainPrefix:      domainPrefix,
		DomainSuffix:      domainSuffix,
		LogLevel:          getEnv("FWD_LOG_LEVEL", "debug"),
		LogFormat:         getEnv("FWD_LOG_FORMAT", "console"),
		Listeners:         listeners,
		DialTimeout:       time.Duration(getEnvInt("FWD_DIAL_TIMEOUT_SECONDS", 5)) * time.Second,
		ResolverCacheTTL:  time.Duration(getEnvInt("FWD_RESOLVER_CACHE_TTL_SECONDS", 5)) * time.Second,
		AllowedGuestPorts: allowedPorts,
	}

	for _, listener := range cfg.Listeners {
		if _, ok := cfg.AllowedGuestPorts[listener.GuestPort]; !ok {
			return Config{}, fmt.Errorf("listener guest port is not allowed: %d", listener.GuestPort)
		}
	}

	return cfg, nil
}

func domainBase(prefix, suffix string) string {
	if prefix == "" {
		return suffix
	}
	return prefix + "." + suffix
}

func normalizeDomainPart(raw string) string {
	part := strings.ToLower(strings.TrimSpace(raw))
	part = strings.Trim(part, ".")
	return part
}

func parseListeners(raw string) ([]Listener, error) {
	parts := strings.Split(raw, ",")
	listeners := make([]Listener, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		pair := strings.SplitN(part, "=", 2)
		if len(pair) != 2 {
			return nil, fmt.Errorf("invalid listener format: %q", part)
		}

		addr := strings.TrimSpace(pair[0])
		if addr == "" {
			return nil, fmt.Errorf("listener address is empty in %q", part)
		}
		if !strings.Contains(addr, ":") {
			addr = ":" + addr
		}

		guestPort, err := strconv.Atoi(strings.TrimSpace(pair[1]))
		if err != nil || guestPort <= 0 || guestPort > 65535 {
			return nil, fmt.Errorf("invalid guest port in listener %q", part)
		}

		listeners = append(listeners, Listener{
			Addr:      addr,
			GuestPort: guestPort,
		})
	}

	if len(listeners) == 0 {
		return nil, fmt.Errorf("no listeners configured")
	}

	return listeners, nil
}

func parseAllowedPorts(raw string) (map[int]struct{}, error) {
	ports := map[int]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		port, err := strconv.Atoi(part)
		if err != nil || port <= 0 || port > 65535 {
			return nil, fmt.Errorf("invalid allowed guest port: %q", part)
		}
		ports[port] = struct{}{}
	}
	if len(ports) == 0 {
		return nil, fmt.Errorf("allowed guest ports list is empty")
	}
	return ports, nil
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
