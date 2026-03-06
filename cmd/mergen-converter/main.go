package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/alperreha/mergen-fire/internal/converter"
	"github.com/alperreha/mergen-fire/internal/logging"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "mergen-converter",
		Short:         "Convert OCI images to Firecracker payload rootfs bundles",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	root.AddCommand(newConvertCmd())
	root.AddCommand(newDoctorCmd())
	return root
}

type convertOptions struct {
	Image             string
	OutputDir         string
	Name              string
	SizeMiB           int
	AgentSizeMiB      int
	SkipPull          bool
	DeleteRootFS      bool
	SbinInitPath      string
	AgentPath         string
	VSockEnable       bool
	VSockGuestPath    string
	VSockAuthToken    string
	LegacyFullBundle  bool
	BaseAssetsDir     string
	BaseKernelPath    string
	BaseRootFSPath    string
	BaseAgentDiskPath string
	BaseEnvDiskPath   string
	GoldenRootFS      string
	GoldenSizeMiB     int
	EnvSizeMiB        int
	EnvLine           string
	LogLevel          string
	LogFormat         string
}

func newConvertCmd() *cobra.Command {
	opts := &convertOptions{}
	cmd := &cobra.Command{
		Use:   "convert",
		Short: "Convert image and produce payload rootfs artifacts",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runConvert(*opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.Image, "image", "", "Docker/OCI image reference (required), e.g. nginx:alpine")
	f.StringVar(&opts.OutputDir, "output-dir", "", "Output directory (default: /var/lib/mergen/images/<image-ref>)")
	f.StringVar(&opts.Name, "name", "", "Output name (used when output-dir is empty)")
	f.IntVar(&opts.SizeMiB, "size-mib", 0, "Payload ext4 image size in MiB (0 = auto)")
	f.IntVar(&opts.AgentSizeMiB, "agent-size-mib", 0, "Agent ext4 image size in MiB (legacy mode only)")
	f.BoolVar(&opts.SkipPull, "skip-pull", false, "Skip remote pull and reuse previously cached image blobs in output-dir/image-cache")
	f.BoolVar(&opts.DeleteRootFS, "delete-rootfs", false, "Delete converted rootfs output for the selected image and exit")
	f.StringVar(&opts.SbinInitPath, "sbin-init", "./artifacts/sbin-init/sbin-init", "Path to mergen-init binary (legacy mode only)")
	f.StringVar(&opts.AgentPath, "sbin-agent", "./artifacts/sbin-init/mergen-agent", "Path to mergen-agent binary (legacy mode only)")
	f.BoolVar(&opts.VSockEnable, "vsock-enable", false, "Enable VSock guest helper metadata")
	f.StringVar(&opts.VSockGuestPath, "sbin-vsock-guest", "./artifacts/sbin-init/mergen-vsock-guest", "Path to mergen-vsock-guest binary (legacy mode only)")
	f.StringVar(&opts.VSockAuthToken, "vsock-auth-token", "", "Optional VSock auth token written into runtime metadata")
	f.BoolVar(&opts.LegacyFullBundle, "legacy-full-bundle", false, "Build golden/agent/env disks per image (legacy mode). Default mode builds payload only")
	f.StringVar(&opts.BaseAssetsDir, "base-assets-dir", "/var/lib/mergen/base/current", "Base assets directory used in suggested VM request (kernel/rootfs/agent)")
	f.StringVar(&opts.BaseKernelPath, "base-kernel", "", "Override base kernel path for suggested VM request")
	f.StringVar(&opts.BaseRootFSPath, "base-rootfs", "", "Override base rootfs path for suggested VM request")
	f.StringVar(&opts.BaseAgentDiskPath, "base-agent-disk", "", "Override base agent disk path for suggested VM request")
	f.StringVar(&opts.BaseEnvDiskPath, "base-env-disk", "", "Optional base env disk path for suggested VM request")
	f.StringVar(&opts.GoldenRootFS, "golden-rootfs-dir", "", "Optional base golden rootfs directory to copy before injecting binaries/runtime metadata (legacy mode)")
	f.IntVar(&opts.GoldenSizeMiB, "golden-size-mib", 0, "Golden rootfs ext4 size in MiB (legacy mode only)")
	f.IntVar(&opts.EnvSizeMiB, "env-size-mib", 0, "Env disk ext4 size in MiB (legacy mode only)")
	f.StringVar(&opts.EnvLine, "env-line", "Mergen=is super", "Single KEY=VALUE line written to env disk (legacy mode only)")
	f.StringVar(&opts.LogLevel, "log-level", "info", "Log level (debug|info|warn|error)")
	f.StringVar(&opts.LogFormat, "log-format", "console", "Log format (console|json|text)")
	_ = cmd.MarkFlagRequired("image")

	return cmd
}

func runConvert(opts convertOptions) error {
	logger := logging.New(opts.LogLevel, opts.LogFormat).With("component", "mergen-converter")
	runner := converter.NewRunner(logger)

	if opts.DeleteRootFS {
		result, err := runner.Delete(context.Background(), converter.DeleteOptions{
			Image:     opts.Image,
			OutputDir: opts.OutputDir,
			Name:      opts.Name,
		})
		if err != nil {
			return fmt.Errorf("delete rootfs failed: %w", err)
		}

		_, _ = fmt.Fprintf(os.Stdout, "image: %s\n", result.Image)
		_, _ = fmt.Fprintf(os.Stdout, "deleted output dir: %s\n", result.OutputDir)
		return nil
	}

	result, err := runner.Run(context.Background(), converter.Options{
		Image:             opts.Image,
		OutputDir:         opts.OutputDir,
		Name:              opts.Name,
		SizeMiB:           opts.SizeMiB,
		AgentSizeMiB:      opts.AgentSizeMiB,
		SkipPull:          opts.SkipPull,
		SbinInitPath:      opts.SbinInitPath,
		AgentPath:         opts.AgentPath,
		VSockEnable:       opts.VSockEnable,
		VSockGuestPath:    opts.VSockGuestPath,
		VSockAuthToken:    opts.VSockAuthToken,
		LegacyFullBundle:  opts.LegacyFullBundle,
		BaseAssetsDir:     opts.BaseAssetsDir,
		BaseKernelPath:    opts.BaseKernelPath,
		BaseRootFSPath:    opts.BaseRootFSPath,
		BaseAgentDiskPath: opts.BaseAgentDiskPath,
		BaseEnvDiskPath:   opts.BaseEnvDiskPath,
		GoldenRootFS:      opts.GoldenRootFS,
		GoldenSizeMiB:     opts.GoldenSizeMiB,
		EnvSizeMiB:        opts.EnvSizeMiB,
		EnvLine:           opts.EnvLine,
	})
	if err != nil {
		return fmt.Errorf("conversion failed: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stdout, "image: %s\n", result.Image)
	_, _ = fmt.Fprintf(os.Stdout, "output dir: %s\n", result.OutputDir)
	_, _ = fmt.Fprintf(os.Stdout, "payload rootfs dir: %s\n", result.PayloadRootFSDir)
	_, _ = fmt.Fprintf(os.Stdout, "payload rootfs tar: %s\n", result.PayloadRootFSTarPath)
	_, _ = fmt.Fprintf(os.Stdout, "payload rootfs ext4 (disk2): %s\n", result.PayloadRootFSExt4Path)
	if opts.LegacyFullBundle {
		_, _ = fmt.Fprintf(os.Stdout, "golden rootfs dir: %s\n", result.RootFSDir)
		_, _ = fmt.Fprintf(os.Stdout, "golden rootfs tar: %s\n", result.RootFSTarPath)
		_, _ = fmt.Fprintf(os.Stdout, "golden rootfs ext4 (disk0): %s\n", result.RootFSExt4Path)
		_, _ = fmt.Fprintf(os.Stdout, "agent rootfs dir: %s\n", result.AgentRootFSDir)
		_, _ = fmt.Fprintf(os.Stdout, "agent rootfs tar: %s\n", result.AgentRootFSTarPath)
		_, _ = fmt.Fprintf(os.Stdout, "agent rootfs ext4 (disk1): %s\n", result.AgentRootFSExt4Path)
		_, _ = fmt.Fprintf(os.Stdout, "env rootfs dir: %s\n", result.EnvRootFSDir)
		_, _ = fmt.Fprintf(os.Stdout, "env rootfs ext4 (disk3): %s\n", result.EnvRootFSExt4Path)
	}
	_, _ = fmt.Fprintf(os.Stdout, "runtime metadata: %s\n", result.RuntimePath)
	if result.EnvFilePath != "" {
		_, _ = fmt.Fprintf(os.Stdout, "env file: %s\n", result.EnvFilePath)
	}
	_, _ = fmt.Fprintf(os.Stdout, "image metadata: %s\n", result.MetadataPath)
	_, _ = fmt.Fprintf(os.Stdout, "suggested boot args: %s\n", result.SuggestedBootArgsPath)
	_, _ = fmt.Fprintf(os.Stdout, "suggested VM request: %s\n", result.SuggestedVMPath)
	if result.SuggestedHTTPPort > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "suggested httpPort: %d\n", result.SuggestedHTTPPort)
	}
	return nil
}

type doctorOptions struct {
	BaseDir          string
	RequireEnvDisk   bool
	RequireVSockHost bool
	JSON             bool
}

func newDoctorCmd() *cobra.Command {
	opts := &doctorOptions{}
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate base assets and converter runtime prerequisites",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDoctor(*opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.BaseDir, "base-dir", "/var/lib/mergen/base/current", "Base assets directory")
	f.BoolVar(&opts.RequireEnvDisk, "require-env-disk", false, "Fail if env-rootfs.ext4 is missing")
	f.BoolVar(&opts.RequireVSockHost, "require-vsock-host", false, "Fail if mergen-vsock-host is not found in PATH")
	f.BoolVar(&opts.JSON, "json", false, "Print doctor report as JSON")
	return cmd
}

func runDoctor(opts doctorOptions) error {
	report := converter.RunDoctor(converter.DoctorOptions{
		BaseDir:          opts.BaseDir,
		RequireEnvDisk:   opts.RequireEnvDisk,
		RequireVSockHost: opts.RequireVSockHost,
	})

	if opts.JSON {
		body, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(os.Stdout, string(body))
	} else {
		_, _ = fmt.Fprintf(os.Stdout, "doctor base dir: %s\n", report.BaseDir)
		if report.ResolvedBaseDir != "" && report.ResolvedBaseDir != report.BaseDir {
			_, _ = fmt.Fprintf(os.Stdout, "resolved base dir: %s\n", report.ResolvedBaseDir)
		}
		for _, check := range report.Checks {
			_, _ = fmt.Fprintf(os.Stdout, "[%s %s] %s", statusEmoji(check.Status), statusLabel(check.Status), check.Name)
			if check.Path != "" {
				_, _ = fmt.Fprintf(os.Stdout, " (%s)", check.Path)
			}
			_, _ = fmt.Fprintf(os.Stdout, " -> %s\n", check.Message)
		}
		_, _ = fmt.Fprintf(os.Stdout, "summary: %s passed=%t failed_required=%d warnings=%d\n", summaryEmoji(report.Passed), report.Passed, report.FailedRequired, report.Warnings)
	}

	if !report.Passed {
		return fmt.Errorf("doctor failed: %d required check(s) failed", report.FailedRequired)
	}
	return nil
}

func statusEmoji(status string) string {
	switch status {
	case "pass":
		return "✅"
	case "fail":
		return "❌"
	case "warn":
		return "⚠️"
	default:
		return "•"
	}
}

func statusLabel(status string) string {
	switch status {
	case "pass":
		return "PASS"
	case "fail":
		return "FAIL"
	case "warn":
		return "WARN"
	default:
		return "INFO"
	}
}

func summaryEmoji(passed bool) string {
	if passed {
		return "✅"
	}
	return "❌"
}
