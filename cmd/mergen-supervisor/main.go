//go:build linux

package main

import (
	"bufio"
	"encoding/json"
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
)

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
