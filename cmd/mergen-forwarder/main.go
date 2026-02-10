package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/alperreha/mergen-fire/internal/forwarder"
	"github.com/alperreha/mergen-fire/internal/logging"
)

func main() {
	cfg, err := forwarder.FromEnv()
	if err != nil {
		_, _ = os.Stderr.WriteString("forwarder config error: " + err.Error() + "\n")
		os.Exit(1)
	}

	logger := logging.New(cfg.LogLevel, cfg.LogFormat).With("component", "mergen-forwarder")
	logger.Info(
		"starting forwarder",
		"configRoot", cfg.ConfigRoot,
		"netnsRoot", cfg.NetNSRoot,
		"listeners", len(cfg.Listeners),
		"domainPrefix", cfg.DomainPrefix,
		"domainSuffix", cfg.DomainSuffix,
	)

	resolver := forwarder.NewResolver(cfg.ConfigRoot, cfg.DomainPrefix, cfg.DomainSuffix, cfg.ResolverCacheTTL, logger.With("component", "resolver"))
	dialer := forwarder.NewNetNSDialer(cfg.DialTimeout, cfg.NetNSRoot)

	server, err := forwarder.NewServer(cfg, resolver, dialer, logger.With("component", "server"))
	if err != nil {
		logger.Error("forwarder server init failed", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := server.Run(ctx); err != nil && err != context.Canceled {
		logger.Error("forwarder stopped with error", "error", err)
		os.Exit(1)
	}
	logger.Info("forwarder stopped")
}
