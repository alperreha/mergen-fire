package converter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type AgentRootFSOptions struct {
	BaseDir    string
	OutputPath string
	SizeMiB    int
}

type AgentRootFSResult struct {
	BaseDir               string
	BaseBinDir            string
	Ext4Path              string
	SizeMiB               int
	Files                 []string
	ModeChangedToReadOnly bool
}

func (r *Runner) BuildAgentRootFS(ctx context.Context, opts AgentRootFSOptions) (AgentRootFSResult, error) {
	baseDir := strings.TrimSpace(opts.BaseDir)
	if baseDir == "" {
		baseDir = defaultBaseAssetsDir
	}
	baseDir = filepath.Clean(baseDir)

	if opts.SizeMiB < 0 {
		return AgentRootFSResult{}, fmt.Errorf("agent rootfs sizeMiB must be >= 0, got %d", opts.SizeMiB)
	}

	outputPath := strings.TrimSpace(opts.OutputPath)
	if outputPath == "" {
		outputPath = filepath.Join(baseDir, "agent-rootfs.ext4")
	}
	outputPath = filepath.Clean(outputPath)

	baseBinDir := filepath.Join(baseDir, "bin")
	if err := ensureReadableDir(baseBinDir); err != nil {
		return AgentRootFSResult{}, err
	}
	if err := ensureCommand("truncate"); err != nil {
		return AgentRootFSResult{}, err
	}
	if err := ensureCommand("mkfs.ext4"); err != nil {
		return AgentRootFSResult{}, err
	}

	stageDir, err := os.MkdirTemp("", "mergen-agent-rootfs-*")
	if err != nil {
		return AgentRootFSResult{}, fmt.Errorf("create temp agent rootfs dir: %w", err)
	}
	defer os.RemoveAll(stageDir)

	files, err := copyAgentBinariesFromBase(baseBinDir, stageDir)
	if err != nil {
		return AgentRootFSResult{}, err
	}

	sizeMiB, err := resolveSizeMiB(stageDir, opts.SizeMiB, defaultAgentOverhead, defaultAgentSizeMiB)
	if err != nil {
		return AgentRootFSResult{}, err
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return AgentRootFSResult{}, fmt.Errorf("prepare output dir: %w", err)
	}
	if err := buildExt4(ctx, stageDir, outputPath, sizeMiB); err != nil {
		return AgentRootFSResult{}, err
	}

	modeChanged := ensureReadOnlyBestEffort(outputPath, r.logger)

	result := AgentRootFSResult{
		BaseDir:               baseDir,
		BaseBinDir:            baseBinDir,
		Ext4Path:              outputPath,
		SizeMiB:               sizeMiB,
		Files:                 files,
		ModeChangedToReadOnly: modeChanged,
	}

	r.logger.Info(
		"agent rootfs ext4 generated from base",
		"baseDir", result.BaseDir,
		"baseBinDir", result.BaseBinDir,
		"ext4Path", result.Ext4Path,
		"sizeMiB", result.SizeMiB,
		"fileCount", len(result.Files),
	)
	return result, nil
}

func copyAgentBinariesFromBase(baseBinDir, destDir string) ([]string, error) {
	entries, err := os.ReadDir(baseBinDir)
	if err != nil {
		return nil, fmt.Errorf("read base bin dir %s: %w", baseBinDir, err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("read file info %s: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if info.Mode()&0o111 == 0 {
			continue
		}

		srcPath := filepath.Join(baseBinDir, entry.Name())
		content, err := os.ReadFile(srcPath)
		if err != nil {
			return nil, fmt.Errorf("read base binary %s: %w", srcPath, err)
		}

		dstPath := filepath.Join(destDir, entry.Name())
		if err := writeExecutableFileReplacingSymlink(dstPath, content); err != nil {
			return nil, fmt.Errorf("write staged agent binary %s: %w", dstPath, err)
		}
		files = append(files, entry.Name())
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no executable binaries found in base bin dir: %s", baseBinDir)
	}
	if !containsString(files, "mergen-agent") {
		return nil, fmt.Errorf("required binary mergen-agent not found in base bin dir: %s", baseBinDir)
	}

	return files, nil
}

func ensureReadOnlyBestEffort(path string, logger loggerLike) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	perms := info.Mode().Perm()
	if perms&0o222 == 0 {
		return false
	}

	targetPerms := perms &^ 0o222
	if targetPerms == 0 {
		targetPerms = 0o444
	}
	if err := os.Chmod(path, targetPerms); err != nil {
		if logger != nil {
			logger.Warn("chmod read-only failed for ext4 output", "path", path, "error", err)
		}
		return false
	}
	return true
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

type loggerLike interface {
	Warn(msg string, args ...any)
}
