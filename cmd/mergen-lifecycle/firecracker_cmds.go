package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/alperreha/mergen-fire/internal/firecracker"
	"github.com/alperreha/mergen-fire/internal/model"
)

const (
	defaultConfigureTimeoutSeconds = 20
	defaultControlTimeoutSeconds   = 10
	entropyDisabledEnvFlag         = "0"
)

func runConfigureStart(ctx context.Context, vmID string, logger *slog.Logger) error {
	paths := resolvePaths(vmID)
	timeout := time.Duration(getEnvInt("MGN_CONFIGURE_TIMEOUT_SECONDS", defaultConfigureTimeoutSeconds)) * time.Second
	enableEntropy := strings.TrimSpace(getEnv("MGN_ENABLE_ENTROPY_DEVICE", "1")) != entropyDisabledEnvFlag

	vmCfg, err := readVMConfig(paths.vmJSONPath)
	if err != nil {
		return err
	}
	if err := validateVMConfig(vmCfg); err != nil {
		return err
	}

	waitCtx, waitCancel := context.WithTimeout(ctx, timeout)
	defer waitCancel()
	if err := firecracker.WaitForSocket(waitCtx, paths.socketPath, 200*time.Millisecond); err != nil {
		return err
	}

	configurator := firecracker.NewConfigurator(timeout, logger.With("component", "firecracker"))
	configureOpts := firecracker.ConfigureOptions{
		EnableEntropyDevice: enableEntropy,
	}
	if err := configurator.ConfigureAndStart(ctx, paths.socketPath, vmCfg, configureOpts); err != nil {
		return err
	}

	logger.Info("firecracker configured and started", "vmID", vmID, "socketPath", paths.socketPath, "entropyEnabled", enableEntropy)
	return nil
}

func runGracefulStop(ctx context.Context, vmID string, logger *slog.Logger) error {
	paths := resolvePaths(vmID)
	timeout := time.Duration(getEnvInt("MGN_GRACEFUL_STOP_TIMEOUT_SECONDS", defaultControlTimeoutSeconds)) * time.Second

	socketPresent, err := firecracker.SocketPresent(paths.socketPath)
	if err != nil {
		return err
	}
	if !socketPresent {
		logger.Debug("socket absent, skipping graceful stop", "vmID", vmID, "socketPath", paths.socketPath)
		return nil
	}

	if err := firecracker.SendCtrlAltDel(ctx, paths.socketPath, timeout); err != nil {
		// Keep ExecStop best-effort behavior.
		logger.Warn("graceful stop signal failed", "vmID", vmID, "socketPath", paths.socketPath, "error", err)
		return nil
	}

	logger.Info("graceful stop signal sent", "vmID", vmID, "socketPath", paths.socketPath)
	return nil
}

func runRestart(ctx context.Context, vmID string, logger *slog.Logger) error {
	paths := resolvePaths(vmID)
	timeout := time.Duration(getEnvInt("MGN_RESTART_TIMEOUT_SECONDS", defaultControlTimeoutSeconds)) * time.Second

	socketPresent, err := firecracker.SocketPresent(paths.socketPath)
	if err != nil {
		return err
	}
	if !socketPresent {
		return fmt.Errorf("socket not present: %s", paths.socketPath)
	}

	if err := firecracker.SendCtrlAltDel(ctx, paths.socketPath, timeout); err != nil {
		return err
	}
	logger.Info("restart signal sent", "vmID", vmID, "socketPath", paths.socketPath)
	return nil
}

func runStatus(ctx context.Context, vmID string, logger *slog.Logger) error {
	paths := resolvePaths(vmID)
	timeout := time.Duration(getEnvInt("MGN_STATUS_TIMEOUT_SECONDS", 3)) * time.Second

	socketPresent, err := firecracker.SocketPresent(paths.socketPath)
	if err != nil {
		return err
	}

	response := map[string]any{
		"vmID":          vmID,
		"socketPath":    paths.socketPath,
		"socketPresent": socketPresent,
	}

	if socketPresent {
		info, infoErr := firecracker.GetInstanceInfo(ctx, paths.socketPath, timeout)
		if infoErr != nil {
			response["error"] = infoErr.Error()
		} else {
			response["instance"] = info
		}
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(response); err != nil {
		return err
	}
	logger.Debug("status emitted", "vmID", vmID, "socketPresent", socketPresent)
	return nil
}

func readVMConfig(path string) (model.VMConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return model.VMConfig{}, fmt.Errorf("read vm config: %w", err)
	}

	var cfg model.VMConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return model.VMConfig{}, fmt.Errorf("parse vm config: %w", err)
	}
	return cfg, nil
}

func validateVMConfig(cfg model.VMConfig) error {
	return model.ValidateVMConfig(cfg)
}
