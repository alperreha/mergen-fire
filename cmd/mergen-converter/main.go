package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/alperreha/mergen-fire/internal/converter"
	"github.com/alperreha/mergen-fire/internal/logging"
)

func main() {
	var (
		image          string
		outputDir      string
		name           string
		sizeMiB        int
		skipPull       bool
		deleteRootFS   bool
		sbinInitPath   string
		telemetryPath  string
		supervisorPath string
		goldenRootFS   string
		goldenSizeMiB  int
		envSizeMiB     int
		envLine        string
		logLevel       string
		logFormat      string
	)

	flag.StringVar(&image, "image", "", "Docker/OCI image reference (required), e.g. nginx:alpine")
	flag.StringVar(&outputDir, "output-dir", "", "Output directory (default: /var/lib/mergen/images/<image-ref>)")
	flag.StringVar(&name, "name", "", "Output name (used when output-dir is empty)")
	flag.IntVar(&sizeMiB, "size-mib", 0, "Payload ext4 image size in MiB (0 = auto)")
	flag.BoolVar(&skipPull, "skip-pull", false, "Skip remote pull and reuse previously cached image blobs in output-dir/image-cache")
	flag.BoolVar(&deleteRootFS, "delete-rootfs", false, "Delete converted rootfs output for the selected image and exit")
	flag.StringVar(&sbinInitPath, "sbin-init", "./artifacts/sbin-init/sbin-init", "Path to mergen-init binary copied into golden rootfs (/sbin/init)")
	flag.StringVar(&telemetryPath, "sbin-telemetry", "./artifacts/sbin-init/mergen-telemetry", "Path to mergen-telemetry binary copied into golden rootfs")
	flag.StringVar(&supervisorPath, "sbin-supervisor", "./artifacts/sbin-init/mergen-supervisor", "Path to mergen-supervisor binary copied into golden rootfs")
	flag.StringVar(&goldenRootFS, "golden-rootfs-dir", "", "Optional base golden rootfs directory to copy before injecting binaries/runtime metadata")
	flag.IntVar(&goldenSizeMiB, "golden-size-mib", 0, "Golden rootfs ext4 size in MiB (0 = auto/minimum)")
	flag.IntVar(&envSizeMiB, "env-size-mib", 0, "Env disk ext4 size in MiB (0 = auto/minimum)")
	flag.StringVar(&envLine, "env-line", "Mergen=is super", "Single KEY=VALUE line written to env disk for initial testing")
	flag.StringVar(&logLevel, "log-level", "info", "Log level (debug|info|warn|error)")
	flag.StringVar(&logFormat, "log-format", "console", "Log format (console|json|text)")
	flag.Parse()

	if image == "" {
		_, _ = fmt.Fprintln(os.Stderr, "error: -image is required")
		flag.Usage()
		os.Exit(1)
	}

	logger := logging.New(logLevel, logFormat).With("component", "mergen-converter")
	runner := converter.NewRunner(logger)

	if deleteRootFS {
		result, err := runner.Delete(context.Background(), converter.DeleteOptions{
			Image:     image,
			OutputDir: outputDir,
			Name:      name,
		})
		if err != nil {
			logger.Error("delete rootfs failed", "error", err)
			os.Exit(1)
		}

		logger.Info("delete rootfs completed", "image", result.Image, "outputDir", result.OutputDir)
		_, _ = fmt.Fprintf(os.Stdout, "image: %s\n", result.Image)
		_, _ = fmt.Fprintf(os.Stdout, "deleted output dir: %s\n", result.OutputDir)
		return
	}

	result, err := runner.Run(context.Background(), converter.Options{
		Image:          image,
		OutputDir:      outputDir,
		Name:           name,
		SizeMiB:        sizeMiB,
		SkipPull:       skipPull,
		SbinInitPath:   sbinInitPath,
		TelemetryPath:  telemetryPath,
		SupervisorPath: supervisorPath,
		GoldenRootFS:   goldenRootFS,
		GoldenSizeMiB:  goldenSizeMiB,
		EnvSizeMiB:     envSizeMiB,
		EnvLine:        envLine,
	})
	if err != nil {
		logger.Error("conversion failed", "error", err)
		os.Exit(1)
	}

	logger.Info("conversion completed", "image", result.Image, "outputDir", result.OutputDir)
	_, _ = fmt.Fprintf(os.Stdout, "image: %s\n", result.Image)
	_, _ = fmt.Fprintf(os.Stdout, "output dir: %s\n", result.OutputDir)
	_, _ = fmt.Fprintf(os.Stdout, "golden rootfs dir: %s\n", result.RootFSDir)
	_, _ = fmt.Fprintf(os.Stdout, "golden rootfs tar: %s\n", result.RootFSTarPath)
	_, _ = fmt.Fprintf(os.Stdout, "golden rootfs ext4 (disk0): %s\n", result.RootFSExt4Path)
	_, _ = fmt.Fprintf(os.Stdout, "payload rootfs dir: %s\n", result.PayloadRootFSDir)
	_, _ = fmt.Fprintf(os.Stdout, "payload rootfs tar: %s\n", result.PayloadRootFSTarPath)
	_, _ = fmt.Fprintf(os.Stdout, "payload rootfs ext4 (disk1): %s\n", result.PayloadRootFSExt4Path)
	_, _ = fmt.Fprintf(os.Stdout, "env rootfs dir: %s\n", result.EnvRootFSDir)
	_, _ = fmt.Fprintf(os.Stdout, "env rootfs ext4 (disk2): %s\n", result.EnvRootFSExt4Path)
	_, _ = fmt.Fprintf(os.Stdout, "runtime metadata: %s\n", result.RuntimePath)
	_, _ = fmt.Fprintf(os.Stdout, "env file: %s\n", result.EnvFilePath)
	_, _ = fmt.Fprintf(os.Stdout, "image metadata: %s\n", result.MetadataPath)
	_, _ = fmt.Fprintf(os.Stdout, "suggested boot args: %s\n", result.SuggestedBootArgsPath)
	_, _ = fmt.Fprintf(os.Stdout, "suggested VM request: %s\n", result.SuggestedVMPath)
	if result.SuggestedHTTPPort > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "suggested httpPort: %d\n", result.SuggestedHTTPPort)
	}
}
