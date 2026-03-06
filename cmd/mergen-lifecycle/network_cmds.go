package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"

	lifecyclehooks "github.com/alperreha/mergen-fire/cmd/mergen-lifecycle/hooks"
	hookpoststop "github.com/alperreha/mergen-fire/cmd/mergen-lifecycle/hooks/poststop"
	hookprestart "github.com/alperreha/mergen-fire/cmd/mergen-lifecycle/hooks/prestart"
)

func runJailerStart(ctx context.Context, vmID string, logger *slog.Logger) error {
	if ctx == nil {
		ctx = context.Background()
	}

	paths := resolvePaths(vmID)
	netnsName := strings.TrimSpace(getEnv("MGN_NETNS", ""))
	firecrackerBinary := strings.TrimSpace(getEnv("MGN_FIRECRACKER_BIN", getEnv("FIRECRACKER_BIN", "firecracker")))

	if err := os.MkdirAll(paths.runDir, 0o755); err != nil {
		return err
	}
	if err := os.Remove(paths.socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	firecrackerPath, err := exec.LookPath(firecrackerBinary)
	if err != nil {
		return fmt.Errorf("firecracker binary not found: %s", firecrackerBinary)
	}

	firecrackerArgs := []string{firecrackerPath, "--api-sock", paths.socketPath}
	if netnsName != "" {
		ipPath, ipErr := exec.LookPath("ip")
		if ipErr == nil {
			exists, existsErr := lifecyclehooks.NetNSExists(ctx, netnsName)
			if existsErr != nil {
				return existsErr
			}
			if exists {
				logger.Info("starting firecracker in netns", "vmID", vmID, "netns", netnsName, "socketPath", paths.socketPath)
				return syscall.Exec(ipPath, []string{"ip", "netns", "exec", netnsName, firecrackerPath, "--api-sock", paths.socketPath}, os.Environ())
			}
			logger.Warn("netns not found, starting firecracker in host netns", "vmID", vmID, "netns", netnsName)
		}
	}

	logger.Info("starting firecracker in host netns", "vmID", vmID, "socketPath", paths.socketPath)
	return syscall.Exec(firecrackerPath, firecrackerArgs, os.Environ())
}

func runNetSetup(ctx context.Context, vmID string, logger *slog.Logger) error {
	req, err := buildHookRequest(stagePreStart, vmID, logger)
	if err != nil {
		return err
	}
	return hookprestart.HandleCreateNetwork(ctx, req)
}

func runNetCleanup(ctx context.Context, vmID string, logger *slog.Logger) error {
	req, err := buildHookRequest(stagePostStop, vmID, logger)
	if err != nil {
		return err
	}
	return hookpoststop.HandleDeleteNetwork(ctx, req)
}

func runCommandCombined(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		return trimmed, err
	}
	return trimmed, nil
}
