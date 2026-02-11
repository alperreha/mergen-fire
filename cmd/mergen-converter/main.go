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
		image        string
		outputDir    string
		name         string
		sizeMiB      int
		skipPull     bool
		sbinInitPath string
		logLevel     string
		logFormat    string
	)

	flag.StringVar(&image, "image", "", "Docker/OCI image reference (required), e.g. nginx:alpine")
	flag.StringVar(&outputDir, "output-dir", "", "Output directory (default: ./artifacts/converter/<sanitized-image>)")
	flag.StringVar(&name, "name", "", "Output name (used when output-dir is empty)")
	flag.IntVar(&sizeMiB, "size-mib", 0, "ext4 image size in MiB (0 = auto)")
	flag.BoolVar(&skipPull, "skip-pull", false, "Skip remote pull and reuse previously cached image blobs in output-dir/image-cache")
	flag.StringVar(&sbinInitPath, "sbin-init", "./artifacts/sbin-init/sbin-init", "Path to sbin init binary to inject into rootfs")
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

	result, err := runner.Run(context.Background(), converter.Options{
		Image:        image,
		OutputDir:    outputDir,
		Name:         name,
		SizeMiB:      sizeMiB,
		SkipPull:     skipPull,
		SbinInitPath: sbinInitPath,
	})
	if err != nil {
		logger.Error("conversion failed", "error", err)
		os.Exit(1)
	}

	logger.Info("conversion completed", "image", result.Image, "outputDir", result.OutputDir)
	_, _ = fmt.Fprintf(os.Stdout, "image: %s\n", result.Image)
	_, _ = fmt.Fprintf(os.Stdout, "output dir: %s\n", result.OutputDir)
	_, _ = fmt.Fprintf(os.Stdout, "rootfs dir: %s\n", result.RootFSDir)
	_, _ = fmt.Fprintf(os.Stdout, "rootfs tar: %s\n", result.RootFSTarPath)
	_, _ = fmt.Fprintf(os.Stdout, "rootfs ext4: %s\n", result.RootFSExt4Path)
	_, _ = fmt.Fprintf(os.Stdout, "image metadata: %s\n", result.MetadataPath)
	_, _ = fmt.Fprintf(os.Stdout, "suggested boot args: %s\n", result.SuggestedBootArgsPath)
	_, _ = fmt.Fprintf(os.Stdout, "suggested VM request: %s\n", result.SuggestedVMPath)
	if result.SuggestedHTTPPort > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "suggested httpPort: %d\n", result.SuggestedHTTPPort)
	}
}
