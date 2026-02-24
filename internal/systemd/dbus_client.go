package systemd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	sddb "github.com/coreos/go-systemd/v22/dbus"
	gdbus "github.com/godbus/dbus/v5"
)

type DBusClient struct {
	conn       *sddb.Conn
	unitPrefix string
	timeout    time.Duration
	available  bool
	logger     *slog.Logger
}

func NewDBusClient(unitPrefix string, timeout time.Duration, logger *slog.Logger) *DBusClient {
	if logger == nil {
		logger = slog.Default()
	}

	// godbus attaches the connection lifecycle to the provided context.
	// If that context is canceled (including timeout cancellation), the
	// DBus connection is closed and subsequent calls fail with:
	// "dbus: connection closed by user".
	conn, err := sddb.NewSystemConnectionContext(context.Background())
	if err != nil {
		logger.Warn("systemd dbus unavailable", "error", err)
		return &DBusClient{
			unitPrefix: unitPrefix,
			timeout:    timeout,
			available:  false,
			logger:     logger,
		}
	}

	logger.Debug("systemd dbus client initialized", "unitPrefix", unitPrefix, "timeout", timeout.String())
	return &DBusClient{
		conn:       conn,
		unitPrefix: unitPrefix,
		timeout:    timeout,
		available:  true,
		logger:     logger,
	}
}

func (c *DBusClient) Start(ctx context.Context, id string) error {
	c.logger.Debug("systemd dbus start requested", "vmID", id, "unit", c.unitName(id))
	active, err := c.IsActive(ctx, id)
	if err != nil && !errors.Is(err, ErrUnitNotFound) {
		return err
	}
	if active {
		c.logger.Debug("systemd dbus start skipped because unit is already active", "vmID", id, "unit", c.unitName(id))
		return nil
	}

	runCtx, cancel := withTimeout(ctx, c.timeout)
	defer cancel()

	resultCh := make(chan string, 1)
	if _, err := c.conn.StartUnitContext(runCtx, c.unitName(id), "replace", resultCh); err != nil {
		return c.mapError(err, c.unitName(id))
	}

	select {
	case result := <-resultCh:
		if result == "done" || result == "skipped" {
			c.logger.Debug("systemd dbus start succeeded", "vmID", id, "unit", c.unitName(id), "result", result)
			return nil
		}
		return fmt.Errorf("systemd start job for %s finished with result=%s", c.unitName(id), result)
	case <-runCtx.Done():
		return runCtx.Err()
	}
}

func (c *DBusClient) Stop(ctx context.Context, id string) error {
	c.logger.Debug("systemd dbus stop requested", "vmID", id, "unit", c.unitName(id))
	active, err := c.IsActive(ctx, id)
	if err != nil && !errors.Is(err, ErrUnitNotFound) {
		return err
	}
	if !active {
		c.logger.Debug("systemd dbus stop skipped because unit is already inactive", "vmID", id, "unit", c.unitName(id))
		return nil
	}

	runCtx, cancel := withTimeout(ctx, c.timeout)
	defer cancel()

	resultCh := make(chan string, 1)
	if _, err := c.conn.StopUnitContext(runCtx, c.unitName(id), "replace", resultCh); err != nil {
		return c.mapError(err, c.unitName(id))
	}

	select {
	case result := <-resultCh:
		if result == "done" || result == "skipped" {
			c.logger.Debug("systemd dbus stop succeeded", "vmID", id, "unit", c.unitName(id), "result", result)
			return nil
		}
		return fmt.Errorf("systemd stop job for %s finished with result=%s", c.unitName(id), result)
	case <-runCtx.Done():
		return runCtx.Err()
	}
}

func (c *DBusClient) Disable(ctx context.Context, id string) error {
	c.logger.Debug("systemd dbus disable requested", "vmID", id, "unit", c.unitName(id))
	if !c.available {
		return ErrUnavailable
	}

	runCtx, cancel := withTimeout(ctx, c.timeout)
	defer cancel()

	if _, err := c.conn.DisableUnitFilesContext(runCtx, []string{c.unitName(id)}, false); err != nil {
		return c.mapError(err, c.unitName(id))
	}
	c.logger.Debug("systemd dbus disable succeeded", "vmID", id, "unit", c.unitName(id))
	return nil
}

func (c *DBusClient) IsActive(ctx context.Context, id string) (bool, error) {
	if !c.available {
		return false, ErrUnavailable
	}

	props, err := c.unitProperties(ctx, id)
	if err != nil {
		return false, err
	}
	state, _ := props["ActiveState"].(string)
	active := state == "active"
	if active {
		c.logger.Debug("systemd dbus unit is active", "vmID", id, "unit", c.unitName(id))
	} else {
		c.logger.Debug("systemd dbus unit is inactive", "vmID", id, "unit", c.unitName(id), "activeState", state)
	}
	return active, nil
}

func (c *DBusClient) Status(ctx context.Context, id string) (Status, error) {
	status := Status{
		Available: c.available,
		Unit:      c.unitName(id),
	}
	if !c.available {
		return status, nil
	}

	props, err := c.unitProperties(ctx, id)
	if err != nil {
		return status, err
	}

	if value, ok := props["ActiveState"].(string); ok {
		status.ActiveState = value
	}
	if value, ok := props["SubState"].(string); ok {
		status.SubState = value
	}
	if pid, ok := asInt(props["MainPID"]); ok {
		status.MainPID = pid
	}

	status.Active = status.ActiveState == "active"
	c.logger.Debug("systemd dbus status read", "vmID", id, "unit", status.Unit, "activeState", status.ActiveState, "subState", status.SubState, "mainPID", status.MainPID)
	return status, nil
}

func (c *DBusClient) unitName(id string) string {
	return fmt.Sprintf("%s@%s.service", c.unitPrefix, id)
}

func (c *DBusClient) unitProperties(ctx context.Context, id string) (map[string]any, error) {
	if !c.available {
		return nil, ErrUnavailable
	}

	runCtx, cancel := withTimeout(ctx, c.timeout)
	defer cancel()

	props, err := c.conn.GetUnitPropertiesContext(runCtx, c.unitName(id))
	if err != nil {
		return nil, c.mapError(err, c.unitName(id))
	}
	return props, nil
}

func (c *DBusClient) mapError(err error, unit string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, gdbus.ErrClosed) {
		c.logger.Warn("systemd dbus unavailable", "unit", unit, "error", err)
		return ErrUnavailable
	}

	var dbusErr *gdbus.Error
	if errors.As(err, &dbusErr) {
		name := string(dbusErr.Name)
		switch name {
		case "org.freedesktop.systemd1.NoSuchUnit":
			c.logger.Warn("systemd dbus unit not found", "unit", unit, "error", dbusErr)
			return fmt.Errorf("%w: %s", ErrUnitNotFound, unit)
		case "org.freedesktop.DBus.Error.NoServer",
			"org.freedesktop.DBus.Error.NoReply",
			"org.freedesktop.DBus.Error.Disconnected",
			"org.freedesktop.DBus.Error.ServiceUnknown":
			c.logger.Warn("systemd dbus unavailable", "unit", unit, "error", dbusErr)
			return ErrUnavailable
		}
	}

	message := err.Error()
	switch {
	case strings.Contains(message, "NoSuchUnit"):
		c.logger.Warn("systemd dbus unit not found", "unit", unit, "error", message)
		return fmt.Errorf("%w: %s", ErrUnitNotFound, unit)
	case strings.Contains(message, "connection closed by user"):
		c.logger.Warn("systemd dbus unavailable", "unit", unit, "error", message)
		return ErrUnavailable
	case strings.Contains(message, "Failed to connect to bus"),
		strings.Contains(message, "/run/systemd/private"):
		c.logger.Warn("systemd dbus unavailable", "unit", unit, "error", message)
		return ErrUnavailable
	}

	return err
}

func withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func asInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case uint32:
		return int(typed), true
	case uint64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
