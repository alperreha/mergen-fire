package converter

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultOutputBase     = "./artifacts/converter"
	defaultSbinInitPath   = "./artifacts/sbin-init/sbin-init"
	defaultBootArgs       = "console=ttyS0 reboot=k panic=1 pci=off init=/sbin/init mergen.meta=/etc/mergen/image-meta.json"
	defaultRootFSOverhead = 256
)

type Options struct {
	Image        string
	OutputDir    string
	Name         string
	SizeMiB      int
	SkipPull     bool
	SbinInitPath string
}

type Result struct {
	Image                 string
	OutputDir             string
	RootFSDir             string
	RootFSTarPath         string
	RootFSExt4Path        string
	MetadataPath          string
	SuggestedBootArgsPath string
	SuggestedVMPath       string
	StartCommand          []string
	SuggestedHTTPPort     int
	BootArgs              string
}

type Runner struct {
	logger *slog.Logger
}

func NewRunner(logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{logger: logger}
}

func (r *Runner) Run(ctx context.Context, opts Options) (Result, error) {
	normalized, err := normalizeOptions(opts)
	if err != nil {
		return Result{}, err
	}

	if err := ensureCommand("docker"); err != nil {
		return Result{}, err
	}
	if err := ensureCommand("truncate"); err != nil {
		return Result{}, err
	}
	if err := ensureCommand("mkfs.ext4"); err != nil {
		return Result{}, err
	}
	if err := ensureReadableFile(normalized.SbinInitPath); err != nil {
		return Result{}, err
	}

	r.logger.Info(
		"converter started",
		"image", normalized.Image,
		"outputDir", normalized.OutputDir,
		"skipPull", normalized.SkipPull,
	)

	if err := os.MkdirAll(normalized.OutputDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create output dir: %w", err)
	}

	rootfsDir := filepath.Join(normalized.OutputDir, "rootfs")
	if err := os.RemoveAll(rootfsDir); err != nil {
		return Result{}, fmt.Errorf("clean rootfs dir: %w", err)
	}
	if err := os.MkdirAll(rootfsDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create rootfs dir: %w", err)
	}

	if !normalized.SkipPull {
		r.logger.Info("pulling image", "image", normalized.Image)
		if _, err := runCommand(ctx, "docker", "pull", normalized.Image); err != nil {
			return Result{}, err
		}
	}

	cfg, err := inspectImageConfig(ctx, normalized.Image)
	if err != nil {
		return Result{}, err
	}
	startCmd := composeStartCommand(cfg.Entrypoint, cfg.Cmd)
	suggestedHTTPPort := inferHTTPPort(cfg.ExposedPorts)

	r.logger.Info("exporting image filesystem", "image", normalized.Image)
	containerID, err := createContainer(ctx, normalized.Image)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		_, _ = runCommand(context.Background(), "docker", "rm", "-f", containerID)
	}()

	if err := exportContainerFS(ctx, containerID, rootfsDir); err != nil {
		return Result{}, err
	}

	imageMeta := metadata{
		Image:             normalized.Image,
		CreatedAt:         time.Now().UTC(),
		Entrypoint:        cloneStrings(cfg.Entrypoint),
		Cmd:               cloneStrings(cfg.Cmd),
		StartCmd:          cloneStrings(startCmd),
		Env:               cloneStrings(cfg.Env),
		WorkingDir:        cfg.WorkingDir,
		User:              cfg.User,
		ExposedPorts:      exposedPortsList(cfg.ExposedPorts),
		SuggestedHTTPPort: suggestedHTTPPort,
	}

	if err := injectSbinInit(normalized.SbinInitPath, rootfsDir); err != nil {
		return Result{}, err
	}

	if err := writeMetadataFiles(rootfsDir, normalized.OutputDir, imageMeta); err != nil {
		return Result{}, err
	}

	rootfsTar := filepath.Join(normalized.OutputDir, "rootfs.tar")
	if err := createTarFromDir(rootfsDir, rootfsTar); err != nil {
		return Result{}, err
	}

	sizeMiB := normalized.SizeMiB
	if sizeMiB == 0 {
		rootfsBytes, err := directorySizeBytes(rootfsDir)
		if err != nil {
			return Result{}, err
		}
		sizeMiB = int((rootfsBytes+1024*1024-1)/(1024*1024)) + defaultRootFSOverhead
	}
	if sizeMiB <= 0 {
		return Result{}, errors.New("sizeMiB must be > 0")
	}

	rootfsExt4 := filepath.Join(normalized.OutputDir, "rootfs.ext4")
	if err := buildExt4(ctx, rootfsDir, rootfsExt4, sizeMiB); err != nil {
		return Result{}, err
	}

	bootArgsPath := filepath.Join(normalized.OutputDir, "suggested-bootargs.txt")
	if err := os.WriteFile(bootArgsPath, []byte(defaultBootArgs+"\n"), 0o644); err != nil {
		return Result{}, fmt.Errorf("write suggested boot args: %w", err)
	}

	suggestedVMPath := filepath.Join(normalized.OutputDir, "suggested-vm-request.json")
	if err := writeSuggestedVMRequest(suggestedVMPath, normalized.Image, rootfsExt4, suggestedHTTPPort); err != nil {
		return Result{}, err
	}

	result := Result{
		Image:                 normalized.Image,
		OutputDir:             normalized.OutputDir,
		RootFSDir:             rootfsDir,
		RootFSTarPath:         rootfsTar,
		RootFSExt4Path:        rootfsExt4,
		MetadataPath:          filepath.Join(normalized.OutputDir, "image-meta.json"),
		SuggestedBootArgsPath: bootArgsPath,
		SuggestedVMPath:       suggestedVMPath,
		StartCommand:          startCmd,
		SuggestedHTTPPort:     suggestedHTTPPort,
		BootArgs:              defaultBootArgs,
	}
	r.logger.Info(
		"converter completed",
		"image", result.Image,
		"outputDir", result.OutputDir,
		"rootfsExt4", result.RootFSExt4Path,
		"httpPort", result.SuggestedHTTPPort,
	)
	return result, nil
}

type normalizedOptions struct {
	Image        string
	OutputDir    string
	Name         string
	SizeMiB      int
	SkipPull     bool
	SbinInitPath string
}

func normalizeOptions(opts Options) (normalizedOptions, error) {
	image := strings.TrimSpace(opts.Image)
	if image == "" {
		return normalizedOptions{}, errors.New("image is required")
	}

	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = sanitizeName(image)
	}

	outputDir := strings.TrimSpace(opts.OutputDir)
	if outputDir == "" {
		outputDir = filepath.Join(defaultOutputBase, name)
	}

	sbinInitPath := strings.TrimSpace(opts.SbinInitPath)
	if sbinInitPath == "" {
		sbinInitPath = defaultSbinInitPath
	}

	if opts.SizeMiB < 0 {
		return normalizedOptions{}, fmt.Errorf("sizeMiB must be >= 0, got %d", opts.SizeMiB)
	}

	return normalizedOptions{
		Image:        image,
		OutputDir:    outputDir,
		Name:         name,
		SizeMiB:      opts.SizeMiB,
		SkipPull:     opts.SkipPull,
		SbinInitPath: sbinInitPath,
	}, nil
}

func sanitizeName(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '_', r == '-':
			b.WriteRune(r)
		case r == '/', r == ':', r == '@':
			b.WriteRune('-')
		}
	}

	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "image-rootfs"
	}
	return out
}

func ensureCommand(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("required command not found in PATH: %s", name)
	}
	return nil
}

func ensureReadableFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot open sbin init file %q: %w", path, err)
	}
	_ = file.Close()
	return nil
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err == nil {
		return output, nil
	}

	stderrText := strings.TrimSpace(stderr.String())
	if stderrText != "" {
		return nil, fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, stderrText)
	}
	return nil, fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
}

type dockerImageConfig struct {
	Entrypoint   []string            `json:"Entrypoint"`
	Cmd          []string            `json:"Cmd"`
	Env          []string            `json:"Env"`
	WorkingDir   string              `json:"WorkingDir"`
	User         string              `json:"User"`
	ExposedPorts map[string]struct{} `json:"ExposedPorts"`
}

func inspectImageConfig(ctx context.Context, image string) (dockerImageConfig, error) {
	out, err := runCommand(ctx, "docker", "image", "inspect", image, "--format", "{{json .Config}}")
	if err != nil {
		return dockerImageConfig{}, err
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "null" {
		return dockerImageConfig{}, fmt.Errorf("docker inspect returned empty config for image %q", image)
	}

	var cfg dockerImageConfig
	if err := json.Unmarshal([]byte(trimmed), &cfg); err != nil {
		return dockerImageConfig{}, fmt.Errorf("decode docker image config: %w", err)
	}
	return cfg, nil
}

func composeStartCommand(entrypoint, cmd []string) []string {
	joined := make([]string, 0, len(entrypoint)+len(cmd))
	joined = append(joined, entrypoint...)
	joined = append(joined, cmd...)
	if len(joined) == 0 {
		return []string{"/bin/sh"}
	}
	return joined
}

func createContainer(ctx context.Context, image string) (string, error) {
	out, err := runCommand(ctx, "docker", "create", image)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return "", errors.New("docker create returned empty container id")
	}
	return id, nil
}

func exportContainerFS(ctx context.Context, containerID, rootfsDir string) error {
	cmd := exec.CommandContext(ctx, "docker", "export", containerID)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("docker export stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start docker export: %w", err)
	}

	extractErr := extractTarStream(stdout, rootfsDir)
	waitErr := cmd.Wait()
	if extractErr != nil {
		return extractErr
	}
	if waitErr != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			return fmt.Errorf("docker export %s failed: %w: %s", containerID, waitErr, stderrText)
		}
		return fmt.Errorf("docker export %s failed: %w", containerID, waitErr)
	}
	return nil
}

func extractTarStream(reader io.Reader, dest string) error {
	tr := tar.NewReader(reader)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar stream: %w", err)
		}

		targetPath, err := secureJoin(dest, hdr.Name)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, fs.FileMode(hdr.Mode)); err != nil {
				return fmt.Errorf("create dir %s: %w", targetPath, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create parent dir for %s: %w", targetPath, err)
			}
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fs.FileMode(hdr.Mode))
			if err != nil {
				return fmt.Errorf("create file %s: %w", targetPath, err)
			}
			if _, err := io.Copy(file, tr); err != nil {
				_ = file.Close()
				return fmt.Errorf("write file %s: %w", targetPath, err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("close file %s: %w", targetPath, err)
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create parent dir for symlink %s: %w", targetPath, err)
			}
			_ = os.Remove(targetPath)
			if err := os.Symlink(hdr.Linkname, targetPath); err != nil {
				return fmt.Errorf("create symlink %s -> %s: %w", targetPath, hdr.Linkname, err)
			}
		case tar.TypeLink:
			linkTarget, err := secureJoin(dest, hdr.Linkname)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create parent dir for hardlink %s: %w", targetPath, err)
			}
			_ = os.Remove(targetPath)
			if err := os.Link(linkTarget, targetPath); err != nil {
				return fmt.Errorf("create hardlink %s -> %s: %w", targetPath, linkTarget, err)
			}
		default:
			// Skip device/special files while keeping extraction resilient on non-root hosts.
		}
	}
}

func secureJoin(base, rel string) (string, error) {
	clean := filepath.Clean(rel)
	if clean == "." {
		return base, nil
	}
	if filepath.IsAbs(clean) {
		clean = strings.TrimPrefix(clean, string(filepath.Separator))
	}
	target := filepath.Join(base, clean)
	if !strings.HasPrefix(target, filepath.Clean(base)+string(filepath.Separator)) && target != filepath.Clean(base) {
		return "", fmt.Errorf("tar entry escapes destination: %q", rel)
	}
	return target, nil
}

type metadata struct {
	Image             string    `json:"image"`
	CreatedAt         time.Time `json:"createdAt"`
	Entrypoint        []string  `json:"entrypoint"`
	Cmd               []string  `json:"cmd"`
	StartCmd          []string  `json:"startCmd"`
	Env               []string  `json:"env"`
	WorkingDir        string    `json:"workingDir"`
	User              string    `json:"user"`
	ExposedPorts      []string  `json:"exposedPorts"`
	SuggestedHTTPPort int       `json:"suggestedHTTPPort,omitempty"`
}

func writeMetadataFiles(rootfsDir, outputDir string, meta metadata) error {
	body, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("encode image metadata: %w", err)
	}
	body = append(body, '\n')

	rootMetaDir := filepath.Join(rootfsDir, "etc", "mergen")
	if err := os.MkdirAll(rootMetaDir, 0o755); err != nil {
		return fmt.Errorf("create metadata dir in rootfs: %w", err)
	}

	rootMetaPath := filepath.Join(rootMetaDir, "image-meta.json")
	if err := os.WriteFile(rootMetaPath, body, 0o644); err != nil {
		return fmt.Errorf("write rootfs metadata: %w", err)
	}

	outputMetaPath := filepath.Join(outputDir, "image-meta.json")
	if err := os.WriteFile(outputMetaPath, body, 0o644); err != nil {
		return fmt.Errorf("write output metadata: %w", err)
	}
	return nil
}

func injectSbinInit(hostPath, rootfsDir string) error {
	content, err := os.ReadFile(hostPath)
	if err != nil {
		return fmt.Errorf("read sbin init file: %w", err)
	}

	targetSbinDir := filepath.Join(rootfsDir, "sbin")
	if err := os.MkdirAll(targetSbinDir, 0o755); err != nil {
		return fmt.Errorf("create /sbin dir: %w", err)
	}
	targetSbinInit := filepath.Join(targetSbinDir, "init")
	if err := os.WriteFile(targetSbinInit, content, 0o755); err != nil {
		return fmt.Errorf("write /sbin/init: %w", err)
	}

	targetSbinCopy := filepath.Join(targetSbinDir, "mergen-init")
	if err := os.WriteFile(targetSbinCopy, content, 0o755); err != nil {
		return fmt.Errorf("write /sbin/mergen-init: %w", err)
	}
	return nil
}

func createTarFromDir(srcDir, tarPath string) error {
	out, err := os.Create(tarPath)
	if err != nil {
		return fmt.Errorf("create tar file: %w", err)
	}
	defer out.Close()

	tw := tar.NewWriter(out)
	defer tw.Close()

	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == srcDir {
			return nil
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		info, err := d.Info()
		if err != nil {
			return err
		}

		var linkTarget string
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}

		hdr, err := tar.FileInfoHeader(info, linkTarget)
		if err != nil {
			return err
		}
		hdr.Name = rel
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, file)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}

func directorySizeBytes(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("calculate directory size: %w", err)
	}
	return total, nil
}

func buildExt4(ctx context.Context, rootfsDir, ext4Path string, sizeMiB int) error {
	if _, err := runCommand(ctx, "truncate", "-s", fmt.Sprintf("%dM", sizeMiB), ext4Path); err != nil {
		return err
	}
	if _, err := runCommand(ctx, "mkfs.ext4", "-q", "-F", "-d", rootfsDir, ext4Path); err != nil {
		return err
	}
	return nil
}

func exposedPortsList(in map[string]struct{}) []string {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func inferHTTPPort(exposed map[string]struct{}) int {
	if len(exposed) == 0 {
		return 0
	}

	type candidate struct {
		port int
		tcp  bool
	}
	candidates := make([]candidate, 0, len(exposed))
	for key := range exposed {
		parts := strings.Split(key, "/")
		if len(parts) != 2 {
			continue
		}
		port, err := strconv.Atoi(parts[0])
		if err != nil || port <= 0 || port > 65535 {
			continue
		}
		candidates = append(candidates, candidate{port: port, tcp: strings.EqualFold(parts[1], "tcp")})
	}

	if len(candidates) == 0 {
		return 0
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].tcp == candidates[j].tcp {
			return candidates[i].port < candidates[j].port
		}
		return candidates[i].tcp
	})

	for _, c := range candidates {
		if c.tcp && (c.port == 80 || c.port == 8080 || c.port == 3000 || c.port == 8000) {
			return c.port
		}
	}
	for _, c := range candidates {
		if c.tcp {
			return c.port
		}
	}
	return candidates[0].port
}

func writeSuggestedVMRequest(path, image, rootfsExt4 string, httpPort int) error {
	if httpPort <= 0 {
		httpPort = 80
	}

	payload := map[string]any{
		"rootfs":   rootfsExt4,
		"kernel":   "/var/lib/mergen/base/vmlinux",
		"vcpu":     1,
		"memMiB":   512,
		"httpPort": httpPort,
		"ports": []map[string]any{
			{
				"guest": httpPort,
				"host":  0,
			},
		},
		"bootArgs": defaultBootArgs,
		"metadata": map[string]any{
			"image": image,
		},
	}

	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode suggested vm request: %w", err)
	}
	body = append(body, '\n')

	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write suggested vm request: %w", err)
	}
	return nil
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
