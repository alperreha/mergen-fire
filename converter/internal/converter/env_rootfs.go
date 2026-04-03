package converter

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type EnvRootFSOptions struct {
	RuntimePath string
	OutputPath  string
	SizeMiB     int
	EnvLines    []string
	EnvFilePath string
}

type EnvRootFSResult struct {
	RuntimePath           string
	OutputPath            string
	SizeMiB               int
	EnvFilePath           string
	EnvLineCount          int
	ModeChangedToReadOnly bool
}

func (r *Runner) BuildEnvRootFS(ctx context.Context, opts EnvRootFSOptions) (EnvRootFSResult, error) {
	runtimePath := strings.TrimSpace(opts.RuntimePath)
	if runtimePath == "" {
		return EnvRootFSResult{}, fmt.Errorf("runtime path is required")
	}
	if err := ensureReadableFile(runtimePath); err != nil {
		return EnvRootFSResult{}, err
	}

	if opts.SizeMiB < 0 {
		return EnvRootFSResult{}, fmt.Errorf("env rootfs sizeMiB must be >= 0, got %d", opts.SizeMiB)
	}

	outputPath := strings.TrimSpace(opts.OutputPath)
	if outputPath == "" {
		outputPath = filepath.Join(filepath.Dir(runtimePath), "env-rootfs.ext4")
	}
	outputPath = filepath.Clean(outputPath)

	if err := ensureCommand("truncate"); err != nil {
		return EnvRootFSResult{}, err
	}
	if err := ensureCommand("mkfs.ext4"); err != nil {
		return EnvRootFSResult{}, err
	}

	stageDir, err := os.MkdirTemp("", "mergen-env-rootfs-*")
	if err != nil {
		return EnvRootFSResult{}, fmt.Errorf("create temp env rootfs dir: %w", err)
	}
	defer os.RemoveAll(stageDir)

	if err := injectRuntimeMetadata(runtimePath, stageDir); err != nil {
		return EnvRootFSResult{}, err
	}

	lines, err := collectEnvLines(opts.EnvFilePath, opts.EnvLines)
	if err != nil {
		return EnvRootFSResult{}, err
	}
	envFilePath := filepath.Join(stageDir, "mergen.env")
	if err := writeEnvDiskLines(envFilePath, lines); err != nil {
		return EnvRootFSResult{}, err
	}

	sizeMiB, err := resolveSizeMiB(stageDir, opts.SizeMiB, defaultEnvOverhead, defaultEnvSizeMiB)
	if err != nil {
		return EnvRootFSResult{}, err
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return EnvRootFSResult{}, fmt.Errorf("prepare output dir: %w", err)
	}
	if err := buildExt4(ctx, stageDir, outputPath, sizeMiB); err != nil {
		return EnvRootFSResult{}, err
	}

	modeChanged := ensureReadOnlyBestEffort(outputPath, r.logger)

	result := EnvRootFSResult{
		RuntimePath:           runtimePath,
		OutputPath:            outputPath,
		SizeMiB:               sizeMiB,
		EnvFilePath:           envFilePath,
		EnvLineCount:          len(lines),
		ModeChangedToReadOnly: modeChanged,
	}
	r.logger.Info(
		"env rootfs ext4 generated",
		"runtimePath", result.RuntimePath,
		"outputPath", result.OutputPath,
		"sizeMiB", result.SizeMiB,
		"envLineCount", result.EnvLineCount,
	)
	return result, nil
}

func collectEnvLines(filePath string, inline []string) ([]string, error) {
	lines := make([]string, 0, len(inline)+4)

	filePath = strings.TrimSpace(filePath)
	if filePath != "" {
		f, err := os.Open(filePath)
		if err != nil {
			return nil, fmt.Errorf("open env file %s: %w", filePath, err)
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := normalizeEnvLine(scanner.Text())
			if line == "" {
				continue
			}
			lines = append(lines, line)
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read env file %s: %w", filePath, err)
		}
	}

	for _, raw := range inline {
		line := normalizeEnvLine(raw)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}

	return lines, nil
}

func normalizeEnvLine(raw string) string {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return ""
	}
	return line
}

func writeEnvDiskLines(path string, lines []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("prepare env disk dir: %w", err)
	}

	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write env disk file: %w", err)
	}
	return nil
}
