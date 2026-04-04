package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	root.AddCommand(newImagesCmd())
	return root
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

func newImagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "images",
		Short: "Manage local and remote Mergen base/payload images",
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
		Short: "Push local artifacts to S3-compatible object storage",
	}
	cmd.AddCommand(newImagesPushBaseCmd())
	cmd.AddCommand(newImagesPushPayloadCmd())
	return cmd
}

func newImagesPullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull artifacts from S3-compatible object storage",
	}
	cmd.AddCommand(newImagesPullBaseCmd())
	cmd.AddCommand(newImagesPullPayloadCmd())
	return cmd
}

func newImagesPushBaseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "base",
		Short: "Push /var/lib/mergen/base/current to S3",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := config.FromEnv()
			reporter := images.NewCLIReporter(os.Stderr)
			svc := images.NewService(cfg, logging.New(cfg.LogLevel, cfg.LogFormat).With("component", "images"))
			summary, err := svc.PushBase(context.Background(), reporter)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(os.Stdout, images.FormatSummary(summary))
			return nil
		},
	}
}

func newImagesPullBaseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "base",
		Short: "Pull /var/lib/mergen/base/current from S3",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := config.FromEnv()
			reporter := images.NewCLIReporter(os.Stderr)
			svc := images.NewService(cfg, logging.New(cfg.LogLevel, cfg.LogFormat).With("component", "images"))
			summary, err := svc.PullBase(context.Background(), reporter)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(os.Stdout, images.FormatSummary(summary))
			return nil
		},
	}
}

func newImagesPushPayloadCmd() *cobra.Command {
	var imageRef string
	cmd := &cobra.Command{
		Use:   "payload",
		Short: "Push a converted payload image directory to S3",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := config.FromEnv()
			reporter := images.NewCLIReporter(os.Stderr)
			svc := images.NewService(cfg, logging.New(cfg.LogLevel, cfg.LogFormat).With("component", "images"))
			summary, err := svc.PushPayload(context.Background(), imageRef, reporter)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(os.Stdout, images.FormatSummary(summary))
			return nil
		},
	}
	cmd.Flags().StringVar(&imageRef, "image", "", "Image reference under /var/lib/mergen/images/<image-ref>")
	_ = cmd.MarkFlagRequired("image")
	return cmd
}

func newImagesPullPayloadCmd() *cobra.Command {
	var imageRef string
	cmd := &cobra.Command{
		Use:   "payload",
		Short: "Pull a payload image directory from S3",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := config.FromEnv()
			reporter := images.NewCLIReporter(os.Stderr)
			svc := images.NewService(cfg, logging.New(cfg.LogLevel, cfg.LogFormat).With("component", "images"))
			summary, err := svc.PullPayload(context.Background(), imageRef, reporter)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(os.Stdout, images.FormatSummary(summary))
			return nil
		},
	}
	cmd.Flags().StringVar(&imageRef, "image", "", "Image reference to pull into /var/lib/mergen/images/<image-ref>")
	_ = cmd.MarkFlagRequired("image")
	return cmd
}
