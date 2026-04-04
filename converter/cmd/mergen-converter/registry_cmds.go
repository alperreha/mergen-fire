package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alperreha/mergen-fire/converter/internal/converter"
	"github.com/alperreha/mergen-fire/pkg/logging"
)

type initOptions struct {
	ConfigPath      string
	Registry        string
	Endpoint        string
	Region          string
	Bucket          string
	Prefix          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Username        string
	UsePathStyle    bool
	NonInteractive  bool
}

type loginOptions struct {
	ConfigPath     string
	Registry       string
	Username       string
	Password       string
	NonInteractive bool
}

type syncOptions struct {
	ConfigPath string
	Registry   string
	Image      string
	OutputDir  string
	Name       string
	LogLevel   string
	LogFormat  string
}

func newInitCmd() *cobra.Command {
	opts := &initOptions{}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a user S3 registry profile for payload rootfs push/pull",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runInit(*opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.ConfigPath, "config", "", "Path to converter config file (default: ~/.config/mergen-converter/config.json)")
	f.StringVar(&opts.Registry, "registry", "default", "Registry profile name")
	f.StringVar(&opts.Endpoint, "endpoint", "", "User S3 endpoint URL (MinIO or S3-compatible)")
	f.StringVar(&opts.Region, "region", "us-east-1", "User S3 region")
	f.StringVar(&opts.Bucket, "bucket", "", "User S3 bucket name")
	f.StringVar(&opts.Prefix, "prefix", "users", "User S3 key prefix")
	f.StringVar(&opts.AccessKeyID, "access-key", "", "User S3 access key")
	f.StringVar(&opts.SecretAccessKey, "secret-key", "", "User S3 secret key")
	f.StringVar(&opts.SessionToken, "session-token", "", "Optional user S3 session token")
	f.StringVar(&opts.Username, "username", "", "Username used in remote object keys")
	f.BoolVar(&opts.UsePathStyle, "use-path-style", true, "Use path-style S3 requests (recommended for MinIO)")
	f.BoolVar(&opts.NonInteractive, "non-interactive", false, "Fail instead of prompting for missing fields")
	return cmd
}

func newLoginCmd() *cobra.Command {
	opts := &loginOptions{}
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store username/password for a registry profile (docker login style, no validation yet)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runLogin(*opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.ConfigPath, "config", "", "Path to converter config file (default: ~/.config/mergen-converter/config.json)")
	f.StringVar(&opts.Registry, "registry", "", "Registry profile name (default: current profile)")
	f.StringVar(&opts.Username, "username", "", "Login username")
	f.StringVar(&opts.Password, "password", "", "Login password (stored locally, not validated yet)")
	f.BoolVar(&opts.NonInteractive, "non-interactive", false, "Fail instead of prompting for missing fields")
	return cmd
}

func newPushCmd() *cobra.Command {
	opts := &syncOptions{}
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push the converted payload-rootfs.ext4 for an image to user S3",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runPush(*opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.ConfigPath, "config", "", "Path to converter config file")
	f.StringVar(&opts.Registry, "registry", "", "Registry profile name (default: current profile)")
	f.StringVar(&opts.Image, "image", "", "Image reference whose payload-rootfs.ext4 will be uploaded")
	f.StringVar(&opts.OutputDir, "output-dir", "", "Override local image output directory")
	f.StringVar(&opts.Name, "name", "", "Output name override when output-dir is empty")
	f.StringVar(&opts.LogLevel, "log-level", "info", "Log level (debug|info|warn|error)")
	f.StringVar(&opts.LogFormat, "log-format", "console", "Log format (console|json|text)")
	_ = cmd.MarkFlagRequired("image")
	return cmd
}

func newPullCmd() *cobra.Command {
	opts := &syncOptions{}
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull payload-rootfs.ext4 for an image from user S3 into the local image store",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runPull(*opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.ConfigPath, "config", "", "Path to converter config file")
	f.StringVar(&opts.Registry, "registry", "", "Registry profile name (default: current profile)")
	f.StringVar(&opts.Image, "image", "", "Image reference whose payload-rootfs.ext4 will be downloaded")
	f.StringVar(&opts.OutputDir, "output-dir", "", "Override local image output directory")
	f.StringVar(&opts.Name, "name", "", "Output name override when output-dir is empty")
	f.StringVar(&opts.LogLevel, "log-level", "info", "Log level (debug|info|warn|error)")
	f.StringVar(&opts.LogFormat, "log-format", "console", "Log format (console|json|text)")
	_ = cmd.MarkFlagRequired("image")
	return cmd
}

func runInit(opts initOptions) error {
	configPath, cfg, err := loadClientConfig(opts.ConfigPath)
	if err != nil {
		return err
	}

	registryAlias := opts.Registry
	profile, _, err := converter.ResolveRegistry(cfg, registryAlias)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)

	profile.Name = registryAlias
	profile.Username, err = requiredString("username", firstNonEmpty(opts.Username, profile.Username), opts.NonInteractive, reader)
	if err != nil {
		return err
	}
	profile.Endpoint, err = optionalString("user S3 endpoint (optional for AWS S3)", firstNonEmpty(opts.Endpoint, profile.Endpoint), opts.NonInteractive, reader)
	if err != nil {
		return err
	}
	profile.Region, err = optionalString("user S3 region", firstNonEmpty(opts.Region, profile.Region), opts.NonInteractive, reader)
	if err != nil {
		return err
	}
	profile.Bucket, err = requiredString("user S3 bucket", firstNonEmpty(opts.Bucket, profile.Bucket), opts.NonInteractive, reader)
	if err != nil {
		return err
	}
	profile.Prefix, err = optionalString("user S3 prefix", firstNonEmpty(opts.Prefix, profile.Prefix), opts.NonInteractive, reader)
	if err != nil {
		return err
	}
	profile.AccessKeyID, err = requiredString("user S3 access key", firstNonEmpty(opts.AccessKeyID, profile.AccessKeyID), opts.NonInteractive, reader)
	if err != nil {
		return err
	}
	profile.SecretAccessKey, err = requiredString("user S3 secret key", firstNonEmpty(opts.SecretAccessKey, profile.SecretAccessKey), opts.NonInteractive, reader)
	if err != nil {
		return err
	}
	profile.SessionToken = firstNonEmpty(opts.SessionToken, profile.SessionToken)
	profile.UsePathStyle = opts.UsePathStyle

	cfg = converter.UpsertRegistry(cfg, registryAlias, profile, true)
	if err := converter.SaveClientConfig(configPath, cfg); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stdout, "config: %s\n", configPath)
	_, _ = fmt.Fprintf(os.Stdout, "registry: %s\n", registryAlias)
	_, _ = fmt.Fprintf(os.Stdout, "endpoint: %s\n", profile.Endpoint)
	_, _ = fmt.Fprintf(os.Stdout, "bucket: %s\n", profile.Bucket)
	_, _ = fmt.Fprintf(os.Stdout, "prefix: %s\n", profile.Prefix)
	_, _ = fmt.Fprintf(os.Stdout, "username: %s\n", profile.Username)
	return nil
}

func runLogin(opts loginOptions) error {
	configPath, cfg, err := loadClientConfig(opts.ConfigPath)
	if err != nil {
		return err
	}

	profile, registryAlias, err := converter.ResolveRegistry(cfg, opts.Registry)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)

	profile.Username, err = requiredString("username", firstNonEmpty(opts.Username, profile.Username), opts.NonInteractive, reader)
	if err != nil {
		return err
	}
	profile.Password, err = requiredString("password", firstNonEmpty(opts.Password, profile.Password), opts.NonInteractive, reader)
	if err != nil {
		return err
	}

	cfg = converter.UpsertRegistry(cfg, registryAlias, profile, true)
	if err := converter.SaveClientConfig(configPath, cfg); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stdout, "config: %s\n", configPath)
	_, _ = fmt.Fprintf(os.Stdout, "registry: %s\n", registryAlias)
	_, _ = fmt.Fprintf(os.Stdout, "username: %s\n", profile.Username)
	_, _ = fmt.Fprintln(os.Stdout, "login: stored locally (no remote validation yet)")
	return nil
}

func runPush(opts syncOptions) error {
	logger := logging.New(opts.LogLevel, opts.LogFormat).With("component", "mergen-converter")
	configPath, cfg, err := loadClientConfig(opts.ConfigPath)
	if err != nil {
		return err
	}

	profile, registryAlias, err := converter.ResolveRegistry(cfg, opts.Registry)
	if err != nil {
		return err
	}
	target, err := converter.ResolveImageTarget(opts.Image, opts.OutputDir, opts.Name)
	if err != nil {
		return err
	}
	localPath := filepath.Join(target.OutputDir, "payload-rootfs.ext4")
	if _, err := os.Stat(localPath); err != nil {
		return fmt.Errorf("payload rootfs ext4 not found: %s", localPath)
	}

	logger.Info("pushing payload rootfs to user s3", "image", target.Image, "registry", registryAlias, "path", localPath)
	reporter := converter.NewCLIProgressReporter(os.Stderr)
	result, err := converter.PushPayloadToUserS3(context.Background(), registryAlias, profile, target.Image, localPath, reporter)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stdout, "config: %s\n", configPath)
	_, _ = fmt.Fprintf(os.Stdout, "registry: %s\n", result.Registry)
	_, _ = fmt.Fprintf(os.Stdout, "image: %s\n", result.Image)
	_, _ = fmt.Fprintf(os.Stdout, "bucket: %s\n", result.Bucket)
	_, _ = fmt.Fprintf(os.Stdout, "object key: %s\n", result.ObjectKey)
	_, _ = fmt.Fprintf(os.Stdout, "local path: %s\n", result.LocalPath)
	_, _ = fmt.Fprintf(os.Stdout, "size bytes: %d\n", result.SizeBytes)
	return nil
}

func runPull(opts syncOptions) error {
	logger := logging.New(opts.LogLevel, opts.LogFormat).With("component", "mergen-converter")
	configPath, cfg, err := loadClientConfig(opts.ConfigPath)
	if err != nil {
		return err
	}

	profile, registryAlias, err := converter.ResolveRegistry(cfg, opts.Registry)
	if err != nil {
		return err
	}
	target, err := converter.ResolveImageTarget(opts.Image, opts.OutputDir, opts.Name)
	if err != nil {
		return err
	}
	localPath := filepath.Join(target.OutputDir, "payload-rootfs.ext4")

	logger.Info("pulling payload rootfs from user s3", "image", target.Image, "registry", registryAlias, "path", localPath)
	reporter := converter.NewCLIProgressReporter(os.Stderr)
	result, err := converter.PullPayloadFromUserS3(context.Background(), registryAlias, profile, target.Image, localPath, reporter)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stdout, "config: %s\n", configPath)
	_, _ = fmt.Fprintf(os.Stdout, "registry: %s\n", result.Registry)
	_, _ = fmt.Fprintf(os.Stdout, "image: %s\n", result.Image)
	_, _ = fmt.Fprintf(os.Stdout, "bucket: %s\n", result.Bucket)
	_, _ = fmt.Fprintf(os.Stdout, "object key: %s\n", result.ObjectKey)
	_, _ = fmt.Fprintf(os.Stdout, "local path: %s\n", result.LocalPath)
	_, _ = fmt.Fprintf(os.Stdout, "size bytes: %d\n", result.SizeBytes)
	return nil
}

func loadClientConfig(path string) (string, converter.ClientConfig, error) {
	if strings.TrimSpace(path) == "" {
		resolved, err := converter.DefaultClientConfigPath()
		if err != nil {
			return "", converter.ClientConfig{}, err
		}
		path = resolved
	}
	cfg, err := converter.LoadClientConfig(path)
	if err != nil {
		return "", converter.ClientConfig{}, err
	}
	return path, cfg, nil
}

func requiredString(label, current string, nonInteractive bool, reader *bufio.Reader) (string, error) {
	value, err := optionalString(label, current, nonInteractive, reader)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	return value, nil
}

func optionalString(label, current string, nonInteractive bool, reader *bufio.Reader) (string, error) {
	current = strings.TrimSpace(current)
	if current != "" || nonInteractive {
		return current, nil
	}

	_, _ = fmt.Fprintf(os.Stdout, "%s: ", label)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
