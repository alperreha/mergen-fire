package images

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alperreha/mergen-fire/internal/config"
)

type Service struct {
	cfg    config.Config
	local  *LocalStore
	logger *slog.Logger
}

func NewService(cfg config.Config, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		cfg:    cfg,
		local:  NewLocalStore(cfg.DataRoot, cfg.BaseAssetsDir, cfg.ImagesRoot, cfg.ProgressEvery),
		logger: logger,
	}
}

func (s *Service) EnsureLayout() error {
	return s.local.EnsureLayout()
}

func (s *Service) ListLocal(_ context.Context) (LocalCatalog, error) {
	return s.local.ListLocal()
}

func (s *Service) PushBase(ctx context.Context, ref BaseRef, setLatest bool, reporter ProgressReporter) (SyncSummary, error) {
	if err := s.local.EnsureLayout(); err != nil {
		return SyncSummary{}, err
	}
	remote, err := NewS3Store(ctx, s.cfg)
	if err != nil {
		return SyncSummary{}, err
	}

	manifest := remote.NewBaseManifest(ref)
	files := []struct {
		name       string
		localPath  string
		remoteName string
		required   bool
	}{
		{name: "vmlinux", localPath: filepath.Join(s.cfg.BaseAssetsDir, "vmlinux"), remoteName: "vmlinux.bin", required: true},
		{name: "golden-rootfs.ext4", localPath: filepath.Join(s.cfg.BaseAssetsDir, "golden-rootfs.ext4"), remoteName: ref.Flavor + "-disk.ext4", required: true},
		{name: "agent-rootfs.ext4", localPath: filepath.Join(s.cfg.BaseAssetsDir, "agent-rootfs.ext4"), remoteName: "agent-disk.ext4", required: true},
		{name: "env-rootfs.ext4", localPath: filepath.Join(s.cfg.BaseAssetsDir, "env-rootfs.ext4"), remoteName: "env-disk.ext4", required: false},
		{name: "bin/sbin-init", localPath: filepath.Join(s.cfg.BaseAssetsDir, "bin", "sbin-init"), remoteName: "bin/sbin-init", required: false},
		{name: "bin/mergen-agent", localPath: filepath.Join(s.cfg.BaseAssetsDir, "bin", "mergen-agent"), remoteName: "bin/mergen-agent", required: false},
	}

	transferred := make([]ManifestFile, 0, len(files))
	for _, file := range files {
		if _, err := os.Stat(file.localPath); err != nil {
			if file.required {
				return SyncSummary{}, fmt.Errorf("base artifact missing: %s", file.localPath)
			}
			continue
		}
		remoteKey := remote.BaseFileKey(ref, file.remoteName)
		desc, err := remote.UploadFile(ctx, file.localPath, remoteKey, reporter, ProgressUpdate{
			Kind:      ArtifactKindBase,
			Name:      ref.Name(),
			FileName:  file.name,
			Direction: "push",
		})
		if err != nil {
			return SyncSummary{}, err
		}
		transferred = append(transferred, desc)
	}

	manifest.Files = transferred
	manifestKey := remote.BaseManifestKey(ref)
	if err := remote.PutManifest(ctx, manifestKey, manifest); err != nil {
		return SyncSummary{}, err
	}
	if setLatest && ref.Version != defaultBaseVersion {
		latestRef := ref.LatestAlias()
		latestFiles := make([]ManifestFile, 0, len(transferred))
		for _, file := range transferred {
			latestKey := remote.BaseFileKey(latestRef, remoteBaseFileName(ref, file.Name))
			if err := remote.CopyObject(ctx, file.Key, latestKey); err != nil {
				return SyncSummary{}, err
			}
			latestFiles = append(latestFiles, ManifestFile{
				Name:      file.Name,
				Key:       latestKey,
				SizeBytes: file.SizeBytes,
				SHA256:    file.SHA256,
			})
		}
		latestManifest := remote.NewBaseManifest(latestRef)
		latestManifest.Version = ref.Version
		latestManifest.Files = latestFiles
		if err := remote.PutManifest(ctx, remote.BaseManifestKey(latestRef), latestManifest); err != nil {
			return SyncSummary{}, err
		}
	}
	if err := writeLocalBaseManifest(s.cfg.BaseAssetsDir, manifest); err != nil {
		return SyncSummary{}, err
	}
	summary := SyncSummary{
		Kind:        ArtifactKindBase,
		Name:        ref.Name(),
		Bucket:      s.cfg.ConfigS3Bucket,
		Prefix:      remote.BasePrefix(ref),
		ManifestKey: manifestKey,
		LocalDir:    s.cfg.BaseAssetsDir,
		Platform:    ref.Platform,
		Flavor:      ref.Flavor,
		Version:     ref.Version,
		Transferred: transferred,
		CompletedAt: time.Now().UTC(),
	}
	return summary, nil
}

func (s *Service) PullBase(ctx context.Context, ref BaseRef, reporter ProgressReporter) (SyncSummary, error) {
	if err := s.local.EnsureLayout(); err != nil {
		return SyncSummary{}, err
	}
	remote, err := NewS3Store(ctx, s.cfg)
	if err != nil {
		return SyncSummary{}, err
	}

	manifestKey := remote.BaseManifestKey(ref)
	manifest, err := remote.GetManifest(ctx, manifestKey)
	if err != nil {
		return SyncSummary{}, err
	}

	transferred := make([]ManifestFile, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		localPath := filepath.Join(s.cfg.BaseAssetsDir, filepath.FromSlash(file.Name))
		desc, err := remote.DownloadFile(ctx, file.Key, localPath, reporter, ProgressUpdate{
			Kind:      ArtifactKindBase,
			Name:      manifest.Name,
			FileName:  file.Name,
			Direction: "pull",
		})
		if err != nil {
			return SyncSummary{}, err
		}
		transferred = append(transferred, desc)
	}
	if err := writeLocalBaseManifest(s.cfg.BaseAssetsDir, manifest); err != nil {
		return SyncSummary{}, err
	}

	return SyncSummary{
		Kind:        ArtifactKindBase,
		Name:        manifest.Name,
		Bucket:      s.cfg.ConfigS3Bucket,
		Prefix:      remote.BasePrefix(ref),
		ManifestKey: manifestKey,
		LocalDir:    s.cfg.BaseAssetsDir,
		Platform:    manifest.Platform,
		Flavor:      manifest.Flavor,
		Version:     manifest.Version,
		Transferred: transferred,
		CompletedAt: time.Now().UTC(),
	}, nil
}

func (s *Service) PushPayload(ctx context.Context, imageRef string, reporter ProgressReporter) (SyncSummary, error) {
	if err := s.local.EnsureLayout(); err != nil {
		return SyncSummary{}, err
	}
	remote, err := NewS3Store(ctx, s.cfg)
	if err != nil {
		return SyncSummary{}, err
	}

	payloadDir, err := s.local.ResolvePayloadDir(imageRef)
	if err != nil {
		return SyncSummary{}, err
	}

	files := []struct {
		name      string
		localPath string
		remoteKey string
		required  bool
	}{
		{name: payloadRootFSFile, localPath: filepath.Join(payloadDir, payloadRootFSFile), remoteKey: remote.PayloadFileKey(imageRef, payloadRootFSFile), required: true},
		{name: imageMetaFile, localPath: filepath.Join(payloadDir, imageMetaFile), remoteKey: remote.PayloadFileKey(imageRef, imageMetaFile), required: false},
		{name: runtimeMetaFile, localPath: filepath.Join(payloadDir, runtimeMetaFile), remoteKey: remote.PayloadFileKey(imageRef, runtimeMetaFile), required: false},
		{name: suggestedVMFile, localPath: filepath.Join(payloadDir, suggestedVMFile), remoteKey: remote.PayloadFileKey(imageRef, suggestedVMFile), required: false},
	}

	manifest := remote.NewManifest(ArtifactKindPayload, imageRef)
	transferred := make([]ManifestFile, 0, len(files))
	for _, file := range files {
		if _, err := os.Stat(file.localPath); err != nil {
			if file.required {
				return SyncSummary{}, fmt.Errorf("payload artifact missing: %s", file.localPath)
			}
			continue
		}
		desc, err := remote.UploadFile(ctx, file.localPath, file.remoteKey, reporter, ProgressUpdate{
			Kind:      ArtifactKindPayload,
			Name:      imageRef,
			FileName:  file.name,
			Direction: "push",
		})
		if err != nil {
			return SyncSummary{}, err
		}
		transferred = append(transferred, desc)
	}

	manifest.Files = transferred
	manifestKey := remote.PayloadManifestKey(imageRef)
	if err := remote.PutManifest(ctx, manifestKey, manifest); err != nil {
		return SyncSummary{}, err
	}

	return SyncSummary{
		Kind:        ArtifactKindPayload,
		Name:        imageRef,
		Bucket:      s.cfg.ConfigS3Bucket,
		Prefix:      remote.joinKey(remote.prefix, remote.username, "payload", escapeRef(imageRef)),
		ManifestKey: manifestKey,
		LocalDir:    payloadDir,
		Transferred: transferred,
		CompletedAt: time.Now().UTC(),
	}, nil
}

func (s *Service) PullPayload(ctx context.Context, imageRef string, reporter ProgressReporter) (SyncSummary, error) {
	if err := s.local.EnsureLayout(); err != nil {
		return SyncSummary{}, err
	}
	remote, err := NewS3Store(ctx, s.cfg)
	if err != nil {
		return SyncSummary{}, err
	}

	payloadDir, err := s.local.ResolvePayloadDir(imageRef)
	if err != nil {
		return SyncSummary{}, err
	}
	manifestKey := remote.PayloadManifestKey(imageRef)
	manifest, err := remote.GetManifest(ctx, manifestKey)
	if err != nil {
		return SyncSummary{}, err
	}

	transferred := make([]ManifestFile, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		localPath := filepath.Join(payloadDir, filepath.FromSlash(file.Name))
		desc, err := remote.DownloadFile(ctx, file.Key, localPath, reporter, ProgressUpdate{
			Kind:      ArtifactKindPayload,
			Name:      imageRef,
			FileName:  file.Name,
			Direction: "pull",
		})
		if err != nil {
			return SyncSummary{}, err
		}
		transferred = append(transferred, desc)
	}

	return SyncSummary{
		Kind:        ArtifactKindPayload,
		Name:        imageRef,
		Bucket:      s.cfg.ConfigS3Bucket,
		Prefix:      remote.joinKey(remote.prefix, remote.username, "payload", escapeRef(imageRef)),
		ManifestKey: manifestKey,
		LocalDir:    payloadDir,
		Transferred: transferred,
		CompletedAt: time.Now().UTC(),
	}, nil
}

func FormatSummary(summary SyncSummary) string {
	var builder strings.Builder
	_, _ = fmt.Fprintf(&builder, "kind: %s\n", summary.Kind)
	_, _ = fmt.Fprintf(&builder, "name: %s\n", summary.Name)
	_, _ = fmt.Fprintf(&builder, "bucket: %s\n", summary.Bucket)
	_, _ = fmt.Fprintf(&builder, "prefix: %s\n", summary.Prefix)
	_, _ = fmt.Fprintf(&builder, "manifest: %s\n", summary.ManifestKey)
	if strings.TrimSpace(summary.Platform) != "" {
		_, _ = fmt.Fprintf(&builder, "platform: %s\n", summary.Platform)
	}
	if strings.TrimSpace(summary.Flavor) != "" {
		_, _ = fmt.Fprintf(&builder, "flavor: %s\n", summary.Flavor)
	}
	if strings.TrimSpace(summary.Version) != "" {
		_, _ = fmt.Fprintf(&builder, "version: %s\n", summary.Version)
	}
	_, _ = fmt.Fprintf(&builder, "local dir: %s\n", summary.LocalDir)
	_, _ = fmt.Fprintf(&builder, "files: %d\n", len(summary.Transferred))
	for _, file := range summary.Transferred {
		_, _ = fmt.Fprintf(&builder, "  - %s (%d bytes)\n", file.Name, file.SizeBytes)
	}
	return strings.TrimRight(builder.String(), "\n")
}

func remoteBaseFileName(ref BaseRef, localName string) string {
	switch localName {
	case "vmlinux":
		return "vmlinux.bin"
	case "golden-rootfs.ext4":
		return ref.Flavor + "-disk.ext4"
	case "agent-rootfs.ext4":
		return "agent-disk.ext4"
	case "env-rootfs.ext4":
		return "env-disk.ext4"
	default:
		return filepath.ToSlash(localName)
	}
}

func writeLocalBaseManifest(baseDir string, manifest Manifest) error {
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal local base manifest: %w", err)
	}
	body = append(body, '\n')
	path := filepath.Join(baseDir, manifestFile)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write local base manifest %s: %w", path, err)
	}
	return nil
}
