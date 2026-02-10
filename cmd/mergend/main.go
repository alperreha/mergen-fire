package main

import (
	"os"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/alperreha/mergen-fire/internal/api"
	"github.com/alperreha/mergen-fire/internal/config"
	"github.com/alperreha/mergen-fire/internal/hooks"
	"github.com/alperreha/mergen-fire/internal/logging"
	"github.com/alperreha/mergen-fire/internal/manager"
	"github.com/alperreha/mergen-fire/internal/network"
	"github.com/alperreha/mergen-fire/internal/store"
	"github.com/alperreha/mergen-fire/internal/systemd"
)

func main() {
	cfg := config.FromEnv()
	logger := logging.New(cfg.LogLevel, cfg.LogFormat).With("component", "mergend")
	logger.Info("bootstrapping daemon", "pid", os.Getpid(), "logLevel", cfg.LogLevel, "logFormat", cfg.LogFormat)

	fsStore := store.
		NewFSStore(cfg.ConfigRoot, cfg.DataRoot, cfg.RunRoot, cfg.GlobalHooksDir).
		WithLogger(logger.With("component", "store"))
	if err := fsStore.EnsureBaseDirs(); err != nil {
		logger.Error("failed to create base directories", "error", err)
		os.Exit(1)
	}

	systemdClient := systemd.NewExecClient(cfg.SystemctlPath, cfg.UnitPrefix, cfg.CommandTimeout, logger.With("component", "systemd"))
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
	api.Register(e, service, logger.With("component", "api"))

	logger.Info("daemon started", "addr", cfg.HTTPAddr)
	if err := e.Start(cfg.HTTPAddr); err != nil {
		logger.Error("http server stopped", "error", err)
		os.Exit(1)
	}
}
