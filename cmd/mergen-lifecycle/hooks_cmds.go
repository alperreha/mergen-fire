package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alperreha/mergen-fire/internal/model"
)

const defaultLifecycleLogFile = "/var/log/mergen/vm-lifecycle.log"

type lifecycleEmitter struct {
	vmID    string
	stage   string
	unit    string
	logFile string
}

func runPreflightCheck(_ context.Context, vmID string, _ *slog.Logger) error {
	paths := resolvePaths(vmID)
	logFile := getEnv("MGN_LIFECYCLE_LOG_FILE", defaultLifecycleLogFile)
	emitter := lifecycleEmitter{
		vmID:    vmID,
		stage:   "exec-condition",
		logFile: logFile,
	}

	if err := requireFile(paths.vmJSONPath); err != nil {
		return preflightFail(emitter, "missing_vm_json", fmt.Sprintf("vm_json not found: %s", paths.vmJSONPath))
	}
	if err := requireFile(paths.metaPath); err != nil {
		return preflightFail(emitter, "missing_meta_json", fmt.Sprintf("meta_json not found: %s", paths.metaPath))
	}

	content, err := os.ReadFile(paths.vmJSONPath)
	if err != nil {
		return preflightFail(emitter, "invalid_vm_json", err.Error())
	}

	var cfg model.VMConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return preflightFail(emitter, "invalid_vm_json", fmt.Sprintf("cannot parse vm.json: %v", err))
	}

	if cfg.BootSource == nil {
		return preflightFail(emitter, "invalid_vm_json", "boot-source is missing")
	}
	kernelPath := strings.TrimSpace(model.StringValue(cfg.BootSource.KernelImagePath))
	if kernelPath == "" {
		return preflightFail(emitter, "invalid_vm_json", "boot-source.kernel_image_path is empty")
	}
	if err := requireFile(kernelPath); err != nil {
		return preflightFail(emitter, "missing_kernel_image", fmt.Sprintf("kernel_image not found: %s", kernelPath))
	}

	rootDriveFound := false
	for _, drive := range cfg.Drives {
		if drive == nil {
			continue
		}
		drivePath := strings.TrimSpace(model.StringValue(drive.PathOnHost))
		if drivePath == "" {
			continue
		}
		if err := requireFile(drivePath); err != nil {
			return preflightFail(emitter, "missing_drive", fmt.Sprintf("drive not found: %s", drivePath))
		}
		if model.BoolValue(drive.IsRootDevice) {
			rootDriveFound = true
		}
	}
	if !rootDriveFound {
		return preflightFail(emitter, "missing_root_drive", "vm.json has no root drive entry")
	}

	emitter.emit("info", "preflight_ok", "all required vm files are present")
	return nil
}

func runOnFailure(_ context.Context, vmID string, _ *slog.Logger) error {
	unitPrefix := getEnv("MGN_UNIT_PREFIX", "mergen")
	unitName := getEnv("MGN_UNIT_NAME", fmt.Sprintf("%s@%s.service", unitPrefix, vmID))
	logFile := getEnv("MGN_LIFECYCLE_LOG_FILE", defaultLifecycleLogFile)
	tailLines := getEnvInt("MGN_FAILURE_JOURNAL_TAIL", 40)

	emitter := lifecycleEmitter{
		vmID:    vmID,
		stage:   "on-failure",
		unit:    unitName,
		logFile: logFile,
	}

	emitter.emit("error", "vm_unit_failed", "systemd unit entered failed state")

	if commandExists("systemctl") {
		output, cmdErr := runCommandCombined(
			"systemctl", "show", unitName,
			"--property=Result",
			"--property=ExecMainStatus",
			"--property=ActiveState",
			"--property=SubState",
			"--property=StateChangeTimestamp",
			"--no-pager",
		)
		if strings.TrimSpace(output) != "" {
			for _, line := range strings.Split(output, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				emitter.emit("error", "systemd_status", line)
			}
		} else {
			emitter.emit("warn", "systemd_status_empty", "systemctl show returned no output")
		}
		if cmdErr != nil {
			emitter.emit("warn", "systemd_status_error", cmdErr.Error())
		}
	} else {
		emitter.emit("warn", "systemctl_missing", "systemctl command not found in PATH")
	}

	if commandExists("journalctl") {
		emitter.emit("error", "journal_tail_begin", fmt.Sprintf("collecting last %d lines", tailLines))
		output, cmdErr := runCommandCombined(
			"journalctl", "-u", unitName, "-n", strconv.Itoa(tailLines), "--no-pager", "--output=short-iso",
		)
		if strings.TrimSpace(output) != "" {
			for _, line := range strings.Split(output, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				fmt.Println(line)
				emitter.appendRaw(line)
			}
		} else {
			emitter.emit("warn", "journal_tail_empty", "no recent journal lines found")
		}
		if cmdErr != nil {
			emitter.emit("warn", "journal_tail_error", cmdErr.Error())
		}
		emitter.emit("error", "journal_tail_end", "journal capture completed")
	} else {
		emitter.emit("warn", "journalctl_missing", "journalctl command not found in PATH")
	}

	return nil
}

func preflightFail(emitter lifecycleEmitter, event, message string) error {
	emitter.emit("error", event, message)
	return &cliError{
		code: 255,
		err:  fmt.Errorf("%s", message),
	}
}

func requireFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("expected file, got directory: %s", path)
	}
	return nil
}

func (e lifecycleEmitter) emit(level, event, message string) {
	ts := time.Now().UTC().Format(time.RFC3339)
	escaped := strings.ReplaceAll(message, "\"", "\\\"")

	line := fmt.Sprintf("%s level=%s vm_id=%s stage=%s event=%s msg=\"%s\"", ts, level, e.vmID, e.stage, event, escaped)
	if e.unit != "" {
		line = fmt.Sprintf("%s level=%s vm_id=%s stage=%s unit=%s event=%s msg=\"%s\"", ts, level, e.vmID, e.stage, e.unit, event, escaped)
	}
	fmt.Println(line)
	e.appendRaw(line)
}

func (e lifecycleEmitter) appendRaw(line string) {
	if strings.TrimSpace(e.logFile) == "" {
		return
	}

	logDir := filepath.Dir(e.logFile)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return
	}

	file, err := os.OpenFile(e.logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(line + "\n")
}
