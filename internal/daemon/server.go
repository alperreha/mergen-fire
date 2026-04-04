package daemon

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/alperreha/mergen-fire/internal/api"
	"github.com/alperreha/mergen-fire/internal/config"
	"github.com/alperreha/mergen-fire/internal/hooks"
	"github.com/alperreha/mergen-fire/internal/images"
	"github.com/alperreha/mergen-fire/internal/manager"
	"github.com/alperreha/mergen-fire/internal/network"
	"github.com/alperreha/mergen-fire/internal/store"
	"github.com/alperreha/mergen-fire/internal/systemd"
	"github.com/alperreha/mergen-fire/pkg/logging"
)

func RunFromEnv(ctx context.Context) error {
	cfg := config.FromEnv()
	logger := logging.New(cfg.LogLevel, cfg.LogFormat).With("component", "mergen")
	logger.Info("bootstrapping daemon", "pid", os.Getpid(), "logLevel", cfg.LogLevel, "logFormat", cfg.LogFormat)

	fsStore := store.
		NewFSStore(cfg.ConfigRoot, cfg.DataRoot, cfg.RunRoot, cfg.GlobalHooksDir).
		WithLogger(logger.With("component", "store"))
	if err := fsStore.EnsureBaseDirs(); err != nil {
		return err
	}

	imageService := images.NewService(cfg, logger.With("component", "images"))
	if err := imageService.EnsureLayout(); err != nil {
		return err
	}

	systemdClient := systemd.NewClient(cfg.SystemctlPath, cfg.UnitPrefix, cfg.CommandTimeout, logger.With("component", "systemd"))
	hookRunner := hooks.NewRunner(logger.With("component", "hooks"))
	allocator := network.
		NewAllocator(cfg.PortStart, cfg.PortEnd, cfg.GuestCIDR).
		WithLogger(logger.With("component", "network"))
	service := manager.NewService(fsStore, systemdClient, hookRunner, allocator, logger.With("component", "service"))

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())

	e.GET("/healthz", func(c echo.Context) error {
		logger.Debug("healthz requested", "remoteAddr", c.Request().RemoteAddr)
		return c.JSON(200, map[string]string{"status": "ok"})
	})
	api.Register(e, service, imageService, logger.With("component", "api"))

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           e,
		ReadHeaderTimeout: cfg.CommandTimeout,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		logger.Info("daemon started", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
			return
		}
		serverErrCh <- nil
	}()

	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	sigCtx, cancel := signal.NotifyContext(runCtx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	select {
	case err := <-serverErrCh:
		return err
	case <-sigCtx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownTimeout := cfg.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = 15 * time.Second
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return <-serverErrCh
}
