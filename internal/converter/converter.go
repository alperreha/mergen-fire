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
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	digest "github.com/opencontainers/go-digest"
	dockertransport "go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/pkg/blobinfocache/none"
	"go.podman.io/image/v5/types"
	storagearchive "go.podman.io/storage/pkg/archive"
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

	cacheDir := filepath.Join(normalized.OutputDir, "image-cache")
	var pulled pulledImage
	if normalized.SkipPull {
		r.logger.Info("loading cached pulled image", "cacheDir", cacheDir)
		pulled, err = readCachedImage(cacheDir)
		if err != nil {
			return Result{}, err
		}
	} else {
		r.logger.Info("pulling image via containers/image docker transport", "image", normalized.Image, "cacheDir", cacheDir)
		pulled, err = pullAndCacheImage(ctx, normalized.Image, cacheDir)
		if err != nil {
			return Result{}, err
		}
	}

	startCmd := composeStartCommand(pulled.Config.Entrypoint, pulled.Config.Cmd)
	suggestedHTTPPort := inferHTTPPort(pulled.Config.ExposedPorts)

	if err := applyLayers(pulled.Layers, rootfsDir); err != nil {
		return Result{}, err
	}

	imageMeta := metadata{
		Image:             normalized.Image,
		CreatedAt:         time.Now().UTC(),
		Entrypoint:        cloneStrings(pulled.Config.Entrypoint),
		Cmd:               cloneStrings(pulled.Config.Cmd),
		StartCmd:          cloneStrings(startCmd),
		Env:               cloneStrings(pulled.Config.Env),
		WorkingDir:        pulled.Config.WorkingDir,
		User:              pulled.Config.User,
		ExposedPorts:      exposedPortsList(pulled.Config.ExposedPorts),
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

type imageRuntimeConfig struct {
	Entrypoint   []string
	Cmd          []string
	Env          []string
	WorkingDir   string
	User         string
	ExposedPorts map[string]struct{}
}

type layerFile struct {
	Digest digest.Digest
	Path   string
}

type pulledImage struct {
	Config imageRuntimeConfig
	Layers []layerFile
}

type configBlob struct {
	Config struct {
		Entrypoint   []string            `json:"Entrypoint"`
		Cmd          []string            `json:"Cmd"`
		Env          []string            `json:"Env"`
		WorkingDir   string              `json:"WorkingDir"`
		User         string              `json:"User"`
		ExposedPorts map[string]struct{} `json:"ExposedPorts"`
	} `json:"config"`
}

type platformSpec struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Variant      string `json:"variant,omitempty"`
}

type manifestDescriptor struct {
	MediaType string        `json:"mediaType"`
	Digest    string        `json:"digest"`
	Size      int64         `json:"size"`
	URLs      []string      `json:"urls,omitempty"`
	Platform  *platformSpec `json:"platform,omitempty"`
}

type manifestList struct {
	MediaType string               `json:"mediaType"`
	Manifests []manifestDescriptor `json:"manifests"`
}

type imageManifest struct {
	MediaType string               `json:"mediaType"`
	Config    manifestDescriptor   `json:"config"`
	Layers    []manifestDescriptor `json:"layers"`
}

func pullAndCacheImage(ctx context.Context, image, cacheDir string) (pulledImage, error) {
	if err := os.RemoveAll(cacheDir); err != nil {
		return pulledImage{}, fmt.Errorf("clean image cache dir: %w", err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return pulledImage{}, fmt.Errorf("create image cache dir: %w", err)
	}

	ref, err := dockertransport.ParseReference(normalizedDockerReference(image))
	if err != nil {
		return pulledImage{}, fmt.Errorf("parse docker image reference: %w", err)
	}

	src, err := ref.NewImageSource(ctx, &types.SystemContext{})
	if err != nil {
		return pulledImage{}, fmt.Errorf("open image source: %w", err)
	}
	defer src.Close()

	manifestBytes, manifestMIME, err := resolveSingleManifest(ctx, src)
	if err != nil {
		return pulledImage{}, err
	}
	_ = manifestMIME

	parsedManifest, err := parseImageManifest(manifestBytes)
	if err != nil {
		return pulledImage{}, err
	}

	configDigest, err := parseDigest(parsedManifest.Config.Digest)
	if err != nil {
		return pulledImage{}, fmt.Errorf("invalid config digest: %w", err)
	}
	configBytes, err := downloadBlobToBytes(ctx, src, types.BlobInfo{
		Digest:    configDigest,
		Size:      parsedManifest.Config.Size,
		MediaType: parsedManifest.Config.MediaType,
		URLs:      cloneStrings(parsedManifest.Config.URLs),
	})
	if err != nil {
		return pulledImage{}, fmt.Errorf("download config blob: %w", err)
	}

	if err := os.WriteFile(filepath.Join(cacheDir, "manifest.json"), manifestBytes, 0o644); err != nil {
		return pulledImage{}, fmt.Errorf("write cached manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "config.json"), configBytes, 0o644); err != nil {
		return pulledImage{}, fmt.Errorf("write cached config: %w", err)
	}

	var cfgBlob configBlob
	if err := json.Unmarshal(configBytes, &cfgBlob); err != nil {
		return pulledImage{}, fmt.Errorf("decode image config blob: %w", err)
	}

	layers := make([]layerFile, 0, len(parsedManifest.Layers))
	for idx, layer := range parsedManifest.Layers {
		layerDigest, err := parseDigest(layer.Digest)
		if err != nil {
			return pulledImage{}, fmt.Errorf("invalid layer digest at index %d: %w", idx, err)
		}

		layerPath := cachedLayerPath(cacheDir, layerDigest)
		layerInfo := types.BlobInfo{
			Digest:    layerDigest,
			Size:      layer.Size,
			MediaType: layer.MediaType,
			URLs:      cloneStrings(layer.URLs),
		}
		if err := downloadBlobToFile(ctx, src, layerInfo, layerPath); err != nil {
			return pulledImage{}, fmt.Errorf("download layer %d (%s): %w", idx, layerDigest.String(), err)
		}
		layers = append(layers, layerFile{Digest: layerDigest, Path: layerPath})
	}

	return pulledImage{
		Config: imageRuntimeConfig{
			Entrypoint:   cloneStrings(cfgBlob.Config.Entrypoint),
			Cmd:          cloneStrings(cfgBlob.Config.Cmd),
			Env:          cloneStrings(cfgBlob.Config.Env),
			WorkingDir:   cfgBlob.Config.WorkingDir,
			User:         cfgBlob.Config.User,
			ExposedPorts: clonePorts(cfgBlob.Config.ExposedPorts),
		},
		Layers: layers,
	}, nil
}

func readCachedImage(cacheDir string) (pulledImage, error) {
	manifestPath := filepath.Join(cacheDir, "manifest.json")
	configPath := filepath.Join(cacheDir, "config.json")

	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return pulledImage{}, fmt.Errorf("read cached manifest: %w", err)
	}
	parsedManifest, err := parseImageManifest(manifestBytes)
	if err != nil {
		return pulledImage{}, err
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return pulledImage{}, fmt.Errorf("read cached config: %w", err)
	}
	var cfgBlob configBlob
	if err := json.Unmarshal(configBytes, &cfgBlob); err != nil {
		return pulledImage{}, fmt.Errorf("decode cached config blob: %w", err)
	}

	layers := make([]layerFile, 0, len(parsedManifest.Layers))
	for idx, layer := range parsedManifest.Layers {
		layerDigest, err := parseDigest(layer.Digest)
		if err != nil {
			return pulledImage{}, fmt.Errorf("invalid cached layer digest at index %d: %w", idx, err)
		}
		layerPath := cachedLayerPath(cacheDir, layerDigest)
		if _, err := os.Stat(layerPath); err != nil {
			return pulledImage{}, fmt.Errorf("cached layer missing for digest %s: %w", layerDigest.String(), err)
		}
		layers = append(layers, layerFile{Digest: layerDigest, Path: layerPath})
	}

	return pulledImage{
		Config: imageRuntimeConfig{
			Entrypoint:   cloneStrings(cfgBlob.Config.Entrypoint),
			Cmd:          cloneStrings(cfgBlob.Config.Cmd),
			Env:          cloneStrings(cfgBlob.Config.Env),
			WorkingDir:   cfgBlob.Config.WorkingDir,
			User:         cfgBlob.Config.User,
			ExposedPorts: clonePorts(cfgBlob.Config.ExposedPorts),
		},
		Layers: layers,
	}, nil
}

func normalizedDockerReference(image string) string {
	trimmed := strings.TrimSpace(image)
	trimmed = strings.TrimPrefix(trimmed, "docker://")
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return "//" + trimmed
}

func resolveSingleManifest(ctx context.Context, src types.ImageSource) ([]byte, string, error) {
	manifestBytes, manifestMIME, err := src.GetManifest(ctx, nil)
	if err != nil {
		return nil, "", fmt.Errorf("read image manifest: %w", err)
	}

	for depth := 0; depth < 4; depth++ {
		list, isList := parseManifestList(manifestBytes)
		if !isList {
			return manifestBytes, manifestMIME, nil
		}
		if len(list.Manifests) == 0 {
			return nil, "", errors.New("image manifest list is empty")
		}

		descriptor, err := selectManifestDescriptor(list.Manifests)
		if err != nil {
			return nil, "", err
		}
		instanceDigest, err := parseDigest(descriptor.Digest)
		if err != nil {
			return nil, "", fmt.Errorf("invalid selected manifest digest: %w", err)
		}

		manifestBytes, manifestMIME, err = src.GetManifest(ctx, &instanceDigest)
		if err != nil {
			return nil, "", fmt.Errorf("read selected image manifest %s: %w", instanceDigest.String(), err)
		}
	}

	return nil, "", errors.New("manifest list nesting is too deep")
}

func parseManifestList(manifestBytes []byte) (manifestList, bool) {
	var list manifestList
	if err := json.Unmarshal(manifestBytes, &list); err != nil {
		return manifestList{}, false
	}
	if len(list.Manifests) == 0 {
		return manifestList{}, false
	}
	return list, true
}

func parseImageManifest(manifestBytes []byte) (imageManifest, error) {
	if list, ok := parseManifestList(manifestBytes); ok {
		return imageManifest{}, fmt.Errorf("resolved manifest is still an index/list (%d manifests)", len(list.Manifests))
	}

	var m imageManifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return imageManifest{}, fmt.Errorf("decode image manifest: %w", err)
	}
	if len(m.Layers) == 0 {
		return imageManifest{}, errors.New("image manifest contains no layers")
	}
	if strings.TrimSpace(m.Config.Digest) == "" {
		return imageManifest{}, errors.New("image manifest config digest is empty")
	}
	return m, nil
}

func selectManifestDescriptor(manifests []manifestDescriptor) (manifestDescriptor, error) {
	if len(manifests) == 0 {
		return manifestDescriptor{}, errors.New("manifest list has no entries")
	}

	targetOS := runtime.GOOS
	targetArch := runtime.GOARCH

	for _, d := range manifests {
		if d.Platform == nil {
			continue
		}
		if strings.EqualFold(d.Platform.OS, targetOS) && strings.EqualFold(d.Platform.Architecture, targetArch) {
			return d, nil
		}
	}

	for _, d := range manifests {
		if d.Platform == nil {
			return d, nil
		}
	}

	return manifests[0], nil
}

func downloadBlobToBytes(ctx context.Context, src types.ImageSource, info types.BlobInfo) ([]byte, error) {
	reader, _, err := src.GetBlob(ctx, info, none.NoCache)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	payload, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	if err := verifyDigest(info.Digest, payload); err != nil {
		return nil, err
	}

	return payload, nil
}

func downloadBlobToFile(ctx context.Context, src types.ImageSource, info types.BlobInfo, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("create blob dir: %w", err)
	}

	reader, _, err := src.GetBlob(ctx, info, none.NoCache)
	if err != nil {
		return err
	}
	defer reader.Close()

	file, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("create blob file %s: %w", targetPath, err)
	}
	defer file.Close()

	digester := info.Digest.Algorithm().Digester()
	writer := io.MultiWriter(file, digester.Hash())
	if _, err := io.Copy(writer, reader); err != nil {
		return fmt.Errorf("write blob file %s: %w", targetPath, err)
	}

	if got := digester.Digest(); got != info.Digest {
		return fmt.Errorf("blob digest mismatch for %s: expected %s, got %s", targetPath, info.Digest.String(), got.String())
	}
	return nil
}

func verifyDigest(expected digest.Digest, payload []byte) error {
	if expected == "" {
		return nil
	}
	if err := expected.Validate(); err != nil {
		return fmt.Errorf("invalid digest %q: %w", expected.String(), err)
	}
	got := expected.Algorithm().FromBytes(payload)
	if got != expected {
		return fmt.Errorf("blob digest mismatch: expected %s, got %s", expected.String(), got.String())
	}
	return nil
}

func parseDigest(raw string) (digest.Digest, error) {
	d, err := digest.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if err := d.Validate(); err != nil {
		return "", err
	}
	return d, nil
}

func cachedLayerPath(cacheDir string, d digest.Digest) string {
	return filepath.Join(cacheDir, "layers", d.Algorithm().String(), d.Encoded()+".blob")
}

func applyLayers(layers []layerFile, rootfsDir string) error {
	if len(layers) == 0 {
		return errors.New("pulled image contains no layers")
	}

	for idx, layer := range layers {
		layerFileHandle, err := os.Open(layer.Path)
		if err != nil {
			return fmt.Errorf("open cached layer %s: %w", layer.Path, err)
		}

		_, applyErr := storagearchive.ApplyLayer(rootfsDir, layerFileHandle)
		closeErr := layerFileHandle.Close()
		if applyErr != nil {
			return fmt.Errorf("apply layer %d (%s): %w", idx, layer.Digest.String(), applyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close cached layer %s: %w", layer.Path, closeErr)
		}
	}
	return nil
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

func clonePorts(in map[string]struct{}) map[string]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(in))
	for key := range in {
		out[key] = struct{}{}
	}
	return out
}
