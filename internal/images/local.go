package images

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	payloadRootFSFile = "payload-rootfs.ext4"
	imageMetaFile     = "image-meta.json"
	runtimeMetaFile   = "mergen.runtime.json"
	suggestedVMFile   = "suggested-vm-request.json"
	manifestFile      = "manifest.json"
)

type LocalStore struct {
	dataRoot     string
	baseDir      string
	imagesRoot   string
	progressTick time.Duration
}

func NewLocalStore(dataRoot, baseDir, imagesRoot string, progressTick time.Duration) *LocalStore {
	return &LocalStore{
		dataRoot:     strings.TrimSpace(dataRoot),
		baseDir:      strings.TrimSpace(baseDir),
		imagesRoot:   strings.TrimSpace(imagesRoot),
		progressTick: progressTick,
	}
}

func (s *LocalStore) EnsureLayout() error {
	dirs := []string{
		s.baseDir,
		filepath.Join(s.baseDir, "bin"),
		s.imagesRoot,
	}
	for _, dir := range dirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	return nil
}

func (s *LocalStore) BaseDir() string {
	return s.baseDir
}

func (s *LocalStore) BaseFiles() []string {
	return []string{
		filepath.Join(s.baseDir, "vmlinux"),
		filepath.Join(s.baseDir, "golden-rootfs.ext4"),
		filepath.Join(s.baseDir, "agent-rootfs.ext4"),
		filepath.Join(s.baseDir, "bin", "sbin-init"),
		filepath.Join(s.baseDir, "bin", "mergen-agent"),
	}
}

func (s *LocalStore) ResolvePayloadDir(imageRef string) (string, error) {
	cleaned, err := cleanImageRef(imageRef)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.imagesRoot, filepath.FromSlash(cleaned)), nil
}

func (s *LocalStore) ListLocal() (LocalCatalog, error) {
	if err := s.EnsureLayout(); err != nil {
		return LocalCatalog{}, err
	}

	base := BaseArtifact{
		Name:      "current",
		Directory: s.baseDir,
	}
	for _, filePath := range s.BaseFiles() {
		info, err := os.Stat(filePath)
		if err != nil {
			continue
		}
		base.Files = append(base.Files, LocalFile{
			Name:         filepath.Base(filePath),
			Path:         filePath,
			SizeBytes:    info.Size(),
			LastModified: info.ModTime(),
		})
	}
	if len(base.Files) > 0 {
		base.Ready = hasLocalFile(base.Files, "vmlinux") &&
			hasLocalFile(base.Files, "golden-rootfs.ext4") &&
			hasLocalFile(base.Files, "agent-rootfs.ext4")
		modified := latestModified(base.Files)
		base.LastModified = &modified
	}

	payloads := make([]PayloadArtifact, 0)
	walkErr := filepath.WalkDir(s.imagesRoot, func(current string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		payloadPath := filepath.Join(current, payloadRootFSFile)
		info, statErr := os.Stat(payloadPath)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return nil
			}
			return statErr
		}

		rel, relErr := filepath.Rel(s.imagesRoot, current)
		if relErr != nil {
			return relErr
		}
		imageRef := filepath.ToSlash(rel)
		files := []LocalFile{
			{Name: payloadRootFSFile, Path: payloadPath, SizeBytes: info.Size(), LastModified: info.ModTime()},
		}
		for _, name := range []string{imageMetaFile, runtimeMetaFile, suggestedVMFile, manifestFile} {
			filePath := filepath.Join(current, name)
			metaInfo, metaErr := os.Stat(filePath)
			if metaErr != nil {
				continue
			}
			files = append(files, LocalFile{Name: name, Path: filePath, SizeBytes: metaInfo.Size(), LastModified: metaInfo.ModTime()})
		}

		modified := latestModified(files)
		payloads = append(payloads, PayloadArtifact{
			ImageRef:     imageRef,
			Directory:    current,
			Files:        files,
			Ready:        true,
			LastModified: &modified,
		})
		return filepath.SkipDir
	})
	if walkErr != nil && !errors.Is(walkErr, os.ErrNotExist) {
		return LocalCatalog{}, walkErr
	}

	sort.Slice(payloads, func(i, j int) bool {
		return payloads[i].ImageRef < payloads[j].ImageRef
	})

	return LocalCatalog{
		Base:     base,
		Payloads: payloads,
	}, nil
}

func cleanImageRef(imageRef string) (string, error) {
	ref := strings.TrimSpace(imageRef)
	if ref == "" {
		return "", errors.New("image reference is empty")
	}
	if strings.Contains(ref, `\`) {
		return "", errors.New("image reference must use forward slashes")
	}
	cleaned := path.Clean(ref)
	if cleaned == "." || strings.HasPrefix(cleaned, "/") || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("invalid image reference %q", imageRef)
	}
	return cleaned, nil
}

func hasLocalFile(files []LocalFile, name string) bool {
	for _, file := range files {
		if file.Name == name {
			return true
		}
	}
	return false
}

func latestModified(files []LocalFile) time.Time {
	var latest time.Time
	for _, file := range files {
		if file.LastModified.After(latest) {
			latest = file.LastModified
		}
	}
	return latest
}
