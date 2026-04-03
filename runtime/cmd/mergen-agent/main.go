//go:build linux

package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alperreha/mergen-fire/pkg/guestspec"
	"golang.org/x/sys/unix"
)

const (
	defaultRuntimePath      = "/mnt/env/mergen.runtime.json"
	defaultPayloadDevice    = "/dev/vdc"
	defaultPayloadFSType    = "ext4"
	defaultPayloadMountPath = "/mnt/payload"
	defaultEnvDevice        = "/dev/vdd"
	defaultEnvFSType        = "ext4"
	defaultEnvMountPath     = "/mnt/env"
	defaultEnvFile          = "mergen.env"
	defaultPathEnv          = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

	defaultTelemetryIntervalSeconds = 10
)

type runtimeSpec = guestspec.Runtime
type imageMetaSpec = guestspec.ImageMeta

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	runtimePath := strings.TrimSpace(os.Getenv("MERGEN_RUNTIME_PATH"))
	if runtimePath == "" {
		runtimePath = defaultRuntimePath
	}

	bootstrap := defaultRuntimeSpec()
	if err := mountEnvDisk(bootstrap); err != nil {
		logger.Error("mount env disk failed", "device", bootstrap.EnvDevice, "mount", bootstrap.EnvMountPoint, "error", err)
		os.Exit(1)
	}

	spec, specSource, err := resolveRuntimeSpec(runtimePath, bootstrap, logger)
	if err != nil {
		logger.Error("resolve runtime spec failed", "path", runtimePath, "error", err)
		os.Exit(1)
	}
	logger.Info(
		"runtime spec resolved",
		"source", specSource,
		"runtimePath", runtimePath,
		"envDevice", spec.EnvDevice,
	)

	stopTelemetry := startTelemetry(logger)
	defer stopTelemetry()

	if err := mountPayload(spec); err != nil {
		logger.Error("mount payload failed", "device", spec.PayloadDevice, "mount", spec.PayloadMountPoint, "error", err)
		os.Exit(1)
	}
	cleanupPayloadMounts, err := preparePayloadRuntimeMounts(spec, logger)
	if err != nil {
		logger.Error("prepare payload runtime mounts failed", "mount", spec.PayloadMountPoint, "error", err)
		os.Exit(1)
	}
	defer cleanupPayloadMounts()

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
		"specSource", specSource,
	)

	exitCode, err := runPayload(spec, argv, env)
	if err != nil {
		logger.Error("payload runtime failed", "error", err)
		os.Exit(1)
	}
	logger.Info("payload process exited", "exitCode", exitCode)
	os.Exit(exitCode)
}

func defaultRuntimeSpec() runtimeSpec {
	return runtimeSpec{
		PayloadDevice:     defaultPayloadDevice,
		PayloadFSType:     defaultPayloadFSType,
		PayloadMountPoint: defaultPayloadMountPath,
		EnvDevice:         defaultEnvDevice,
		EnvFSType:         defaultEnvFSType,
		EnvMountPoint:     defaultEnvMountPath,
		EnvReadOnly:       true,
		EnvFile:           filepath.Join(defaultEnvMountPath, defaultEnvFile),
	}
}

func loadRuntimeSpec(path string) (runtimeSpec, error) {
	spec, err := guestspec.ReadRuntime(path)
	if err != nil {
		return runtimeSpec{}, err
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

func resolveRuntimeSpec(runtimePath string, bootstrap runtimeSpec, logger *slog.Logger) (runtimeSpec, string, error) {
	spec, err := loadRuntimeSpec(runtimePath)
	if err == nil {
		return spec, "runtime-json", nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return runtimeSpec{}, "", err
	}

	if logger != nil {
		logger.Warn("runtime file not found, falling back to payload image metadata", "path", runtimePath)
	}

	if err := mountPayload(bootstrap); err != nil {
		return runtimeSpec{}, "", fmt.Errorf("mount payload for metadata fallback: %w", err)
	}

	metaPath := filepath.Join(bootstrap.PayloadMountPoint, "etc", "mergen", "image-meta.json")
	spec, err = loadRuntimeSpecFromImageMeta(metaPath, bootstrap)
	if err != nil {
		return runtimeSpec{}, "", err
	}
	return spec, "payload-image-meta", nil
}

func loadRuntimeSpecFromImageMeta(path string, bootstrap runtimeSpec) (runtimeSpec, error) {
	meta, err := guestspec.ReadImageMeta(path)
	if err != nil {
		return runtimeSpec{}, err
	}

	spec := bootstrap
	spec.Image = meta.Image
	spec.Entrypoint = append([]string(nil), meta.Entrypoint...)
	spec.Cmd = append([]string(nil), meta.Cmd...)
	spec.StartCmd = append([]string(nil), meta.StartCmd...)
	spec.Env = append([]string(nil), meta.Env...)
	spec.WorkingDir = strings.TrimSpace(meta.WorkingDir)
	spec.User = strings.TrimSpace(meta.User)
	spec.EnvDevice = ""
	spec.EnvFile = ""

	if len(spec.StartCmd) == 0 {
		spec.StartCmd = composeStartCommand(spec.StartCmd, spec.Entrypoint, spec.Cmd)
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

func preparePayloadRuntimeMounts(spec runtimeSpec, logger *slog.Logger) (func(), error) {
	devTarget := filepath.Join(spec.PayloadMountPoint, "dev")
	if err := os.MkdirAll(devTarget, 0o755); err != nil {
		return nil, fmt.Errorf("prepare payload /dev: %w", err)
	}

	if err := unix.Mount("/dev", devTarget, "", uintptr(unix.MS_BIND|unix.MS_REC), ""); err != nil {
		return nil, fmt.Errorf("bind mount /dev into payload: %w", err)
	}
	logger.Info("payload runtime /dev prepared", "source", "/dev", "target", devTarget)

	return func() {
		if err := unix.Unmount(devTarget, unix.MNT_DETACH); err != nil && !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.ENOENT) {
			logger.Warn("payload runtime /dev unmount failed", "target", devTarget, "error", err)
		}
	}, nil
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
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
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
						return status.ExitStatus(), nil
					}
					if status.Signaled() {
						return 128 + int(status.Signal()), nil
					}
				}
			}
			return 1, err
		}
	}
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

func boolFromEnv(key string) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch raw {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func startTelemetry(logger *slog.Logger) func() {
	interval := readTelemetryInterval()
	logger.Info("mergen agent telemetry started", "interval", interval.String())

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		publishTelemetry(logger)
		for {
			select {
			case <-ticker.C:
				publishTelemetry(logger)
			case <-stopCh:
				logger.Info("mergen agent telemetry stopped")
				return
			}
		}
	}()

	return func() {
		close(stopCh)
		<-doneCh
	}
}

func readTelemetryInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("MERGEN_AGENT_TELEMETRY_INTERVAL_SECONDS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("MERGEN_TELEMETRY_INTERVAL_SECONDS"))
	}
	if raw == "" {
		return defaultTelemetryIntervalSeconds * time.Second
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return defaultTelemetryIntervalSeconds * time.Second
	}
	return time.Duration(seconds) * time.Second
}

func publishTelemetry(logger *slog.Logger) {
	label := randomLabel(8)
	logger.Info(
		"mergen agent heartbeat",
		"time", time.Now().UTC().Format(time.RFC3339Nano),
		"label", label,
	)
}

func randomLabel(byteLen int) string {
	if byteLen <= 0 {
		byteLen = 8
	}
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "fallback-random"
	}
	return hex.EncodeToString(buf)
}
