package systemd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("systemd unavailable on this host")

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
}

func NewExecClient(systemctlPath, unitPrefix string, timeout time.Duration) *ExecClient {
	path, err := exec.LookPath(systemctlPath)
	if err != nil {
		return &ExecClient{
			systemctl:  systemctlPath,
			unitPrefix: unitPrefix,
			timeout:    timeout,
			available:  false,
		}
	}

	return &ExecClient{
		systemctl:  path,
		unitPrefix: unitPrefix,
		timeout:    timeout,
		available:  true,
	}
}

func (c *ExecClient) Start(ctx context.Context, id string) error {
	active, err := c.IsActive(ctx, id)
	if err != nil {
		return err
	}
	if active {
		return nil
	}
	_, err = c.run(ctx, "start", c.unitName(id))
	return err
}

func (c *ExecClient) Stop(ctx context.Context, id string) error {
	active, err := c.IsActive(ctx, id)
	if err != nil {
		return err
	}
	if !active {
		return nil
	}
	_, err = c.run(ctx, "stop", c.unitName(id))
	return err
}

func (c *ExecClient) Disable(ctx context.Context, id string) error {
	_, err := c.run(ctx, "disable", c.unitName(id))
	return err
}

func (c *ExecClient) IsActive(ctx context.Context, id string) (bool, error) {
	_, err := c.run(ctx, "is-active", "--quiet", c.unitName(id))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrUnavailable) {
		return false, ErrUnavailable
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
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
	return status, nil
}

func (c *ExecClient) unitName(id string) string {
	return fmt.Sprintf("%s@%s.service", c.unitPrefix, id)
}

func (c *ExecClient) run(ctx context.Context, args ...string) ([]byte, error) {
	if !c.available {
		return nil, ErrUnavailable
	}

	runCtx := ctx
	cancel := func() {}
	if c.timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, c.timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, c.systemctl, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err == nil {
		return output, nil
	}

	fullErrText := strings.TrimSpace(stderr.String())
	if fullErrText == "" {
		fullErrText = strings.TrimSpace(string(output))
	}
	if strings.Contains(fullErrText, "System has not been booted with systemd") || strings.Contains(fullErrText, "Failed to connect to bus") {
		return nil, ErrUnavailable
	}
	if _, ok := err.(*exec.ExitError); ok {
		return output, err
	}
	return nil, fmt.Errorf("systemctl %s failed: %w", strings.Join(args, " "), err)
}
