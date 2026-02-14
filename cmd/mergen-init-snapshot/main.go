package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	defaultMetaPath   = "/etc/mergen/image-meta.json"
	defaultFlyRunPath = "/fly/run.json"
)

func main() {
	logger := newLogger()
	if os.Getpid() != 1 {
		logger.Warn("mergen-init-snapshot is expected to run as PID 1", "pid", os.Getpid())
	}

	exitCode, err := run(logger)
	if err != nil {
		logger.Error("init failed", "error", err)
		os.Exit(1)
	}
	os.Exit(exitCode)
}

func run(logger *slog.Logger) (int, error) {
	if err := setupBaseMounts(logger); err != nil {
		return 1, err
	}

	spec, source, err := loadStartSpec()
	if err != nil {
		return 1, err
	}
	logger.Info("startup config loaded", "source", source, "argv", strings.Join(spec.Argv, " "), "user", spec.User, "workDir", spec.WorkingDir)

	if err := applyRuntimeSetup(spec, logger); err != nil {
		return 1, err
	}

	code, err := runAndSupervise(spec, logger)
	if err != nil {
		return 1, err
	}
	return code, nil
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MERGEN_INIT_LOG_LEVEL"))) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

type imageMeta struct {
	Image      string   `json:"image"`
	Entrypoint []string `json:"entrypoint"`
	Cmd        []string `json:"cmd"`
	StartCmd   []string `json:"startCmd"`
	Env        []string `json:"env"`
	WorkingDir string   `json:"workingDir"`
	User       string   `json:"user"`
}

type flyRunConfig struct {
	ImageConfig  *flyImageConfig   `json:"ImageConfig"`
	ExecOverride []string          `json:"ExecOverride"`
	ExtraEnv     map[string]string `json:"ExtraEnv"`
	UserOverride string            `json:"UserOverride"`
	CmdOverride  string            `json:"CmdOverride"`
	IPConfigs    []flyIPConfig     `json:"IPConfigs"`
	TTY          bool              `json:"TTY"`
	Hostname     string            `json:"Hostname"`
	Mounts       []flyMount        `json:"Mounts"`
	EtcResolv    *flyEtcResolv     `json:"EtcResolv"`
	EtcHosts     []flyEtcHost      `json:"EtcHosts"`
	RootDevice   string            `json:"RootDevice"`
}

type flyImageConfig struct {
	Entrypoint []string `json:"Entrypoint"`
	Cmd        []string `json:"Cmd"`
	Env        []string `json:"Env"`
	WorkingDir string   `json:"WorkingDir"`
	User       string   `json:"User"`
}

type flyIPConfig struct {
	Gateway string `json:"Gateway"`
	IP      string `json:"IP"`
	Mask    uint8  `json:"Mask"`
}

type flyMount struct {
	MountPath  string `json:"MountPath"`
	DevicePath string `json:"DevicePath"`
}

type flyEtcHost struct {
	Host string `json:"Host"`
	IP   string `json:"IP"`
	Desc string `json:"Desc"`
}

type flyEtcResolv struct {
	Nameservers []string `json:"Nameservers"`
}

type startSpec struct {
	Argv       []string
	Env        map[string]string
	User       string
	WorkingDir string
	Hostname   string
	IPConfigs  []flyIPConfig
	Mounts     []flyMount
	EtcHosts   []flyEtcHost
	EtcResolv  *flyEtcResolv
}

func loadStartSpec() (startSpec, string, error) {
	metaPath := resolveMetaPath(defaultMetaPath)
	if fileExists(metaPath) {
		meta, err := loadImageMeta(metaPath)
		if err != nil {
			return startSpec{}, "", fmt.Errorf("read metadata %s: %w", metaPath, err)
		}
		return buildSpecFromMeta(meta), metaPath, nil
	}

	if fileExists(defaultFlyRunPath) {
		cfg, err := loadFlyRunConfig(defaultFlyRunPath)
		if err != nil {
			return startSpec{}, "", fmt.Errorf("read fly run config %s: %w", defaultFlyRunPath, err)
		}
		spec := buildSpecFromFlyConfig(cfg)
		return spec, defaultFlyRunPath, nil
	}

	return startSpec{}, "", fmt.Errorf("no startup metadata found at %s or %s", metaPath, defaultFlyRunPath)
}

func loadImageMeta(path string) (imageMeta, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return imageMeta{}, err
	}
	var meta imageMeta
	if err := json.Unmarshal(body, &meta); err != nil {
		return imageMeta{}, err
	}
	return meta, nil
}

func loadFlyRunConfig(path string) (flyRunConfig, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return flyRunConfig{}, err
	}
	var cfg flyRunConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return flyRunConfig{}, err
	}
	return cfg, nil
}

func buildSpecFromMeta(meta imageMeta) startSpec {
	argv := cloneSlice(meta.StartCmd)
	if len(argv) == 0 {
		argv = append(argv, meta.Entrypoint...)
		argv = append(argv, meta.Cmd...)
	}
	if len(argv) == 0 {
		argv = []string{"/bin/sh"}
	}
	userSpec := strings.TrimSpace(meta.User)
	if userSpec == "" {
		userSpec = "root"
	}
	return startSpec{
		Argv:       argv,
		Env:        parseEnvList(meta.Env),
		User:       userSpec,
		WorkingDir: strings.TrimSpace(meta.WorkingDir),
	}
}

func buildSpecFromFlyConfig(cfg flyRunConfig) startSpec {
	image := flyImageConfig{}
	if cfg.ImageConfig != nil {
		image = *cfg.ImageConfig
	}

	argv := cloneSlice(cfg.ExecOverride)
	if len(argv) == 0 {
		argv = append(argv, image.Entrypoint...)
		if strings.TrimSpace(cfg.CmdOverride) != "" {
			argv = append(argv, strings.TrimSpace(cfg.CmdOverride))
		} else {
			argv = append(argv, image.Cmd...)
		}
	}
	if len(argv) == 0 {
		argv = []string{"/bin/sh"}
	}

	envMap := parseEnvList(image.Env)
	for k, v := range cfg.ExtraEnv {
		envMap[k] = v
	}

	userSpec := strings.TrimSpace(cfg.UserOverride)
	if userSpec == "" {
		userSpec = strings.TrimSpace(image.User)
	}
	if userSpec == "" {
		userSpec = "root"
	}

	return startSpec{
		Argv:       argv,
		Env:        envMap,
		User:       userSpec,
		WorkingDir: strings.TrimSpace(image.WorkingDir),
		Hostname:   strings.TrimSpace(cfg.Hostname),
		IPConfigs:  cloneIPConfigs(cfg.IPConfigs),
		Mounts:     cloneMounts(cfg.Mounts),
		EtcHosts:   cloneEtcHosts(cfg.EtcHosts),
		EtcResolv:  cloneEtcResolv(cfg.EtcResolv),
	}
}

func parseEnvList(envs []string) map[string]string {
	out := make(map[string]string, len(envs))
	for _, item := range envs {
		if item == "" {
			continue
		}
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

func resolveMetaPath(defaultPath string) string {
	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return defaultPath
	}
	if path := metadataPathFromCmdline(string(cmdline)); path != "" {
		return path
	}
	return defaultPath
}

func metadataPathFromCmdline(cmdline string) string {
	for _, field := range strings.Fields(cmdline) {
		if !strings.HasPrefix(field, "mergen.meta=") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(field, "mergen.meta="))
		if value != "" {
			return value
		}
	}
	return ""
}

func applyRuntimeSetup(spec startSpec, logger *slog.Logger) error {
	if spec.Hostname != "" {
		if err := unix.Sethostname([]byte(spec.Hostname)); err != nil {
			logger.Warn("sethostname failed", "hostname", spec.Hostname, "error", err)
		}
		if err := os.MkdirAll("/etc", 0o755); err == nil {
			_ = os.WriteFile("/etc/hostname", []byte(spec.Hostname+"\n"), 0o644)
		}
	}

	if spec.EtcResolv != nil && len(spec.EtcResolv.Nameservers) > 0 {
		if err := os.MkdirAll("/etc", 0o755); err != nil {
			return fmt.Errorf("prepare /etc for resolv.conf: %w", err)
		}
		var b strings.Builder
		for _, ns := range spec.EtcResolv.Nameservers {
			ns = strings.TrimSpace(ns)
			if ns == "" {
				continue
			}
			b.WriteString("nameserver ")
			b.WriteString(ns)
			b.WriteString("\n")
		}
		if b.Len() > 0 {
			if err := os.WriteFile("/etc/resolv.conf", []byte(b.String()), 0o644); err != nil {
				return fmt.Errorf("write /etc/resolv.conf: %w", err)
			}
		}
	}

	if len(spec.EtcHosts) > 0 {
		if err := os.MkdirAll("/etc", 0o755); err != nil {
			return fmt.Errorf("prepare /etc for hosts: %w", err)
		}
		f, err := os.OpenFile("/etc/hosts", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("open /etc/hosts: %w", err)
		}
		defer f.Close()
		for _, host := range spec.EtcHosts {
			ip := strings.TrimSpace(host.IP)
			h := strings.TrimSpace(host.Host)
			if ip == "" || h == "" {
				continue
			}
			if strings.TrimSpace(host.Desc) != "" {
				_, _ = fmt.Fprintf(f, "\n# %s\n", strings.TrimSpace(host.Desc))
			}
			_, _ = fmt.Fprintf(f, "%s\t%s\n", ip, h)
		}
	}

	for _, m := range spec.Mounts {
		if strings.TrimSpace(m.MountPath) == "" || strings.TrimSpace(m.DevicePath) == "" {
			continue
		}
		if err := os.MkdirAll(m.MountPath, 0o755); err != nil {
			return fmt.Errorf("prepare mount path %s: %w", m.MountPath, err)
		}
		if err := mountIfNeeded(m.DevicePath, m.MountPath, "ext4", uintptr(unix.MS_RELATIME), ""); err != nil {
			return fmt.Errorf("mount %s on %s: %w", m.DevicePath, m.MountPath, err)
		}
	}

	if err := bringLinkUp("lo"); err != nil {
		logger.Warn("bringing up lo failed", "error", err)
	}
	if err := bringLinkUp("eth0"); err != nil {
		logger.Warn("bringing up eth0 failed", "error", err)
	}
	if len(spec.IPConfigs) > 0 {
		if err := applyIPConfigs("eth0", spec.IPConfigs); err != nil {
			logger.Warn("applying IP configs failed", "error", err)
		}
	}

	return nil
}

func setupBaseMounts(logger *slog.Logger) error {
	if err := os.MkdirAll("/dev", 0o755); err != nil {
		return fmt.Errorf("prepare /dev: %w", err)
	}
	if err := mountIfNeeded("devtmpfs", "/dev", "devtmpfs", uintptr(unix.MS_NOSUID), "mode=0755"); err != nil {
		logger.Warn("mount /dev failed", "error", err)
	}

	if err := os.MkdirAll("/proc", 0o555); err != nil {
		return fmt.Errorf("prepare /proc: %w", err)
	}
	if err := mountIfNeeded("proc", "/proc", "proc", uintptr(unix.MS_NODEV|unix.MS_NOEXEC|unix.MS_NOSUID), ""); err != nil {
		logger.Warn("mount /proc failed", "error", err)
	}

	if err := os.MkdirAll("/sys", 0o555); err != nil {
		return fmt.Errorf("prepare /sys: %w", err)
	}
	if err := mountIfNeeded("sysfs", "/sys", "sysfs", uintptr(unix.MS_NODEV|unix.MS_NOEXEC|unix.MS_NOSUID), ""); err != nil {
		logger.Warn("mount /sys failed", "error", err)
	}

	if err := os.MkdirAll("/dev/pts", 0o755); err != nil {
		return fmt.Errorf("prepare /dev/pts: %w", err)
	}
	if err := mountIfNeeded("devpts", "/dev/pts", "devpts", uintptr(unix.MS_NOEXEC|unix.MS_NOSUID|unix.MS_NOATIME), "mode=0620,gid=5,ptmxmode=666"); err != nil {
		logger.Warn("mount /dev/pts failed", "error", err)
	}

	if err := os.MkdirAll("/dev/shm", 0o1777); err != nil {
		return fmt.Errorf("prepare /dev/shm: %w", err)
	}
	if err := mountIfNeeded("tmpfs", "/dev/shm", "tmpfs", uintptr(unix.MS_NOSUID|unix.MS_NODEV), "mode=1777"); err != nil {
		logger.Warn("mount /dev/shm failed", "error", err)
	}

	if err := os.MkdirAll("/run", 0o755); err != nil {
		return fmt.Errorf("prepare /run: %w", err)
	}
	if err := mountIfNeeded("tmpfs", "/run", "tmpfs", uintptr(unix.MS_NOSUID|unix.MS_NODEV), "mode=0755"); err != nil {
		logger.Warn("mount /run failed", "error", err)
	}

	if err := os.MkdirAll("/tmp", 0o1777); err != nil {
		return fmt.Errorf("prepare /tmp: %w", err)
	}
	if err := os.Chmod("/tmp", 0o1777); err != nil {
		logger.Warn("chmod /tmp failed", "error", err)
	}

	_ = ensureSymlink("/proc/self/fd", "/dev/fd")
	_ = ensureSymlink("/proc/self/fd/0", "/dev/stdin")
	_ = ensureSymlink("/proc/self/fd/1", "/dev/stdout")
	_ = ensureSymlink("/proc/self/fd/2", "/dev/stderr")

	return nil
}

func ensureSymlink(target, link string) error {
	if current, err := os.Readlink(link); err == nil {
		if current == target {
			return nil
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	}
	return os.Symlink(target, link)
}

func mountIfNeeded(source, target, fsType string, flags uintptr, data string) error {
	err := unix.Mount(source, target, fsType, flags, data)
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.EBUSY) {
		return nil
	}
	return err
}

func bringLinkUp(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return err
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return err
	}
	return nil
}

func applyIPConfigs(ifaceName string, cfgs []flyIPConfig) error {
	if len(cfgs) == 0 {
		return nil
	}
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return err
	}

	for _, cfg := range cfgs {
		ipNet, err := parseIPConfigAddress(cfg.IP, cfg.Mask)
		if err != nil {
			return fmt.Errorf("parse IP config address %q: %w", cfg.IP, err)
		}
		addr := &netlink.Addr{IPNet: ipNet}
		if err := netlink.AddrAdd(link, addr); err != nil && !errors.Is(err, syscall.EEXIST) {
			return fmt.Errorf("add address %s: %w", ipNet.String(), err)
		}

		gw, err := parseGatewayIP(cfg.Gateway)
		if err != nil {
			return fmt.Errorf("parse gateway %q: %w", cfg.Gateway, err)
		}
		if gw != nil {
			route := &netlink.Route{LinkIndex: link.Attrs().Index, Gw: gw}
			if err := netlink.RouteAdd(route); err != nil && !errors.Is(err, syscall.EEXIST) {
				return fmt.Errorf("add default route via %s: %w", gw.String(), err)
			}
		}
	}

	return nil
}

func parseIPConfigAddress(raw string, mask uint8) (*net.IPNet, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty IP")
	}
	if strings.Contains(raw, "/") {
		ip, ipNet, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, err
		}
		if mask > 0 {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			ipNet.Mask = net.CIDRMask(int(mask), bits)
		}
		ipNet.IP = ip
		return ipNet, nil
	}

	ip := net.ParseIP(raw)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP %q", raw)
	}
	bits := 32
	if ip.To4() == nil {
		bits = 128
	}
	if mask == 0 {
		if bits == 32 {
			mask = 32
		} else {
			mask = 128
		}
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(int(mask), bits)}, nil
}

func parseGatewayIP(raw string) (net.IP, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.Contains(raw, "/") {
		ip, _, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, err
		}
		return ip, nil
	}
	ip := net.ParseIP(raw)
	if ip == nil {
		return nil, fmt.Errorf("invalid gateway IP %q", raw)
	}
	return ip, nil
}

func runAndSupervise(spec startSpec, logger *slog.Logger) (int, error) {
	uid, gid, home, err := resolveUser(spec.User)
	if err != nil {
		return 1, err
	}

	if spec.Env == nil {
		spec.Env = make(map[string]string)
	}
	if strings.TrimSpace(spec.Env["HOME"]) == "" {
		spec.Env["HOME"] = home
	}
	if strings.TrimSpace(spec.Env["PATH"]) == "" {
		spec.Env["PATH"] = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}
	_ = os.Setenv("PATH", spec.Env["PATH"])

	if len(spec.Argv) == 0 {
		spec.Argv = []string{"/bin/sh"}
	}

	cmd, startedArgv, err := startMainProcess(spec, uid, gid, logger)
	if err != nil {
		return 1, err
	}

	mainPID := cmd.Process.Pid
	logger.Info("started main process", "pid", mainPID, "argv", strings.Join(startedArgv, " "))

	sigCh := make(chan os.Signal, 64)
	signal.Notify(
		sigCh,
		syscall.SIGCHLD,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGHUP,
		syscall.SIGQUIT,
		syscall.SIGUSR1,
		syscall.SIGUSR2,
		syscall.SIGWINCH,
		syscall.SIGCONT,
		syscall.SIGTSTP,
		syscall.SIGTTIN,
		syscall.SIGTTOU,
	)
	defer signal.Stop(sigCh)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		exited, exitCode, err := reapChildren(mainPID, logger)
		if err != nil {
			return 1, err
		}
		if exited {
			logger.Info("main process exited", "pid", mainPID, "exitCode", exitCode)
			return exitCode, nil
		}

		select {
		case sig := <-sigCh:
			sysSig, ok := sig.(syscall.Signal)
			if !ok {
				continue
			}
			if sysSig == syscall.SIGCHLD {
				continue
			}
			if err := forwardSignal(mainPID, sysSig); err != nil {
				logger.Warn("signal forwarding failed", "signal", sysSig, "error", err)
			}
		case <-ticker.C:
		}
	}
}

func startMainProcess(spec startSpec, uid, gid uint32, logger *slog.Logger) (*exec.Cmd, []string, error) {
	envList := envMapToList(spec.Env)
	candidates := commandCandidates(spec.Argv)
	var lastErr error

	for idx, argv := range candidates {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		cmd.Env = envList
		if spec.WorkingDir != "" {
			cmd.Dir = spec.WorkingDir
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
			Credential: &syscall.Credential{
				Uid: uid,
				Gid: gid,
			},
		}

		if err := cmd.Start(); err != nil {
			lastErr = err
			logger.Warn("start command attempt failed", "attempt", idx+1, "argv", strings.Join(argv, " "), "error", err)
			continue
		}
		return cmd, argv, nil
	}

	if lastErr == nil {
		lastErr = errors.New("no command candidates")
	}
	return nil, nil, fmt.Errorf("start command %q: %w", strings.Join(spec.Argv, " "), lastErr)
}

func commandCandidates(argv []string) [][]string {
	if len(argv) == 0 {
		argv = []string{"/bin/sh"}
	}

	out := make([][]string, 0, 2)
	primary := cloneSlice(argv)
	out = append(out, primary)

	shellLine := shellCommandLine(primary)
	if shellLine == "" {
		return out
	}
	fallback := []string{"/bin/sh", "-lc", shellLine}
	if !equalStringSlices(primary, fallback) {
		out = append(out, fallback)
	}
	return out
}

func shellCommandLine(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(raw string) string {
	if raw == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(raw, "'", `'"'"'`) + "'"
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func resolveUser(spec string) (uid uint32, gid uint32, home string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = "root"
	}

	parts := strings.SplitN(spec, ":", 2)
	userPart := strings.TrimSpace(parts[0])
	groupPart := ""
	if len(parts) == 2 {
		groupPart = strings.TrimSpace(parts[1])
	}

	uidVal, userInfo, err := resolveUserPart(userPart)
	if err != nil {
		return 0, 0, "", err
	}

	gidVal := uidVal
	homeDir := "/"
	if userInfo != nil {
		homeDir = userInfo.HomeDir
		if parsedGID, parseErr := parseUint32(userInfo.Gid); parseErr == nil {
			gidVal = parsedGID
		}
	}

	if groupPart != "" {
		resolvedGID, err := resolveGroupPart(groupPart)
		if err != nil {
			return 0, 0, "", err
		}
		gidVal = resolvedGID
	}

	if homeDir == "" {
		homeDir = "/"
	}
	return uidVal, gidVal, homeDir, nil
}

func resolveUserPart(value string) (uint32, *user.User, error) {
	if value == "" {
		value = "root"
	}
	if n, err := strconv.ParseUint(value, 10, 32); err == nil {
		u := uint32(n)
		info, lookupErr := user.LookupId(strconv.FormatUint(uint64(u), 10))
		if lookupErr != nil {
			return u, nil, nil
		}
		return u, info, nil
	}

	info, err := user.Lookup(value)
	if err != nil {
		return 0, nil, fmt.Errorf("lookup user %q: %w", value, err)
	}
	u, err := parseUint32(info.Uid)
	if err != nil {
		return 0, nil, fmt.Errorf("parse uid for user %q: %w", value, err)
	}
	return u, info, nil
}

func resolveGroupPart(value string) (uint32, error) {
	if value == "" {
		return 0, nil
	}
	if n, err := strconv.ParseUint(value, 10, 32); err == nil {
		return uint32(n), nil
	}
	group, err := user.LookupGroup(value)
	if err != nil {
		return 0, fmt.Errorf("lookup group %q: %w", value, err)
	}
	g, err := parseUint32(group.Gid)
	if err != nil {
		return 0, fmt.Errorf("parse gid for group %q: %w", value, err)
	}
	return g, nil
}

func parseUint32(raw string) (uint32, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(n), nil
}

func envMapToList(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

func forwardSignal(mainPID int, sig syscall.Signal) error {
	if sig == syscall.SIGCHLD {
		return nil
	}
	// Prefer signaling the whole process group created by Setpgid.
	if err := syscall.Kill(-mainPID, sig); err == nil {
		return nil
	} else if !errors.Is(err, syscall.ESRCH) {
		// Fall back to direct PID if group signaling fails unexpectedly.
		if directErr := syscall.Kill(mainPID, sig); directErr != nil && !errors.Is(directErr, syscall.ESRCH) {
			return directErr
		}
		return nil
	}
	return nil
}

func reapChildren(mainPID int, logger *slog.Logger) (bool, int, error) {
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			if errors.Is(err, syscall.ECHILD) {
				return false, 0, nil
			}
			return false, 0, fmt.Errorf("wait4: %w", err)
		}
		if pid == 0 {
			return false, 0, nil
		}

		exitCode := waitStatusToExitCode(status)
		if pid == mainPID {
			return true, exitCode, nil
		}
		logger.Debug("reaped child", "pid", pid, "exitCode", exitCode)
	}
}

func waitStatusToExitCode(status syscall.WaitStatus) int {
	switch {
	case status.Exited():
		return status.ExitStatus()
	case status.Signaled():
		return 128 + int(status.Signal())
	default:
		return 1
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func cloneSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneIPConfigs(in []flyIPConfig) []flyIPConfig {
	if len(in) == 0 {
		return nil
	}
	out := make([]flyIPConfig, len(in))
	copy(out, in)
	return out
}

func cloneMounts(in []flyMount) []flyMount {
	if len(in) == 0 {
		return nil
	}
	out := make([]flyMount, len(in))
	copy(out, in)
	return out
}

func cloneEtcHosts(in []flyEtcHost) []flyEtcHost {
	if len(in) == 0 {
		return nil
	}
	out := make([]flyEtcHost, len(in))
	copy(out, in)
	return out
}

func cloneEtcResolv(in *flyEtcResolv) *flyEtcResolv {
	if in == nil {
		return nil
	}
	out := &flyEtcResolv{Nameservers: make([]string, len(in.Nameservers))}
	copy(out.Nameservers, in.Nameservers)
	return out
}
