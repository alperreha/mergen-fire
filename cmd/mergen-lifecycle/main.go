package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type cliError struct {
	code int
	err  error
}

func (e *cliError) Error() string {
	return e.err.Error()
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{}))

	if len(os.Args) < 3 {
		printUsage()
		os.Exit(2)
	}

	command := strings.TrimSpace(os.Args[1])
	vmID := strings.TrimSpace(os.Args[2])
	if vmID == "" {
		logger.Error("vm id is required")
		os.Exit(2)
	}

	loadVMEnvFile(vmID, logger)
	ctx := context.Background()

	var err error
	switch command {
	case "pre-start":
		err = runPreStart(ctx, vmID, logger)
	case "start":
		err = runStart(ctx, vmID, logger)
	case "post-start":
		err = runPostStart(ctx, vmID, logger)
	case "pre-stop":
		err = runPreStop(ctx, vmID, logger)
	case "stop":
		err = runStop(ctx, vmID, logger)
	case "post-stop":
		err = runPostStop(ctx, vmID, logger)
	case "pre-delete":
		err = runPreDelete(ctx, vmID, logger)
	case "delete":
		err = runDelete(ctx, vmID, logger)
	case "post-delete":
		err = runPostDelete(ctx, vmID, logger)
	case "preflight-check":
		err = runPreflightCheck(ctx, vmID, logger)
	case "net-setup":
		err = runNetSetup(ctx, vmID, logger)
	case "jailer-start":
		err = runJailerStart(ctx, vmID, logger)
	case "configure-start":
		err = runConfigureStart(ctx, vmID, logger)
	case "graceful-stop":
		err = runGracefulStop(ctx, vmID, logger)
	case "net-cleanup":
		err = runNetCleanup(ctx, vmID, logger)
	case "on-failure":
		err = runOnFailure(ctx, vmID, logger)
	case "status":
		err = runStatus(ctx, vmID, logger)
	case "restart":
		err = runRestart(ctx, vmID, logger)
	default:
		printUsage()
		os.Exit(2)
	}

	if err == nil {
		return
	}

	var codeErr *cliError
	if errors.As(err, &codeErr) {
		logger.Error("lifecycle command failed", "command", command, "vmID", vmID, "error", codeErr.err)
		os.Exit(codeErr.code)
	}

	logger.Error("lifecycle command failed", "command", command, "vmID", vmID, "error", err)
	os.Exit(1)
}

func printUsage() {
	_, _ = fmt.Fprintf(os.Stderr, "usage: %s <command> <vm-id>\n", os.Args[0])
	_, _ = fmt.Fprintln(os.Stderr, "commands: pre-start|start|post-start|pre-stop|stop|post-stop|pre-delete|delete|post-delete|preflight-check|net-setup|jailer-start|configure-start|graceful-stop|net-cleanup|on-failure|status|restart")
}
