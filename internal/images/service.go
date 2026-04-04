package images

import (
	"context"
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

func (s *Service) PushBase(ctx context.Context, reporter ProgressReporter) (SyncSummary, error) {
	if err := s.local.EnsureLayout(); err != nil {
		return SyncSummary{}, err
	}
	remote, err := NewS3Store(ctx, s.cfg)
	if err != nil {
		return SyncSummary{}, err
	}

	manifest := remote.NewManifest(ArtifactKindBase, "current")
	files := []struct {
		name      string
		localPath string
		remoteKey string
	}{
		{name: "vmlinux", localPath: filepath.Join(s.cfg.BaseAssetsDir, "vmlinux"), remoteKey: remote.BaseFileKey("vmlinux")},
		{name: "golden-rootfs.ext4", localPath: filepath.Join(s.cfg.BaseAssetsDir, "golden-rootfs.ext4"), remoteKey: remote.BaseFileKey("golden-rootfs.ext4")},
		{name: "agent-rootfs.ext4", localPath: filepath.Join(s.cfg.BaseAssetsDir, "agent-rootfs.ext4"), remoteKey: remote.BaseFileKey("agent-rootfs.ext4")},
		{name: "bin/sbin-init", localPath: filepath.Join(s.cfg.BaseAssetsDir, "bin", "sbin-init"), remoteKey: remote.BaseFileKey("bin/sbin-init")},
		{name: "bin/mergen-agent", localPath: filepath.Join(s.cfg.BaseAssetsDir, "bin", "mergen-agent"), remoteKey: remote.BaseFileKey("bin/mergen-agent")},
	}

	transferred := make([]ManifestFile, 0, len(files))
	for _, file := range files {
		if _, err := os.Stat(file.localPath); err != nil {
			return SyncSummary{}, fmt.Errorf("base artifact missing: %s", file.localPath)
		}
		desc, err := remote.UploadFile(ctx, file.localPath, file.remoteKey, reporter, ProgressUpdate{
			Kind:      ArtifactKindBase,
			Name:      "current",
			FileName:  file.name,
			Direction: "push",
		})
		if err != nil {
			return SyncSummary{}, err
		}
		transferred = append(transferred, desc)
	}

	manifest.Files = transferred
	manifestKey := remote.BaseManifestKey()
	if err := remote.PutManifest(ctx, manifestKey, manifest); err != nil {
		return SyncSummary{}, err
	}
	summary := SyncSummary{
		Kind:        ArtifactKindBase,
		Name:        "current",
		Bucket:      s.cfg.S3Bucket,
		Prefix:      remote.joinKey(remote.prefix, remote.username, "base", "current"),
		ManifestKey: manifestKey,
		LocalDir:    s.cfg.BaseAssetsDir,
		Transferred: transferred,
		CompletedAt: time.Now().UTC(),
	}
	return summary, nil
}

func (s *Service) PullBase(ctx context.Context, reporter ProgressReporter) (SyncSummary, error) {
	if err := s.local.EnsureLayout(); err != nil {
		return SyncSummary{}, err
	}
	remote, err := NewS3Store(ctx, s.cfg)
	if err != nil {
		return SyncSummary{}, err
	}

	manifestKey := remote.BaseManifestKey()
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

	return SyncSummary{
		Kind:        ArtifactKindBase,
		Name:        manifest.Name,
		Bucket:      s.cfg.S3Bucket,
		Prefix:      remote.joinKey(remote.prefix, remote.username, "base", "current"),
		ManifestKey: manifestKey,
		LocalDir:    s.cfg.BaseAssetsDir,
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
		Bucket:      s.cfg.S3Bucket,
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
		Bucket:      s.cfg.S3Bucket,
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
	_, _ = fmt.Fprintf(&builder, "local dir: %s\n", summary.LocalDir)
	_, _ = fmt.Fprintf(&builder, "files: %d\n", len(summary.Transferred))
	for _, file := range summary.Transferred {
		_, _ = fmt.Fprintf(&builder, "  - %s (%d bytes)\n", file.Name, file.SizeBytes)
	}
	return strings.TrimRight(builder.String(), "\n")
}
