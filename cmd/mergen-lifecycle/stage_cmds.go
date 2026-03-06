package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultDaemonLogFile = "/var/lib/mergen/daemon.log"

func runPreStart(ctx context.Context, vmID string, logger *slog.Logger) error {
	if err := runPreflightCheck(ctx, vmID, logger); err != nil {
		return err
	}
	if err := runStageHooks(ctx, stagePreStart, vmID, logger); err != nil {
		return &cliError{code: 255, err: err}
	}
	appendDaemonLog(vmID, string(stagePreStart), "vm pre-start completed")
	return nil
}

func runStart(ctx context.Context, vmID string, logger *slog.Logger) error {
	if err := runStageHooks(ctx, stageStart, vmID, logger); err != nil {
		return err
	}
	appendDaemonLog(vmID, string(stageStart), "vm start pipeline entered")
	return runJailerStart(ctx, vmID, logger)
}

func runPostStart(ctx context.Context, vmID string, logger *slog.Logger) error {
	if err := runConfigureStart(ctx, vmID, logger); err != nil {
		return err
	}
	if err := runStageHooks(ctx, stagePostStart, vmID, logger); err != nil {
		return err
	}
	appendDaemonLog(vmID, string(stagePostStart), "vm post-start completed")
	return nil
}

func runPreStop(ctx context.Context, vmID string, logger *slog.Logger) error {
	if err := runStageHooks(ctx, stagePreStop, vmID, logger); err != nil {
		return err
	}
	appendDaemonLog(vmID, string(stagePreStop), "vm pre-stop completed")
	return nil
}

func runStop(ctx context.Context, vmID string, logger *slog.Logger) error {
	if err := runPreStop(ctx, vmID, logger); err != nil {
		return err
	}
	if err := runGracefulStop(ctx, vmID, logger); err != nil {
		return err
	}
	if err := runStageHooks(ctx, stageStop, vmID, logger); err != nil {
		return err
	}
	appendDaemonLog(vmID, string(stageStop), "vm stop signal sent")
	return nil
}

func runPostStop(ctx context.Context, vmID string, logger *slog.Logger) error {
	if err := runStageHooks(ctx, stagePostStop, vmID, logger); err != nil {
		return err
	}
	appendDaemonLog(vmID, string(stagePostStop), "vm post-stop completed")
	return nil
}

func runPreDelete(ctx context.Context, vmID string, logger *slog.Logger) error {
	if err := runStageHooks(ctx, stagePreDelete, vmID, logger); err != nil {
		return err
	}
	appendDaemonLog(vmID, string(stagePreDelete), "vm pre-delete completed")
	return nil
}

func runDelete(ctx context.Context, vmID string, logger *slog.Logger) error {
	if err := runStageHooks(ctx, stageDelete, vmID, logger); err != nil {
		return err
	}
	appendDaemonLog(vmID, string(stageDelete), "vm delete stage completed")
	return nil
}

func runPostDelete(ctx context.Context, vmID string, logger *slog.Logger) error {
	if err := runStageHooks(ctx, stagePostDelete, vmID, logger); err != nil {
		return err
	}
	appendDaemonLog(vmID, string(stagePostDelete), "vm post-delete completed")
	return nil
}

func appendDaemonLog(vmID, stage, message string) {
	logFile := strings.TrimSpace(getEnv("MGN_DAEMON_LOG_FILE", defaultDaemonLogFile))
	if logFile == "" {
		return
	}

	logDir := filepath.Dir(logFile)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return
	}

	file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return
	}
	defer file.Close()

	line := fmt.Sprintf("%s vm_id=%s stage=%s msg=\"%s\"\n", time.Now().UTC().Format(time.RFC3339), vmID, stage, message)
	_, _ = file.WriteString(line)
}
