package main

import (
	"log/slog"
	"os"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/alperreha/mergen-fire/internal/api"
	"github.com/alperreha/mergen-fire/internal/config"
	"github.com/alperreha/mergen-fire/internal/hooks"
	"github.com/alperreha/mergen-fire/internal/manager"
	"github.com/alperreha/mergen-fire/internal/network"
	"github.com/alperreha/mergen-fire/internal/store"
	"github.com/alperreha/mergen-fire/internal/systemd"
)

func main() {
	cfg := config.FromEnv()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	fsStore := store.NewFSStore(cfg.ConfigRoot, cfg.DataRoot, cfg.RunRoot, cfg.GlobalHooksDir)
	if err := fsStore.EnsureBaseDirs(); err != nil {
		logger.Error("failed to create base directories", "error", err)
		os.Exit(1)
	}

	systemdClient := systemd.NewExecClient(cfg.SystemctlPath, cfg.UnitPrefix, cfg.CommandTimeout)
	hookRunner := hooks.NewRunner(logger)
	allocator := network.NewAllocator(cfg.PortStart, cfg.PortEnd, cfg.GuestCIDR)
	service := manager.NewService(fsStore, systemdClient, hookRunner, allocator, logger)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())

	e.GET("/healthz", func(c echo.Context) error {
		return c.JSON(200, map[string]string{"status": "ok"})
	})
	api.Register(e, service)

	logger.Info("manager started", "addr", cfg.HTTPAddr)
	if err := e.Start(cfg.HTTPAddr); err != nil {
		logger.Error("http server stopped", "error", err)
		os.Exit(1)
	}
}
