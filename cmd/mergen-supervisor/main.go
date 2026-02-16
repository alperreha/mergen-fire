//go:build linux

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	defaultRuntimePath      = "/etc/mergen/mergen.runtime.json"
	defaultPayloadDevice    = "/dev/vdb"
	defaultPayloadFSType    = "ext4"
	defaultPayloadMountPath = "/mnt/payload"
	defaultEnvDevice        = "/dev/vdc"
	defaultEnvFSType        = "ext4"
	defaultEnvMountPath     = "/mnt/env"
	defaultEnvFile          = "mergen.env"
	defaultPathEnv          = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	defaultTailBytes        = 64 * 1024
	defaultFileTailBytes    = 256 * 1024
	defaultFileTailLines    = 60
)

var defaultPayloadLogPaths = []string{
	"/var/log/messages",
	"/var/log/syslog",
	"/var/log/nginx/error.log",
	"/var/log/apache2/error.log",
	"/usr/local/apache2/logs/error_log",
	"/var/log/httpd/error_log",
}

type runtimeSpec struct {
	Image             string   `json:"image"`
	BootArgs          string   `json:"bootArgs,omitempty"`
	HTTPPort          int      `json:"httpPort,omitempty"`
	Entrypoint        []string `json:"entrypoint,omitempty"`
	Cmd               []string `json:"cmd,omitempty"`
	StartCmd          []string `json:"startCmd,omitempty"`
	Env               []string `json:"env,omitempty"`
	WorkingDir        string   `json:"workingDir,omitempty"`
	User              string   `json:"user,omitempty"`
	PayloadDevice     string   `json:"payloadDevice,omitempty"`
	PayloadFSType     string   `json:"payloadFSType,omitempty"`
	PayloadMountPoint string   `json:"payloadMountPoint,omitempty"`
	PayloadReadOnly   bool     `json:"payloadReadOnly,omitempty"`
	EnvDevice         string   `json:"envDevice,omitempty"`
	EnvFSType         string   `json:"envFSType,omitempty"`
	EnvMountPoint     string   `json:"envMountPoint,omitempty"`
	EnvReadOnly       bool     `json:"envReadOnly,omitempty"`
	EnvFile           string   `json:"envFile,omitempty"`
	LogPaths          []string `json:"logPaths,omitempty"`
}

type payloadExitError struct {
	ExitCode    int
	Diagnostics string
}

func (e *payloadExitError) Error() string {
	return fmt.Sprintf("payload exited with code %d", e.ExitCode)
}

type tailBuffer struct {
	mu     sync.Mutex
	maxLen int
	buf    []byte
}

func newTailBuffer(maxLen int) *tailBuffer {
	if maxLen <= 0 {
		maxLen = defaultTailBytes
	}
	return &tailBuffer{maxLen: maxLen}
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.buf = append(b.buf, p...)
	if len(b.buf) > b.maxLen {
		b.buf = b.buf[len(b.buf)-b.maxLen:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	runtimePath := strings.TrimSpace(os.Getenv("MERGEN_RUNTIME_PATH"))
	if runtimePath == "" {
		runtimePath = defaultRuntimePath
	}

	spec, err := loadRuntimeSpec(runtimePath)
	if err != nil {
		logger.Error("load runtime spec failed", "path", runtimePath, "error", err)
		os.Exit(1)
	}

	if err := mountPayload(spec); err != nil {
		logger.Error("mount payload failed", "device", spec.PayloadDevice, "mount", spec.PayloadMountPoint, "error", err)
		os.Exit(1)
	}
	if err := mountEnvDisk(spec); err != nil {
		logger.Error("mount env disk failed", "device", spec.EnvDevice, "mount", spec.EnvMountPoint, "error", err)
		os.Exit(1)
	}

	env, err := buildRuntimeEnv(spec)
	if err != nil {
		logger.Error("build runtime env failed", "error", err)
		os.Exit(1)
	}

	argv := composeStartCommand(spec.StartCmd, spec.Entrypoint, spec.Cmd)
	execPath, err := resolveExecutableInPayload(spec.PayloadMountPoint, argv[0], env["PATH"], spec.WorkingDir)
	if err != nil {
		logger.Error("resolve executable failed", "argv0", argv[0], "error", err)
		os.Exit(1)
	}
	argv[0] = execPath

	logger.Info(
		"starting payload process",
		"image", spec.Image,
		"exec", execPath,
		"argv", strings.Join(argv, " "),
		"workDir", normalizeWorkingDir(spec.WorkingDir),
		"payloadMount", spec.PayloadMountPoint,
		"envDisk", spec.EnvDevice,
	)

	exitCode, err := runPayload(spec, argv, env)
	if err != nil {
		var payloadErr *payloadExitError
		if errors.As(err, &payloadErr) {
			logger.Error("payload process exited with non-zero status", "exitCode", payloadErr.ExitCode)
			if strings.TrimSpace(payloadErr.Diagnostics) != "" {
				_, _ = fmt.Fprintf(os.Stderr, "payload diagnostics (exit=%d):\n%s\n", payloadErr.ExitCode, payloadErr.Diagnostics)
			}
			os.Exit(payloadErr.ExitCode)
		}
		logger.Error("payload runtime failed", "error", err)
		os.Exit(1)
	}
	logger.Info("payload process exited", "exitCode", exitCode)
	os.Exit(exitCode)
}

func loadRuntimeSpec(path string) (runtimeSpec, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return runtimeSpec{}, fmt.Errorf("read runtime file: %w", err)
	}

	var spec runtimeSpec
	if err := json.Unmarshal(body, &spec); err != nil {
		return runtimeSpec{}, fmt.Errorf("decode runtime file: %w", err)
	}

	spec.PayloadDevice = defaultIfEmpty(spec.PayloadDevice, defaultPayloadDevice)
	spec.PayloadFSType = defaultIfEmpty(spec.PayloadFSType, defaultPayloadFSType)
	spec.PayloadMountPoint = defaultIfEmpty(spec.PayloadMountPoint, defaultPayloadMountPath)

	spec.EnvDevice = defaultIfEmpty(spec.EnvDevice, defaultEnvDevice)
	spec.EnvFSType = defaultIfEmpty(spec.EnvFSType, defaultEnvFSType)
	spec.EnvMountPoint = defaultIfEmpty(spec.EnvMountPoint, defaultEnvMountPath)
	if !spec.EnvReadOnly {
		spec.EnvReadOnly = true
	}

	spec.EnvFile = strings.TrimSpace(spec.EnvFile)
	if spec.EnvFile == "" {
		spec.EnvFile = filepath.Join(spec.EnvMountPoint, defaultEnvFile)
	}

	return spec, nil
}

func mountPayload(spec runtimeSpec) error {
	return mountDisk(spec.PayloadDevice, spec.PayloadMountPoint, spec.PayloadFSType, spec.PayloadReadOnly)
}

func mountEnvDisk(spec runtimeSpec) error {
	if strings.TrimSpace(spec.EnvDevice) == "" {
		return nil
	}
	if _, err := os.Stat(spec.EnvDevice); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return mountDisk(spec.EnvDevice, spec.EnvMountPoint, spec.EnvFSType, spec.EnvReadOnly)
}

func mountDisk(device, mountPoint, fsType string, readOnly bool) error {
	if strings.TrimSpace(device) == "" {
		return errors.New("empty device")
	}
	if err := os.MkdirAll(mountPoint, 0o755); err != nil {
		return fmt.Errorf("prepare mount point: %w", err)
	}

	flags := uintptr(unix.MS_RELATIME)
	if readOnly {
		flags |= uintptr(unix.MS_RDONLY)
	}
	if err := unix.Mount(device, mountPoint, fsType, flags, ""); err != nil {
		if errors.Is(err, unix.EBUSY) {
			return nil
		}
		return err
	}
	return nil
}

func buildRuntimeEnv(spec runtimeSpec) (map[string]string, error) {
	env := envFromList(os.Environ())
	mergeMap(env, envFromList(spec.Env))
	mergeMap(env, envFromFile(spec.EnvFile))
	if strings.TrimSpace(env["PATH"]) == "" {
		env["PATH"] = defaultPathEnv
	}
	return env, nil
}

func envFromFile(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if key == "" {
			continue
		}
		out[key] = parts[1]
	}
	return out
}

func envFromList(values []string) map[string]string {
	out := make(map[string]string, len(values))
	for _, item := range values {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if key == "" {
			continue
		}
		out[key] = parts[1]
	}
	return out
}

func mergeMap(dst, src map[string]string) {
	for k, v := range src {
		dst[k] = v
	}
}

func composeStartCommand(startCmd, entrypoint, cmd []string) []string {
	argv := append([]string(nil), startCmd...)
	if len(argv) == 0 {
		argv = append(argv, entrypoint...)
		argv = append(argv, cmd...)
	}
	if len(argv) == 0 {
		return []string{"/bin/sh"}
	}
	return argv
}

func resolveExecutableInPayload(payloadRoot, argv0, pathEnv, workingDir string) (string, error) {
	argv0 = strings.TrimSpace(argv0)
	if argv0 == "" {
		return "", errors.New("empty argv0")
	}

	if strings.Contains(argv0, "/") {
		if strings.HasPrefix(argv0, "/") {
			if executableInPayload(payloadRoot, argv0) {
				return filepath.Clean(argv0), nil
			}
			return "", fmt.Errorf("executable not found in payload: %s", argv0)
		}

		workDir := normalizeWorkingDir(workingDir)
		abs := filepath.Clean(filepath.Join(workDir, argv0))
		if !strings.HasPrefix(abs, "/") {
			abs = "/" + abs
		}
		if executableInPayload(payloadRoot, abs) {
			return abs, nil
		}
		return "", fmt.Errorf("executable not found in payload: %s", abs)
	}

	searchPath := strings.TrimSpace(pathEnv)
	if searchPath == "" {
		searchPath = defaultPathEnv
	}
	for _, dir := range strings.Split(searchPath, ":") {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if !strings.HasPrefix(dir, "/") {
			dir = "/" + dir
		}
		candidate := filepath.Clean(filepath.Join(dir, argv0))
		if executableInPayload(payloadRoot, candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("cannot resolve %q in payload PATH=%q", argv0, searchPath)
}

func executableInPayload(payloadRoot, absPath string) bool {
	hostPath := filepath.Join(payloadRoot, strings.TrimPrefix(absPath, "/"))
	info, err := os.Stat(hostPath)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

func runPayload(spec runtimeSpec, argv []string, env map[string]string) (int, error) {
	stdioTail := newTailBuffer(readPositiveIntEnv("MERGEN_SUPERVISOR_STDIO_TAIL_BYTES", defaultTailBytes))
	combinedOut := io.MultiWriter(os.Stdout, stdioTail)

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = combinedOut
	cmd.Stderr = combinedOut
	cmd.Stdin = os.Stdin
	cmd.Dir = normalizeWorkingDir(spec.WorkingDir)
	cmd.Env = envMapToList(env)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Chroot:  spec.PayloadMountPoint,
	}

	if uid, gid, ok := parseCredential(spec.User); ok {
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uid, Gid: gid}
	}

	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("start payload: %w", err)
	}

	sigCh := make(chan os.Signal, 16)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU, syscall.SIGCONT)
	defer signal.Stop(sigCh)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	for {
		select {
		case sig := <-sigCh:
			sysSig, ok := sig.(syscall.Signal)
			if !ok {
				continue
			}
			_ = syscall.Kill(-cmd.Process.Pid, sysSig)
		case err := <-done:
			if err == nil {
				return 0, nil
			}
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
					if status.Exited() {
						exitCode := status.ExitStatus()
						diagnostics := buildCrashDiagnostics(spec, stdioTail.String())
						return exitCode, &payloadExitError{
							ExitCode:    exitCode,
							Diagnostics: diagnostics,
						}
					}
					if status.Signaled() {
						exitCode := 128 + int(status.Signal())
						diagnostics := buildCrashDiagnostics(spec, stdioTail.String())
						return exitCode, &payloadExitError{
							ExitCode:    exitCode,
							Diagnostics: diagnostics,
						}
					}
				}
			}
			return 1, err
		}
	}
}

func buildCrashDiagnostics(spec runtimeSpec, recentOutput string) string {
	sections := make([]string, 0, 2)

	recentOutput = strings.TrimSpace(recentOutput)
	if recentOutput != "" {
		sections = append(sections, "recent stdout/stderr:\n"+recentOutput)
	}

	fileDiagnostics := collectPayloadFileLogTails(
		spec.PayloadMountPoint,
		spec.LogPaths,
		readPositiveIntEnv("MERGEN_SUPERVISOR_FILE_TAIL_BYTES", defaultFileTailBytes),
		readPositiveIntEnv("MERGEN_SUPERVISOR_FILE_TAIL_LINES", defaultFileTailLines),
	)
	fileDiagnostics = strings.TrimSpace(fileDiagnostics)
	if fileDiagnostics != "" {
		sections = append(sections, "known payload log files:\n"+fileDiagnostics)
	}

	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func collectPayloadFileLogTails(payloadRoot string, extraPaths []string, maxBytes, maxLines int) string {
	candidates := mergeLogPaths(defaultPayloadLogPaths, extraPaths)
	var out strings.Builder

	for _, guestPath := range candidates {
		hostPath := filepath.Join(payloadRoot, strings.TrimPrefix(guestPath, "/"))
		info, err := os.Stat(hostPath)
		if err != nil || info.IsDir() {
			continue
		}

		tail, err := readFileTailLines(hostPath, maxBytes, maxLines)
		if err != nil {
			continue
		}
		tail = strings.TrimSpace(tail)
		if tail == "" {
			continue
		}

		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(guestPath)
		out.WriteString(":\n")
		out.WriteString(tail)
	}

	return out.String()
}

func mergeLogPaths(base, extra []string) []string {
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))

	appendPath := func(raw string) {
		path := strings.TrimSpace(raw)
		if path == "" {
			return
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		path = filepath.Clean(path)
		if path == "/" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}

	for _, path := range base {
		appendPath(path)
	}
	for _, path := range extra {
		appendPath(path)
	}
	return out
}

func readFileTailLines(path string, maxBytes, maxLines int) (string, error) {
	if maxBytes <= 0 {
		maxBytes = defaultFileTailBytes
	}
	if maxLines <= 0 {
		maxLines = defaultFileTailLines
	}

	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}

	var start int64
	if info.Size() > int64(maxBytes) {
		start = info.Size() - int64(maxBytes)
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return "", err
	}

	body, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}

	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	if start > 0 {
		if idx := strings.IndexByte(text, '\n'); idx >= 0 {
			text = text[idx+1:]
		}
	}

	lines := strings.Split(text, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), nil
}

func readPositiveIntEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func parseCredential(spec string) (uid uint32, gid uint32, ok bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, 0, false
	}
	parts := strings.SplitN(spec, ":", 2)
	uid64, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 32)
	if err != nil {
		return 0, 0, false
	}
	gid64 := uid64
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		parsedGID, gidErr := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 32)
		if gidErr != nil {
			return 0, 0, false
		}
		gid64 = parsedGID
	}
	return uint32(uid64), uint32(gid64), true
}

func normalizeWorkingDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "/"
	}
	if !strings.HasPrefix(dir, "/") {
		dir = "/" + dir
	}
	return filepath.Clean(dir)
}

func envMapToList(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

func defaultIfEmpty(in, fallback string) string {
	if strings.TrimSpace(in) == "" {
		return fallback
	}
	return strings.TrimSpace(in)
}
