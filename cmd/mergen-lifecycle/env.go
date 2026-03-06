package main

import (
	"bufio"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultConfigRoot = "/var/lib/mergen/vm.d"
	defaultRunRoot    = "/run/mergen"
	defaultSocketName = "mergen.socket"
	defaultVMJSONName = "vm.json"
	defaultMetaName   = "meta.json"
	defaultEnvName    = "env"
)

type vmRuntimePaths struct {
	vmDir      string
	runDir     string
	socketPath string
	vmJSONPath string
	metaPath   string
}

func resolvePaths(vmID string) vmRuntimePaths {
	configRoot := getEnv("MGN_CONFIG_ROOT", defaultConfigRoot)
	vmDir := filepath.Join(configRoot, vmID)
	runDir := getEnv("MGN_RUN_DIR", filepath.Join(defaultRunRoot, vmID))

	return vmRuntimePaths{
		vmDir:      vmDir,
		runDir:     runDir,
		socketPath: getEnv("MGN_SOCKET_PATH", filepath.Join(runDir, defaultSocketName)),
		vmJSONPath: getEnv("MGN_VM_JSON", filepath.Join(vmDir, defaultVMJSONName)),
		metaPath:   getEnv("MGN_META_JSON", filepath.Join(vmDir, defaultMetaName)),
	}
}

func loadVMEnvFile(vmID string, logger *slog.Logger) {
	paths := resolvePaths(vmID)
	envPath := getEnv("MGN_ENV_PATH", filepath.Join(paths.vmDir, defaultEnvName))

	file, err := os.Open(envPath)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := parseEnvAssignment(line)
		if !ok {
			continue
		}
		_ = os.Setenv(key, value)
	}
	if err := scanner.Err(); err != nil && logger != nil {
		logger.Warn("failed to read vm env file", "path", envPath, "error", err)
	}
}

func parseEnvAssignment(line string) (string, string, bool) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}

	key := strings.TrimSpace(parts[0])
	if key == "" {
		return "", "", false
	}

	rawValue := strings.TrimSpace(parts[1])
	if strings.HasPrefix(rawValue, "'") && strings.HasSuffix(rawValue, "'") && len(rawValue) >= 2 {
		unquoted := rawValue[1 : len(rawValue)-1]
		unquoted = strings.ReplaceAll(unquoted, `'\''`, `'`)
		return key, unquoted, true
	}

	if strings.HasPrefix(rawValue, "\"") && strings.HasSuffix(rawValue, "\"") {
		unquoted, err := strconv.Unquote(rawValue)
		if err == nil {
			return key, unquoted, true
		}
	}

	return key, rawValue, true
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

func shortID(vmID string) string {
	if len(vmID) >= 8 {
		return vmID[:8]
	}
	return vmID
}
