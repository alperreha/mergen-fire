package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/alperreha/mergen-fire/internal/config"
	"github.com/alperreha/mergen-fire/internal/daemon"
	"github.com/alperreha/mergen-fire/internal/images"
	"github.com/alperreha/mergen-fire/pkg/logging"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "mergen",
		Short:         "Mergen daemon and image lifecycle CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newServerCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newImagesCmd())
	return root
}

type registryOptions struct {
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
	Password        string
	UsePathStyle    bool
	NonInteractive  bool
}

type baseOptions struct {
	ConfigPath string
	Registry   string
	Platform   string
	Flavor     string
	Version    string
	SetLatest  bool
}

func newServerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Run the Mergen HTTP daemon",
		RunE: func(_ *cobra.Command, _ []string) error {
			return daemon.RunFromEnv(context.Background())
		},
	}
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage config-S3 registry profiles for base image distribution",
	}
	cmd.AddCommand(newConfigInitCmd())
	cmd.AddCommand(newConfigLoginCmd())
	return cmd
}

func newConfigInitCmd() *cobra.Command {
	opts := &registryOptions{}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a config-S3 registry profile for base image pull/push",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runConfigInit(*opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.ConfigPath, "config", "", "Path to mergen config file (default: ~/.config/mergen/config.json)")
	f.StringVar(&opts.Registry, "registry", "default", "Registry profile name")
	f.StringVar(&opts.Endpoint, "endpoint", "", "Config S3 endpoint URL (optional for AWS S3)")
	f.StringVar(&opts.Region, "region", "us-east-1", "Config S3 region")
	f.StringVar(&opts.Bucket, "bucket", "", "Config S3 bucket name")
	f.StringVar(&opts.Prefix, "prefix", "artifacts", "Config S3 key prefix")
	f.StringVar(&opts.AccessKeyID, "access-key", "", "Config S3 access key")
	f.StringVar(&opts.SecretAccessKey, "secret-key", "", "Config S3 secret key")
	f.StringVar(&opts.SessionToken, "session-token", "", "Optional config S3 session token")
	f.StringVar(&opts.Username, "username", "", "Admin username for this config registry profile")
	f.BoolVar(&opts.UsePathStyle, "use-path-style", true, "Use path-style S3 requests (recommended for MinIO)")
	f.BoolVar(&opts.NonInteractive, "non-interactive", false, "Fail instead of prompting for missing fields")
	return cmd
}

func newConfigLoginCmd() *cobra.Command {
	opts := &registryOptions{}
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store admin username/password for a config-S3 registry profile",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runConfigLogin(*opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.ConfigPath, "config", "", "Path to mergen config file (default: ~/.config/mergen/config.json)")
	f.StringVar(&opts.Registry, "registry", "", "Registry profile name (default: current profile)")
	f.StringVar(&opts.Username, "username", "", "Login username")
	f.StringVar(&opts.Password, "password", "", "Login password (stored locally, not validated yet)")
	f.BoolVar(&opts.NonInteractive, "non-interactive", false, "Fail instead of prompting for missing fields")
	return cmd
}

func newImagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "images",
		Short: "Manage local images and config-S3 base artifacts",
	}
	cmd.AddCommand(newImagesListCmd())
	cmd.AddCommand(newImagesPushCmd())
	cmd.AddCommand(newImagesPullCmd())
	return cmd
}

func newImagesListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List local base and payload images",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := config.FromEnv()
			svc := images.NewService(cfg, logging.New(cfg.LogLevel, cfg.LogFormat).With("component", "images"))
			catalog, err := svc.ListLocal(context.Background())
			if err != nil {
				return err
			}
			if asJSON {
				body, err := json.MarshalIndent(catalog, "", "  ")
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintln(os.Stdout, string(body))
				return nil
			}

			tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "TYPE\tNAME\tREADY\tPATH")
			_, _ = fmt.Fprintf(tw, "base\t%s\t%t\t%s\n", catalog.Base.Name, catalog.Base.Ready, catalog.Base.Directory)
			for _, payload := range catalog.Payloads {
				_, _ = fmt.Fprintf(tw, "payload\t%s\t%t\t%s\n", payload.ImageRef, payload.Ready, payload.Directory)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print JSON output")
	return cmd
}

func newImagesPushCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push local base artifacts to config-S3",
	}
	cmd.AddCommand(newImagesPushBaseCmd())
	return cmd
}

func newImagesPullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull base artifacts from config-S3",
	}
	cmd.AddCommand(newImagesPullBaseCmd())
	return cmd
}

func newImagesPushBaseCmd() *cobra.Command {
	opts := &baseOptions{}
	cmd := &cobra.Command{
		Use:   "base",
		Short: "Push /var/lib/mergen/base/current into a versioned config-S3 path",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, _, err := loadRegistryAwareConfig(opts.ConfigPath, opts.Registry)
			if err != nil {
				return err
			}
			if strings.TrimSpace(cfg.ConfigS3Username) == "" || strings.TrimSpace(cfg.ConfigS3Password) == "" {
				return fmt.Errorf("admin config login required; run `mergen config login` or set MGR_CONFIG_S3_USERNAME/MGR_CONFIG_S3_PASSWORD")
			}
			ref, err := images.NormalizeBaseRef(opts.Platform, opts.Flavor, opts.Version)
			if err != nil {
				return err
			}
			reporter := images.NewCLIReporter(os.Stderr)
			svc := images.NewService(cfg, logging.New(cfg.LogLevel, cfg.LogFormat).With("component", "images"))
			summary, err := svc.PushBase(context.Background(), ref, opts.SetLatest, reporter)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(os.Stdout, images.FormatSummary(summary))
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.ConfigPath, "config", "", "Path to mergen config file")
	cmd.Flags().StringVar(&opts.Registry, "registry", "", "Registry profile name (default: current profile)")
	cmd.Flags().StringVar(&opts.Platform, "platform", config.FromEnv().BasePlatform, "Guest platform key, for example linux-amd64")
	cmd.Flags().StringVar(&opts.Flavor, "flavor", config.FromEnv().BaseFlavor, "Base flavor/distribution key, for example buildroot")
	cmd.Flags().StringVar(&opts.Version, "version", "", "Immutable version to publish, for example v0.0.1")
	cmd.Flags().BoolVar(&opts.SetLatest, "set-latest", true, "Also publish the same files under the latest alias")
	_ = cmd.MarkFlagRequired("version")
	return cmd
}

func newImagesPullBaseCmd() *cobra.Command {
	opts := &baseOptions{}
	cmd := &cobra.Command{
		Use:   "base",
		Short: "Pull a versioned base image set from config-S3 into /var/lib/mergen/base/current",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, _, err := loadRegistryAwareConfig(opts.ConfigPath, opts.Registry)
			if err != nil {
				return err
			}
			ref, err := images.NormalizeBaseRef(opts.Platform, opts.Flavor, opts.Version)
			if err != nil {
				return err
			}
			reporter := images.NewCLIReporter(os.Stderr)
			svc := images.NewService(cfg, logging.New(cfg.LogLevel, cfg.LogFormat).With("component", "images"))
			summary, err := svc.PullBase(context.Background(), ref, reporter)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(os.Stdout, images.FormatSummary(summary))
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.ConfigPath, "config", "", "Path to mergen config file")
	cmd.Flags().StringVar(&opts.Registry, "registry", "", "Registry profile name (default: current profile)")
	cmd.Flags().StringVar(&opts.Platform, "platform", config.FromEnv().BasePlatform, "Guest platform key, for example linux-amd64")
	cmd.Flags().StringVar(&opts.Flavor, "flavor", config.FromEnv().BaseFlavor, "Base flavor/distribution key, for example buildroot")
	cmd.Flags().StringVar(&opts.Version, "version", "latest", "Version to pull, default latest")
	return cmd
}

func runConfigInit(opts registryOptions) error {
	configPath, cfg, err := loadRegistryConfig(opts.ConfigPath)
	if err != nil {
		return err
	}

	profile, registryAlias, err := images.ResolveRegistry(cfg, opts.Registry)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)

	profile.Name = registryAlias
	profile.Endpoint, err = optionalString("config S3 endpoint (optional for AWS S3)", firstNonEmpty(opts.Endpoint, profile.Endpoint), opts.NonInteractive, reader)
	if err != nil {
		return err
	}
	profile.Region, err = optionalString("config S3 region", firstNonEmpty(opts.Region, profile.Region), opts.NonInteractive, reader)
	if err != nil {
		return err
	}
	profile.Bucket, err = requiredString("config S3 bucket", firstNonEmpty(opts.Bucket, profile.Bucket), opts.NonInteractive, reader)
	if err != nil {
		return err
	}
	profile.Prefix, err = optionalString("config S3 prefix", firstNonEmpty(opts.Prefix, profile.Prefix), opts.NonInteractive, reader)
	if err != nil {
		return err
	}
	profile.AccessKeyID, err = requiredString("config S3 access key", firstNonEmpty(opts.AccessKeyID, profile.AccessKeyID), opts.NonInteractive, reader)
	if err != nil {
		return err
	}
	profile.SecretAccessKey, err = requiredString("config S3 secret key", firstNonEmpty(opts.SecretAccessKey, profile.SecretAccessKey), opts.NonInteractive, reader)
	if err != nil {
		return err
	}
	profile.Username, err = optionalString("admin username", firstNonEmpty(opts.Username, profile.Username), opts.NonInteractive, reader)
	if err != nil {
		return err
	}
	profile.SessionToken = firstNonEmpty(opts.SessionToken, profile.SessionToken)
	profile.UsePathStyle = opts.UsePathStyle

	cfg = images.UpsertRegistry(cfg, registryAlias, profile, true)
	if err := images.SaveRegistryConfig(configPath, cfg); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stdout, "config: %s\n", configPath)
	_, _ = fmt.Fprintf(os.Stdout, "registry: %s\n", registryAlias)
	_, _ = fmt.Fprintf(os.Stdout, "endpoint: %s\n", profile.Endpoint)
	_, _ = fmt.Fprintf(os.Stdout, "bucket: %s\n", profile.Bucket)
	_, _ = fmt.Fprintf(os.Stdout, "prefix: %s\n", profile.Prefix)
	return nil
}

func runConfigLogin(opts registryOptions) error {
	configPath, cfg, err := loadRegistryConfig(opts.ConfigPath)
	if err != nil {
		return err
	}

	profile, registryAlias, err := images.ResolveRegistry(cfg, opts.Registry)
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

	cfg = images.UpsertRegistry(cfg, registryAlias, profile, true)
	if err := images.SaveRegistryConfig(configPath, cfg); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stdout, "config: %s\n", configPath)
	_, _ = fmt.Fprintf(os.Stdout, "registry: %s\n", registryAlias)
	_, _ = fmt.Fprintf(os.Stdout, "username: %s\n", profile.Username)
	_, _ = fmt.Fprintln(os.Stdout, "login: stored locally (no remote validation yet)")
	return nil
}

func loadRegistryAwareConfig(configPath, registry string) (config.Config, string, error) {
	cfg := config.FromEnv()
	resolvedPath, profileCfg, err := loadRegistryConfig(configPath)
	if err != nil {
		return config.Config{}, "", err
	}
	profile, _, err := images.ResolveRegistry(profileCfg, registry)
	if err != nil {
		return config.Config{}, "", err
	}
	return images.ApplyRegistryProfile(cfg, profile), resolvedPath, nil
}

func loadRegistryConfig(path string) (string, images.RegistryConfig, error) {
	if strings.TrimSpace(path) == "" {
		resolved, err := images.DefaultRegistryConfigPath()
		if err != nil {
			return "", images.RegistryConfig{}, err
		}
		path = resolved
	}
	cfg, err := images.LoadRegistryConfig(path)
	if err != nil {
		return "", images.RegistryConfig{}, err
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
