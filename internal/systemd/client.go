package systemd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("systemd unavailable on this host")
var ErrUnitNotFound = errors.New("systemd unit not found")

type Status struct {
	Available   bool
	Unit        string
	Active      bool
	ActiveState string
	SubState    string
	MainPID     int
}

type Client interface {
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Disable(ctx context.Context, id string) error
	IsActive(ctx context.Context, id string) (bool, error)
	Status(ctx context.Context, id string) (Status, error)
}

type ExecClient struct {
	systemctl  string
	unitPrefix string
	timeout    time.Duration
	available  bool
	logger     *slog.Logger
}

func NewExecClient(systemctlPath, unitPrefix string, timeout time.Duration, logger *slog.Logger) *ExecClient {
	if logger == nil {
		logger = slog.Default()
	}
	path, err := exec.LookPath(systemctlPath)
	if err != nil {
		logger.Warn("systemctl not found in PATH", "path", systemctlPath, "error", err)
		return &ExecClient{
			systemctl:  systemctlPath,
			unitPrefix: unitPrefix,
			timeout:    timeout,
			available:  false,
			logger:     logger,
		}
	}

	logger.Debug("systemd client initialized", "systemctl", path, "unitPrefix", unitPrefix, "timeout", timeout.String())
	return &ExecClient{
		systemctl:  path,
		unitPrefix: unitPrefix,
		timeout:    timeout,
		available:  true,
		logger:     logger,
	}
}

func (c *ExecClient) Start(ctx context.Context, id string) error {
	c.logger.Debug("systemd start requested", "vmID", id, "unit", c.unitName(id))
	active, err := c.IsActive(ctx, id)
	if err != nil {
		return err
	}
	if active {
		c.logger.Debug("systemd start skipped because unit is already active", "vmID", id, "unit", c.unitName(id))
		return nil
	}
	_, err = c.run(ctx, "start", c.unitName(id))
	if err == nil {
		c.logger.Debug("systemd start succeeded", "vmID", id, "unit", c.unitName(id))
	}
	return err
}

func (c *ExecClient) Stop(ctx context.Context, id string) error {
	c.logger.Debug("systemd stop requested", "vmID", id, "unit", c.unitName(id))
	active, err := c.IsActive(ctx, id)
	if err != nil {
		return err
	}
	if !active {
		c.logger.Debug("systemd stop skipped because unit is already inactive", "vmID", id, "unit", c.unitName(id))
		return nil
	}
	_, err = c.run(ctx, "stop", c.unitName(id))
	if err == nil {
		c.logger.Debug("systemd stop succeeded", "vmID", id, "unit", c.unitName(id))
	}
	return err
}

func (c *ExecClient) Disable(ctx context.Context, id string) error {
	c.logger.Debug("systemd disable requested", "vmID", id, "unit", c.unitName(id))
	_, err := c.run(ctx, "disable", c.unitName(id))
	if err == nil {
		c.logger.Debug("systemd disable succeeded", "vmID", id, "unit", c.unitName(id))
	}
	return err
}

func (c *ExecClient) IsActive(ctx context.Context, id string) (bool, error) {
	_, err := c.run(ctx, "is-active", "--quiet", c.unitName(id))
	if err == nil {
		c.logger.Debug("systemd unit is active", "vmID", id, "unit", c.unitName(id))
		return true, nil
	}
	if errors.Is(err, ErrUnavailable) {
		return false, ErrUnavailable
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		c.logger.Debug("systemd unit is inactive", "vmID", id, "unit", c.unitName(id))
		return false, nil
	}
	return false, err
}

func (c *ExecClient) Status(ctx context.Context, id string) (Status, error) {
	status := Status{
		Available: c.available,
		Unit:      c.unitName(id),
	}

	if !c.available {
		return status, nil
	}

	output, err := c.run(ctx, "show", c.unitName(id), "--property=MainPID", "--property=ActiveState", "--property=SubState")
	if err != nil {
		return status, err
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "MainPID":
			if pid, convErr := strconv.Atoi(value); convErr == nil {
				status.MainPID = pid
			}
		case "ActiveState":
			status.ActiveState = value
		case "SubState":
			status.SubState = value
		}
	}

	status.Active = status.ActiveState == "active"
	c.logger.Debug("systemd status read", "vmID", id, "unit", status.Unit, "activeState", status.ActiveState, "subState", status.SubState, "mainPID", status.MainPID)
	return status, nil
}

func (c *ExecClient) unitName(id string) string {
	return fmt.Sprintf("%s@%s.service", c.unitPrefix, id)
}

func (c *ExecClient) run(ctx context.Context, args ...string) ([]byte, error) {
	if !c.available {
		c.logger.Debug("systemd run skipped because client unavailable", "args", strings.Join(args, " "))
		return nil, ErrUnavailable
	}

	runCtx := ctx
	cancel := func() {}
	if c.timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, c.timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, c.systemctl, args...)
	c.logger.Debug("executing systemctl command", "command", c.systemctl, "args", strings.Join(args, " "))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err == nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			c.logger.Debug("systemctl command output", "args", strings.Join(args, " "), "output", trimmed)
		}
		return output, nil
	}

	fullErrText := strings.TrimSpace(stderr.String())
	if fullErrText == "" {
		fullErrText = strings.TrimSpace(string(output))
	}
	if strings.Contains(fullErrText, "System has not been booted with systemd") || strings.Contains(fullErrText, "Failed to connect to bus") {
		c.logger.Warn("systemd appears unavailable", "args", strings.Join(args, " "), "error", fullErrText)
		return nil, ErrUnavailable
	}
	if strings.Contains(fullErrText, "Unit ") && strings.Contains(fullErrText, " not found") {
		c.logger.Warn("systemd unit not found", "args", strings.Join(args, " "), "error", fullErrText)
		return nil, fmt.Errorf("%w: %s", ErrUnitNotFound, fullErrText)
	}
	if _, ok := err.(*exec.ExitError); ok {
		c.logger.Debug("systemctl command exited with non-zero status", "args", strings.Join(args, " "), "error", fullErrText)
		return output, err
	}
	c.logger.Error("systemctl command failed", "args", strings.Join(args, " "), "error", err)
	return nil, fmt.Errorf("systemctl %s failed: %w", strings.Join(args, " "), err)
}
