package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alperreha/mergen-fire/internal/logging"
	"github.com/alperreha/mergen-fire/internal/xdscenter"
)

func main() {
	cfg, err := xdscenter.FromEnv()
	if err != nil {
		_, _ = os.Stderr.WriteString("xds center config error: " + err.Error() + "\n")
		os.Exit(1)
	}

	logger := logging.New(cfg.LogLevel, cfg.LogFormat).With("component", "mergen-xds-center")
	service := xdscenter.NewService(cfg, logger.With("component", "service"))

	command := "serve"
	if len(os.Args) > 1 {
		command = strings.ToLower(strings.TrimSpace(os.Args[1]))
	}

	switch command {
	case "serve":
		runServe(service, logger)
	case "resolve":
		runResolve(service)
	case "list-routes":
		runList(service, cfg.Domain)
	case "sync-consul":
		runSyncConsul(service, cfg.RequestTimeout)
	default:
		_, _ = os.Stderr.WriteString("unknown command: " + command + "\n")
		_, _ = os.Stderr.WriteString("supported commands: serve | resolve --host <fqdn> | list-routes | sync-consul\n")
		os.Exit(2)
	}
}

func runServe(service *xdscenter.Service, logger *slog.Logger) {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := service.Run(ctx); err != nil && err != context.Canceled {
		logger.Error("xds center stopped with error", "error", err)
		os.Exit(1)
	}
}

func runResolve(service *xdscenter.Service) {
	fs := flag.NewFlagSet("resolve", flag.ExitOnError)
	host := fs.String("host", "", "fqdn to resolve, e.g. app1.vm.example.com")
	_ = fs.Parse(os.Args[2:])

	if strings.TrimSpace(*host) == "" {
		_, _ = os.Stderr.WriteString("resolve requires --host\n")
		os.Exit(2)
	}
	route, err := service.Resolve(*host)
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	printJSON(route)
}

func runList(service *xdscenter.Service, domain string) {
	routes, err := service.ListRoutes()
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	printJSON(xdscenter.RoutesResponse{
		Domain: domain,
		Count:  len(routes),
		Routes: routes,
	})
}

func runSyncConsul(service *xdscenter.Service, timeoutDuration time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)
	defer cancel()

	result, err := service.SyncConsul(ctx)
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	printJSON(result)
}

func printJSON(payload any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
}
